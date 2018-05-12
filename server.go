package main

import (
	"errors"
	pc "github.com/maklesoft/padlock-cloud/padlockcloud"
	"github.com/stripe/stripe-go"
	"github.com/stripe/stripe-go/plan"
	"path/filepath"
)

type StripeConfig struct {
	SecretKey string `yaml:"stripe_secret_key"`
	PublicKey string `yaml:"stripe_public_key"`
}

type MixpanelConfig struct {
	Token string `yaml:"token"`
}

type Server struct {
	*pc.Server
	Tracker
	Templates      *Templates
	StripeConfig   *StripeConfig
	MixpanelConfig *MixpanelConfig
}

func (server *Server) CreateAccount(email string) (*Account, error) {
	acc, err := NewAccount(email)
	if err != nil {
		return nil, err
	}

	if err := server.Storage.Put(acc); err != nil {
		return nil, err
	}

	return acc, nil
}

func (server *Server) GetAccount(email string) (*Account, error) {
	acc := &Account{Email: email}
	if err := server.Storage.Get(acc); err != nil {
		if err != pc.ErrNotFound {
			return nil, err
		} else {
			return nil, nil
		}
	}
	return acc, nil
}

func (server *Server) GetOrCreateAccount(email string) (*Account, error) {
	acc, err := server.GetAccount(email)
	if err != nil {
		return nil, err
	}

	if acc == nil {
		if acc, err = server.CreateAccount(email); err != nil {
			return nil, err
		}
	} else {
		if err := acc.UpdateCustomer(); err != nil {
			return nil, err
		}
		if err := server.Storage.Put(acc); err != nil {
			return nil, err
		}
	}

	return acc, nil
}

func (server *Server) InitEndpoints() {
	store := server.Endpoints["/store/"]
	store.Handlers["GET"] = (&CheckSubscription{server, false}).Wrap(store.Handlers["GET"])
	store.Handlers["HEAD"] = (&CheckSubscription{server, false}).Wrap(store.Handlers["HEAD"])
	store.Handlers["PUT"] = (&CheckSubscription{server, true}).Wrap(store.Handlers["PUT"])
	store.Handlers["POST"] = (&CheckSubscription{server, true}).Wrap(store.Handlers["POST"])

	server.Server.Endpoints["/dashboard/"].Handlers["GET"] = &Dashboard{server}

	server.Server.Endpoints["/subscribe/"] = &pc.Endpoint{
		Handlers: map[string]pc.Handler{
			"POST": &Subscribe{server},
		},
		AuthType: "universal",
	}

	server.Server.Endpoints["/unsubscribe/"] = &pc.Endpoint{
		Handlers: map[string]pc.Handler{
			"POST": &Unsubscribe{server},
		},
		AuthType: "universal",
	}

	server.Server.Endpoints["/billing/"] = &pc.Endpoint{
		Handlers: map[string]pc.Handler{
			"POST": &UpdateBilling{server},
		},
		AuthType: "web",
	}

	server.Server.Endpoints["/stripehook/"] = &pc.Endpoint{
		Handlers: map[string]pc.Handler{
			"POST": &StripeHook{server},
		},
	}

	server.Server.Endpoints["/track/"] = &pc.Endpoint{
		Handlers: map[string]pc.Handler{
			"POST": &Track{server},
		},
	}

	server.Server.Endpoints["/invoices/"] = &pc.Endpoint{
		Handlers: map[string]pc.Handler{
			"GET": &Invoices{server},
		},
		AuthType: "web",
	}

	server.Server.Endpoints["/plans/"] = &pc.Endpoint{
		Handlers: map[string]pc.Handler{
			"GET": &Plans{server},
		},
	}

	server.Server.Endpoints["/account/"] = &pc.Endpoint{
		Handlers: map[string]pc.Handler{
			"GET": (&CheckSubscription{server, false}).Wrap(&AccountInfo{server}),
		},
		AuthType: "universal",
	}

	server.Server.Endpoints["/apply-promo/"] = &pc.Endpoint{
		Handlers: map[string]pc.Handler{
			"POST": &ApplyPromo{server},
		},
	}

	server.Server.Endpoints["/deleteaccount/"] = &pc.Endpoint{
		Handlers: map[string]pc.Handler{
			"POST": &DeleteAccount{server},
		},
		AuthType: "web",
	}
}

func (server *Server) Init() error {
	stripe.Logger = server.Info

	server.InitEndpoints()

	if server.Templates == nil {
		server.Templates = &Templates{
			Templates: server.Server.Templates,
		}
		// Load templates from assets directory
		if err := LoadTemplates(
			server.Templates,
			filepath.Join("assets", "templates"),
		); err != nil {
			return err
		}
	}

	stripe.Key = server.StripeConfig.SecretKey

	i := plan.List(nil)
	for i.Next() {
		plan := i.Plan()
		if plan.Meta["available"] == "true" {
			AvailablePlans = append(AvailablePlans, i.Plan())
		}
	}

	if len(AvailablePlans) == 0 {
		return errors.New("No available plans found!")
	}

	// Set up tracking
	server.Tracker = NewMixpanelTracker(server.MixpanelConfig.Token, server.Storage)

	return nil
}

func NewServer(pcServer *pc.Server, stripeConfig *StripeConfig, mixpanelConfig *MixpanelConfig) *Server {
	// Initialize server instance
	server := &Server{
		Server:         pcServer,
		StripeConfig:   stripeConfig,
		MixpanelConfig: mixpanelConfig,
	}
	return server
}

func init() {
	pc.RegisterStorable(&Account{}, "sub-accounts")
}

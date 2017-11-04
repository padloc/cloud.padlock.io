package main

import (
	"github.com/dukex/mixpanel"
	pc "github.com/maklesoft/padlock-cloud/padlockcloud"
	"github.com/stripe/stripe-go"
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
	Templates      *Templates
	StripeConfig   *StripeConfig
	MixpanelConfig *MixpanelConfig
	mixpanel       mixpanel.Mixpanel
}

func (server *Server) AccountFromEmail(email string, create bool) (*Account, error) {
	acc := &Account{Email: email}
	if err := server.Storage.Get(acc); err != nil {
		if err != pc.ErrNotFound {
			return nil, err
		}
		if create {
			if acc, err = NewAccount(email); err != nil {
				return nil, err
			}
			if err = server.Storage.Put(acc); err != nil {
				return nil, err
			}
		}
	}
	return acc, nil
}

func (server *Server) InitEndpoints() {
	auth := server.Endpoints["/auth/"]
	auth.Handlers["PUT"] = (&CheckSubscription{server, false}).Wrap(auth.Handlers["PUT"])
	auth.Handlers["POST"] = (&CheckSubscription{server, false}).Wrap(auth.Handlers["POST"])

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
		// AuthType: "web",
	}

	server.Server.Endpoints["/unsubscribe/"] = &pc.Endpoint{
		Handlers: map[string]pc.Handler{
			"POST": &Unsubscribe{server},
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

	server.Log.Info.Printf("Setting up mixpanel with token %s", server.MixpanelConfig.Token)
	// Set up tracking
	server.mixpanel = mixpanel.New(server.MixpanelConfig.Token, "")

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

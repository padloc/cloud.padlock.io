package main

import "path/filepath"
import "github.com/stripe/stripe-go"

import pc "github.com/maklesoft/padlock-cloud/padlockcloud"

type Server struct {
	*pc.Server
	Templates *Templates
}

func (server *Server) AccountFromEmail(email string) (*Account, error) {
	acc := &Account{Email: email}
	if err := server.Storage.Get(acc); err != nil {
		if err != pc.ErrNotFound {
			return nil, err
		}
		if acc, err = NewAccount(email); err != nil {
			return nil, err
		}
	}
	return acc, nil
}

func (server *Server) InitEndpoints() {
	store := server.Endpoints["/store/"]
	store.Handlers["PUT"] = (&CheckSubscription{server}).Wrap(store.Handlers["PUT"])

	server.Endpoints["/dashboard/"].Handlers["GET"] = &Dashboard{server}

	// Endpoint for validating purchase receipts, only POST method is supported
	server.Server.Endpoints["/subscribe/"] = &pc.Endpoint{
		Handlers: map[string]pc.Handler{
			"POST": &Subscribe{server},
		},
		// AuthType: "web",
	}

	server.Server.Endpoints["/stripehook/"] = &pc.Endpoint{
		Handlers: map[string]pc.Handler{
			"POST": &StripeHook{server},
		},
	}
}

func (server *Server) Init() error {
	stripe.Logger = server.Info

	if err := server.Server.Init(); err != nil {
		return err
	}
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

	return nil
}

func NewServer(pcServer *pc.Server) *Server {
	// Initialize server instance
	server := &Server{
		Server: pcServer,
	}
	return server
}

func init() {
	pc.RegisterStorable(&Account{}, "sub-accounts")
}

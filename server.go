package main

import "errors"
import "path/filepath"

import pc "github.com/maklesoft/padlock-cloud/padlockcloud"

var ErrInvalidReceipt = errors.New("padlock: invalid receipt")

type Server struct {
	*pc.Server
	Templates *Templates
	Itunes    ItunesInterface
}

func (server *Server) UpdatePlansForAccount(acc *Account) error {
	if acc.Plans.Itunes != nil {
		// Revalidate itunes receipt to see if the plan has been renewed
		plan, err := server.Itunes.ValidateReceipt(acc.Plans.Itunes.Receipt)
		if err != nil {
			return err
		}

		acc.Plans.Itunes = plan
		if err := server.Storage.Put(acc); err != nil {
			return err
		}

		// If the itunes plan has been renewed then we can stop right here
		if acc.Plans.Itunes.Active() {
			return nil
		}
	}

	return nil
}

func (server *Server) CheckPlansForAccount(acc *Account) (bool, error) {
	if acc.HasActivePlan() {
		return true, nil
	}

	if err := server.UpdatePlansForAccount(acc); err != nil {
		return false, err
	}

	return acc.HasActivePlan(), nil
}

func (server *Server) InitEndpoints() {
	// auth := server.Server.Endpoints["/auth/"]
	// auth.Handlers["POST"] = (&CheckPlan{server}).Wrap(auth.Handlers["POST"])

	act := server.Endpoints["/activate/"]
	act.Handlers["GET"] = &ActivateAuthToken{
		server,
		act.Handlers["GET"].(*pc.ActivateAuthToken),
	}

	store := server.Endpoints["/store/"]
	store.Handlers["PUT"] = (&CheckPlan{server}).Wrap(store.Handlers["PUT"])

	server.Endpoints["/dashboard/"].Handlers["GET"] = &Dashboard{server}

	// Endpoint for validating purchase receipts, only POST method is supported
	server.Server.Endpoints["/validatereceipt/"] = &pc.Endpoint{
		Handlers: map[string]pc.Handler{
			"POST": &ValidateReceipt{server},
		},
	}
}

func (server *Server) Init() error {
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

func NewServer(pcServer *pc.Server, itunes ItunesInterface) *Server {
	// Initialize server instance
	server := &Server{
		Server: pcServer,
		Itunes: itunes,
	}
	return server
}

func init() {
	pc.RegisterStorable(&Account{}, "plan-accounts")
}

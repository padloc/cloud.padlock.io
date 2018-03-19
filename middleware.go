package main

import (
	pc "github.com/maklesoft/padlock-cloud/padlockcloud"
	"net/http"
	"strconv"
)

func NoSubRequired(a *pc.AuthToken) bool {
	// return a != nil && a.Device != nil && a.Device.Platform == "iOS" && a.Device.AppVersion == "2.2.0"
	return false
}

type CheckSubscription struct {
	*Server
	RequireSub bool
}

func (m *CheckSubscription) Wrap(h pc.Handler) pc.Handler {
	return pc.HandlerFunc(func(w http.ResponseWriter, r *http.Request, a *pc.AuthToken) error {
		if m.RequireSub {
			w.Header().Set("X-Sub-Required", "true")
		}

		var email string
		var createAccount bool
		if a != nil {
			email = a.Email
			// Email is verified, so we can safely create an account
			createAccount = true
		}

		if email == "" {
			email = r.PostFormValue("email")
			// Email is not necessarily verified, so we can not safely create an account
			createAccount = false
		}

		if email == "" {
			return &SubscriptionRequired{}
		}

		// Get plan account for this email
		acc, err := m.AccountFromEmail(email, createAccount)
		if err != nil {
			return err
		}

		if err := acc.UpdateCustomer(m.Storage); err != nil {
			return err
		}
		status, trialEnd := acc.SubscriptionStatus()

		if NoSubRequired(a) {
			status = "active"
		}

		w.Header().Set("X-Sub-Status", status)
		w.Header().Set("X-Sub-Trial-End", strconv.FormatInt(trialEnd, 10))

		if m.RequireSub && status != "trialing" && status != "active" {
			return &SubscriptionRequired{}
		}

		return h.Handle(w, r, a)
	})
}

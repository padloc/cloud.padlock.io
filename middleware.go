package main

import (
	pc "github.com/maklesoft/padlock-cloud/padlockcloud"
	"github.com/stripe/stripe-go/customer"
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
			return &pc.BadRequest{"Neither valid auth token nor email parameter provided"}
		}

		// Get plan account for this email
		acc, err := m.AccountFromEmail(email, createAccount)
		if err != nil {
			return err
		}

		status := "inactive"
		var trialEnd int64 = 0

		if s := acc.Subscription(); s != nil {
			status = string(s.Status)
			trialEnd = s.TrialEnd
		}

		if NoSubRequired(a) {
			status = "active"
		}

		// If subscription is not active, check back with stripe to make sure the subscription
		// status is up to date.
		// TODO: Think of a more robust way to ensure proper synchronization between Stripe and
		// Padlock Ploud to avoid having to this this in a request handler.
		if m.RequireSub && status != "trialing" && status != "active" {
			var err error
			if acc.Customer, err = customer.Get(acc.Customer.ID, nil); err != nil {
				return err
			}
			if err := m.Storage.Put(acc); err != nil {
				return err
			}
			if s := acc.Subscription(); s != nil {
				status = string(s.Status)
				trialEnd = s.TrialEnd
			}
		}

		w.Header().Set("X-Sub-Status", status)

		if status == "trialing" {
			w.Header().Set("X-Sub-Trial-End", strconv.FormatInt(trialEnd, 10))
		}

		if m.RequireSub && status != "trialing" && status != "active" {
			return &SubscriptionRequired{}
		}

		return h.Handle(w, r, a)
	})
}

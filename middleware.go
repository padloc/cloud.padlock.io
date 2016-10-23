package main

import "net/http"
import pc "github.com/maklesoft/padlock-cloud/padlockcloud"

type CheckSubscription struct {
	*Server
}

func (m *CheckSubscription) Wrap(h pc.Handler) pc.Handler {
	return pc.HandlerFunc(func(w http.ResponseWriter, r *http.Request, a *pc.AuthToken) error {
		var email string
		if a != nil {
			email = a.Email
		}

		if email == "" {
			email = r.PostFormValue("email")
		}

		if email == "" {
			return &pc.BadRequest{"Missing field 'email'"}
		}

		// Get plan account for this email
		acc := &Account{Email: email}

		// Load existing data for this account
		if err := m.Storage.Get(acc); err == pc.ErrNotFound {
			// No plan account found. Rejecting request
			return &SubscriptionRequired{}
		} else if err != nil {
			return err
		}

		if acc.RemainingTrialPeriod() == 0 && !acc.HasActiveSubscription() {
			return &SubscriptionRequired{}
		}

		return h.Handle(w, r, a)
	})
}

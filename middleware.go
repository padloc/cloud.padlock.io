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

		if a == nil {
			return &pc.InvalidAuthToken{}
		}

		acc, err := m.GetOrCreateAccount(a.Email)
		if err != nil {
			return err
		}

		status, trialEnd := acc.SubscriptionStatus()

		if NoSubRequired(a) {
			status = "active"
		}

		w.Header().Set("X-Sub-Status", status)
		w.Header().Set("X-Sub-Trial-End", strconv.FormatInt(trialEnd, 10))
		w.Header().Set("X-Stripe-Pub-Key", m.StripeConfig.PublicKey)

		if m.RequireSub && status != "trialing" && status != "active" {
			return &SubscriptionRequired{}
		}

		return h.Handle(w, r, a)
	})
}

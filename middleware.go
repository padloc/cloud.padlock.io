package main

import (
	"fmt"
	pc "github.com/maklesoft/padlock-cloud/padlockcloud"
	"net/http"
	"strconv"
)

type CheckSubscription struct {
	*Server
	RequireSub bool
}

func (m *CheckSubscription) Wrap(h pc.Handler) pc.Handler {
	return pc.HandlerFunc(func(w http.ResponseWriter, r *http.Request, a *pc.AuthToken) error {
		fmt.Println("checking subscription")

		status := "inactive"
		var trialEnd int64 = 0

		if a != nil {
			// Get plan account for this email
			acc, err := m.AccountFromEmail(a.Email, true)
			if err != nil {
				return err
			}

			fmt.Println("account", acc)

			if acc.Customer == nil {
				if err := acc.CreateCustomer(); err != nil {
					return err
				}
			} else {
				if err := acc.UpdateCustomer(); err != nil {
					return err
				}
			}

			if err := m.Storage.Put(acc); err != nil {
				return err
			}

			if s := acc.Subscription(); s != nil {
				status = string(s.Status)
				trialEnd = s.TrialEnd
			}
		}

		if m.RequireSub {
			w.Header().Set("X-Sub-Required", "true")
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

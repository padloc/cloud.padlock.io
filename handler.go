package main

import (
	"bytes"
	"encoding/json"
	pc "github.com/maklesoft/padlock-cloud/padlockcloud"
	"github.com/stripe/stripe-go"
	"github.com/stripe/stripe-go/customer"
	"github.com/stripe/stripe-go/sub"
	"io/ioutil"
	"net/http"
)

type Dashboard struct {
	*Server
}

func (h *Dashboard) Handle(w http.ResponseWriter, r *http.Request, auth *pc.AuthToken) error {
	acc := auth.Account()
	subAcc, err := h.AccountFromEmail(acc.Email)
	if err != nil {
		return err
	}

	var b bytes.Buffer
	if err := h.Templates.Dashboard.Execute(&b, map[string]interface{}{
		"account":          acc,
		"subAccount":       subAcc,
		"stripePublicKey":  h.StripeConfig.PublicKey,
		"paired":           r.URL.Query().Get("paired"),
		"revoked":          r.URL.Query().Get("revoked"),
		"subscribed":       r.URL.Query().Get("subscribed"),
		"unsubscribed":     r.URL.Query().Get("unsubscribed"),
		"datareset":        r.URL.Query().Get("datareset"),
		"action":           r.URL.Query().Get("action"),
		pc.CSRFTemplateTag: pc.CSRFTemplateField(r),
	}); err != nil {
		return err
	}

	b.WriteTo(w)
	return nil
}

type Subscribe struct {
	*Server
}

func (h *Subscribe) Handle(w http.ResponseWriter, r *http.Request, a *pc.AuthToken) error {
	if a == nil {
		return &pc.InvalidAuthToken{}
	}

	token := r.PostFormValue("stripeToken")

	if token == "" {
		return &pc.BadRequest{"No stripe token provided"}
	}

	acc, err := h.AccountFromEmail(a.Account().Email)
	if err != nil {
		return err
	}

	if err := acc.SetPaymentSource(token); err != nil {
		return err
	}

	s := acc.Subscription()
	if s == nil {
		var err error
		if s, err = sub.New(&stripe.SubParams{
			Customer:    acc.Customer.ID,
			Plan:        PlanMonthly,
			TrialEndNow: true,
		}); err != nil {
			return err
		}
		acc.Customer.Subs.Values = []*stripe.Sub{s}
	} else {
		if s_, err := sub.Update(s.ID, &stripe.SubParams{
			TrialEndNow: true,
		}); err != nil {
			return err
		} else {
			*s = *s_
		}
	}

	if err := h.Storage.Put(acc); err != nil {
		return err
	}

	http.Redirect(w, r, "/dashboard/?subscribed=1", http.StatusFound)

	h.Info.Printf("%s - subcribe - %s\n", pc.FormatRequest(r), acc.Email)

	return nil
}

type Unsubscribe struct {
	*Server
}

func (h *Unsubscribe) Handle(w http.ResponseWriter, r *http.Request, a *pc.AuthToken) error {
	acc, err := h.AccountFromEmail(a.Account().Email)
	if err != nil {
		return err
	}

	s := acc.Subscription()

	if s == nil {
		return &pc.BadRequest{"This account does not have an active subscription"}
	}

	if s_, err := sub.Cancel(s.ID, nil); err != nil {
		return err
	} else {
		*s = *s_
	}

	if err := h.Storage.Put(acc); err != nil {
		return err
	}

	http.Redirect(w, r, "/dashboard/?unsubscribed=1", http.StatusFound)

	h.Info.Printf("%s - unsubscribe - %s\n", pc.FormatRequest(r), acc.Email)

	return nil
}

type StripeHook struct {
	*Server
}

func (h *StripeHook) Handle(w http.ResponseWriter, r *http.Request, a *pc.AuthToken) error {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return err
	}
	event := &stripe.Event{}
	if err := json.Unmarshal(body, event); err != nil {
		return err
	}

	var c *stripe.Customer

	switch event.Type {
	case "customer.created", "customer.updated":
		c = &stripe.Customer{}
		if err := json.Unmarshal(event.Data.Raw, c); err != nil {
			return err
		}

	case "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted":
		var err error
		if c, err = customer.Get(event.GetObjValue("customer"), nil); err != nil {
			return err
		}
	}

	if c != nil {
		acc, err := h.AccountFromEmail(c.Email)
		if err != nil {
			return err
		}

		acc.Customer = c

		if err := h.Storage.Put(acc); err != nil {
			return err
		}

		h.Info.Printf("%s - stripe_hook - %s:%s", pc.FormatRequest(r), acc.Email, event.Type)
	}

	return nil
}

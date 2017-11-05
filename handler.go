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
	subAcc, err := h.AccountFromEmail(acc.Email, true)
	if err != nil {
		return err
	}

	params := pc.DashboardParams(r, auth)
	params["subAccount"] = subAcc
	params["stripePublicKey"] = h.StripeConfig.PublicKey
	params["subscribed"] = r.URL.Query().Get("subscribed")
	params["unsubscribed"] = r.URL.Query().Get("unsubscribed")
	params["hideSub"] = NoSubRequired(auth)
	params["ref"] = r.URL.Query().Get("ref")

	var b bytes.Buffer
	if err := h.Templates.Dashboard.Execute(&b, params); err != nil {
		return err
	}

	b.WriteTo(w)

	h.Track(&TrackingEvent{
		Name: "Open Dashboard",
		Properties: map[string]interface{}{
			"Action": params["action"],
			"Source": sourceFromRef(params["ref"].(string)),
		},
	}, r, auth)

	return nil
}

type Subscribe struct {
	*Server
}

func wrapCardError(err error) error {
	// For now, card errors are the only errors we are expecting from stripe. Any other
	// errors we treat as unexpected errors (i.e. ServerError)
	if stripeErr, ok := err.(*stripe.Error); ok && stripeErr.Type == stripe.ErrorTypeCard {
		return &StripeError{stripeErr}
	} else {
		return err
	}
}

func (h *Subscribe) Handle(w http.ResponseWriter, r *http.Request, a *pc.AuthToken) error {
	if a == nil {
		return &pc.InvalidAuthToken{}
	}

	token := r.PostFormValue("stripeToken")

	if token == "" {
		return &pc.BadRequest{"No stripe token provided"}
	}

	acc, err := h.AccountFromEmail(a.Account().Email, true)
	if err != nil {
		return err
	}

	if err := acc.SetPaymentSource(token); err != nil {
		return wrapCardError(err)
	}

	s := acc.Subscription()
	if s == nil {
		var err error
		if s, err = sub.New(&stripe.SubParams{
			Customer:    acc.Customer.ID,
			Plan:        PlanYearly,
			TrialEndNow: true,
		}); err != nil {
			return wrapCardError(err)
		}
		acc.Customer.Subs.Values = []*stripe.Sub{s}
	} else {
		if s_, err := sub.Update(s.ID, &stripe.SubParams{
			Plan:        PlanYearly,
			TrialEndNow: true,
		}); err != nil {
			return wrapCardError(err)
		} else {
			*s = *s_
		}
	}

	if err := h.Storage.Put(acc); err != nil {
		return err
	}

	http.Redirect(w, r, "/dashboard/?subscribed=1", http.StatusFound)

	h.Info.Printf("%s - subcribe - %s\n", pc.FormatRequest(r), acc.Email)

	h.Track(&TrackingEvent{
		Name: "Buy Subscription",
		Properties: map[string]interface{}{
			"Plan":   PlanYearly,
			"Source": sourceFromRef(r.URL.Query().Get("ref")),
		},
	}, r, a)

	return nil
}

type Unsubscribe struct {
	*Server
}

func (h *Unsubscribe) Handle(w http.ResponseWriter, r *http.Request, a *pc.AuthToken) error {
	acc, err := h.AccountFromEmail(a.Account().Email, true)
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
		acc, err := h.AccountFromEmail(c.Email, true)
		if err != nil {
			return err
		}

		// Only update customer if the ids match (even though that theoretically shouldn't happen,
		// it's possible that there are two stripe customers with the same email. In that case, this guard
		// against unexpected behaviour by making sure only one of the customers is used)
		if acc.Customer.ID == c.ID {
			acc.Customer = c
		}

		if err := h.Storage.Put(acc); err != nil {
			return err
		}

		h.Info.Printf("%s - stripe_hook - %s:%s", pc.FormatRequest(r), acc.Email, event.Type)
	}

	return nil
}

type Track struct {
	*Server
}

func (h *Track) Handle(w http.ResponseWriter, r *http.Request, a *pc.AuthToken) error {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return err
	}
	event := &TrackingEvent{}
	if err := json.Unmarshal(body, event); err != nil {
		return err
	}

	if err := h.Track(event, r, a); err != nil {
		return err
	}

	var response []byte
	if response, err = json.Marshal(event); err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(response)

	return nil
}

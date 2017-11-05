package main

import (
	"bytes"
	"encoding/json"
	"github.com/dukex/mixpanel"
	pc "github.com/maklesoft/padlock-cloud/padlockcloud"
	"github.com/satori/go.uuid"
	"github.com/stripe/stripe-go"
	"github.com/stripe/stripe-go/customer"
	"github.com/stripe/stripe-go/sub"
	"io/ioutil"
	"net/http"
	"time"
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

	var b bytes.Buffer
	if err := h.Templates.Dashboard.Execute(&b, params); err != nil {
		return err
	}

	b.WriteTo(w)
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

type TrackingEvent struct {
	TrackingID string                 `json:"trackingID"`
	Name       string                 `json:"event"`
	Properties map[string]interface{} `json:"props"`
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

	if event.TrackingID == "" {
		event.TrackingID = uuid.NewV4().String()
	}

	var acc *Account
	if a != nil {
		acc, _ = h.AccountFromEmail(a.Email, false)
	}

	if acc != nil {
		if acc.TrackingID == "" {
			acc.TrackingID = event.TrackingID
			h.Storage.Put(acc)
		} else {
			event.TrackingID = acc.TrackingID
		}
	}

	props := event.Properties

	device := pc.DeviceFromRequest(r)

	props["Platform"] = device.Platform
	props["Device UUID"] = device.UUID
	props["Device Manufacturer"] = device.Manufacturer
	props["Device Model"] = device.Model
	props["OS Version"] = device.OSVersion
	props["Device Name"] = device.HostName
	props["App Version"] = device.AppVersion
	props["Authenticated"] = a != nil

	if acc != nil {
		subStatus := "inactive"
		if s := acc.Subscription(); s != nil {
			subStatus = string(s.Status)
			props["Plan"] = s.Plan.Name
		}
		props["Subscription Status"] = subStatus
	}

	if err := h.mixpanel.Track(event.TrackingID, event.Name, &mixpanel.Event{
		IP:         pc.IPFromRequest(r),
		Properties: props,
	}); err != nil {
		return err
	}

	updateProps := map[string]interface{}{
		"$created":         props["First Launch"],
		"First App Launch": props["First Launch"],
		"First Platform":   props["Platform"],
	}

	if acc != nil {
		updateProps["$email"] = acc.Email
		updateProps["Created Padlock Cloud Account"] = acc.Created.UTC().Format(time.RFC3339)
	}

	if err := h.mixpanel.Update(event.TrackingID, &mixpanel.Update{
		IP:         pc.IPFromRequest(r),
		Operation:  "$set_once",
		Properties: updateProps,
	}); err != nil {
		return err
	}

	if a != nil {
		nDevices := 0
		platforms := make([]string, 0)
		pMap := make(map[string]bool)
		for _, token := range a.Account().AuthTokens {
			if token.Type == "api" && !token.Expired() {
				nDevices = nDevices + 1
			}
			if token.Device != nil && token.Device.Platform != "" && !pMap[token.Device.Platform] {
				platforms = append(platforms, token.Device.Platform)
				pMap[token.Device.Platform] = true
			}
		}

		updateProps = map[string]interface{}{
			"Paired Devices":      nDevices,
			"Platforms":           platforms,
			"Last Sync":           props["Last Sync"],
			"Subscription Status": props["Subscription Status"],
			"Plan":                props["Plan"],
		}
	} else {
		updateProps = make(map[string]interface{})
	}

	updateProps["Last Rated"] = props["Last Rated"]
	updateProps["Rated Version"] = props["Rated Version"]
	updateProps["Rating"] = props["Rating"]
	updateProps["Last Reviewed"] = props["Last Reviewed"]

	if err := h.mixpanel.Update(event.TrackingID, &mixpanel.Update{
		IP:         pc.IPFromRequest(r),
		Operation:  "$set",
		Properties: updateProps,
	}); err != nil {
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

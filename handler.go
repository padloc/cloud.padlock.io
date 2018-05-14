package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	pc "github.com/maklesoft/padlock-cloud/padlockcloud"
	"github.com/stripe/stripe-go"
	"github.com/stripe/stripe-go/customer"
	"github.com/stripe/stripe-go/invoice"
	"github.com/stripe/stripe-go/sub"
	"io/ioutil"
	"net/http"
	"strings"
)

type Dashboard struct {
	*Server
}

func (h *Dashboard) Handle(w http.ResponseWriter, r *http.Request, auth *pc.AuthToken) error {
	acc, err := h.GetOrCreateAccount(auth.Email)
	if err != nil {
		return err
	}

	if coupon := r.URL.Query().Get("coupon"); coupon != "" {
		if promo, _ := PromoFromCoupon(coupon); promo != nil {
			acc.Promo = promo

			if err := h.Storage.Put(acc); err != nil {
				return err
			}
		}
	}

	accMap := acc.ToMap(auth.Account())

	params := pc.DashboardParams(r, auth)
	params["account"] = accMap

	params["stripePublicKey"] = h.StripeConfig.PublicKey
	params["mixpanelToken"] = h.MixpanelConfig.Token

	ref := r.URL.Query().Get("ref")
	if ref == "" && params["action"] != "" {
		ref = fmt.Sprintf("action: %s", params["action"])
	}
	params["ref"] = ref

	var b bytes.Buffer
	if err := h.Templates.Dashboard.Execute(&b, params); err != nil {
		return err
	}

	b.WriteTo(w)

	go h.Track(&TrackingEvent{
		TrackingID: r.URL.Query().Get("tid"),
		Name:       "Open Dashboard",
		Properties: map[string]interface{}{
			"Action": params["action"],
			"Source": sourceFromRef(ref),
		},
		AuthToken: auth,
		Request:   r,
	})

	return nil
}

type Subscribe struct {
	*Server
}

func wrapCardError(err error) error {
	// For now, card errors are the only errors we are expecting from stripe. Any other
	// errors we treat as unexpected errors (i.e. ServerError)
	if stripeErr, ok := err.(*stripe.Error); ok {
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
	coupon := r.PostFormValue("coupon")
	source := r.PostFormValue("source")
	plan := AvailablePlans[0].ID

	if source == "" {
		source = sourceFromRef(r.URL.Query().Get("ref"))
	}

	acc, err := h.GetOrCreateAccount(a.Email)
	if err != nil {
		return err
	}

	if acc.GetPaymentSource() == nil && token == "" {
		return &pc.BadRequest{"No existing payment source and no stripe token provided"}
	}

	hadSub := acc.HasActiveSubscription()
	hadSource := acc.GetPaymentSource() != nil
	prevStatus, _ := acc.SubscriptionStatus()
	prevPlan := ""

	if sub := acc.Subscription(); sub != nil {
		prevPlan = sub.Plan.ID
	}

	if token != "" {
		if err := acc.SetPaymentSource(token); err != nil {
			return wrapCardError(err)
		}
	}

	s := acc.Subscription()
	if s == nil {
		var err error
		if s, err = sub.New(&stripe.SubParams{
			Customer:    acc.Customer.ID,
			Plan:        plan,
			TrialEndNow: true,
			Coupon:      coupon,
		}); err != nil {
			return wrapCardError(err)
		}
		acc.Customer.Subs.Values = []*stripe.Sub{s}
	} else {
		if s_, err := sub.Update(s.ID, &stripe.SubParams{
			Plan:        plan,
			TrialEndNow: true,
			Coupon:      coupon,
		}); err != nil {
			return wrapCardError(err)
		} else {
			*s = *s_
		}
	}

	if err := h.Storage.Put(acc); err != nil {
		return err
	}

	if s.Status == "unpaid" || s.Status == "past_due" {
		// Attempt to pay any unpaid invoices
		i := invoice.List(&stripe.InvoiceListParams{
			Sub: s.ID,
		})
		for i.Next() {
			inv := i.Invoice()
			if inv.Attempted && !inv.Paid {
				if _, err := invoice.Pay(inv.ID, nil); err != nil {
					return wrapCardError(err)
				}
			}
		}
	}

	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		action := "subscribed"
		if hadSub {
			action = "payment-updated"
		}
		http.Redirect(w, r, "/dashboard/?action="+action, http.StatusFound)
	}

	h.Info.Printf("%s - subscribe - %s\n", pc.FormatRequest(r), acc.Email)

	go h.Track(&TrackingEvent{
		Name: "Update Subscription",
		Properties: map[string]interface{}{
			"Coupon":                  coupon,
			"Plan":                    plan,
			"Source":                  source,
			"Previous Status":         prevStatus,
			"Previous Plan":           prevPlan,
			"Had Payment Source":      hadSource,
			"Updating Payment Source": token != "",
		},
		AuthToken: a,
		Request:   r,
	})

	return nil
}

type Unsubscribe struct {
	*Server
}

func (h *Unsubscribe) Handle(w http.ResponseWriter, r *http.Request, a *pc.AuthToken) error {
	acc, err := h.GetOrCreateAccount(a.Email)
	if err != nil {
		return err
	}

	s := acc.Subscription()

	if s == nil {
		return &pc.BadRequest{"This account does not have an active subscription"}
	}

	if _, err := sub.Cancel(s.ID, nil); err != nil {
		return err
	}

	if err := acc.UpdateCustomer(); err != nil {
		return err
	}

	if err := h.Storage.Put(acc); err != nil {
		return err
	}

	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		http.Redirect(w, r, "/dashboard/?action=unsubscribed", http.StatusFound)
	}

	h.Info.Printf("%s - unsubscribe - %s\n", pc.FormatRequest(r), acc.Email)

	go h.Track(&TrackingEvent{
		Name:      "Cancel Subscription",
		AuthToken: a,
		Request:   r,
	})

	return nil
}

type UpdateBilling struct {
	*Server
}

func (h *UpdateBilling) Handle(w http.ResponseWriter, r *http.Request, a *pc.AuthToken) error {
	if a == nil {
		return &pc.InvalidAuthToken{}
	}

	acc, err := h.GetOrCreateAccount(a.Email)
	if err != nil {
		return err
	}

	params := &stripe.CustomerParams{
		Shipping: &stripe.CustomerShippingDetails{
			Name: r.PostFormValue("name"),
			Address: stripe.Address{
				Line1:   r.PostFormValue("address1"),
				Line2:   r.PostFormValue("address2"),
				Zip:     r.PostFormValue("zip"),
				City:    r.PostFormValue("city"),
				Country: r.PostFormValue("country"),
			},
		},
		BusinessVatID: r.PostFormValue("vat"),
	}

	if customer, err := customer.Update(acc.Customer.ID, params); err != nil {
		return err
	} else {
		acc.SetCustomer(customer)
	}

	if err := h.Storage.Put(acc); err != nil {
		return err
	}

	http.Redirect(w, r, "/dashboard/?action=billing-updated", http.StatusFound)

	h.Info.Printf("%s - update_billing - %s\n", pc.FormatRequest(r), acc.Email)

	go h.Track(&TrackingEvent{
		Name:      "Update Billing Info",
		AuthToken: a,
		Request:   r,
	})

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
	case "customer.updated":
		c = &stripe.Customer{}
		if err := json.Unmarshal(event.Data.Raw, c); err != nil {
			return err
		}

	case "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted":
		var err error
		if c, err = customer.Get(event.GetObjValue("customer"), nil); err != nil {
			h.LogError(err, r)
		}
	}

	if c == nil {
		return nil
	}

	h.LockAccount(c.Email)
	defer h.UnlockAccount(c.Email)

	acc, err := h.GetAccount(c.Email)
	if err != nil {
		return err
	}

	// Only update customer if the ids match (even though that theoretically shouldn't happen,
	// it's possible that there are two stripe customers with the same email. In that case, this guard
	// against unexpected behaviour by making sure only one of the customers is used)
	if acc == nil || acc.Customer.ID != c.ID {
		return nil
	}

	acc.SetCustomer(c)

	if err := h.Storage.Put(acc); err != nil {
		return err
	}

	h.Info.Printf("%s - stripe_hook - %s:%s", pc.FormatRequest(r), acc.Email, event.Type)

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

	event.AuthToken = a
	event.Request = r

	if err := h.Track(event); err != nil {
		return err
	}

	var response []byte
	if response, err = json.Marshal(event); err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(response)

	h.Info.Printf("%s - track", pc.FormatRequest(r))

	return nil
}

type Invoices struct {
	*Server
}

func (h *Invoices) Handle(w http.ResponseWriter, r *http.Request, a *pc.AuthToken) error {
	if a == nil {
		return &pc.InvalidAuthToken{}
	}

	acc, err := h.GetOrCreateAccount(a.Email)
	if err != nil {
		return err
	}

	var id string
	if p := strings.Split(r.URL.Path, "/"); len(p) > 2 {
		id = p[2]
	}

	if id != "" {

		inv, err := invoice.Get(id, nil)
		if err != nil {
			return err
		}

		if inv.Customer.ID != acc.Customer.ID {
			return &pc.UnauthorizedError{}
		}

		var b bytes.Buffer
		if err := h.Templates.Invoice.Execute(&b, &map[string]interface{}{
			"invoice":  inv,
			"customer": acc.Customer,
		}); err != nil {
			return err
		}

		b.WriteTo(w)

	} else {

		var invoices []*stripe.Invoice
		i := invoice.List(&stripe.InvoiceListParams{
			Customer: acc.Customer.ID,
		})
		for i.Next() {
			inv := i.Invoice()
			if inv.Paid {
				invoices = append(invoices, inv)
			}
		}

		if r.Header.Get("Accept") == "application/json" {
			if b, err := json.Marshal(invoices); err != nil {
				return err
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.Write(b)
			}
		} else {
			var b bytes.Buffer
			if err := h.Templates.InvoiceList.Execute(&b, &map[string]interface{}{
				"invoices": invoices,
				"customer": acc.Customer,
			}); err != nil {
				return err
			}

			b.WriteTo(w)
		}

	}

	return nil
}

type AccountInfo struct {
	*Server
}

func (h *AccountInfo) Handle(w http.ResponseWriter, r *http.Request, auth *pc.AuthToken) error {
	acc := auth.Account()
	subAcc, err := h.GetOrCreateAccount(acc.Email)
	if err != nil {
		return err
	}

	res, err := json.Marshal(subAcc.ToMap(acc))
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(res)

	h.Info.Printf("%s - account_info - %s\n", pc.FormatRequest(r), acc.Email)

	return nil
}

type Plans struct {
	*Server
}

func (h *Plans) Handle(w http.ResponseWriter, r *http.Request, auth *pc.AuthToken) error {
	plans := make([]map[string]interface{}, len(AvailablePlans))
	for i, v := range AvailablePlans {
		plans[i] = planToMap(v)
	}
	res, err := json.Marshal(plans)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(res)

	return nil
}

type ApplyPromo struct {
	*Server
}

func (h *ApplyPromo) Handle(w http.ResponseWriter, r *http.Request, auth *pc.AuthToken) error {
	usersJSON := []byte(r.PostFormValue("users"))
	var users []struct {
		Properties struct {
			Email        string `json:"$email"`
			Unsubscribed string `json:"$unsubscribed"`
		} `json:"$properties"`
	}

	promo, err := PromoFromCoupon(r.URL.Query().Get("coupon"))
	if err != nil {
		return &pc.BadRequest{fmt.Sprintf("%v", err)}
	}

	if err := json.Unmarshal(usersJSON, &users); err != nil {
		return &pc.BadRequest{fmt.Sprintf("%v", err)}
	}

	for _, user := range users {
		email := user.Properties.Email
		if acc, _ := h.GetAccount(email); acc != nil {

			acc.Promo = promo
			if err := h.Storage.Put(acc); err != nil {
				return err
			}

			authRequest, err := pc.NewAuthRequest(email, "web", "", nil)
			if err != nil {
				return err
			}
			authRequest.Redirect = "/dashboard/?action=subscribe"

			// Save key-token pair to database for activating it later in a separate request
			if err := h.Storage.Put(authRequest); err != nil {
				return err
			}

			if user.Properties.Unsubscribed == "" {
				go func() {
					actLink := fmt.Sprintf("%s/a/?t=%s", h.BaseUrl(r), authRequest.Token)
					optoutLink := fmt.Sprintf("%s/optout/?tid=%s", h.BaseUrl(r), acc.TrackingID)
					message := fmt.Sprintf(
						"%s\n\nUnsubscribe -> %s",
						fmt.Sprintf(promo.Coupon.Meta["emailBody"], actLink),
						optoutLink,
					)

					if err := h.Sender.Send(email, promo.Coupon.Meta["emailSubject"], message); err != nil {
						h.LogError(err, r)
					}
				}()
			}
		}
	}

	return nil
}

type DeleteAccount struct {
	*Server
}

func (h *DeleteAccount) Handle(w http.ResponseWriter, r *http.Request, a *pc.AuthToken) error {
	acc, err := h.GetAccount(a.Email)

	if err != nil {
		return err
	}

	if acc.Customer != nil {
		if _, err := customer.Del(acc.Customer.ID, nil); err != nil {
			h.LogError(err, r)
		}
	}

	if err := h.DeleteProfile(acc); err != nil {
		h.LogError(err, r)
	}

	if err := h.Storage.Delete(acc); err != nil {
		return err
	}

	if err := h.DeleteAccount(acc.Email); err != nil {
		return err
	}

	if err := h.Sender.Send(acc.Email, "Padlock Account Deletion", fmt.Sprintf(`
Hi there,

we're writing you to inform you that your Padlock online account %s was deleted successfully.

Sorry to see you go! We'll continue to work hard on making Padlock better and we hope you'll
give us another chance in the future! If you have any suggestions on how we can improve our
product, please let us know! Just reply to this email to send us your feedback.

Thanks!
Your Padlock Team
`, acc.Email)); err != nil {
		h.LogError(err, r)
	}

	return nil
}

type OptOutEmail struct {
	*Server
}

func (h *OptOutEmail) Handle(w http.ResponseWriter, r *http.Request, auth *pc.AuthToken) error {
	var tid string

	if tid = r.URL.Query().Get("tid"); tid == "" {
		return &pc.BadRequest{}
	}

	if err := h.UnsubscribeProfile(tid); err != nil {
		return err
	}

	w.Write([]byte("You have been unsubscribed successfully!"))

	return nil
}

package main

import "net/http"
import "bytes"
import "io/ioutil"
import pc "github.com/maklesoft/padlock-cloud/padlockcloud"

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
		"paired":           r.URL.Query()["paired"],
		"subscribed":       r.URL.Query()["subscribed"],
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
	token := r.PostFormValue("stripeToken")

	if token == "" {
		return &pc.BadRequest{"No stripe token provided"}
	}

	sub, err := NewSubscription(token)
	if err != nil {
		return err
	}

	acc := a.Account()

	subAcc := &Account{Email: acc.Email}
	if err := h.Storage.Get(subAcc); err != nil && err != pc.ErrNotFound {
		return err
	}

	subAcc.Subscription = sub

	if err := h.Storage.Put(subAcc); err != nil {
		return err
	}

	http.Redirect(w, r, "/dashboard/?subscribed=1", http.StatusFound)

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
	h.Info.Println(string(body))
	return nil
}

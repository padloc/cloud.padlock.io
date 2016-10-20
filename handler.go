package main

import "net/http"
import "time"
import "bytes"
import pc "github.com/maklesoft/padlock-cloud/padlockcloud"

type ValidateReceipt struct {
	*Server
}

func (h *ValidateReceipt) Handle(w http.ResponseWriter, r *http.Request, a *pc.AuthToken) error {
	receiptType := r.PostFormValue("type")
	receiptData := r.PostFormValue("receipt")
	email := r.PostFormValue("email")

	// Make sure all required parameters are there
	if email == "" || receiptType == "" || receiptData == "" {
		return &pc.BadRequest{"Missing email, receiptType or receiptData field"}
	}

	acc := &Account{Email: email}

	// Load existing account data if there is any. If not, that's fine, one will be created later
	// if the receipt turns out fine
	if err := h.Storage.Get(acc); err != nil && err != pc.ErrNotFound {
		return err
	}

	switch receiptType {
	case ReceiptTypeItunes:
		// Validate receipt
		plan, err := h.Itunes.ValidateReceipt(receiptData)
		// If the receipt is invalid or the subcription expired, return the appropriate error
		if err == ErrInvalidReceipt || plan.Status == ItunesStatusExpired {
			return &pc.BadRequest{"Invalid itunes receipt"}
		}

		if err != nil {
			return err
		}

		// Save the plan with the corresponding account
		acc.Plans.Itunes = plan
		if err := h.Storage.Put(acc); err != nil {
			return err
		}
	default:
		return &pc.BadRequest{"Invalid receipt type"}
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

type ActivateAuthToken struct {
	*Server
	bh *pc.ActivateAuthToken
}

func (h *ActivateAuthToken) Handle(w http.ResponseWriter, r *http.Request, a *pc.AuthToken) error {
	h.Log.Info.Println("activate auth token")
	_ = "breakpoint"
	authRequest, err := h.bh.GetAuthRequest(r)
	if err != nil {
		return err
	}

	acc := &Account{Email: authRequest.AuthToken.Email}
	if err := h.Storage.Get(acc); err != nil && err != pc.ErrNotFound {
		return err
	}

	if err := h.bh.Activate(authRequest); err != nil {
		return err
	}

	if authRequest.AuthToken.Type == "web" || acc.HasActivePlan() {
		h.Log.Info.Println("active plan found")
		return h.bh.Success(w, r, authRequest)
	}

	freePlan := false
	if acc.Plans.Free == nil {
		h.Log.Info.Println("handing out free plan")
		acc.Plans.Free = NewFreePlan(30 * 24 * time.Hour)
		if err := h.Storage.Put(acc); err != nil {
			return err
		}
		freePlan = true
	}

	if authRequest.Redirect != "" {
		http.Redirect(w, r, authRequest.Redirect, http.StatusFound)
	} else {
		var b bytes.Buffer
		if err := h.Templates.ActivateAuthTokenSuccess.Execute(&b, map[string]interface{}{
			"token":    authRequest.AuthToken,
			"freePlan": freePlan,
		}); err != nil {
			return err
		}
		b.WriteTo(w)
	}

	return nil
}

type Dashboard struct {
	*Server
}

func (h *Dashboard) Handle(w http.ResponseWriter, r *http.Request, auth *pc.AuthToken) error {
	acc := auth.Account()

	var b bytes.Buffer
	if err := h.Templates.Dashboard.Execute(&b, map[string]interface{}{
		"account":          acc,
		pc.CSRFTemplateTag: pc.CSRFTemplateField(r),
	}); err != nil {
		return err
	}

	b.WriteTo(w)
	return nil
}

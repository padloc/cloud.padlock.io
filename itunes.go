// +build none

package main

import "time"
import "encoding/json"
import "strconv"
import "net/http"
import "bytes"
import "io/ioutil"
import "errors"
import "fmt"

const (
	ItunesStatusOK                   = 0
	ItunesStatusInvalidJSON          = 21000
	ItunesStatusInvalidReceipt       = 21002
	ItunesStatusNotAuthenticated     = 21003
	ItunesStatusWrongSecret          = 21004
	ItunesStatusServerUnavailable    = 21005
	ItunesStatusExpired              = 21006
	ItunesStatusWrongEnvironmentProd = 21007
	ItunesStatusWrongEnvironmentTest = 21008
)

const (
	ItunesUrlProduction = "https://buy.itunes.apple.com/verifyReceipt"
	ItunesUrlSandbox    = "https://sandbox.itunes.apple.com/verifyReceipt"
)

const ReceiptTypeItunes = "ios-appstore"

var ErrInvalidReceipt = errors.New("padlock: invalid receipt")

type ItunesInterface interface {
	ValidateReceipt(string) (*ItunesPlan, error)
}

type ItunesConfig struct {
	SharedSecret string `yaml:"shared_secret"`
	Environment  string `yaml:"environment"`
}

type ItunesServer struct {
	Config *ItunesConfig
}

func parseItunesResult(data []byte) (*ItunesPlan, error) {
	result := &struct {
		Status            int `json:"status"`
		LatestReceiptInfo struct {
			Expires string `json:"expires_date"`
		} `json:"latest_receipt_info"`
		LatestExpiredReceiptInfo struct {
			Expires string `json:"expires_date"`
		} `json:"latest_expired_receipt_info"`
		LatestReceipt string `json:"latest_receipt"`
	}{}

	if err := json.Unmarshal(data, result); err != nil {
		return nil, err
	}

	expiresStr := result.LatestReceiptInfo.Expires
	if expiresStr == "" {
		expiresStr = result.LatestExpiredReceiptInfo.Expires
	}

	var expiresInt int
	var err error
	if expiresStr != "" {
		if expiresInt, err = strconv.Atoi(expiresStr); err != nil {
			return nil, err
		}
	}

	expires := time.Unix(0, int64(expiresInt)*1000000)

	plan := NewItunesPlan()
	plan.Expires = expires
	plan.Receipt = result.LatestReceipt
	plan.Status = result.Status

	return plan, nil
}

func (itunes *ItunesServer) ValidateReceipt(receipt string) (*ItunesPlan, error) {
	fmt.Printf("validating receipt %s, environment: %s", receipt, itunes.Config.Environment)
	body, err := json.Marshal(map[string]string{
		"receipt-data": receipt,
		"password":     itunes.Config.SharedSecret,
	})
	if err != nil {
		return nil, err
	}

	var itunesUrl string
	if itunes.Config.Environment == "production" {
		itunesUrl = ItunesUrlProduction
	} else {
		itunesUrl = ItunesUrlSandbox
	}

	resp, err := http.Post(itunesUrl, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}

	respData, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	result, err := parseItunesResult(respData)

	switch result.Status {
	case ItunesStatusOK, ItunesStatusExpired:
		return result, nil
	case ItunesStatusInvalidReceipt, ItunesStatusNotAuthenticated:
		return nil, ErrInvalidReceipt
	default:
		return nil, errors.New(fmt.Sprintf("Failed to validate receipt, status: %d", result.Status))
	}
}

type ValidateItunesReceipt struct {
	*Server
}

func (h *ValidateItunesReceipt) Handle(w http.ResponseWriter, r *http.Request, a *pc.AuthToken) error {
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
		plan, err := h.Itunes.ValidateItunesReceipt(receiptData)
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

package main

import (
	"encoding/json"
	pc "github.com/maklesoft/padlock-cloud/padlockcloud"
	"github.com/stripe/stripe-go"
	"github.com/stripe/stripe-go/coupon"
	"github.com/stripe/stripe-go/customer"
	"github.com/stripe/stripe-go/sub"
	"math/rand"
	"strconv"
	"time"
)

var AvailablePlans []*stripe.Plan

func planToMap(plan *stripe.Plan) map[string]interface{} {
	planJSON, _ := json.Marshal(plan)
	var planMap map[string]interface{}
	json.Unmarshal(planJSON, &planMap)
	planMap["name"] = plan.Nickname
	return planMap
}

func ChoosePlan() string {
	plan := AvailablePlans[rand.Intn(len(AvailablePlans))]
	return plan.ID
}

type Promo struct {
	Created      time.Time      `json:"created"`
	Coupon       *stripe.Coupon `json:"coupon"`
	Title        string         `json:"title"`
	Description  string         `json:"description"`
	RedeemWithin int            `json:"redeemWithin"`
}

type Account struct {
	Email           string
	Created         time.Time
	Customer        *stripe.Customer
	TrackingID      string
	Promo           *Promo
	CustomerUpdated time.Time
}

func (acc *Account) Subscription() *stripe.Sub {
	if acc.Customer == nil {
		return nil
	} else if subs := acc.Customer.Subs.Values; len(subs) == 0 {
		return nil
	} else {
		return subs[0]
	}
}

// Implements the `Key` method of the `Storable` interface
func (acc *Account) Key() []byte {
	return []byte(acc.Email)
}

// Implementation of the `Storable.Deserialize` method
func (acc *Account) Deserialize(data []byte) error {
	return json.Unmarshal(data, acc)
}

// Implementation of the `Storable.Serialize` method
func (acc *Account) Serialize() ([]byte, error) {
	return json.Marshal(acc)
}

func (acc *Account) SetCustomer(c *stripe.Customer) {
	acc.Customer = c
	acc.CustomerUpdated = time.Now()
}

func (acc *Account) CreateCustomer() error {
	params := &stripe.CustomerParams{
		Email: acc.Email,
	}

	if c, err := customer.New(params); err != nil {
		return err
	} else {
		acc.SetCustomer(c)
	}

	return acc.CreateSubscription()
}

func (acc *Account) UpdateCustomer() error {
	if acc.Customer == nil {
		if err := acc.CreateCustomer(); err != nil {
			return err
		}
	} else if time.Since(acc.CustomerUpdated) > time.Hour*24 {
		if c, err := customer.Get(acc.Customer.ID, nil); err != nil {
			return err
		} else {
			acc.SetCustomer(c)
		}
	}

	return nil
}

func (acc *Account) CreateSubscription() error {
	if s, err := sub.New(&stripe.SubParams{
		Customer: acc.Customer.ID,
		Plan:     ChoosePlan(),
	}); err != nil {
		return err
	} else {
		acc.Customer.Subs.Values = []*stripe.Sub{s}
	}

	return nil
}

func (acc *Account) GetPaymentSource() *stripe.PaymentSource {
	if acc.Customer == nil {
		return nil
	}

	return acc.Customer.DefaultSource
}

func (acc *Account) SetPaymentSource(token string) error {
	params := &stripe.CustomerParams{}
	params.SetSource(token)

	var err error
	acc.Customer, err = customer.Update(acc.Customer.ID, params)
	return err
}

func (acc *Account) HasActiveSubscription() bool {
	subStatus, _ := acc.SubscriptionStatus()
	return subStatus == "active"
}

func (acc *Account) SubscriptionStatus() (string, int64) {
	status := ""
	hasPaymentSource := acc.GetPaymentSource() != nil
	var trialEnd int64 = 0

	if s := acc.Subscription(); s != nil {
		status = string(s.Status)
		trialEnd = s.TrialEnd
	} else if hasPaymentSource {
		status = "canceled"
	}

	if (status == "past_due" || status == "unpaid") && !hasPaymentSource {
		status = "trial_expired"
	}

	return status, trialEnd
}

func (acc *Account) RemainingTrialPeriod() time.Duration {
	sub := acc.Subscription()

	if sub == nil {
		return 0
	}

	trialEnd := time.Unix(sub.TrialEnd, 0)
	remaining := trialEnd.Sub(time.Now())
	if remaining < 0 {
		return 0
	} else {
		return remaining
	}
}

func (acc *Account) RemainingTrialDays() int {
	return int(acc.RemainingTrialPeriod().Hours()/24) + 1
}

func (subAcc *Account) ToMap(acc *pc.Account) map[string]interface{} {
	accMap := acc.ToMap()
	accMap["trackingID"] = subAcc.TrackingID

	subStatus, trialEnd := subAcc.SubscriptionStatus()
	accMap["subscription"] = map[string]interface{}{
		"status":   subStatus,
		"trialEnd": trialEnd,
	}

	accMap["plan"] = planToMap(AvailablePlans[0])

	if c := subAcc.Customer; c != nil {
		var card *stripe.Card
		if len(c.Sources.Values) != 0 && c.Sources.Values[0].Card != nil {
			card = c.Sources.Values[0].Card
			accMap["paymentSource"] = map[string]string{
				"brand":    string(card.Brand),
				"lastFour": card.LastFour,
			}
		}

		billing := map[string]string{
			"vat": c.BusinessVatID,
		}

		if c.Shipping != nil {
			billing["name"] = c.Shipping.Name
			billing["address1"] = c.Shipping.Address.Line1
			billing["address2"] = c.Shipping.Address.Line2
			billing["postalCode"] = c.Shipping.Address.Zip
			billing["city"] = c.Shipping.Address.City
			billing["country"] = c.Shipping.Address.Country
		} else if card != nil {
			billing["name"] = card.Name
			billing["address1"] = card.Address1
			billing["address2"] = card.Address2
			billing["postalCode"] = card.Zip
			billing["city"] = card.City
			if card.Country != "" {
				billing["country"] = card.Country
			} else {
				billing["country"] = card.CardCountry
			}
		}

		accMap["billing"] = billing
	}

	accMap["promo"] = subAcc.Promo

	return accMap
}

func NewAccount(email string) (*Account, error) {
	acc := &Account{
		Email:   email,
		Created: time.Now(),
	}

	if err := acc.CreateCustomer(); err != nil {
		return nil, err
	}

	return acc, nil
}

func PromoFromCoupon(couponCode string) (*Promo, error) {
	if coup, err := coupon.Get(couponCode, nil); err != nil {
		return nil, err
	} else {
		redeemWithin, _ := strconv.Atoi(coup.Meta["redeemWithin"])

		return &Promo{
			Coupon:       coup,
			Created:      time.Now(),
			Title:        coup.Meta["title"],
			Description:  coup.Meta["description"],
			RedeemWithin: redeemWithin,
		}, nil
	}
}

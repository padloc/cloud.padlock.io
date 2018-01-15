package main

import (
	"encoding/json"
	pc "github.com/maklesoft/padlock-cloud/padlockcloud"
	"github.com/stripe/stripe-go"
	"github.com/stripe/stripe-go/customer"
	"github.com/stripe/stripe-go/sub"
	"math/rand"
	"time"
)

var AvailablePlans []*stripe.Plan

func ChoosePlan() string {
	plan := AvailablePlans[rand.Intn(len(AvailablePlans))]
	return plan.ID
}

type Account struct {
	Email      string
	Created    time.Time
	Customer   *stripe.Customer
	TrackingID string
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

func (acc *Account) CreateCustomer() error {
	params := &stripe.CustomerParams{
		Email: acc.Email,
	}

	var err error
	acc.Customer, err = customer.New(params)

	return err
}

func (acc *Account) CreateSubscription() error {
	if acc.Customer == nil {
		if err := acc.CreateCustomer(); err != nil {
			return err
		}
	}

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

func (acc *Account) SetPaymentSource(token string) error {
	params := &stripe.CustomerParams{}
	params.SetSource(token)

	var err error
	acc.Customer, err = customer.Update(acc.Customer.ID, params)
	return err
}

func (acc *Account) HasActiveSubscription() bool {
	sub := acc.Subscription()
	return sub != nil && sub.Status == "active"
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

func NewAccount(email string) (*Account, error) {
	acc := &Account{
		Email:   email,
		Created: time.Now(),
	}

	if err := acc.CreateSubscription(); err != nil {
		return nil, err
	}

	return acc, nil
}

func AccountFromEmail(email string, create bool, storage pc.Storage) (*Account, error) {
	acc := &Account{Email: email}
	if err := storage.Get(acc); err != nil {
		if err != pc.ErrNotFound {
			return nil, err
		}
		if create {
			if acc, err = NewAccount(email); err != nil {
				return nil, err
			}
			if err = storage.Put(acc); err != nil {
				return nil, err
			}
		}
	}
	return acc, nil
}

func EnsureSubscription(acc *Account, store pc.Storage) (*stripe.Sub, error) {
	if acc.Subscription() == nil {
		if err := acc.CreateSubscription(); err != nil {
			return nil, err
		}
		if err := store.Put(acc); err != nil {
			return acc.Subscription(), err
		}
	}

	return acc.Subscription(), nil
}

package main

import (
	"github.com/stripe/stripe-go"
	"github.com/stripe/stripe-go/customer"
	"time"
)

const PlanMonthly = "padlock-cloud-monthly"

type Subscription struct {
	Created    time.Time
	Expires    time.Time
	CustomerID string
}

func (s *Subscription) Active() bool {
	return s.Expires.After(time.Now())
}

func init() {
	stripe.Key = "sk_test_x6LQWbCbcOVtLigVGzf5X5Bc"
}

func NewSubscription(token string) (*Subscription, error) {
	params := &stripe.CustomerParams{
		Plan: PlanMonthly,
	}

	params.SetSource(token)

	c, err := customer.New(params)
	if err != nil {
		return nil, err
	}

	sub := &Subscription{
		Created:    time.Now(),
		Expires:    time.Now().Add(time.Hour * 24 * 30),
		CustomerID: c.ID,
	}

	return sub, nil
}

package main

import (
	"fmt"
	"github.com/stripe/stripe-go"
	"net/http"
)

type SubscriptionRequired struct {
}

func (e *SubscriptionRequired) Code() string {
	return "subscription_required"
}

func (e *SubscriptionRequired) Error() string {
	return fmt.Sprintf("%s", e.Code())
}

func (e *SubscriptionRequired) Status() int {
	return http.StatusForbidden
}

func (e *SubscriptionRequired) Message() string {
	return http.StatusText(e.Status())
}

type InvalidReceipt struct {
}

func (e *InvalidReceipt) Code() string {
	return "invalid_receipt"
}

func (e *InvalidReceipt) Error() string {
	return fmt.Sprintf("%s", e.Code())
}

func (e *InvalidReceipt) Status() int {
	return http.StatusBadRequest
}

func (e *InvalidReceipt) Message() string {
	return http.StatusText(e.Status())
}

type StripeError struct {
	Err *stripe.Error
}

func (e *StripeError) Code() string {
	return string(e.Err.Code)
}

func (e *StripeError) Error() string {
	return fmt.Sprintf("%s - %s", e.Code(), e.Err)
}

func (e *StripeError) Status() int {
	return e.Err.HTTPStatusCode
}

func (e *StripeError) Message() string {
	return e.Err.Msg
}

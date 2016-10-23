package main

import "fmt"
import "net/http"

type SubscriptionRequired struct {
}

func (e *SubscriptionRequired) Code() string {
	return "plan_required"
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

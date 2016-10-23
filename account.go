package main

import "encoding/json"
import "time"

type Account struct {
	Email        string
	Created      time.Time
	Subscription *Subscription
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

func (acc *Account) HasActiveSubscription() bool {
	return acc.Subscription != nil && acc.Subscription.Active()
}

func (acc *Account) RemainingTrialPeriod() time.Duration {
	remaining := acc.Created.Add(24 * 30 * time.Hour).Sub(time.Now())
	if remaining < 0 {
		return 0
	} else {
		return remaining
	}
}

func (acc *Account) RemainingTrialDays() int {
	return int(acc.RemainingTrialPeriod().Hours()/24) + 1
}

func NewAccount(email string) *Account {
	return &Account{
		Email:   email,
		Created: time.Now(),
	}
}

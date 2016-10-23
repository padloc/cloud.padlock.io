// +build none

package main

import "time"

type Plan struct {
	Created time.Time
	Expires time.Time
}

func (s *Plan) Active() bool {
	return s.Expires.After(time.Now())
}

func NewPlan(duration time.Duration) *Plan {
	plan := &Plan{
		Created: time.Now(),
	}

	if duration != 0 {
		plan.Expires = time.Now().Add(duration)
	}

	return plan
}

type FreePlan struct {
	*Plan
}

func NewFreePlan(duration time.Duration) *FreePlan {
	return &FreePlan{
		NewPlan(duration),
	}
}

type ItunesPlan struct {
	*Plan
	Receipt string
	Status  int
}

func NewItunesPlan() *ItunesPlan {
	return &ItunesPlan{
		Plan: NewPlan(0),
	}
}

func (server *Server) UpdatePlansForAccount(acc *Account) error {
	if acc.Plans.Itunes != nil {
		// Revalidate itunes receipt to see if the plan has been renewed
		plan, err := server.Itunes.ValidateReceipt(acc.Plans.Itunes.Receipt)
		if err != nil {
			return err
		}

		acc.Plans.Itunes = plan
		if err := server.Storage.Put(acc); err != nil {
			return err
		}

		// If the itunes plan has been renewed then we can stop right here
		if acc.Plans.Itunes.Active() {
			return nil
		}
	}

	return nil
}

func (server *Server) CheckPlansForAccount(acc *Account) (bool, error) {
	if acc.HasActivePlan() {
		return true, nil
	}

	if err := server.UpdatePlansForAccount(acc); err != nil {
		return false, err
	}

	return acc.HasActivePlan(), nil
}

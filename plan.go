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

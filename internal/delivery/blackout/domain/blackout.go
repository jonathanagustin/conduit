// Package domain is an environment blackout. A blackout is an interval during
// which a change is refused, in addition to the weekly maintenance window.
package domain

import (
	"fmt"
	"strings"
	"time"
)

// Blackout is one stored interval and the reason an operator wrote.
type Blackout struct {
	Environment string    `json:"environment"`
	StartsAt    time.Time `json:"startsAt"`
	EndsAt      time.Time `json:"endsAt"`
	Reason      string    `json:"reason"`
	Author      string    `json:"author"`
}

// New rejects an interval that has no reason or that ends before it starts.
func New(environment, author, reason string, starts, ends time.Time) (Blackout, error) {
	reason = strings.TrimSpace(reason)
	if strings.TrimSpace(environment) == "" || strings.TrimSpace(author) == "" || reason == "" || !ends.After(starts) {
		return Blackout{}, fmt.Errorf("a blackout needs an environment, an author, a reason, and an end after its start")
	}
	return Blackout{Environment: environment, Author: author, Reason: reason, StartsAt: starts, EndsAt: ends}, nil
}

// Reason returns the operator's text when at falls inside a blackout.
// Outside every blackout it returns empty, and the weekly window still applies.
func Reason(blackouts []Blackout, environment string, at time.Time) string {
	for _, blackout := range blackouts {
		if blackout.Environment != environment {
			continue
		}
		if !at.Before(blackout.StartsAt) && at.Before(blackout.EndsAt) {
			return blackout.Reason
		}
	}
	return ""
}

package domain

import (
	"errors"
	"testing"
	"time"
)

func request(t *testing.T) Event {
	t.Helper()
	e, err := New("dsr_abcdefghijklmnopqrstuvwxyz", Target{Environment: "env-one", Namespace: "app-one", Profile: "profile-one"}, Identity{Actor: "automation@example.org", Owner: "operator@example.org"}, time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC), MaximumLifetime)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func TestExpiryIsExclusiveAndReattachmentCannotExtendIt(t *testing.T) {
	e := request(t)
	if _, err := e.Transition(Expired, "system:expiry", e.ExpiresAt.Add(-time.Nanosecond)); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	expired, err := e.Transition(Expired, "system:expiry", e.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if expired.ExpiresAt != e.ExpiresAt || expired.Owner != e.Owner || expired.Target != e.Target || expired.CreatedAt != e.CreatedAt {
		t.Fatal("immutable binding changed")
	}
	for _, state := range []string{Requested, "READY", "ACTIVE", "ATTACHED", Revoked, Expired} {
		if _, err := expired.Transition(state, e.Owner, e.ExpiresAt); err == nil {
			t.Fatalf("accepted %s", state)
		}
	}
	deleted, err := expired.Transition(Deleted, e.Owner, e.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateHistory([]Event{e, expired, deleted}); err != nil {
		t.Fatal(err)
	}
	if _, err := deleted.Transition(Deleted, e.Owner, e.ExpiresAt); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}
func TestRevocationAndDeletionKeepOwnerAndExpiry(t *testing.T) {
	e := request(t)
	revoked, err := e.Transition(Revoked, "admin@example.org", e.CreatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := revoked.Transition(Deleted, e.Owner, e.CreatedAt); !errors.Is(err, ErrInvalid) {
		t.Fatal("clock regression accepted", err)
	}
	deleted, err := revoked.Transition(Deleted, "admin@example.org", e.CreatedAt.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ValidateHistory([]Event{e, revoked, deleted})
	if err != nil {
		t.Fatal(err)
	}
	if got.Owner != e.Owner || got.Actor == e.Actor || got.ExpiresAt != e.ExpiresAt || got.Revision != 3 {
		t.Fatal(got)
	}
	if _, err := e.Transition(Revoked, e.Owner, e.ExpiresAt); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err := e.Transition(Deleted, e.Owner, e.CreatedAt.Add(-time.Nanosecond)); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
func TestHistoryRejectsTamperedScopesAndMissingEvents(t *testing.T) {
	e := request(t)
	revoked, _ := e.Transition(Revoked, e.Owner, e.CreatedAt.Add(time.Minute))
	deleted, _ := revoked.Transition(Deleted, e.Owner, e.CreatedAt.Add(2*time.Minute))
	cases := []struct {
		name   string
		change func(*Event)
	}{
		{"session", func(v *Event) { v.SessionID = "dsr_bcdefghijklmnopqrstuvwxyz2" }},
		{"owner", func(v *Event) { v.Owner = "intruder@example.org" }},
		{"environment", func(v *Event) { v.Target.Environment = "other-env" }},
		{"namespace", func(v *Event) { v.Target.Namespace = "other-namespace" }},
		{"profile", func(v *Event) { v.Target.Profile = "other-profile" }},
		{"expiry", func(v *Event) { v.ExpiresAt = v.ExpiresAt.Add(-time.Minute) }},
		{"creation", func(v *Event) { v.CreatedAt = v.CreatedAt.Add(-time.Minute) }},
		{"revision", func(v *Event) { v.Revision++ }},
		{"tier", func(v *Event) { v.ValueTier = "OBSERVED" }},
		{"actor", func(v *Event) { v.Actor = "\nspoof" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := revoked
			tc.change(&v)
			if _, err := ValidateHistory([]Event{e, v}); !errors.Is(err, ErrInvalid) {
				t.Fatal(err)
			}
		})
	}
	for _, history := range [][]Event{{revoked}, {e, e}, {e, deleted}, {e, revoked, deleted, deleted}, {revoked, e}} {
		if _, err := ValidateHistory(history); !errors.Is(err, ErrInvalid) {
			t.Fatal(history, err)
		}
	}
	if _, err := ValidateHistory(nil); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
func TestCreationRejectsUnboundedAndUnsupportedInputs(t *testing.T) {
	e := request(t)
	for _, duration := range []time.Duration{-1, 0, MaximumLifetime + time.Nanosecond} {
		if _, err := New(e.SessionID, e.Target, Identity{Actor: e.Actor, Owner: e.Owner}, e.CreatedAt, duration); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	for _, duration := range []time.Duration{time.Nanosecond, MaximumLifetime} {
		if _, err := New(e.SessionID, e.Target, Identity{Actor: e.Actor, Owner: e.Owner}, e.CreatedAt, duration); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"", "../escape", "dsr_ABCDEFGHIJKLMNOPQRSTUVWXYZ", e.SessionID + "a"} {
		if _, err := New(id, e.Target, Identity{Actor: e.Actor, Owner: e.Owner}, e.CreatedAt, time.Hour); !errors.Is(err, ErrInvalid) {
			t.Fatal(id, err)
		}
	}
	for _, ns := range []string{"", "-app", "App", "app/other", "app-"} {
		target := e.Target
		target.Namespace = ns
		if target.Validate() == nil {
			t.Fatal(ns)
		}
	}
	for _, state := range []string{"ACTIVE", "READY", "ATTACHED", "UNKNOWN"} {
		e.State = state
		if e.Validate() == nil {
			t.Fatal(state)
		}
	}
}

package schema

import (
	"encoding/json"
	"errors"
	"github.com/jonathanagustin/conduit/internal/liveevents/domain"
	"testing"
	"time"
)

func envelope() domain.Envelope {
	return domain.Envelope{Version: 1, StreamID: "stream_1", Sequence: 1, Cursor: "cursor_1", Content: domain.Content{OccurredAt: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC), Kind: "plan.changed", Resource: domain.Resource{Type: "plan", ID: "plan_1", Revision: 1}, Data: json.RawMessage(`{"status":"RUNNING"}`)}}
}
func TestCommittedSchemaRejectsExtraFreeTextAndDuplicateMembers(t *testing.T) {
	validator, err := New()
	if err != nil {
		t.Fatal(err)
	}
	valid := envelope()
	if err := validator.Envelope(valid); err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{`{"status":"RUNNING","prompt":"secret"}`, `{"status":"secret","status":"RUNNING"}`, `{"status":"RUNNING","\u0073tatus":"RUNNING"}`, `{"status":"invented-status"}`, `{"status":"RUNNING"} {}`} {
		sample := valid
		sample.Data = json.RawMessage(payload)
		if err := validator.Envelope(sample); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("unsafe payload accepted: %v", err)
		}
	}
	for _, mutate := range []func(*domain.Envelope){func(value *domain.Envelope) { value.Sequence = 0 }, func(value *domain.Envelope) { value.Sequence = domain.MaxSequence + 1 }, func(value *domain.Envelope) { value.Resource.Revision = 0 }, func(value *domain.Envelope) { value.Cursor = "a\ninjected" }, func(value *domain.Envelope) { value.Version = 2 }} {
		sample := valid
		mutate(&sample)
		if err := validator.Envelope(sample); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal("invalid envelope metadata accepted")
		}
	}
}

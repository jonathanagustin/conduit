package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/adapters/logging"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

func TestTheLogTransportRecordsEnoughToActOn(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	transport := logging.New(slog.New(slog.NewJSONHandler(&buffer, nil)))
	err := transport.Deliver(t.Context(), domain.Message{
		EventID: "nev-1",
		Recipient: domain.Recipient{
			Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}, Channel: domain.ChannelEmail,
		},
		Type:        domain.EventPlanFailures,
		ResourceRID: "ri.apollo.main.environment.d41d8cd98f00",
		Environment: "tenant-a",
		Subject:     "[Conduit] PLAN_FAILURES in tenant-a",
		Body:        "Reconciliation gave up",
		OccurredAt:  time.Date(2026, time.August, 18, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &record); err != nil {
		t.Fatalf("the transport wrote no parseable record: %v (%s)", err, buffer.String())
	}
	for field, want := range map[string]any{
		"event":       "nev-1",
		"type":        string(domain.EventPlanFailures),
		"resource":    "ri.apollo.main.environment.d41d8cd98f00",
		"environment": "tenant-a",
		"owner":       "alice",
		"channel":     string(domain.ChannelEmail),
		"subject":     "[Conduit] PLAN_FAILURES in tenant-a",
	} {
		if record[field] != want {
			t.Errorf("record[%q] = %v, want %v", field, record[field], want)
		}
	}
}

func TestTheLogTransportNamesItselfAndAcceptsNoLogger(t *testing.T) {
	t.Parallel()
	if name := logging.New(nil).Name(); name != "log" {
		t.Errorf("Name = %q", name)
	}
}

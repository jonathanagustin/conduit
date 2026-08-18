package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
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

// The log record says whether the recipient's own contact was found and never
// where it points. A log is shipped, indexed and retained by systems this
// decision cannot see, so an address reaching one is not a thing that can be
// taken back -- which is why this is a test rather than a comment.
func TestTheLogTransportRecordsTheDestinationKindAndNeverTheAddress(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	transport := logging.New(slog.New(slog.NewJSONHandler(&buffer, nil)))
	if err := transport.Deliver(t.Context(), domain.Message{
		EventID: "nev-1",
		Recipient: domain.Recipient{
			Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}, Channel: domain.ChannelEmail,
		},
		Type:        domain.EventPlanFailures,
		Destination: domain.Destination{Kind: domain.ContactEmail, Address: "alice@example.test"},
		ResourceRID: "ri.apollo.main.environment.d41d8cd98f00",
		Subject:     "[Conduit] PLAN_FAILURES",
		Body:        "Reconciliation gave up",
		OccurredAt:  time.Date(2026, time.August, 18, 9, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buffer.String(), "alice@example.test") {
		t.Fatalf("the delivery log wrote the recipient's address: %s", buffer.String())
	}
	var record map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["destinationKind"] != string(domain.ContactEmail) {
		t.Errorf("destinationKind = %v, want %s", record["destinationKind"], domain.ContactEmail)
	}
	// And an unresolved destination is legible as one, so an operator can tell a
	// delivery that found a contact from one that fell back to the endpoint.
	buffer.Reset()
	if err := transport.Deliver(t.Context(), domain.Message{
		EventID: "nev-2", Type: domain.EventPlanFailures,
		Recipient: domain.Recipient{
			Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}, Channel: domain.ChannelEmail,
		},
		ResourceRID: "ri.apollo.main.environment.d41d8cd98f00",
		Subject:     "[Conduit] PLAN_FAILURES",
		OccurredAt:  time.Date(2026, time.August, 18, 9, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(buffer.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["destinationKind"] != "" {
		t.Errorf("an unresolved delivery logged destinationKind %v", record["destinationKind"])
	}
}

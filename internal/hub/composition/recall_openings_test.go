package composition

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	apolloapplication "github.com/jonathanagustin/conduit/internal/apollo/application"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

const recallDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// recallingState is a DesiredState an operator would submit to withdraw a
// Product: the recall names a Product nothing in this Environment runs, so what
// the Plan does is unchanged and the only new thing is that a recall is in
// force.
func recallingState(generation string) controlplane.DesiredState {
	return controlplane.DesiredState{
		APIVersion:  "platform.delivery/v1alpha1",
		Kind:        "EnvironmentDeployment",
		Environment: controlplane.Environment{ID: "dev-use2", Generation: generation},
		Releases: []controlplane.Release{{
			Product: "app.example:example-api", Version: "0.1.0",
			Chart:  controlplane.Artifact{Repository: "registry.example/example-api", Tag: "0.1.0", Digest: recallDigest},
			Images: []controlplane.Artifact{{Repository: "registry.example/example-api", Tag: "0.1.0", Digest: recallDigest}},
		}},
		Entities: []controlplane.Entity{
			{Name: "example-api", Product: "app.example:example-api", Version: "0.1.0", Namespace: "example"},
		},
		Recalls: []controlplane.Recall{{
			ID: "rec-legacy-data-loss", Product: "app.example:example-legacy", StartingVersion: "3.0.0",
			Reason: "corrupts the migration ledger on rollback",
		}},
	}
}

func recallHub(t *testing.T) (http.Handler, *notificationStore) {
	t.Helper()
	repository := controlplane.NewMemoryRepository()
	store := newNotificationStore()
	openings, err := NewRecallOpenings(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHub(repository, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		RecallOpenings:                       openings,
	}, Options{NotificationStore: store})
	if err != nil {
		t.Fatal(err)
	}
	return handler, store
}

func propose(t *testing.T, handler http.Handler, state controlplane.DesiredState) {
	t.Helper()
	body, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, handler, http.MethodPost, "/v1/plans", "local-developer", string(body))
	if response.Code != http.StatusCreated {
		t.Fatalf("propose plan = %d %s", response.Code, response.Body.String())
	}
}

// The artifact this change exists to produce, driven through the assembled Hub:
// a recall submitted with a Plan becomes an event a subscriber could have
// filtered on, readable through the notification surface, without
// internal/controlplane learning what a notification is.
func TestARecallInAnAcceptedPlanBecomesANotificationEvent(t *testing.T) {
	t.Parallel()
	handler, store := recallHub(t)
	propose(t, handler, recallingState("0123456789abcdef"))

	if len(store.events) != 1 {
		t.Fatalf("a Plan carrying one recall recorded %d events: %#v", len(store.events), store.events)
	}
	event := store.events[0]
	// Open-ended: the recall names no ending version, so every Release of that
	// Product from 3.0.0 onward is withdrawn, including ones not published yet.
	if event.Type != domain.EventProductOpenEndedRecall {
		t.Errorf("the recall was raised as %q", event.Type)
	}
	if event.ResourceRID != apolloapplication.EnvironmentRID("dev-use2") {
		t.Errorf("the event names %q, which is not the Environment identifier a preference joins on", event.ResourceRID)
	}
	if !strings.Contains(event.Summary, "app.example:example-legacy") ||
		!strings.Contains(event.Summary, "corrupts the migration ledger on rollback") {
		t.Errorf("the summary does not say what was withdrawn or why: %q", event.Summary)
	}

	// Readable through the surface an operator actually uses, which is the half
	// that makes this a notification rather than a row.
	response := request(t, handler, http.MethodGet,
		"/v1/notifications/events?resourceRid="+event.ResourceRID, "local-developer", "")
	if response.Code != http.StatusOK {
		t.Fatalf("read events = %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "app.example:example-legacy") {
		t.Errorf("the recall is not in what the surface serves: %s", response.Body.String())
	}

	// Recalls are submitted whole with every DesiredState, so the same recall
	// arrives again with the next Plan. It is the same fact, and the store
	// deduplicates on the identity the adapter derived for it.
	propose(t, handler, recallingState("fedcba9876543210"))
	if len(store.events) != 1 {
		t.Fatalf("a restated recall was recorded again: %#v", store.events)
	}
}

// A deployment with no notification store passes no port, and a recall still
// deploys. The nil is the whole of "this deployment has nobody to tell", and it
// must not become "this deployment refuses recalls".
func TestAHubWithNoNotificationStoreStillAcceptsARecall(t *testing.T) {
	t.Parallel()
	openings, err := NewRecallOpenings(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil || openings != nil {
		t.Fatalf("a deployment with no store got openings=%#v err=%v", openings, err)
	}
	handler, err := NewHub(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		RecallOpenings:                       openings,
	})
	if err != nil {
		t.Fatal(err)
	}
	propose(t, handler, recallingState("0123456789abcdef"))
}

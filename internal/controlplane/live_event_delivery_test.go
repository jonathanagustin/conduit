package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jonathanagustin/conduit/internal/liveevents/adapters/filejournal"
	liveSchema "github.com/jonathanagustin/conduit/internal/liveevents/adapters/schema"
	"github.com/jonathanagustin/conduit/internal/liveevents/application"
	"github.com/jonathanagustin/conduit/internal/liveevents/domain"
	"github.com/jonathanagustin/conduit/internal/liveevents/ports"
)

type deliveryNativeAuthorizer struct{ repository Repository }

func (auth deliveryNativeAuthorizer) Snapshot(context.Context) (domain.Identity, error) {
	hash := sha256.Sum256([]byte("native-source-test-operator"))
	value := hex.EncodeToString(hash[:])
	return domain.Identity{Owner: value, Represented: value, Authorization: value}, nil
}
func (auth deliveryNativeAuthorizer) Visible(ctx context.Context, _ domain.Identity, subject domain.Subject) (bool, error) {
	// Reading the same native repository here would deadlock if source publication
	// ran while a mutation held its repository lock. This is only a test policy.
	if subject.Type != "plan" {
		return false, nil
	}
	_, err := auth.repository.GetPlan(ctx, subject.ID)
	return err == nil && subject.ID != "pln_hidden", err
}

type deliveryReady struct{}

func (deliveryReady) Check(context.Context) error { return nil }

func TestNativeOutboxDeliveryRestart(t *testing.T) {
	for _, adapter := range []string{"memory", "file"} {
		t.Run(adapter, func(t *testing.T) {
			var repository Repository
			if adapter == "memory" {
				repository = NewMemoryRepository()
			} else {
				file, err := NewFileRepository(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				repository = file
			}
			verifyNativeOutboxDelivery(t, repository)
		})
	}
}

func verifyNativeOutboxDelivery(t *testing.T, repository Repository) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	path := filepath.Join(t.TempDir(), "delivery.json")
	newService := func() *application.Service {
		t.Helper()
		journal, err := filejournal.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		validator, err := liveSchema.New()
		if err != nil {
			t.Fatal(err)
		}
		service, err := application.New(application.Config{Journal: journal, Authorizer: deliveryNativeAuthorizer{repository}, Validator: validator, Readiness: deliveryReady{}, Sources: map[string]ports.CommittedSource{"native": repository.(ports.CommittedSource)}})
		if err != nil {
			t.Fatal(err)
		}
		return service
	}
	service := newService()
	if _, err := service.Open(ctx, ""); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal("stream opened before native source check")
	}
	if _, err := service.DrainCommitted(ctx, "native", 64); err != nil {
		t.Fatal(err)
	}
	subscription, err := service.Open(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"pln_visible", "pln_hidden"} {
		if _, err := repository.CreatePlan(ctx, PlanRecord{ID: id, DesiredStateDigest: id, Command: &EntityCommand{Type: "RESTART_RESOURCE"}}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := service.DrainCommitted(ctx, "native", 64)
	if err != nil || result.Records != 2 {
		t.Fatalf("native drain: %+v %v", result, err)
	}
	first, err := subscription.Next(ctx)
	if err != nil || first == nil {
		t.Fatalf("first native event: %v %v", first, err)
	}
	if first.Sequence != 1 || first.Resource.Revision != 1 || first.Resource.ID != domain.IndexID(domain.Subject{Type: "plan", ID: "pln_visible"}) {
		t.Fatalf("hidden source affected visible sequence: %+v", first)
	}
	if err := subscription.Delivered(first.Cursor); err != nil {
		t.Fatal(err)
	}
	subscription.Close()
	if _, err := repository.UpdatePlan(ctx, "pln_visible", func(plan *PlanRecord) error { plan.Status = "RUNNING"; return nil }); err != nil {
		t.Fatal(err)
	}
	if file, ok := repository.(*FileRepository); ok {
		reopened, err := NewFileRepository(file.root)
		if err != nil {
			t.Fatal(err)
		}
		repository = reopened
	}
	restarted := newService()
	result, err = restarted.DrainCommitted(ctx, "native", 64)
	if err != nil || result.Records != 1 {
		t.Fatalf("restart checkpoint: %+v %v", result, err)
	}
	resumed, err := restarted.Open(ctx, first.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	second, err := resumed.Next(ctx)
	if err != nil || second == nil || second.Sequence != 2 || second.Resource.Revision != 2 || second.StreamID != first.StreamID {
		t.Fatalf("recovered native event: %v %v", second, err)
	}
	if result, err := restarted.DrainCommitted(ctx, "native", 64); err != nil || result.Records != 0 {
		t.Fatalf("retry duplicated native event: %+v %v", result, err)
	}
}

func TestPostgresNativeOutboxDeliveryAndRollback(t *testing.T) {
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is required for the PostgreSQL integration gate")
	}
	ctx := context.Background()
	first, err := NewPostgresRepository(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := first.database.ExecContext(ctx, `UPDATE conduit_repository_state SET generation = 0, state = '{}'::jsonb WHERE singleton = TRUE`); err != nil {
		t.Fatal(err)
	}
	second, err := NewPostgresRepository(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	rejected := errors.New("rollback native source")
	err = first.Transaction(ctx, func(view Repository) error {
		if _, err := view.CreatePlan(ctx, PlanRecord{ID: "pln_rolledback"}); err != nil {
			return err
		}
		return rejected
	})
	if !errors.Is(err, rejected) {
		t.Fatal(err)
	}
	records, err := second.ReadCommittedEvents(ctx, nil, 256)
	if err != nil || len(records) != 0 {
		t.Fatalf("rolled-back PostgreSQL source survived: %v %v", records, err)
	}
	verifyNativeOutboxDelivery(t, second)
	// Both native state and producer records are visible to another replica.
	records, err = first.ReadCommittedEvents(ctx, nil, 256)
	if err != nil || len(records) != 3 {
		t.Fatalf("replica lost source records: %v %v", records, err)
	}
}

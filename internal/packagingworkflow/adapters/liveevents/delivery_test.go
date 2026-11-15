package liveevents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/jonathanagustin/conduit/internal/liveevents/adapters/filejournal"
	"github.com/jonathanagustin/conduit/internal/liveevents/adapters/schema"
	"github.com/jonathanagustin/conduit/internal/liveevents/application"
	live "github.com/jonathanagustin/conduit/internal/liveevents/domain"
	"github.com/jonathanagustin/conduit/internal/liveevents/ports"
	packaging "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

type operatorKey struct{}
type visibleRuns struct{}

func (visibleRuns) Snapshot(ctx context.Context) (live.Identity, error) {
	name, _ := ctx.Value(operatorKey{}).(string)
	sum := sha256.Sum256([]byte(name))
	value := hex.EncodeToString(sum[:])
	return live.Identity{Owner: value, Represented: value, Authorization: value}, nil
}
func (visibleRuns) Visible(ctx context.Context, _ live.Identity, subject live.Subject) (bool, error) {
	name, _ := ctx.Value(operatorKey{}).(string)
	return subject.Type == "packaging-run" && ((name == "alice" && subject.ID == "app-1") || (name == "bob" && subject.ID == "app-2")), nil
}

type ready struct{}

func (ready) Check(context.Context) error { return nil }

func TestPackagingSourceFansOutAndReplaysFilteredNativeEvents(t *testing.T) {
	ctx := context.Background()
	reader := fixture()
	reader.runs = append(reader.runs, packaging.RunStatus{RunID: "app-2"})
	reader.events["app-1"] = nil
	path := filepath.Join(t.TempDir(), "delivery.json")
	serviceFor := func() *application.Service {
		t.Helper()
		source, err := New(reader)
		if err != nil {
			t.Fatal(err)
		}
		journal, err := filejournal.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		validator, err := schema.New()
		if err != nil {
			t.Fatal(err)
		}
		service, err := application.New(application.Config{Journal: journal, Authorizer: visibleRuns{}, Validator: validator, Readiness: ready{}, Sources: map[string]ports.CommittedSource{"packaging": source}})
		if err != nil {
			t.Fatal(err)
		}
		return service
	}
	service := serviceFor()
	if _, err := service.DrainCommitted(ctx, "packaging", 64); err != nil {
		t.Fatal(err)
	}
	aliceContext := context.WithValue(ctx, operatorKey{}, "alice")
	bobContext := context.WithValue(ctx, operatorKey{}, "bob")
	alice, err := service.Open(aliceContext, "")
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()
	bob, err := service.Open(bobContext, "")
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()
	reader.events["app-1"] = []packaging.StepEvent{step(1, packaging.StepAssess, string(packaging.OutcomePass))}
	reader.events["app-2"] = []packaging.StepEvent{step(1, packaging.StepAssess, string(packaging.OutcomeFailRequired))}
	if result, err := service.DrainCommitted(ctx, "packaging", 64); err != nil || result.Records != 2 {
		t.Fatalf("drain=%+v err=%v", result, err)
	}
	first, err := alice.Next(ctx)
	if err != nil || first == nil || first.Sequence != 1 || first.Resource.ID != "app-1" {
		t.Fatalf("alice received hidden run: %v %v", first, err)
	}
	bobFirst, err := bob.Next(ctx)
	if err != nil || bobFirst == nil || bobFirst.Sequence != 1 || bobFirst.Resource.ID != "app-2" {
		t.Fatalf("bob received hidden run: %v %v", bobFirst, err)
	}
	if err := alice.Delivered(first.Cursor); err != nil {
		t.Fatal(err)
	}
	if err := bob.Delivered(bobFirst.Cursor); err != nil {
		t.Fatal(err)
	}
	reader.events["app-1"] = append(reader.events["app-1"], step(2, packaging.StepValidate, string(packaging.OutcomeFailRequired)))
	if _, err := service.DrainCommitted(ctx, "packaging", 64); err != nil {
		t.Fatal(err)
	}
	second, err := alice.Next(ctx)
	if err != nil || second == nil || second.Sequence != 2 || second.Resource.Revision != 2 {
		t.Fatalf("native continuation=%v err=%v", second, err)
	}
	if hidden, err := bob.Next(ctx); err != nil || hidden != nil {
		t.Fatal("hidden step leaked into another subscriber")
	}
	alice.Close()
	bob.Close()
	restarted := serviceFor()
	if result, err := restarted.DrainCommitted(ctx, "packaging", 64); err != nil || result.Records != 0 {
		t.Fatalf("recovered source duplicated journal: %+v %v", result, err)
	}
	resumed, err := restarted.Open(aliceContext, first.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	replay, err := resumed.Next(ctx)
	if err != nil || replay == nil || replay.Sequence != second.Sequence || replay.Cursor != second.Cursor || replay.StreamID != second.StreamID {
		t.Fatalf("restart changed replay=%v err=%v", replay, err)
	}
}

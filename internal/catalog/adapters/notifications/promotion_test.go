package notificationsadapter

import (
	"context"
	"errors"
	"strings"
	"testing"

	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	notifications "github.com/jonathanagustin/conduit/internal/notifications/domain"
)

func evaluation(lifecycle catalog.PromotionLifecycle) catalog.PromotionRecord {
	return catalog.PromotionRecord{
		ID: "pro_abc", RunID: "run_abc", Product: "app.example:example-api", Version: "0.2.0",
		FromChannel: "DEV", ToChannel: "STABLE", Status: catalog.PromotionBlocked,
		Lifecycle: lifecycle, LifecycleReason: "evaluation window elapsed",
		Reasons: []string{"evaluation window elapsed"}, EvaluatedAt: at(),
	}
}

func promotions(t *testing.T, emitter *recordingEmitter) *PromotionFailures {
	t.Helper()
	failures, err := NewPromotionFailures(emitter)
	if err != nil {
		t.Fatal(err)
	}
	return failures
}

// An aborted run is the failure: it is over and the Release did not cross.
func TestAnAbortedPromotionIsRaisedOnTheProduct(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	if err := promotions(t, emitter).PromotionEvaluated(context.Background(), evaluation(catalog.PromotionAborted)); err != nil {
		t.Fatal(err)
	}
	if len(emitter.events) != 1 {
		t.Fatalf("one aborted promotion produced %d events", len(emitter.events))
	}
	event := emitter.events[0]
	if event.Type != notifications.EventPromotionFailed {
		t.Errorf("raised as %q", event.Type)
	}
	if event.ResourceRID != ProductRID("app.example:example-api") || event.ResourceRID != "product/app.example:example-api" {
		t.Errorf("the event names %q, not the Product", event.ResourceRID)
	}
	if event.Environment != "" || event.Actor != "" {
		t.Errorf("a promotion crosses channels and names nobody: environment %q actor %q", event.Environment, event.Actor)
	}
	if !event.OccurredAt.Equal(at()) {
		t.Errorf("the event moved in time: %s", event.OccurredAt)
	}
	for _, want := range []string{"app.example:example-api", "0.2.0", "DEV", "STABLE", "evaluation window elapsed"} {
		if !strings.Contains(event.Summary, want) {
			t.Errorf("the summary does not say %q: %q", want, event.Summary)
		}
	}
	if err := event.Validate(); err != nil {
		t.Errorf("the adapter produced an event the domain refuses: %v", err)
	}
}

// BLOCKED alone is a stage still soaking, and is every evaluation of a healthy
// staged promotion until it passes. Announcing those would announce the stage
// working. A cancelled run is a person's decision; a legacy record says
// nothing about whether it is over.
func TestOnlyAnAbortedRunIsAFailure(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	failures := promotions(t, emitter)
	for _, lifecycle := range []catalog.PromotionLifecycle{
		catalog.PromotionRunning, catalog.PromotionSucceeded, catalog.PromotionCancelled, "",
	} {
		if err := failures.PromotionEvaluated(context.Background(), evaluation(lifecycle)); err != nil {
			t.Errorf("%q: %v", lifecycle, err)
		}
	}
	if len(emitter.events) != 0 {
		t.Errorf("a promotion that did not fail was announced as failed: %#v", emitter.events)
	}
}

func TestAnAbortedEvaluationIsOneEventPerEvaluation(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	failures := promotions(t, emitter)
	other := evaluation(catalog.PromotionAborted)
	other.ID = "pro_def"
	for _, record := range []catalog.PromotionRecord{
		evaluation(catalog.PromotionAborted), evaluation(catalog.PromotionAborted), other,
	} {
		if err := failures.PromotionEvaluated(context.Background(), record); err != nil {
			t.Fatal(err)
		}
	}
	if emitter.events[0].ID != emitter.events[1].ID || emitter.events[0].ID == emitter.events[2].ID {
		t.Errorf("identity is not stable in the evaluation: %q %q %q",
			emitter.events[0].ID, emitter.events[1].ID, emitter.events[2].ID)
	}
}

func TestAnAbortedPromotionWithNoLifecycleReasonUsesItsBlockers(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	record := evaluation(catalog.PromotionAborted)
	record.LifecycleReason = ""
	record.Reasons = []string{"canary recall triggered", "evaluation timed out"}
	if err := promotions(t, emitter).PromotionEvaluated(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(emitter.events[0].Summary, "canary recall triggered; evaluation timed out") {
		t.Errorf("the summary lost the reasons: %q", emitter.events[0].Summary)
	}
}

func TestAPromotionAnnouncerRefusesWhatItCannotAnnounce(t *testing.T) {
	t.Parallel()
	if _, err := NewPromotionFailures(nil); err == nil {
		t.Error("an announcer with no emitter was assembled")
	}
	unnamed := evaluation(catalog.PromotionAborted)
	unnamed.Product = ""
	emitter := &recordingEmitter{}
	if err := promotions(t, emitter).PromotionEvaluated(context.Background(), unnamed); err == nil || len(emitter.events) != 0 {
		t.Errorf("an aborted promotion naming no Product was announced: %v %#v", err, emitter.events)
	}
	broken := errors.New("store unavailable")
	if err := promotions(t, &recordingEmitter{err: broken}).PromotionEvaluated(
		context.Background(), evaluation(catalog.PromotionAborted)); !errors.Is(err, broken) {
		t.Errorf("a failed announcement answered %v", err)
	}
}

package notificationsadapter

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	apolloapplication "github.com/jonathanagustin/conduit/internal/apollo/application"
	"github.com/jonathanagustin/conduit/internal/delivery/plan/ports"
	notifications "github.com/jonathanagustin/conduit/internal/notifications/domain"
)

type recordingEmitter struct {
	events []notifications.Event
	err    error
}

func (emitter *recordingEmitter) Emit(_ context.Context, event notifications.Event) error {
	if emitter.err != nil {
		return emitter.err
	}
	emitter.events = append(emitter.events, event)
	return nil
}

func at() time.Time { return time.Date(2026, time.November, 16, 9, 30, 0, 0, time.UTC) }

func failure() ports.PlanFailure {
	return ports.PlanFailure{
		PlanID: "pln_abc", Environment: "tenant-a", Installation: "example-api",
		Reason: "chart rollout failed", At: at(),
	}
}

func announcer(t *testing.T, emitter *recordingEmitter) *Failures {
	t.Helper()
	failures, err := NewFailures(emitter)
	if err != nil {
		t.Fatal(err)
	}
	return failures
}

// The join, and the sentence: the event names the Environment the way the
// Apollo surface does, and says which Plan failed, where, and why in the
// Agent's own words.
func TestAFailedPlanIsRaisedOnTheEnvironmentASubscriberNamed(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	if err := announcer(t, emitter).PlanFailed(context.Background(), failure()); err != nil {
		t.Fatal(err)
	}
	if len(emitter.events) != 1 {
		t.Fatalf("one failed Plan produced %d events", len(emitter.events))
	}
	event := emitter.events[0]
	if event.Type != notifications.EventPlanFailures {
		t.Errorf("raised as %q", event.Type)
	}
	if event.ResourceRID != apolloapplication.EnvironmentRID("tenant-a") || event.Environment != "tenant-a" {
		t.Errorf("the event names %q / %q, which no preference joins on", event.ResourceRID, event.Environment)
	}
	if event.Actor != "" {
		t.Errorf("a failure nobody caused was attributed to %q", event.Actor)
	}
	if !event.OccurredAt.Equal(at()) {
		t.Errorf("the event moved in time: %s", event.OccurredAt)
	}
	for _, want := range []string{"pln_abc", "installation example-api", "Environment tenant-a", "chart rollout failed"} {
		if !strings.Contains(event.Summary, want) {
			t.Errorf("the summary does not say %q: %q", want, event.Summary)
		}
	}
	if err := event.Validate(); err != nil {
		t.Errorf("the adapter produced an event the domain refuses: %v", err)
	}
}

// An Agent that repeats its finish report restates one fact. A different Plan
// is a different one.
func TestARepeatedReportIsOneEventAndAnotherPlanIsAnother(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	failures := announcer(t, emitter)
	repeated := failure()
	repeated.At = at().Add(time.Minute)
	other := failure()
	other.PlanID = "pln_def"
	for _, each := range []ports.PlanFailure{failure(), repeated, other} {
		if err := failures.PlanFailed(context.Background(), each); err != nil {
			t.Fatal(err)
		}
	}
	if emitter.events[0].ID != emitter.events[1].ID {
		t.Error("a repeated report of one failure got a second identity")
	}
	if emitter.events[0].ID == emitter.events[2].ID {
		t.Error("two Plans share an identity")
	}
}

func TestAFailureThatNamesNoInstallationOrReasonStillReads(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	bare := failure()
	bare.Installation, bare.Reason = "", ""
	if err := announcer(t, emitter).PlanFailed(context.Background(), bare); err != nil {
		t.Fatal(err)
	}
	summary := emitter.events[0].Summary
	if strings.Contains(summary, "installation") || !strings.Contains(summary, "the Agent reported no reason") {
		t.Errorf("a failure with no installation or reason reads wrongly: %q", summary)
	}
	if err := emitter.events[0].Validate(); err != nil {
		t.Error(err)
	}
}

func TestAnAgentErrorIsBoundedOnARuneBoundary(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	long := failure()
	long.Reason = strings.Repeat("é", 2000)
	if err := announcer(t, emitter).PlanFailed(context.Background(), long); err != nil {
		t.Fatal(err)
	}
	summary := emitter.events[0].Summary
	if len(summary) > 700 || !utf8.ValidString(summary) || !strings.HasSuffix(summary, "...") {
		t.Errorf("a long reason was not bounded legibly: %d bytes, valid=%t", len(summary), utf8.ValidString(summary))
	}
}

func TestAFailureThatNamesNoPlanOrEnvironmentIsRefused(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	for name, mutate := range map[string]func(*ports.PlanFailure){
		"no plan":        func(value *ports.PlanFailure) { value.PlanID = " " },
		"no environment": func(value *ports.PlanFailure) { value.Environment = "" },
	} {
		broken := failure()
		mutate(&broken)
		if err := announcer(t, emitter).PlanFailed(context.Background(), broken); err == nil {
			t.Errorf("%s: announced", name)
		}
	}
	if len(emitter.events) != 0 {
		t.Errorf("an unjoinable failure was raised: %#v", emitter.events)
	}
}

func TestAnEmitterFailureIsReturnedAndAnAnnouncerNeedsAnEmitter(t *testing.T) {
	t.Parallel()
	broken := errors.New("store unavailable")
	if err := announcer(t, &recordingEmitter{err: broken}).PlanFailed(context.Background(), failure()); !errors.Is(err, broken) {
		t.Errorf("a failed announcement answered %v", err)
	}
	if _, err := NewFailures(nil); err == nil {
		t.Error("an announcer with no emitter was assembled")
	}
}

package liveevents

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	schema "github.com/jonathanagustin/conduit/internal/liveevents/adapters/schema"
	live "github.com/jonathanagustin/conduit/internal/liveevents/domain"
	packaging "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

type readerFixture struct {
	configured  bool
	runs        []packaging.RunStatus
	events      map[string][]packaging.StepEvent
	listError   error
	eventsError error
	calls       map[string]int
}

func (reader *readerFixture) Configured() bool { return reader.configured }
func (reader *readerFixture) List(context.Context) ([]packaging.RunStatus, error) {
	return reader.runs, reader.listError
}
func (reader *readerFixture) Events(_ context.Context, id string) ([]packaging.StepEvent, error) {
	if reader.calls == nil {
		reader.calls = map[string]int{}
	}
	reader.calls[id]++
	return reader.events[id], reader.eventsError
}
func step(sequence int, kind, outcome string) packaging.StepEvent {
	return packaging.StepEvent{Sequence: sequence, At: time.Date(2026, 11, 15, 18, 0, sequence, 0, time.UTC), Step: kind, Outcome: outcome}
}
func fixture() *readerFixture {
	return &readerFixture{configured: true, runs: []packaging.RunStatus{{RunID: "app-1"}}, events: map[string][]packaging.StepEvent{"app-1": {step(1, packaging.StepAssess, string(packaging.OutcomePass))}}}
}

func TestSourceProjectsOnlyNativeStepCodes(t *testing.T) {
	reader := fixture()
	reader.events["app-1"] = []packaging.StepEvent{
		step(1, packaging.StepAssess, string(packaging.OutcomePass)), step(2, packaging.StepReview, packaging.EventWaiting), step(3, packaging.StepApproval, string(packaging.ReviewAccepted)), step(4, packaging.StepHandoff, string(packaging.OutcomeHandoffRecorded)),
	}
	for index := range reader.events["app-1"] {
		event := &reader.events["app-1"][index]
		event.Reason = "PRIVATE_REASON"
		event.SourceDigest = "PRIVATE_SOURCE"
		event.ArtifactDigest = "PRIVATE_ARTIFACT"
		event.ApprovalID = "PRIVATE_APPROVAL"
		event.ProposalRevision = "PRIVATE_REVISION"
		event.PlanID = "PRIVATE_PLAN"
		event.TargetPlanRef = "PRIVATE_TARGET"
	}
	source, err := New(reader)
	if err != nil {
		t.Fatal(err)
	}
	records, err := source.ReadCommittedEvents(context.Background(), nil, 256)
	if err != nil || len(records) != 4 {
		t.Fatalf("records=%v err=%v", records, err)
	}
	validator, err := schema.New()
	if err != nil {
		t.Fatal(err)
	}
	for index, record := range records {
		if record.Position != uint64(index+1) || record.Content.Resource.Revision != record.Position || record.ID != live.RecordID(record.Producer, record.Position) || record.Content.OccurredAt != reader.events["app-1"][index].At {
			t.Fatalf("native coordinate lost: %+v", record)
		}
		if err := validator.Envelope(live.Envelope{Version: 1, StreamID: "source-test", Sequence: uint64(index + 1), Cursor: "cursor_test", Content: record.Content}); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(record)
		if strings.Contains(string(body), "PRIVATE_") {
			t.Fatalf("native metadata escaped: %s", body)
		}
	}
	if records[0].Content.Kind != "packaging.step" || string(records[0].Content.Data) != `{"phase":"assess","outcome":"PASS"}` {
		t.Fatal("assessment did not retain native result")
	}
	if string(records[1].Content.Data) != `{"phase":"review","outcome":"WAITING"}` {
		t.Fatal("review wait was promoted to a pass")
	}
	if records[2].Content.Kind != "index.invalidated" || records[2].Content.Resource.ID != live.IndexID(records[2].Subject) {
		t.Fatal("packaging approval was fabricated as Plan approval")
	}
	if string(records[3].Content.Data) != `{"phase":"handoff","outcome":"HANDOFF_RECORDED"}` {
		t.Fatal("Plan handoff was fabricated as successful rollout")
	}
}

func TestSourceDeduplicatesContinuedExecutionsAndResumes(t *testing.T) {
	reader := fixture()
	reader.runs = []packaging.RunStatus{{RunID: "app-2"}, {RunID: "app-1"}, {RunID: "app-1"}}
	reader.events["app-1"] = append(reader.events["app-1"], step(2, packaging.StepPropose, string(packaging.OutcomePass)))
	reader.events["app-2"] = []packaging.StepEvent{step(1, packaging.StepAssess, string(packaging.OutcomeFailRequired))}
	source, _ := New(reader)
	first, err := source.ReadCommittedEvents(context.Background(), nil, 1)
	if err != nil || len(first) != 1 {
		t.Fatal(err)
	}
	if reader.calls["app-1"] != 1 || reader.calls["app-2"] != 1 {
		t.Fatalf("continued execution queried twice: %v", reader.calls)
	}
	positions := map[string]uint64{first[0].Producer: first[0].Position}
	next, err := source.ReadCommittedEvents(context.Background(), positions, 256)
	if err != nil || len(next) != 2 {
		t.Fatalf("continuation=%v err=%v", next, err)
	}
	for _, record := range next {
		if record.Producer == first[0].Producer && record.Position <= first[0].Position {
			t.Fatal("already delivered step repeated")
		}
	}
	// Recreating the adapter must not invent a volatile source epoch or ID.
	restarted, _ := New(reader)
	same, err := restarted.ReadCommittedEvents(context.Background(), positions, 256)
	if err != nil || !reflect.DeepEqual(next, same) {
		t.Fatal("restart changed native event identity")
	}
}

func TestSourceDoesNotInferStatusWithoutJournal(t *testing.T) {
	reader := fixture()
	reader.runs[0].Phase = packaging.PhaseComplete
	reader.runs[0].Outcome = string(packaging.OutcomeHandoffRecorded)
	reader.events["app-1"] = nil
	source, _ := New(reader)
	records, err := source.ReadCommittedEvents(context.Background(), nil, 256)
	if err != nil || len(records) != 0 {
		t.Fatal("legacy or missing history fabricated a completion")
	}
	reader.configured = false
	if _, err := source.ReadCommittedEvents(context.Background(), nil, 256); !errors.Is(err, live.ErrUnavailable) {
		t.Fatal("unconfigured worker emitted progress")
	}
}

func TestSourceRejectsMalformedOrUnavailableHistory(t *testing.T) {
	for _, kind := range []string{"list-failure", "event-failure", "too-many-runs", "too-many-events", "missing-run", "path-run", "long-run", "zero-sequence", "gap", "zero-time", "cursor-ahead"} {
		t.Run(kind, func(t *testing.T) {
			reader := fixture()
			positions := map[string]uint64{}
			switch kind {
			case "list-failure":
				reader.listError = errors.New("PRIVATE_TOKEN")
			case "event-failure":
				reader.eventsError = errors.New("PRIVATE_TOKEN")
			case "too-many-runs":
				reader.runs = make([]packaging.RunStatus, 1001)
			case "too-many-events":
				reader.events["app-1"] = make([]packaging.StepEvent, 33)
			case "missing-run":
				reader.runs[0].RunID = ""
			case "path-run":
				reader.runs[0].RunID = "app/hidden"
			case "long-run":
				reader.runs[0].RunID = strings.Repeat("a", 201)
			case "zero-sequence":
				reader.events["app-1"][0].Sequence = 0
			case "gap":
				reader.events["app-1"] = append(reader.events["app-1"], step(3, packaging.StepValidate, string(packaging.OutcomePass)))
			case "zero-time":
				reader.events["app-1"][0].At = time.Time{}
			case "cursor-ahead":
				positions[live.ProducerID(live.Subject{Type: "packaging-run", ID: "app-1"})] = 2
			}
			source, _ := New(reader)
			records, err := source.ReadCommittedEvents(context.Background(), positions, 256)
			if err == nil || len(records) != 0 || strings.Contains(err.Error(), "PRIVATE_TOKEN") {
				t.Fatalf("invalid history was accepted or leaked: %v %v", records, err)
			}
		})
	}
}

func TestSourceUnknownCodesOnlyInvalidate(t *testing.T) {
	for _, event := range []packaging.StepEvent{step(1, "PRIVATE_STEP", "PASS"), step(1, packaging.StepReview, "PRIVATE_OUTCOME")} {
		reader := fixture()
		reader.events["app-1"] = []packaging.StepEvent{event}
		source, _ := New(reader)
		records, err := source.ReadCommittedEvents(context.Background(), nil, 256)
		if err != nil || len(records) != 1 || records[0].Content.Kind != "index.invalidated" {
			t.Fatal("unknown code was invented or lost")
		}
		body, _ := json.Marshal(records)
		if strings.Contains(string(body), "PRIVATE_") {
			t.Fatal("unknown native code escaped")
		}
	}
}

func TestSourceHonorsCancellationAndPageBounds(t *testing.T) {
	source, _ := New(fixture())
	for _, limit := range []int{0, 257} {
		if _, err := source.ReadCommittedEvents(context.Background(), nil, limit); !errors.Is(err, live.ErrInvalid) {
			t.Fatal("invalid limit accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.ReadCommittedEvents(ctx, nil, 1); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled source read accepted")
	}
	if _, err := New(nil); !errors.Is(err, live.ErrInvalid) {
		t.Fatal("missing source reader accepted")
	}
}

func TestSourceProjectsRecordedTerminalStatus(t *testing.T) {
	reader := fixture()
	reader.events["app-1"] = []packaging.StepEvent{step(1, packaging.PhaseBlocked, string(packaging.OutcomeCancelled))}
	source, _ := New(reader)
	records, err := source.ReadCommittedEvents(context.Background(), nil, 256)
	if err != nil || len(records) != 1 || records[0].Content.Kind != "packaging.step" || string(records[0].Content.Data) != `{"phase":"blocked","outcome":"CANCELLED"}` {
		t.Fatalf("native cancellation projection: %v %v", records, err)
	}
	validator, err := schema.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := validator.Envelope(live.Envelope{Version: 1, StreamID: "source-test", Sequence: 1, Cursor: "cursor_test", Content: records[0].Content}); err != nil {
		t.Fatal(err)
	}
}

func TestSourceReadsMonotonicRetainedWindowAndDistinguishesLostCheckpoints(t *testing.T) {
	reader := fixture()
	reader.events["app-1"] = nil
	for sequence := 10; sequence < 10+packaging.MaxStepEvents; sequence++ {
		reader.events["app-1"] = append(reader.events["app-1"], step(sequence, packaging.StepReview, packaging.EventWaiting))
	}
	producer := live.ProducerID(live.Subject{Type: "packaging-run", ID: "app-1"})
	source, _ := New(reader)
	for _, tc := range []struct {
		name      string
		positions map[string]uint64
		count     int
		want      error
	}{
		{"new-baseline", nil, 32, nil}, {"retained-predecessor", map[string]uint64{producer: 9}, 32, nil}, {"retained-position", map[string]uint64{producer: 10}, 31, nil}, {"caught-up", map[string]uint64{producer: 41}, 0, nil},
		{"lost-window", map[string]uint64{producer: 8}, 0, live.ErrSourceRetentionGap}, {"known-zero", map[string]uint64{producer: 0}, 0, live.ErrSourceRetentionGap}, {"ahead", map[string]uint64{producer: 42}, 0, live.ErrReset}, {"overflow-ahead", map[string]uint64{producer: ^uint64(0)}, 0, live.ErrReset},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records, err := source.ReadCommittedEvents(context.Background(), tc.positions, 256)
			if !errors.Is(err, tc.want) || len(records) != tc.count {
				t.Fatalf("records=%d err=%v want %v", len(records), err, tc.want)
			}
			if tc.want == nil && len(records) > 0 {
				if records[0].Position != uint64(reader.events["app-1"][32-len(records)].Sequence) || records[len(records)-1].Content.Resource.Revision != 41 {
					t.Fatal("source relabeled retained positions")
				}
			}
			if tc.name == "ahead" && errors.Is(err, live.ErrSourceRetentionGap) {
				t.Fatal("rollback/corrupt cursor became a recoverable retention gap")
			}
		})
	}
	reader.events["app-1"] = reader.events["app-1"][1:]
	if _, err := source.ReadCommittedEvents(context.Background(), nil, 256); !errors.Is(err, live.ErrInvalid) {
		t.Fatal("partial non-native retained window accepted")
	}
}

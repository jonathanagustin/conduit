package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	liveSchema "github.com/jonathanagustin/conduit/internal/liveevents/adapters/schema"
	"github.com/jonathanagustin/conduit/internal/liveevents/domain"
	"github.com/jonathanagustin/conduit/internal/liveevents/ports"
)

func TestNativeOutboxCommittedState(t *testing.T) {
	for _, adapter := range []string{"memory", "file"} {
		t.Run(adapter, func(t *testing.T) {
			ctx := context.Background()
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
			source := repository.(ports.CommittedSource)
			plan, err := repository.CreatePlan(ctx, PlanRecord{ID: "pln_outbox", DesiredStateDigest: "secret-digest", Failure: "secret-failure"})
			if err != nil {
				t.Fatal(err)
			}
			if plan.Revision != 1 {
				t.Fatalf("native initial revision = %d", plan.Revision)
			}
			first, err := source.ReadCommittedEvents(ctx, nil, 256)
			if err != nil || len(first) != 1 {
				t.Fatalf("first=%v err=%v", first, err)
			}
			record := first[0]
			validator, err := liveSchema.New()
			if err != nil {
				t.Fatal(err)
			}
			if err := validator.Envelope(domain.Envelope{Version: 1, StreamID: "test-stream", Sequence: 1, Cursor: "test_cursor", Content: record.Content}); err != nil {
				t.Fatal(err)
			}
			if record.Subject != (domain.Subject{Type: "plan", ID: plan.ID}) || record.Content.Resource.Revision != plan.Revision || record.Position != 1 {
				t.Fatalf("wrong coordinate: %+v", record)
			}
			if string(record.Content.Data) != `{"areas":["plans","approvals","overview","kpi"]}` {
				t.Fatalf("unbounded payload: %s", record.Content.Data)
			}
			if _, err := repository.CreatePlan(ctx, plan); err != nil {
				t.Fatal(err)
			}
			rejected := errors.New("rejected callback")
			if _, err := repository.UpdatePlan(ctx, plan.ID, func(value *PlanRecord) error { value.Failure = "new-secret"; return rejected }); !errors.Is(err, rejected) {
				t.Fatal(err)
			}
			same, _ := source.ReadCommittedEvents(ctx, nil, 256)
			if !reflect.DeepEqual(first, same) {
				t.Fatal("retry or failed mutation appended an event")
			}
			updated, err := repository.UpdatePlan(ctx, plan.ID, func(value *PlanRecord) error { value.Status = "RUNNING"; return nil })
			if err != nil {
				t.Fatal(err)
			}
			positions := map[string]uint64{record.Producer: record.Position}
			next, err := source.ReadCommittedEvents(ctx, positions, 1)
			if err != nil || len(next) != 1 || next[0].Position != 2 || next[0].Content.Resource.Revision != updated.Revision {
				t.Fatalf("next=%v err=%v", next, err)
			}
			// Returned records are copies, including the raw JSON bytes.
			next[0].Content.Data[0] = '!'
			again, _ := source.ReadCommittedEvents(ctx, positions, 1)
			if again[0].Content.Data[0] != '{' {
				t.Fatal("reader aliases persisted event")
			}
			if file, ok := repository.(*FileRepository); ok {
				reopened, err := NewFileRepository(file.root)
				if err != nil {
					t.Fatal(err)
				}
				recovered, err := reopened.ReadCommittedEvents(ctx, positions, 256)
				if err != nil || !reflect.DeepEqual(again, recovered) {
					t.Fatalf("restart changed events: %v %v", recovered, err)
				}
				public, _ := reopened.GetPlan(ctx, plan.ID)
				body, _ := json.Marshal(public)
				var object map[string]any
				_ = json.Unmarshal(body, &object)
				if _, ok := object["_liveOutbox"]; ok {
					t.Fatal("private outbox leaked into native read model")
				}
			} else {
				memory := repository.(*MemoryRepository)
				body, err := json.Marshal(memory.stateLocked())
				if err != nil {
					t.Fatal(err)
				}
				restored, err := decodeMemoryRepositoryState(body)
				if err != nil {
					t.Fatal(err)
				}
				recovered, err := restored.ReadCommittedEvents(ctx, positions, 256)
				if err != nil || !reflect.DeepEqual(again, recovered) {
					t.Fatalf("PostgreSQL state snapshot lost event: %v %v", recovered, err)
				}
			}
		})
	}
}

func TestNativeOutboxFilePartialCombinedCommit(t *testing.T) {
	ctx := context.Background()
	repository, err := NewFileRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := repository.CreatePlan(ctx, PlanRecord{ID: "pln_partial"})
	if err != nil {
		t.Fatal(err)
	}
	request, err := repository.CreateChangeRequest(ctx, ChangeRequest{ID: "crq_partial", PlanID: plan.ID})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := repository.ReadCommittedEvents(ctx, nil, 256)
	positions := map[string]uint64{}
	for _, record := range before {
		positions[record.Producer] = record.Position
	}
	// Both records have been read before the callback. Make only the later Plan
	// write fail; the first CR file remains committed and must retain its own hint.
	_, _, err = repository.UpdateChangeRequestAndPlan(ctx, request.ID, true, func(cr *ChangeRequest, p *PlanRecord) error {
		cr.Status = "APPROVED"
		p.Status = "APPROVED"
		if err := os.Rename(repository.planPath(plan.ID), repository.planPath(plan.ID)+".backup"); err != nil {
			return err
		}
		return os.Mkdir(repository.planPath(plan.ID), 0700)
	})
	if err == nil {
		t.Fatal("expected second write failure")
	}
	// Restore only the test's untouched old Plan to inspect the committed files.
	if err := os.Remove(repository.planPath(plan.ID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(repository.planPath(plan.ID)+".backup", repository.planPath(plan.ID)); err != nil {
		t.Fatal(err)
	}
	events, err := repository.ReadCommittedEvents(ctx, positions, 256)
	if err != nil || len(events) != 1 || events[0].Subject.Type != "change-request" {
		t.Fatalf("partial commit invented Plan event: %v %v", events, err)
	}
	saved, _ := repository.GetChangeRequest(ctx, request.ID)
	oldPlan, _ := repository.GetPlan(ctx, plan.ID)
	if saved.Status != "APPROVED" || oldPlan.Status == "APPROVED" {
		t.Fatal("failure fixture did not exercise partial commit")
	}
}

func TestNativeOutboxRetentionAndValidation(t *testing.T) {
	outbox := nativeLiveOutbox{Position: domain.MaxReplayEvents, Entries: make([]domain.Record, domain.MaxReplayEvents)}
	producerID := domain.ProducerID(domain.Subject{Type: "plan", ID: "pln_retained"})
	for index := range outbox.Entries {
		outbox.Entries[index] = domain.Record{Producer: producerID, Subject: domain.Subject{Type: "plan", ID: "pln_retained"}, Position: uint64(index + 1)}
	}
	outbox = appendNativeInvalidation(outbox, "plan", "pln_retained", domain.MaxReplayEvents+1, "plans")
	producer := outbox.Entries[0].Producer
	if _, err := readNativeOutboxes(context.Background(), []nativeLiveOutbox{outbox}, map[string]uint64{producer: 0}, 256); !errors.Is(err, domain.ErrReset) {
		t.Fatalf("silently skipped retained source gap: %v", err)
	}
	records, err := readNativeOutboxes(context.Background(), []nativeLiveOutbox{outbox}, nil, 1)
	if err != nil || len(records) != 1 || records[0].Position != 2 {
		t.Fatalf("new source baseline: %v %v", records, err)
	}
	for _, limit := range []int{0, 257} {
		if _, err := readNativeOutboxes(context.Background(), nil, nil, limit); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal("invalid page limit accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readNativeOutboxes(ctx, nil, nil, 1); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled read accepted")
	}
}

func TestNativeOutboxReportAndChangeRequest(t *testing.T) {
	for _, adapter := range []string{"memory", "file"} {
		t.Run(adapter, func(t *testing.T) {
			ctx := context.Background()
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
			source := repository.(ports.CommittedSource)
			request, err := repository.CreateChangeRequest(ctx, ChangeRequest{ID: "crq_events", Description: "private description"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repository.CreateChangeRequest(ctx, request); err != nil {
				t.Fatal(err)
			}
			if _, err := repository.UpdateChangeRequest(ctx, request.ID, func(value *ChangeRequest) error { value.Status = "APPROVED"; return nil }); err != nil {
				t.Fatal(err)
			}
			for index := 0; index < 2; index++ {
				if _, err := repository.PutEnvironmentReport(ctx, EnvironmentReport{Environment: "staging", AgentID: "private-agent"}); err != nil {
					t.Fatal(err)
				}
			}
			records, err := source.ReadCommittedEvents(ctx, nil, 256)
			if err != nil || len(records) != 4 {
				t.Fatalf("events=%v err=%v", records, err)
			}
			seen := map[string]int{}
			for _, record := range records {
				seen[record.Subject.Type]++
				if record.Content.Resource.Revision != record.Position {
					t.Fatalf("revision not native: %+v", record)
				}
			}
			if seen["change-request"] != 2 || seen["environment"] != 2 {
				t.Fatalf("wrong native subjects: %v", seen)
			}
		})
	}
}

func TestNativeOutboxTransactionRollback(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	rejected := errors.New("rollback")
	err := repository.Transaction(ctx, func(view Repository) error {
		if _, err := view.CreatePlan(ctx, PlanRecord{ID: "pln_rollback"}); err != nil {
			return err
		}
		return rejected
	})
	if !errors.Is(err, rejected) {
		t.Fatal(err)
	}
	records, err := repository.ReadCommittedEvents(ctx, nil, 256)
	if err != nil || len(records) != 0 {
		t.Fatalf("rolled-back event survived: %v %v", records, err)
	}
	if _, err := repository.GetPlan(ctx, "pln_rollback"); !errors.Is(err, ErrNotFound) {
		t.Fatal("rolled-back Plan survived")
	}
}

func TestNativeOutboxOnlyValidTruncationCanRetireDeliveryEpoch(t *testing.T) {
	subject := domain.Subject{Type: "plan", ID: "pln_retained"}
	producer := domain.ProducerID(subject)
	outbox := nativeLiveOutbox{Position: domain.MaxReplayEvents + 1}
	for i := 0; i < domain.MaxReplayEvents; i++ {
		outbox.Entries = append(outbox.Entries, domain.Record{Producer: producer, Subject: subject, Position: uint64(i + 2)})
	}
	if _, err := readNativeOutboxes(context.Background(), []nativeLiveOutbox{outbox}, map[string]uint64{producer: 0}, 256); !errors.Is(err, domain.ErrSourceRetentionGap) {
		t.Fatal("valid retained window did not require reset")
	}
	if _, err := readNativeOutboxes(context.Background(), []nativeLiveOutbox{outbox}, map[string]uint64{producer: ^uint64(0)}, 256); !errors.Is(err, domain.ErrReset) || errors.Is(err, domain.ErrSourceRetentionGap) {
		t.Fatal("future checkpoint became recoverable retention loss")
	}
	for _, kind := range []string{"short", "counter", "gap", "producer", "subject", "empty"} {
		t.Run(kind, func(t *testing.T) {
			corrupt := outbox
			corrupt.Entries = append([]domain.Record(nil), outbox.Entries...)
			switch kind {
			case "short":
				corrupt.Entries = corrupt.Entries[1:]
			case "counter":
				corrupt.Position++
			case "gap":
				corrupt.Entries[1].Position++
			case "producer":
				corrupt.Entries[1].Producer = "wrong"
			case "subject":
				corrupt.Entries[1].Subject.ID = "pln_other"
			case "empty":
				corrupt.Entries = nil
			}
			if _, err := readNativeOutboxes(context.Background(), []nativeLiveOutbox{corrupt}, map[string]uint64{producer: 0}, 256); !errors.Is(err, domain.ErrInvalid) || errors.Is(err, domain.ErrSourceRetentionGap) {
				t.Fatalf("corruption classified as recoverable truncation: %v", err)
			}
		})
	}
}

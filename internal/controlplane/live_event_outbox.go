package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jonathanagustin/conduit/internal/liveevents/domain"
)

// Native outboxes share the owning object's commit, rather than publishing from
// a callback before its write succeeds. Entries contain only typed invalidations.
// The ordinal survives retention; a lagging reader must reset instead of skipping.
type nativeLiveOutbox struct {
	Position uint64          `json:"position"`
	Entries  []domain.Record `json:"entries,omitempty"`
}

func appendNativeInvalidation(outbox nativeLiveOutbox, kind, id string, revision uint64, areas ...string) nativeLiveOutbox {
	subject := domain.Subject{Type: kind, ID: id}
	producer := domain.ProducerID(subject)
	outbox.Position++
	payload, _ := json.Marshal(struct {
		Areas []string `json:"areas"`
	}{areas})
	record := domain.Record{Producer: producer, Position: outbox.Position, ID: domain.RecordID(producer, outbox.Position), Subject: subject,
		Content: domain.Content{OccurredAt: time.Now().UTC(), Kind: "index.invalidated", Resource: domain.Resource{Type: "index", ID: domain.IndexID(subject), Revision: revision}, Data: payload}}
	// Copy before append: failed file writes must not mutate a previously read
	// snapshot, and repository callers must never receive an alias to the journal.
	entries := append([]domain.Record(nil), outbox.Entries...)
	entries = append(entries, record)
	if len(entries) > domain.MaxReplayEvents {
		entries = entries[len(entries)-domain.MaxReplayEvents:]
	}
	outbox.Entries = entries
	return outbox
}

func (repository *MemoryRepository) appendNativeInvalidation(kind, id string, revision uint64, areas ...string) {
	key := domain.ProducerID(domain.Subject{Type: kind, ID: id})
	repository.liveOutboxes[key] = appendNativeInvalidation(repository.liveOutboxes[key], kind, id, revision, areas...)
}

type planOutboxEnvelope struct {
	PlanRecord
	LiveOutbox nativeLiveOutbox `json:"_liveOutbox,omitempty"`
}
type changeRequestOutboxEnvelope struct {
	ChangeRequest
	LiveOutbox nativeLiveOutbox `json:"_liveOutbox,omitempty"`
}

// These helpers are called only with the repository lock held. A legacy object
// has an empty outbox and begins recording on its next committed mutation.
func (repository *FileRepository) writePlanWithOutbox(id string, record PlanRecord) error {
	var saved planOutboxEnvelope
	if err := readJSON(repository.planPath(id), &saved); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	saved.PlanRecord = record
	saved.LiveOutbox = appendNativeInvalidation(saved.LiveOutbox, "plan", id, record.Revision, "plans", "approvals", "overview", "kpi")
	return atomicWriteJSON(repository.planPath(id), saved)
}
func (repository *FileRepository) writeChangeRequestWithOutbox(id string, record ChangeRequest) error {
	var saved changeRequestOutboxEnvelope
	if err := readJSON(repository.changeRequestPath(id), &saved); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	saved.ChangeRequest = record
	saved.LiveOutbox = appendNativeInvalidation(saved.LiveOutbox, "change-request", id, record.Revision, "approvals", "overview", "kpi")
	return atomicWriteJSON(repository.changeRequestPath(id), saved)
}

func readNativeOutboxes(ctx context.Context, outboxes []nativeLiveOutbox, positions map[string]uint64, limit int) ([]domain.Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 256 {
		return nil, domain.ErrInvalid
	}
	records := make([]domain.Record, 0)
	for _, outbox := range outboxes {
		if len(outbox.Entries) == 0 {
			if outbox.Position != 0 {
				return nil, domain.ErrInvalid
			}
			continue
		}
		if len(outbox.Entries) > domain.MaxReplayEvents {
			return nil, domain.ErrInvalid
		}
		first := outbox.Entries[0]
		last := outbox.Entries[len(outbox.Entries)-1]
		if first.Position == 0 || first.Producer != domain.ProducerID(first.Subject) || last.Position != outbox.Position || last.Position < first.Position || last.Position-first.Position != uint64(len(outbox.Entries)-1) {
			return nil, domain.ErrInvalid
		}
		if first.Position > 1 && len(outbox.Entries) != domain.MaxReplayEvents {
			return nil, domain.ErrInvalid
		}
		for index, entry := range outbox.Entries {
			if entry.Producer != first.Producer || entry.Subject != first.Subject || entry.Position < first.Position || entry.Position-first.Position != uint64(index) {
				return nil, domain.ErrInvalid
			}
		}
		position, known := positions[first.Producer]
		// A new source consumer may establish a baseline at the first retained entry.
		// An existing consumer may never silently skip a missing committed ordinal.
		if position > outbox.Position {
			return nil, domain.ErrReset
		}
		if known && first.Position > 0 && position < first.Position-1 {
			return nil, domain.ErrSourceRetentionGap
		}
		for _, record := range outbox.Entries {
			if record.Position > position {
				records = append(records, cloneJSON(record))
			}
		}
	}
	// Cross-resource wall-clock order carries no authority. Stable producer order
	// preserves each source's ordinal and makes bounded pages deterministic.
	sort.Slice(records, func(i, j int) bool {
		if records[i].Producer == records[j].Producer {
			return records[i].Position < records[j].Position
		}
		return records[i].Producer < records[j].Producer
	})
	if len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}

// ReadCommittedEvents reads private source records for the delivery adapter.
// It does not authorize a public subscriber; delivery must filter every subject.
func (repository *MemoryRepository) ReadCommittedEvents(ctx context.Context, positions map[string]uint64, limit int) ([]domain.Record, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	outboxes := make([]nativeLiveOutbox, 0, len(repository.liveOutboxes))
	for _, outbox := range repository.liveOutboxes {
		outboxes = append(outboxes, outbox)
	}
	return readNativeOutboxes(ctx, outboxes, positions, limit)
}

func (repository *FileRepository) ReadCommittedEvents(ctx context.Context, positions map[string]uint64, limit int) ([]domain.Record, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	var outboxes []nativeLiveOutbox
	for _, directory := range []string{repository.planDirectory(), repository.changeRequestDirectory(), repository.reportDirectory()} {
		entries, err := os.ReadDir(directory)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			var envelope struct {
				LiveOutbox nativeLiveOutbox `json:"_liveOutbox"`
			}
			if err := readJSON(filepath.Join(directory, entry.Name()), &envelope); err != nil {
				return nil, err
			}
			outboxes = append(outboxes, envelope.LiveOutbox)
		}
	}
	return readNativeOutboxes(ctx, outboxes, positions, limit)
}

func (repository *PostgresRepository) ReadCommittedEvents(ctx context.Context, positions map[string]uint64, limit int) (records []domain.Record, err error) {
	err = repository.withState(ctx, false, func(memory *MemoryRepository) error {
		var readErr error
		records, readErr = memory.ReadCommittedEvents(ctx, positions, limit)
		return readErr
	})
	return records, err
}

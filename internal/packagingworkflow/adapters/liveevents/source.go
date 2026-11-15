// Package liveevents projects the native packaging step journal into bounded
// delivery hints. It never starts workers, reads file bodies, or grants approval.
package liveevents

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"

	live "github.com/jonathanagustin/conduit/internal/liveevents/domain"
	packaging "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

const maxRuns = 1000

var runIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,199}$`)

// Reader is the existing native workflow-history projection, not browser input.
// Events must be stable, ordered journal entries reconstructed from committed
// Temporal history, including entries carried through continue-as-new.
type Reader interface {
	Configured() bool
	List(context.Context) ([]packaging.RunStatus, error)
	Events(context.Context, string) ([]packaging.StepEvent, error)
}

type Source struct{ reader Reader }

func New(reader Reader) (*Source, error) {
	if reader == nil {
		return nil, live.ErrInvalid
	}
	return &Source{reader: reader}, nil
}

// ReadCommittedEvents preserves native journal sequence/timestamp coordinates.
// A legacy empty journal emits nothing. Unavailable or malformed workers fail
// the source instead of fabricating progress or skipping an unreadable run.
func (source *Source) ReadCommittedEvents(ctx context.Context, positions map[string]uint64, limit int) ([]live.Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > live.MaxQueueEvents {
		return nil, live.ErrInvalid
	}
	if !source.reader.Configured() {
		return nil, live.ErrUnavailable
	}
	runs, err := source.reader.List(ctx)
	if err != nil {
		return nil, sourceError(ctx)
	}
	if len(runs) > maxRuns {
		return nil, live.ErrUnavailable
	}
	// Temporal visibility may list several executions for a continued workflow.
	// Events reads the current logical run once; the native journal carries its
	// earlier entries. Never duplicate those entries for each execution row.
	unique := map[string]bool{}
	for _, run := range runs {
		if !runIDPattern.MatchString(run.RunID) || packaging.RejectSensitive(run.RunID) != nil {
			return nil, live.ErrInvalid
		}
		unique[run.RunID] = true
	}
	ids := make([]string, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	records := make([]live.Record, 0)
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		events, err := source.reader.Events(ctx, id)
		if err != nil {
			return nil, sourceError(ctx)
		}
		if len(events) > packaging.MaxStepEvents {
			return nil, live.ErrInvalid
		}
		subject := live.Subject{Type: "packaging-run", ID: id}
		producer := live.ProducerID(subject)
		previous, known := positions[producer]
		var first, last uint64
		for index, event := range events {
			if event.Sequence <= 0 || uint64(event.Sequence) > packaging.MaxStepSequence || event.At.IsZero() {
				return nil, live.ErrInvalid
			}
			if index == 0 {
				first = uint64(event.Sequence)
			}
			if uint64(event.Sequence) != first+uint64(index) {
				return nil, live.ErrInvalid
			}
			last = uint64(event.Sequence)
		}
		// Native v4 histories rotate only when their complete window is full.
		if first > 1 && len(events) != packaging.MaxStepEvents {
			return nil, live.ErrInvalid
		}
		if previous > last {
			return nil, live.ErrReset
		}
		if known && first > 0 && previous < first-1 {
			return nil, live.ErrSourceRetentionGap
		}
		for _, event := range events {
			if uint64(event.Sequence) <= previous {
				continue
			}
			records = append(records, project(subject, producer, event))
		}
	}
	// Stable source order agrees with the native outbox reader. Wall-clock time
	// and visibility-list order do not define causality across different runs.
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

func sourceError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return live.ErrUnavailable
}
func project(subject live.Subject, producer string, event packaging.StepEvent) live.Record {
	position := uint64(event.Sequence)
	content := live.Content{OccurredAt: event.At.UTC(), Resource: live.Resource{Type: subject.Type, ID: subject.ID, Revision: position}}
	if knownStep(event.Step) && knownOutcome(event.Outcome) {
		content.Kind = "packaging.step"
		content.Data, _ = json.Marshal(struct {
			Phase   string `json:"phase"`
			Outcome string `json:"outcome"`
		}{event.Step, event.Outcome})
	} else {
		// Native approval dispositions and future journal codes have no equivalent
		// in the v1 step payload. Invalidate the run without inventing a phase or
		// promoting a packaging review into a Plan approval.
		content.Kind = "index.invalidated"
		content.Resource.Type = "index"
		content.Resource.ID = live.IndexID(subject)
		content.Data = json.RawMessage(`{"areas":["packaging","overview","kpi"]}`)
	}
	return live.Record{Producer: producer, Position: position, ID: live.RecordID(producer, position), Subject: subject, Content: content}
}
func knownStep(step string) bool {
	switch step {
	case packaging.StepAssess, packaging.StepPropose, packaging.StepValidate, packaging.StepReview, packaging.StepHandoff, packaging.PhaseBlocked, packaging.PhaseComplete:
		return true
	}
	return false
}
func knownOutcome(outcome string) bool {
	switch outcome {
	case string(packaging.OutcomePass), string(packaging.OutcomeFailRequired), string(packaging.OutcomeUnknownSecurity), string(packaging.OutcomeUnsupported), string(packaging.OutcomeTimedOut), string(packaging.OutcomeCancelled), string(packaging.OutcomeStaleApproval), string(packaging.OutcomeRejected), string(packaging.OutcomeHandoffRecorded), packaging.EventWaiting, packaging.EventNotRun:
		return true
	}
	return false
}

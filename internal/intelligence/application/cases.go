package application

import (
	"context"
	"errors"
	"sync"
)

// MaxCases bounds the eval case list: the built-in cases plus every case an
// operator added. The bound is global, not per owner, because the case list
// is one shared list with no owner: every operator runs the same evals.
const MaxCases = 256

// ErrCaseLimit means the case list is full.
var ErrCaseLimit = errors.New("evaluation case limit reached")

// CaseStore keeps operator-added eval cases. The built-in cases are code and
// are never stored.
type CaseStore interface {
	// Add stores a case and returns it. Adding an id that is already stored
	// returns the stored case and changes nothing, even at the limit. A new
	// id when limit cases are already stored is ErrCaseLimit. The check and
	// the insert are one atomic step.
	Add(ctx context.Context, item Case, limit int) (Case, error)
	// List returns the stored cases in the order they were added.
	List(ctx context.Context) ([]Case, error)
}

// builtinCases are the gate-rule cases every Hub has.
func builtinCases() []Case {
	return []Case{
		{ID: "mem-512", Scenario: "A 512Mi limit is within the ceiling", BoundObject: "sample-app-1", Value: "512Mi", ExpectedOutcome: "PASS", Source: "gate rule"},
		{ID: "mem-8gi", Scenario: "An 8Gi limit exceeds the ceiling", BoundObject: "sample-app-blocked", Value: "8Gi", ExpectedOutcome: "FAIL_REQUIRED", Source: "gate rule"},
		{ID: "mem-blank", Scenario: "A missing size is not a memory limit", BoundObject: "sample-app-1", Value: "", ExpectedOutcome: "FAIL_REQUIRED", Source: "gate rule"},
	}
}

// StoredCaseLimit is how many operator-added cases fit under MaxCases.
func StoredCaseLimit() int { return MaxCases - len(builtinCases()) }

// MemoryCases is the in-memory CaseStore: the store for a Hub with no
// database and the default for a Service given none. Cases are lost when the
// process exits.
type MemoryCases struct {
	mu    sync.Mutex
	cases []Case
}

// NewMemoryCases returns an empty in-memory store.
func NewMemoryCases() *MemoryCases { return &MemoryCases{} }

func (store *MemoryCases) Add(_ context.Context, item Case, limit int) (Case, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, existing := range store.cases {
		if existing.ID == item.ID {
			return existing, nil
		}
	}
	if len(store.cases) >= limit {
		return Case{}, ErrCaseLimit
	}
	store.cases = append(store.cases, item)
	return item, nil
}

func (store *MemoryCases) List(context.Context) ([]Case, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return append([]Case{}, store.cases...), nil
}

var _ CaseStore = (*MemoryCases)(nil)

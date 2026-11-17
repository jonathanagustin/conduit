package modelgovernor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
)

// Scope is what a context carries to the transport: which governor, who is
// asking, and the run the call belongs to.
type Scope struct {
	Governor *Governor
	// Owner resolves the operator. It is called at most once per model call
	// and only when one happens. Nil or empty is SystemOwner.
	Owner func() string
	run   *Run
}

type scopeKey struct{}

// WithScope starts a run and carries it with the governor and the owner. One
// run is one unit of work: an HTTP request, a workflow activity.
func WithScope(ctx context.Context, scope Scope) context.Context {
	if scope.run == nil {
		scope.run = NewRun()
	}
	return context.WithValue(ctx, scopeKey{}, scope)
}

func scopeFrom(ctx context.Context) Scope {
	scope, _ := ctx.Value(scopeKey{}).(Scope)
	return scope
}

func (scope Scope) owner() string {
	if scope.Owner == nil {
		return SystemOwner
	}
	if owner := strings.TrimSpace(scope.Owner()); owner != "" {
		return owner
	}
	return SystemOwner
}

// RunFrom is the run a context carries, or nil.
func RunFrom(ctx context.Context) *Run { return scopeFrom(ctx).run }

// Annotate names the session the context's run belongs to: an assistant
// thread, a planning session. It is recorded on every later audit event and
// transcript of the run. Without a run it does nothing.
func Annotate(ctx context.Context, session string) {
	if run := RunFrom(ctx); run != nil {
		run.mu.Lock()
		run.session = session
		run.mu.Unlock()
	}
}

// Run is the per-run state: the round counter and the head of the hash chain.
type Run struct {
	id string

	mu              sync.Mutex
	session         string
	round           int
	chain           string
	transcriptBytes int64
	refusal         error
}

// NewRun starts a run with a random id.
func NewRun() *Run {
	var entropy [16]byte
	_, _ = rand.Read(entropy[:])
	return &Run{id: "airun_" + hex.EncodeToString(entropy[:])}
}

// ID is the run id an audit event and a transcript are filed under.
func (run *Run) ID() string { return run.id }

// Rounds is how many model calls the run has audited.
func (run *Run) Rounds() int {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.round
}

func (run *Run) refuse(err error) {
	if !errors.Is(err, ErrBusy) && !errors.Is(err, ErrBudgetExceeded) {
		return
	}
	run.mu.Lock()
	run.refusal = err
	run.mu.Unlock()
}

// Refusal is the last busy or budget refusal in the run, or nil. An HTTP
// layer reads it to answer 429 whatever the capability made of the error.
func (run *Run) Refusal() error {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.refusal
}

// ValidRunID reports whether text has the shape NewRun produces.
func ValidRunID(text string) bool {
	rest, ok := strings.CutPrefix(text, "airun_")
	if !ok || len(rest) != 32 {
		return false
	}
	_, err := hex.DecodeString(rest)
	return err == nil && strings.ToLower(rest) == rest
}

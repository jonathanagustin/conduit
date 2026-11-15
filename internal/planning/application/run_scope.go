package application

import (
	"context"
	"encoding/json"
	"errors"
)

// ToolObserver records a planning tool boundary before execution and its
// outcome afterward. It receives arguments for validation and hashing, not a
// promise that raw arguments belong in persistent audit records.
type ToolObserver interface {
	Started(context.Context, string, json.RawMessage) (string, error)
	Finished(context.Context, string, bool) error
}

// RunScope binds transport-independent correlation and a per-run observer.
// The authenticated Hub adapter creates it; model output cannot set it.
type RunScope struct {
	ThreadID string
	RunID    string
	Tools    ToolObserver
}

type runScopeKey struct{}

// WithRunScope attaches a run's immutable binding to this request context.
func WithRunScope(ctx context.Context, scope RunScope) context.Context {
	return context.WithValue(ctx, runScopeKey{}, scope)
}

// RunIdentifiers returns correlation for audit adapters. Empty IDs mean a
// legacy planning call without an agent-run transport.
func RunIdentifiers(ctx context.Context) (threadID, runID string) {
	scope, _ := ctx.Value(runScopeKey{}).(RunScope)
	return scope.ThreadID, scope.RunID
}

// ToolStarted must succeed before the model adapter executes the tool.
func ToolStarted(ctx context.Context, name string, args json.RawMessage) (string, error) {
	scope, _ := ctx.Value(runScopeKey{}).(RunScope)
	if scope.Tools == nil {
		return "", nil
	}
	id, err := scope.Tools.Started(ctx, name, append(json.RawMessage(nil), args...))
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", errors.New("planning tool observer did not bind a call ID")
	}
	return id, nil
}

// ToolFinished reports only the bounded outcome, not file bodies or tool output.
func ToolFinished(ctx context.Context, id string, succeeded bool) error {
	scope, _ := ctx.Value(runScopeKey{}).(RunScope)
	if scope.Tools == nil {
		return nil
	}
	return scope.Tools.Finished(ctx, id, succeeded)
}

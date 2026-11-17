package composition

import (
	"log/slog"

	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/repoclone"
)

// hubPlanningClone builds the planner's clone function from the operator's
// source-host policy. The policy is read from CONDUIT_SOURCE_ALLOWED_HOSTS,
// CONDUIT_SOURCE_CONNECTIONS, CONDUIT_SOURCE_CLONE_TIMEOUT,
// CONDUIT_SOURCE_CLONE_MAX_BYTES and CONDUIT_SOURCE_CACHE_ENTRIES. A value
// that does not parse is a startup error and not a silent default.
func hubPlanningClone(getenv func(string) string) (planner.CloneFunc, error) {
	policy, err := repoclone.PolicyFromEnv(getenv)
	if err != nil {
		return nil, err
	}
	policy.OnCheckout = logSourceCheckout
	cloner, err := repoclone.New(policy)
	if err != nil {
		return nil, err
	}
	return cloner.Clone, nil
}

// logSourceCheckout records which host, commit and source connection a clone
// used. The connection is named by its id. The credential is never logged.
func logSourceCheckout(event repoclone.Event) {
	attributes := []any{
		"host", event.Host, "commit", event.Commit, "connection", event.ConnectionID, "cached", event.Cached,
	}
	if event.Error != "" {
		slog.Warn("planning source checkout refused or failed", append(attributes, "error", event.Error)...)
		return
	}
	slog.Info("planning source checkout", attributes...)
}

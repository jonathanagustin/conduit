// Package ports declares what the Plan lifecycle tells the rest of the system
// without choosing who listens.
package ports

import (
	"context"
	"time"
)

// PlanFailure is a Plan an Agent reported FAILED, in the terms the lifecycle
// holds at that moment. Identifiers and the Agent's own reason; not the Plan
// record, which belongs to the store that persisted it.
type PlanFailure struct {
	PlanID      string
	Environment string
	// Installation is the step the Agent named as the one that failed. Empty
	// when the Agent named none, which the finish contract allows.
	Installation string
	Reason       string
	At           time.Time
}

// Failures is told when a Plan reached FAILED, so that something outside the
// lifecycle can tell a person.
//
// A port for the reason reconciliation's Escalations is one: the lifecycle must
// not know what a notification is. It is called after the finish transaction
// committed -- a finish that rolled back reports nothing -- and the caller
// drops the error, because the Agent's report is stored by then and refusing it
// would have the Agent retry work that was recorded.
//
// This is not reconciliation's ProposalExhausted and does not replace it. That
// one is a Plan that was never minted; this one is a Plan that was approved,
// leased, executed and failed.
type Failures interface {
	PlanFailed(ctx context.Context, failure PlanFailure) error
}

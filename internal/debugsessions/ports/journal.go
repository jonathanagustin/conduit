// Package ports defines persistence boundaries for non-executing requests.
package ports

import (
	"context"
	"github.com/jonathanagustin/conduit/internal/debugsessions/domain"
)

// Journal stores each lifecycle transition and its audit fact as one immutable
// event. Append atomically compares the complete current revision and validates
// the resulting history before writing. A successful acknowledgement requires
// durable persistence. A failed acknowledgement can be ambiguous after commit;
// callers must inspect history rather than retry with a new identity blindly.
// This port does not authenticate users, authorize access or create terminals.
type Journal interface {
	Append(context.Context, uint64, domain.Event) error
	History(context.Context, string) ([]domain.Event, error)
	// IDs returns lexically ordered IDs strictly after the cursor. Limits are
	// 1..256. Enumeration is an internal storage operation, never authorization.
	IDs(context.Context, string, int) ([]string, error)
}

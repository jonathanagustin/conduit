// Package local is a check-in source with no remote site.
package local

import (
	"context"
	"time"

	"github.com/jonathanagustin/conduit/internal/delivery/checkin/ports"
)

// Adapter reports one state for every site.
type Adapter struct {
	State ports.State
	Now   func() time.Time
}

// CheckIn returns the configured state.
func (adapter Adapter) CheckIn(_ context.Context, site string) (ports.CheckIn, error) {
	now := time.Now()
	if adapter.Now != nil {
		now = adapter.Now()
	}
	return ports.CheckIn{Site: site, State: adapter.State, At: now}, nil
}

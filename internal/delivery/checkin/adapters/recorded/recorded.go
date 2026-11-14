// Package recorded replays check-ins from a fixed list.
package recorded

import (
	"context"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/delivery/checkin/ports"
)

// Adapter is the recorded check-in source.
type Adapter struct {
	Rows map[string]ports.CheckIn
}

// CheckIn returns the recorded row.
func (adapter Adapter) CheckIn(_ context.Context, site string) (ports.CheckIn, error) {
	row, ok := adapter.Rows[site]
	if !ok {
		return ports.CheckIn{}, fmt.Errorf("no recorded check-in for %s", site)
	}
	return row, nil
}

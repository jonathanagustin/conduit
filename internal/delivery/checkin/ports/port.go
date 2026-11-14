// Package ports is the site check-in boundary. A development machine uses a
// local or recorded adapter and does not need a remote site.
package ports

import (
	"context"
	"time"
)

// State is what an operator reads.
type State string

const (
	Online       State = "online"
	Degraded     State = "degraded"
	Disconnected State = "disconnected"
)

// CheckIn is one observation from a site.
type CheckIn struct {
	Site        string
	Environment string
	State       State
	At          time.Time
}

// Checker reports the latest check-in for a site.
type Checker interface {
	CheckIn(context.Context, string) (CheckIn, error)
}

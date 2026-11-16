// Package ports is the native release rollout read boundary.
package ports

import (
	"context"
	"github.com/jonathanagustin/conduit/internal/delivery/releasestatus/domain"
	"time"
)

// Source loads recorded facts. It neither evaluates gates nor advances rollouts.
type Source interface {
	Read(context.Context, string, string, time.Time) (domain.Snapshot, error)
}

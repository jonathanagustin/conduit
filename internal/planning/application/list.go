package application

import (
	"context"
	"sort"

	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

// List reads this service's owner-scoped development store. Production uses
// the Temporal implementation; this map is not a durable inventory fallback.
func (service *Service) List(ctx context.Context) ([]domain.SessionSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if len(service.sessions) > domain.MaxIndexedExecutions {
		return nil, domain.ErrIndexTooLarge
	}
	items := make([]domain.SessionSummary, 0, len(service.sessions))
	for _, session := range service.sessions {
		items = append(items, domain.SummarizeSession(session))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

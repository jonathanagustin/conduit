// Package memory is a process-local journal reference for tests. It supplies
// atomic revisions and audit facts but no restart durability; do not wire it as
// a production persistence component.
package memory

import (
	"context"
	"github.com/jonathanagustin/conduit/internal/debugsessions/domain"
	"github.com/jonathanagustin/conduit/internal/debugsessions/ports"
	"sort"
	"sync"
)

type Store struct {
	mu        sync.Mutex
	histories map[string][]domain.Event
}

func New() *Store { return &Store{histories: make(map[string][]domain.Event)} }
func (s *Store) Append(ctx context.Context, expected uint64, next domain.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	result, err := domain.AppendHistory(s.histories[next.SessionID], expected, next)
	if err != nil {
		return err
	}
	if s.histories == nil {
		s.histories = make(map[string][]domain.Event)
	}
	s.histories[next.SessionID] = result
	return nil
}
func (s *Store) History(ctx context.Context, id string) ([]domain.Event, error) {
	if !domain.ValidID(id) {
		return nil, domain.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := append([]domain.Event(nil), s.histories[id]...)
	if _, err := domain.ValidateHistory(result); err != nil {
		return nil, err
	}
	return result, nil
}
func (s *Store) IDs(ctx context.Context, after string, limit int) ([]string, error) {
	if (after != "" && !domain.ValidID(after)) || limit < 1 || limit > 256 {
		return nil, domain.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(s.histories))
	for id := range s.histories {
		if id > after {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids, nil
}

var _ ports.Journal = (*Store)(nil)

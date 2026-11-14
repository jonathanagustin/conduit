// Package memory supplies explicitly ephemeral discussion storage.
package memory

import (
	"context"
	"github.com/jonathanagustin/conduit/internal/changeactivity/domain"
	"sync"
)

type Store struct {
	mu       sync.Mutex
	comments map[string]domain.Comment
}

func New() *Store { return &Store{comments: map[string]domain.Comment{}} }
func (s *Store) Append(ctx context.Context, c domain.Comment) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.comments[c.RequestID+":"+c.ID]; exists {
		return domain.ErrConflict
	}
	s.comments[c.RequestID+":"+c.ID] = c
	return nil
}
func (s *Store) List(ctx context.Context, requestID string) ([]domain.Comment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]domain.Comment, 0)
	for _, c := range s.comments {
		if c.RequestID == requestID {
			result = append(result, c)
		}
	}
	return result, nil
}

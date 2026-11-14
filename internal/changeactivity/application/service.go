// Package application records discussion without editing approval or review data.
package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/jonathanagustin/conduit/internal/changeactivity/domain"
	"github.com/jonathanagustin/conduit/internal/changeactivity/ports"
	"sort"
	"time"
)

type Service struct {
	store     ports.Store
	authority ports.Authority
	now       func() time.Time
	newID     func() (string, error)
}

func New(store ports.Store, authority ports.Authority) (*Service, error) {
	if store == nil || authority == nil {
		return nil, errors.New("discussion requires storage and caller-bound authority")
	}
	return &Service{store: store, authority: authority, now: time.Now, newID: func() (string, error) {
		var b [16]byte
		if _, e := rand.Read(b[:]); e != nil {
			return "", e
		}
		return "cmt_" + hex.EncodeToString(b[:]), nil
	}}, nil
}
func (s *Service) Append(ctx context.Context, requestID, text string) (domain.Comment, error) {
	if err := ctx.Err(); err != nil {
		return domain.Comment{}, err
	}
	if !domain.ValidRequestID(requestID) || !domain.ValidText(text) {
		return domain.Comment{}, domain.ErrInvalid
	}
	writer, err := s.authority.RequireAnnotation(ctx, requestID)
	if err != nil {
		return domain.Comment{}, err
	}
	id, err := s.newID()
	if err != nil {
		return domain.Comment{}, err
	}
	c := domain.Comment{ID: id, RequestID: requestID, Author: writer.Author, Actor: writer.Actor, CreatedAt: s.now().UTC(), Text: text, ValueTier: domain.ValueTier}
	if err := c.Validate(); err != nil {
		return domain.Comment{}, domain.ErrCorrupt
	}
	if err := ctx.Err(); err != nil {
		return domain.Comment{}, err
	}
	if err := s.store.Append(ctx, c); err != nil {
		return domain.Comment{}, err
	}
	return c, nil
}
func (s *Service) List(ctx context.Context, requestID string) ([]domain.Comment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !domain.ValidRequestID(requestID) {
		return nil, domain.ErrInvalid
	}
	if err := s.authority.RequireRead(ctx, requestID); err != nil {
		return nil, err
	}
	comments, err := s.store.List(ctx, requestID)
	if err != nil {
		return nil, err
	}
	comments = append([]domain.Comment{}, comments...)
	seen := map[string]bool{}
	for _, c := range comments {
		if c.Validate() != nil || c.RequestID != requestID || seen[c.ID] {
			return nil, domain.ErrCorrupt
		}
		seen[c.ID] = true
	}
	sort.Slice(comments, func(i, j int) bool {
		if !comments[i].CreatedAt.Equal(comments[j].CreatedAt) {
			return comments[i].CreatedAt.Before(comments[j].CreatedAt)
		}
		return comments[i].ID < comments[j].ID
	})
	return comments, nil
}

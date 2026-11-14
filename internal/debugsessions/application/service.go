// Package application manages non-executing lifecycle requests over explicit
// current authority and immutable journal boundaries.
package application

import (
	"context"
	"errors"
	"github.com/jonathanagustin/conduit/internal/debugsessions/domain"
	"github.com/jonathanagustin/conduit/internal/debugsessions/ports"
	"time"
)

var ErrUnavailable = errors.New("debug-session request operation unavailable")

const maximumScan = 10000
const expiryActor = "system:conduit.debug-session-expiry"

// Draft requires a caller-retained idempotency key so an ambiguous storage
// acknowledgement can be resolved without creating another request blindly.
type Draft struct {
	SessionID       string
	Target          domain.Target
	LifetimeSeconds int64
}

// Service exposes metadata lifecycle only, with no broker or execution port.
// CONDUIT-INVENTED: debugsessions.RequestAuthority
// Displaced-by: A published or authorized sanitized session-request lifecycle API and authorization contract.
type Service struct {
	journal   ports.Journal
	authority ports.Authority
	now       func() time.Time
}

func New(journal ports.Journal, authority ports.Authority, now func() time.Time) (*Service, error) {
	if journal == nil || authority == nil {
		return nil, ErrUnavailable
	}
	if now == nil {
		now = time.Now
	}
	return &Service{journal, authority, now}, nil
}
func (s *Service) require(ctx context.Context, action string, scope domain.Scope) (ports.Decision, error) {
	if err := ctx.Err(); err != nil {
		return ports.Decision{}, err
	}
	if scope.Validate() != nil {
		return ports.Decision{}, domain.ErrInvalid
	}
	decision, err := s.authority.Require(ctx, action, scope)
	if err != nil {
		return ports.Decision{}, err
	}
	if decision.Identity.Validate() != nil {
		return ports.Decision{}, domain.ErrForbidden
	}
	return decision, nil
}
func (s *Service) repeat(ctx context.Context, action string, scope domain.Scope, previous ports.Decision) (ports.Decision, error) {
	current, err := s.require(ctx, action, scope)
	if err != nil {
		return current, err
	}
	if current.Identity != previous.Identity {
		return ports.Decision{}, domain.ErrForbidden
	}
	return current, nil
}
func (s *Service) load(ctx context.Context, id string) ([]domain.Event, domain.Event, error) {
	if !domain.ValidID(id) {
		return nil, domain.Event{}, domain.ErrInvalid
	}
	events, err := s.journal.History(ctx, id)
	if err != nil {
		return nil, domain.Event{}, err
	}
	current, err := domain.ValidateHistory(events)
	if err != nil || current.SessionID != id {
		return nil, domain.Event{}, domain.ErrCorrupt
	}
	return events, current, nil
}
func visible(e domain.Event, d ports.Decision) bool {
	return e.Owner == d.Identity.Owner || d.ManageOthers
}
func (s *Service) Create(ctx context.Context, draft Draft) (domain.Event, error) {
	if !domain.ValidID(draft.SessionID) || draft.Target.Validate() != nil || draft.LifetimeSeconds < 0 || draft.LifetimeSeconds > int64(domain.MaximumLifetime/time.Second) {
		return domain.Event{}, domain.ErrInvalid
	}
	lifetime := draft.LifetimeSeconds
	if lifetime == 0 {
		lifetime = int64(domain.MaximumLifetime / time.Second)
	}
	decision, err := s.require(ctx, ports.ActionCreate, draft.Target.Scope())
	if err != nil {
		return domain.Event{}, err
	}
	for range 3 {
		_, existing, err := s.load(ctx, draft.SessionID)
		if err == nil {
			if existing.Owner != decision.Identity.Owner {
				return domain.Event{}, domain.ErrNotFound
			}
			if existing.Target != draft.Target || existing.ExpiresAt.Sub(existing.CreatedAt) != time.Duration(lifetime)*time.Second {
				return domain.Event{}, domain.ErrConflict
			}
			return s.expire(ctx, existing, decision, ports.ActionCreate)
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return domain.Event{}, err
		}
		next, err := domain.New(draft.SessionID, draft.Target, decision.Identity, s.now(), time.Duration(lifetime)*time.Second)
		if err != nil {
			return domain.Event{}, err
		}
		if _, err := s.repeat(ctx, ports.ActionCreate, draft.Target.Scope(), decision); err != nil {
			return domain.Event{}, err
		}
		if err := s.journal.Append(ctx, 0, next); errors.Is(err, domain.ErrConflict) {
			continue
		} else if err != nil {
			return domain.Event{}, err
		}
		return next, nil
	}
	return domain.Event{}, domain.ErrConflict
}
func (s *Service) expire(ctx context.Context, current domain.Event, decision ports.Decision, action string) (domain.Event, error) {
	for range 4 {
		now := s.now()
		if current.State != domain.Requested || now.Before(current.ExpiresAt) {
			return current, nil
		}
		fresh, err := s.repeat(ctx, action, current.Target.Scope(), decision)
		if err != nil {
			return domain.Event{}, err
		}
		if !visible(current, fresh) {
			return domain.Event{}, domain.ErrNotFound
		}
		next, err := current.Transition(domain.Expired, expiryActor, now)
		if err != nil {
			return domain.Event{}, err
		}
		if err := s.journal.Append(ctx, current.Revision, next); errors.Is(err, domain.ErrConflict) {
			_, current, err = s.load(ctx, current.SessionID)
			if err != nil {
				return domain.Event{}, err
			}
			if !visible(current, fresh) {
				return domain.Event{}, domain.ErrNotFound
			}
			continue
		} else if err != nil {
			return domain.Event{}, err
		}
		return next, nil
	}
	return domain.Event{}, domain.ErrConflict
}
func (s *Service) Get(ctx context.Context, id string) (domain.Event, error) {
	_, current, err := s.load(ctx, id)
	if err != nil {
		return domain.Event{}, err
	}
	decision, err := s.require(ctx, ports.ActionRead, current.Target.Scope())
	if err != nil {
		return domain.Event{}, err
	}
	if !visible(current, decision) || current.State == domain.Deleted {
		return domain.Event{}, domain.ErrNotFound
	}
	current, err = s.expire(ctx, current, decision, ports.ActionRead)
	if err != nil {
		return domain.Event{}, err
	}
	if current.State == domain.Deleted {
		return domain.Event{}, domain.ErrNotFound
	}
	return current, nil
}
func (s *Service) History(ctx context.Context, id string) ([]domain.Event, error) {
	events, current, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	decision, err := s.require(ctx, ports.ActionRead, current.Target.Scope())
	if err != nil {
		return nil, err
	}
	if !visible(current, decision) {
		return nil, domain.ErrNotFound
	}
	return append([]domain.Event(nil), events...), nil
}
func (s *Service) Mutate(ctx context.Context, id, state string) (domain.Event, error) {
	if state != domain.Revoked && state != domain.Deleted {
		return domain.Event{}, domain.ErrInvalid
	}
	for range 4 {
		_, current, err := s.load(ctx, id)
		if err != nil {
			return domain.Event{}, err
		}
		decision, err := s.require(ctx, ports.ActionMutate, current.Target.Scope())
		if err != nil {
			return domain.Event{}, err
		}
		if !visible(current, decision) {
			return domain.Event{}, domain.ErrNotFound
		}
		if current.State == state {
			return current, nil
		}
		current, err = s.expire(ctx, current, decision, ports.ActionMutate)
		if err != nil {
			return domain.Event{}, err
		}
		if current.State == state {
			return current, nil
		}
		next, err := current.Transition(state, decision.Identity.Actor, s.now())
		if err != nil {
			return domain.Event{}, err
		}
		fresh, err := s.repeat(ctx, ports.ActionMutate, current.Target.Scope(), decision)
		if err != nil {
			return domain.Event{}, err
		}
		if !visible(current, fresh) {
			return domain.Event{}, domain.ErrNotFound
		}
		if err := s.journal.Append(ctx, current.Revision, next); errors.Is(err, domain.ErrConflict) {
			continue
		} else if err != nil {
			return domain.Event{}, err
		}
		return next, nil
	}
	return domain.Event{}, domain.ErrConflict
}

// List returns owner-visible metadata and a cursor made only from a returned
// ID. An exhausted scan fails rather than claiming an incomplete list is empty.
func (s *Service) List(ctx context.Context, scope domain.Scope, after string, limit int) ([]domain.Event, string, error) {
	if (after != "" && !domain.ValidID(after)) || limit < 1 || limit > 100 {
		return nil, "", domain.ErrInvalid
	}
	decision, err := s.require(ctx, ports.ActionRead, scope)
	if err != nil {
		return nil, "", err
	}
	result := []domain.Event{}
	cursor := after
	scanned := 0
	more := false
scan:
	for {
		ids, err := s.journal.IDs(ctx, cursor, 256)
		if err != nil {
			return nil, "", err
		}
		if len(ids) > 256 {
			return nil, "", domain.ErrCorrupt
		}
		for _, id := range ids {
			if !domain.ValidID(id) || id <= cursor {
				return nil, "", domain.ErrCorrupt
			}
			cursor = id
			scanned++
			if scanned > maximumScan {
				return nil, "", ErrUnavailable
			}
			_, current, err := s.load(ctx, id)
			if err != nil {
				return nil, "", err
			}
			if current.Target.Scope() != scope || !visible(current, decision) || current.State == domain.Deleted {
				continue
			}
			current, err = s.expire(ctx, current, decision, ports.ActionRead)
			if err != nil {
				return nil, "", err
			}
			if current.State == domain.Deleted {
				continue
			}
			if len(result) == limit {
				more = true
				break scan
			}
			result = append(result, current)
		}
		if len(ids) < 256 {
			break
		}
	}
	fresh, err := s.repeat(ctx, ports.ActionRead, scope, decision)
	if err != nil {
		return nil, "", err
	}
	for _, e := range result {
		if !visible(e, fresh) {
			return nil, "", domain.ErrForbidden
		}
	}
	next := ""
	if more {
		next = result[len(result)-1].SessionID
	}
	return result, next, nil
}

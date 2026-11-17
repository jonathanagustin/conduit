package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/jonathanagustin/conduit/internal/assistant/domain"
)

// Threads is the conversation store. Every method is scoped to one owner: a
// thread another owner holds is domain.ErrThreadNotFound, never a different
// error, so a caller cannot learn that the id exists.
//
// An adapter enforces domain.MaxThreadsPerOwner on Create and
// domain.MaxMessagesPerThread and Message.Validate on Append, each as one
// atomic step. adapters/threadtest holds the conformance suite.
type Threads interface {
	// Create stores an empty thread, or returns domain.ErrThreadLimit.
	Create(ctx context.Context, thread domain.Thread) error
	// List returns the owner's threads without messages, newest update first,
	// then by id.
	List(ctx context.Context, owner string) ([]domain.Thread, error)
	// Get returns one thread with its messages in append order.
	Get(ctx context.Context, owner, id string) (domain.Thread, error)
	// Append adds messages in order, all or none, and moves UpdatedAt to the
	// last message's CreatedAt. title is used only while the thread has none.
	// It returns domain.ErrThreadFull when they do not all fit.
	Append(ctx context.Context, owner, id string, messages []domain.Message, title string) error
	// Delete removes the thread and its messages.
	Delete(ctx context.Context, owner, id string) error
}

// CreateThread opens an empty thread owned by the caller.
func (service Service) CreateThread(ctx context.Context, actor Actor, title string) (domain.Thread, error) {
	owner, err := service.owner(actor)
	if err != nil {
		return domain.Thread{}, err
	}
	if len(title) > 4*domain.MaxThreadTitleBytes {
		return domain.Thread{}, fmt.Errorf("%w: title is too long", domain.ErrThreadInvalid)
	}
	newID := service.NewID
	if newID == nil {
		newID = randomID
	}
	entropy, err := newID()
	if err != nil {
		return domain.Thread{}, err
	}
	now := service.stamp()
	thread := domain.Thread{
		ID: domain.ThreadID(entropy), Owner: owner, Title: domain.ThreadTitle(title), CreatedAt: now, UpdatedAt: now,
	}
	if !domain.ValidThreadID(thread.ID) {
		return domain.Thread{}, fmt.Errorf("%w: generated id is malformed", domain.ErrThreadInvalid)
	}
	if err := service.Threads.Create(ctx, thread); err != nil {
		return domain.Thread{}, err
	}
	return thread, nil
}

// ListThreads returns the caller's threads without messages.
func (service Service) ListThreads(ctx context.Context, actor Actor) ([]domain.Thread, error) {
	owner, err := service.owner(actor)
	if err != nil {
		return nil, err
	}
	return service.Threads.List(ctx, owner)
}

// GetThread returns one of the caller's threads with its messages.
func (service Service) GetThread(ctx context.Context, actor Actor, id string) (domain.Thread, error) {
	owner, err := service.owner(actor)
	if err != nil {
		return domain.Thread{}, err
	}
	if !domain.ValidThreadID(id) {
		return domain.Thread{}, domain.ErrThreadNotFound
	}
	return service.Threads.Get(ctx, owner, id)
}

// DeleteThread removes one of the caller's threads.
func (service Service) DeleteThread(ctx context.Context, actor Actor, id string) error {
	owner, err := service.owner(actor)
	if err != nil {
		return err
	}
	if !domain.ValidThreadID(id) {
		return domain.ErrThreadNotFound
	}
	return service.Threads.Delete(ctx, owner, id)
}

// owner is the key every store call is scoped to: the authenticated username.
func (service Service) owner(actor Actor) (string, error) {
	if service.Threads == nil {
		return "", domain.ErrThreadsUnavailable
	}
	owner := strings.TrimSpace(actor.Username)
	if !domain.ValidOwner(owner) {
		return "", fmt.Errorf("%w: caller has no usable identity", domain.ErrThreadInvalid)
	}
	return owner, nil
}

func randomID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// Package threadmemory keeps assistant conversations in process memory. It is
// the store for a Hub with no database: threads do not survive a restart and
// are not shared between replicas.
package threadmemory

import (
	"context"
	"sort"
	"sync"

	"github.com/jonathanagustin/conduit/internal/assistant/domain"
)

type key struct{ owner, id string }

// Store is safe for concurrent use.
type Store struct {
	mu      sync.Mutex
	threads map[key]domain.Thread
}

// New returns an empty store.
func New() *Store { return &Store{threads: map[key]domain.Thread{}} }

func (store *Store) Create(_ context.Context, thread domain.Thread) error {
	if !domain.ValidOwner(thread.Owner) || !domain.ValidThreadID(thread.ID) || len(thread.Title) > domain.MaxThreadTitleBytes {
		return domain.ErrThreadInvalid
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	owned := 0
	for existing := range store.threads {
		if existing.owner == thread.Owner {
			owned++
		}
	}
	if owned >= domain.MaxThreadsPerOwner {
		return domain.ErrThreadLimit
	}
	at := key{thread.Owner, thread.ID}
	if _, exists := store.threads[at]; exists {
		return domain.ErrThreadInvalid
	}
	thread.Messages = nil
	thread.MessageCount = 0
	store.threads[at] = thread
	return nil
}

func (store *Store) List(_ context.Context, owner string) ([]domain.Thread, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	out := []domain.Thread{}
	for at, thread := range store.threads {
		if at.owner != owner {
			continue
		}
		thread.Messages = nil
		out = append(out, thread)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (store *Store) Get(_ context.Context, owner, id string) (domain.Thread, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	thread, ok := store.threads[key{owner, id}]
	if !ok {
		return domain.Thread{}, domain.ErrThreadNotFound
	}
	thread.Messages = append([]domain.Message{}, thread.Messages...)
	for i := range thread.Messages {
		thread.Messages[i] = thread.Messages[i].Clone()
	}
	return thread, nil
}

func (store *Store) Append(_ context.Context, owner, id string, messages []domain.Message, title string) error {
	for _, message := range messages {
		if err := message.Validate(); err != nil {
			return err
		}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	at := key{owner, id}
	thread, ok := store.threads[at]
	if !ok {
		return domain.ErrThreadNotFound
	}
	if len(messages) == 0 {
		return nil
	}
	if len(thread.Messages)+len(messages) > domain.MaxMessagesPerThread {
		return domain.ErrThreadFull
	}
	thread.Messages = append(append([]domain.Message{}, thread.Messages...), messages...)
	for i := range thread.Messages {
		thread.Messages[i] = thread.Messages[i].Clone()
	}
	thread.MessageCount = len(thread.Messages)
	thread.UpdatedAt = messages[len(messages)-1].CreatedAt
	if thread.Title == "" {
		thread.Title = domain.ThreadTitle(title)
	}
	store.threads[at] = thread
	return nil
}

func (store *Store) Delete(_ context.Context, owner, id string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	at := key{owner, id}
	if _, ok := store.threads[at]; !ok {
		return domain.ErrThreadNotFound
	}
	delete(store.threads, at)
	return nil
}

// Fork publishes a complete independent snapshot under the same owner lock.
func (store *Store) Fork(_ context.Context, owner, sourceID string, target domain.Thread) (domain.Thread, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	source, ok := store.threads[key{owner, sourceID}]
	if !ok {
		return domain.Thread{}, domain.ErrThreadNotFound
	}
	fork, err := domain.ForkSnapshot(source, target)
	if err != nil {
		return domain.Thread{}, err
	}
	owned := 0
	for at := range store.threads {
		if at.owner == owner {
			owned++
		}
	}
	if owned >= domain.MaxThreadsPerOwner {
		return domain.Thread{}, domain.ErrThreadLimit
	}
	at := key{owner, fork.ID}
	if _, exists := store.threads[at]; exists {
		return domain.Thread{}, domain.ErrThreadInvalid
	}
	store.threads[at] = fork
	fork.Messages = append([]domain.Message{}, fork.Messages...)
	for i := range fork.Messages {
		fork.Messages[i] = fork.Messages[i].Clone()
	}
	return fork, nil
}

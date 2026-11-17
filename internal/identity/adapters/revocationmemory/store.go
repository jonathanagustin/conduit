// Package revocationmemory keeps the Agent deny-list in this process.
//
// It is what a Hub on the file repository uses, and that Hub is single-replica
// by declaration. Two replicas each holding one of these would disagree about
// who is revoked, which is why cmd/platform-delivery opens the PostgreSQL
// adapter whenever there is a PostgreSQL to open. Nothing here survives a
// restart: a revocation added to a file-repository Hub must be added again
// after one.
package revocationmemory

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
)

type Store struct {
	mutex       sync.Mutex
	revocations map[string]map[string]domain.AgentRevocation
	sightings   map[string]map[string]domain.AgentCredentialSighting
}

func New() *Store {
	return &Store{
		revocations: map[string]map[string]domain.AgentRevocation{},
		sightings:   map[string]map[string]domain.AgentCredentialSighting{},
	}
}

func (store *Store) Revocations(_ context.Context, environment string) ([]domain.AgentRevocation, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	out := make([]domain.AgentRevocation, 0, len(store.revocations[environment]))
	for _, revocation := range store.revocations[environment] {
		out = append(out, revocation)
	}
	sort.Slice(out, func(left, right int) bool { return out[left].ID < out[right].ID })
	return out, nil
}

func (store *Store) PutRevocation(_ context.Context, revocation domain.AgentRevocation) (*domain.AgentRevocation, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	existing := store.revocations[revocation.Environment]
	if previous, found := existing[revocation.ID]; found {
		existing[revocation.ID] = revocation
		return &previous, nil
	}
	if len(existing) >= domain.MaximumRevocationsPerEnvironment {
		return nil, domain.ErrRevocationLimit
	}
	if existing == nil {
		existing = map[string]domain.AgentRevocation{}
		store.revocations[revocation.Environment] = existing
	}
	existing[revocation.ID] = revocation
	return nil, nil
}

func (store *Store) DeleteRevocation(_ context.Context, environment, id string) (domain.AgentRevocation, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	revocation, found := store.revocations[environment][id]
	if !found {
		return domain.AgentRevocation{}, domain.ErrRevocationNotFound
	}
	delete(store.revocations[environment], id)
	return revocation, nil
}

// DeleteRevocationAudited holds the deny-list lock until the audit succeeds.
// Readers cannot observe an unaudited removal, and an audit failure leaves the
// original record untouched. After a successful audit the map deletion cannot
// fail, including when the requesting client has disconnected in the meantime.
func (store *Store) DeleteRevocationAudited(
	ctx context.Context, environment, id string, audit func(context.Context, domain.AgentRevocation) error,
) (domain.AgentRevocation, error) {
	if audit == nil {
		return domain.AgentRevocation{}, errors.New("revocation removal requires an audit sink")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return domain.AgentRevocation{}, err
	}
	revocation, found := store.revocations[environment][id]
	if !found {
		return domain.AgentRevocation{}, domain.ErrRevocationNotFound
	}
	if err := audit(ctx, revocation); err != nil {
		return domain.AgentRevocation{}, err
	}
	delete(store.revocations[environment], id)
	return revocation, nil
}

func (store *Store) RecordSighting(_ context.Context, sighting domain.AgentCredentialSighting) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.sightings[sighting.Environment] == nil {
		store.sightings[sighting.Environment] = map[string]domain.AgentCredentialSighting{}
	}
	if held, found := store.sightings[sighting.Environment][sighting.AgentID]; found && held.SeenAt.After(sighting.SeenAt) {
		return nil
	}
	store.sightings[sighting.Environment][sighting.AgentID] = sighting
	return nil
}

func (store *Store) Sightings(_ context.Context, environment string) ([]domain.AgentCredentialSighting, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	out := make([]domain.AgentCredentialSighting, 0, len(store.sightings[environment]))
	for _, sighting := range store.sightings[environment] {
		out = append(out, sighting)
	}
	sort.Slice(out, func(left, right int) bool { return out[left].AgentID < out[right].AgentID })
	return out, nil
}

func (store *Store) EarliestExpiry(_ context.Context, seenSince time.Time) (time.Time, int, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	var earliest time.Time
	agents := 0
	for _, environment := range store.sightings {
		for _, sighting := range environment {
			if sighting.SeenAt.Before(seenSince) {
				continue
			}
			if agents == 0 || sighting.NotAfter.Before(earliest) {
				earliest = sighting.NotAfter
			}
			agents++
		}
	}
	return earliest, agents, nil
}

// Package conduitconverge realises Gitless GitOps without Flux.
//
// Conduit fetches the admitted artifact itself, checks that the bytes it got
// are the bytes that were admitted, and records the result as the environment's
// desired state. Everything after that is Conduit's existing machinery: the
// desired state is compiled into a governed Plan, approved under Governance's
// rules, and converged by the durable reconciler and the Agent.
//
// That last sentence is the reason this adapter exists at all. Delegating to
// Flux converges a cluster on bytes; converging here converges a cluster on
// bytes *through Conduit's approval and Plan semantics*, so a source change is
// a change somebody can be asked to approve rather than something a controller
// has already done. ADR 0003 makes that the whole point of Conduit keeping its
// own reconciler.
//
// The digest is checked again after fetching. Admission proved a publisher
// signed a coordinate; it did not prove the registry then served those bytes.
// Between the two lies a compromised or merely broken mirror, and re-hashing
// what arrived is cheap enough that not doing it would be a choice.
package conduitconverge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

// ArtifactFetcher returns the bytes at an exact coordinate. It is this
// adapter's own need: the Flux converger never fetches anything, because the
// cluster does.
type ArtifactFetcher interface {
	Fetch(ctx context.Context, pin domain.Pin) ([]byte, error)
}

// DesiredStateStore is Conduit's own delivery side, narrowed to the single
// thing this adapter needs of it.
//
// It is deliberately not the reconciliation package's store. This adapter has
// no business knowing about leases, outboxes or Plan receipts; it has a payload
// and a generation, and handing those over is the whole contract.
type DesiredStateStore interface {
	// PutDesiredState records what an environment should be running. The
	// generation is the admitted digest, which makes desired state
	// content-addressed end to end: the same bytes cannot arrive under two
	// generations, and two different bytes cannot share one.
	PutDesiredState(ctx context.Context, environment, generation string, payload []byte) error

	// CurrentGeneration reports the generation an environment is on, or empty.
	CurrentGeneration(ctx context.Context, environment string) (string, error)
}

// Converger is the ports.Converger backed by Conduit's own delivery.
type Converger struct {
	fetcher ArtifactFetcher
	store   DesiredStateStore
	// maximumBytes bounds one artifact. A desired-state document is small, and
	// an unbounded read from a registry is a way for a compromised mirror to
	// exhaust the Hub rather than to lie to it.
	maximumBytes int
}

// DefaultMaximumBytes is a generous bound for a desired-state document while
// still being far below anything that would trouble a Hub.
const DefaultMaximumBytes = 8 << 20

func New(fetcher ArtifactFetcher, store DesiredStateStore, maximumBytes int) (*Converger, error) {
	if fetcher == nil || store == nil {
		return nil, errors.New("Conduit convergence requires an artifact fetcher and a desired-state store")
	}
	if maximumBytes <= 0 {
		maximumBytes = DefaultMaximumBytes
	}
	return &Converger{fetcher: fetcher, store: store, maximumBytes: maximumBytes}, nil
}

func (converger *Converger) Converge(ctx context.Context, environment string, admission domain.Admission) error {
	if strings.TrimSpace(environment) == "" {
		return errors.New("Conduit convergence needs a named environment")
	}
	pin := admission.Pin()
	if err := pin.Validate(); err != nil {
		return fmt.Errorf("refusing to fetch an invalid pin: %w", err)
	}

	payload, err := converger.fetcher.Fetch(ctx, pin)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", pin, err)
	}
	if len(payload) == 0 {
		return fmt.Errorf("%s returned no bytes; an empty desired state is not a desired state", pin)
	}
	if len(payload) > converger.maximumBytes {
		return fmt.Errorf("%s returned %d bytes, over the %d byte bound", pin, len(payload), converger.maximumBytes)
	}

	// The bytes that arrived must be the bytes that were admitted. Admission
	// established that a trusted publisher signed this digest; only this
	// establishes that the registry then served it.
	sum := sha256.Sum256(payload)
	served := "sha256:" + hex.EncodeToString(sum[:])
	if served != pin.Digest {
		return fmt.Errorf(
			"%s served %s: the bytes delivered are not the bytes admitted, so the signature covers something else",
			pin, served,
		)
	}

	// The generation is the digest. Nothing else is: a counter could repeat
	// across environments and a timestamp could go backwards, and either would
	// let two different desired states look like one.
	return converger.store.PutDesiredState(ctx, environment, pin.Digest, payload)
}

func (converger *Converger) Deployed(ctx context.Context, environment string) (string, error) {
	return converger.store.CurrentGeneration(ctx, environment)
}

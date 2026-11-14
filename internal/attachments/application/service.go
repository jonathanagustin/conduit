// Package application stores intake attachments and resolves them as bound,
// read-only context. It does not approve a change and does not publish a release.
package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jonathanagustin/conduit/internal/attachments/domain"
)

// Store keeps attachments by digest. Put is idempotent for the same bytes.
type Store interface {
	Put(ctx context.Context, item domain.Stored) (created bool, err error)
	Get(ctx context.Context, digest string) (domain.Stored, bool, error)
	List(ctx context.Context) ([]domain.Attachment, error)
}

// Audit is one record for the audit chain.
type Audit struct {
	Action  string
	Name    string
	Kind    string
	Size    int
	SHA256  string
	Outcome string
	Reason  string
}

const (
	OutcomeStored   = "stored"
	OutcomeRejected = "rejected"
)

// Auditor writes one audit record.
type Auditor interface {
	Record(ctx context.Context, event Audit) error
}

// Service validates, stores, and resolves attachments.
type Service struct {
	Store Store
	Audit Auditor
}

// Add stores one file when it passes the type, size, and secret checks.
// A rejected file is not stored and the reason is returned as a domain.Rejection.
func (service Service) Add(ctx context.Context, name string, content []byte) (domain.Attachment, error) {
	if service.Store == nil {
		return domain.Attachment{}, errors.New("attachment store is not configured")
	}
	item, err := domain.Inspect(name, content)
	if err != nil {
		service.record(ctx, Audit{Action: "intake.attach", Name: strings.TrimSpace(name), Outcome: OutcomeRejected, Reason: err.Error()})
		return domain.Attachment{}, err
	}
	existing, err := service.Store.List(ctx)
	if err != nil {
		return domain.Attachment{}, err
	}
	total, known := len(content), false
	for _, row := range existing {
		if row.SHA256 == item.SHA256 {
			known = true
		}
		total += row.Size
	}
	if !known {
		if len(existing) >= domain.MaxCount {
			return rejected(ctx, service, item, true, fmt.Sprintf("at most %d attachments are kept", domain.MaxCount))
		}
		if total > domain.MaxTotalBytes {
			return rejected(ctx, service, item, true, fmt.Sprintf("attachments would total %d bytes and the limit is %d", total, domain.MaxTotalBytes))
		}
	}
	created, err := service.Store.Put(ctx, item)
	if err != nil {
		return domain.Attachment{}, err
	}
	if created {
		if err := service.recordStrict(ctx, Audit{
			Action: "intake.attach", Name: item.Name, Kind: item.Kind, Size: item.Size, SHA256: item.SHA256,
			Outcome: OutcomeStored, Reason: "stored by digest",
		}); err != nil {
			return domain.Attachment{}, err
		}
	}
	return item.Attachment, nil
}

func rejected(ctx context.Context, service Service, item domain.Stored, large bool, reason string) (domain.Attachment, error) {
	err := domain.Rejection{Reason: reason, Large: large}
	service.record(ctx, Audit{Action: "intake.attach", Name: item.Name, Outcome: OutcomeRejected, Reason: reason})
	return domain.Attachment{}, err
}

// Get returns one stored attachment by digest.
func (service Service) Get(ctx context.Context, digest string) (domain.Stored, error) {
	if service.Store == nil || !domain.ValidDigest(digest) {
		return domain.Stored{}, domain.ErrNotFound
	}
	item, ok, err := service.Store.Get(ctx, digest)
	if err != nil {
		return domain.Stored{}, err
	}
	if !ok {
		return domain.Stored{}, domain.ErrNotFound
	}
	return item, nil
}

// List returns stored attachment metadata in the order it was stored.
func (service Service) List(ctx context.Context) ([]domain.Attachment, error) {
	if service.Store == nil {
		return []domain.Attachment{}, nil
	}
	rows, err := service.Store.List(ctx)
	if rows == nil {
		rows = []domain.Attachment{}
	}
	return rows, err
}

// Resolve loads the bytes for each digest as bound, read-only context. Duplicate
// digests are dropped. A malformed or unknown digest is a rejection, so a request
// can never name context that was not stored and scanned.
func (service Service) Resolve(ctx context.Context, digests []string) ([]domain.Stored, error) {
	seen := map[string]bool{}
	out := make([]domain.Stored, 0, len(digests))
	for _, digest := range digests {
		digest = strings.TrimSpace(digest)
		if !domain.ValidDigest(digest) {
			return nil, domain.Rejection{Reason: "attachment digest must be 64 lowercase hex characters"}
		}
		if seen[digest] {
			continue
		}
		seen[digest] = true
		item, err := service.Get(ctx, digest)
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.Rejection{Reason: "attachment " + digest + " was not found"}
		}
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if len(out) > domain.MaxCount {
		return nil, domain.Rejection{Reason: fmt.Sprintf("at most %d attachments may be bound", domain.MaxCount), Large: true}
	}
	return out, nil
}

func (service Service) record(ctx context.Context, event Audit) { _ = service.recordStrict(ctx, event) }

func (service Service) recordStrict(ctx context.Context, event Audit) error {
	if service.Audit == nil {
		return nil
	}
	return service.Audit.Record(ctx, event)
}

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
	Actor   string
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
	OutcomeDenied   = "denied"
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

// Add stores one file for actor when it passes the type, size, and secret checks.
// A rejected file is not stored and the reason is returned as a domain.Rejection.
// The same bytes stored again by the same operator are not audited twice.
// Another operator storing those bytes becomes an owner of the existing digest.
func (service Service) Add(ctx context.Context, actor, name string, content []byte) (domain.Attachment, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return domain.Attachment{}, errors.New("attachment owner is required")
	}
	if service.Store == nil {
		return domain.Attachment{}, errors.New("attachment store is not configured")
	}
	item, err := domain.Inspect(name, content)
	if err != nil {
		service.record(ctx, Audit{Action: "intake.attach", Actor: actor, Name: strings.TrimSpace(name), Outcome: OutcomeRejected, Reason: err.Error()})
		return domain.Attachment{}, err
	}
	item.Owners = []string{actor}
	existing, err := service.List(ctx, actor)
	if err != nil {
		return domain.Attachment{}, err
	}
	prior, found, err := service.Store.Get(ctx, item.SHA256)
	if err != nil {
		return domain.Attachment{}, err
	}
	already := found && owns(actor, prior.Owners)
	total, known := len(content), already
	for _, row := range existing {
		if row.SHA256 == item.SHA256 {
			known = true
		}
		total += row.Size
	}
	if !known {
		if len(existing) >= domain.MaxCount {
			return rejected(ctx, service, actor, item, true, fmt.Sprintf("at most %d attachments are kept", domain.MaxCount))
		}
		if total > domain.MaxTotalBytes {
			return rejected(ctx, service, actor, item, true, fmt.Sprintf("attachments would total %d bytes and the limit is %d", total, domain.MaxTotalBytes))
		}
	}
	if _, err := service.Store.Put(ctx, item); err != nil {
		return domain.Attachment{}, err
	}
	if !already {
		if err := service.recordStrict(ctx, Audit{
			Action: "intake.attach", Actor: actor, Name: item.Name, Kind: item.Kind, Size: item.Size, SHA256: item.SHA256,
			Outcome: OutcomeStored, Reason: "stored by digest",
		}); err != nil {
			return domain.Attachment{}, err
		}
	}
	return item.Attachment, nil
}

func rejected(ctx context.Context, service Service, actor string, item domain.Stored, large bool, reason string) (domain.Attachment, error) {
	err := domain.Rejection{Reason: reason, Large: large}
	service.record(ctx, Audit{Action: "intake.attach", Actor: actor, Name: item.Name, Outcome: OutcomeRejected, Reason: reason})
	return domain.Attachment{}, err
}

// Get returns one attachment actor stored. A missing digest and a digest
// stored by someone else are both not found, so a probe cannot tell them apart.
func (service Service) Get(ctx context.Context, actor, digest string) (domain.Stored, error) {
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
	if !owns(actor, item.Owners) {
		if err := service.recordStrict(ctx, Audit{
			Action: "intake.read", Actor: strings.TrimSpace(actor), SHA256: digest,
			Outcome: OutcomeDenied, Reason: "the attachment is not visible to this operator",
		}); err != nil {
			return domain.Stored{}, err
		}
		return domain.Stored{}, domain.ErrNotFound
	}
	return item, nil
}

// List returns the caller's attachment metadata in the order it was stored.
// A row with no owner is omitted.
func (service Service) List(ctx context.Context, actor string) ([]domain.Attachment, error) {
	if service.Store == nil {
		return []domain.Attachment{}, nil
	}
	rows, err := service.Store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Attachment, 0, len(rows))
	for _, row := range rows {
		if owns(actor, row.Owners) {
			out = append(out, row)
		}
	}
	return out, nil
}

func owns(actor string, owners []string) bool {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return false
	}
	for _, owner := range owners {
		if strings.TrimSpace(owner) == actor {
			return true
		}
	}
	return false
}

// Resolve loads the bytes for each digest as bound, read-only context. Duplicate
// digests are dropped. A malformed or unknown digest is a rejection, so a request
// can never name context that was not stored and scanned.
func (service Service) Resolve(ctx context.Context, actor string, digests []string) ([]domain.Stored, error) {
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
		item, err := service.Get(ctx, actor, digest)
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

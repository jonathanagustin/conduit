// Package domain binds native installation override intent to reviewed facts.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	Native                = "CONDUIT_NATIVE"
	Recall                = "RECALL"
	SecretRequirement     = "SECRET_REQUIREMENT"
	OtherConstraint       = "OTHER_CONSTRAINT"
	MaximumReviewLifetime = time.Hour
	MaximumPermitLifetime = 30 * time.Second
	MaximumBlockers       = 64
	MaximumDocumentBytes  = 64 << 10
)

var (
	ErrInvalid      = errors.New("invalid installation override review")
	ErrBlocked      = errors.New("installation override is not currently authorized")
	ErrCorrupt      = errors.New("installation override authority returned inconsistent facts")
	proposalPattern = regexp.MustCompile(`^iov_[a-f0-9]{64}$`)
	digestPattern   = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	idPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$`)
)

// Target is an exact operation, including the state revision it would change.
type Target struct {
	Kind                string `json:"kind"`
	Action              string `json:"action"`
	Environment         string `json:"environment"`
	InstallationID      string `json:"installationId"`
	Resource            string `json:"resource"`
	Version             string `json:"version"`
	BaselineRevision    uint64 `json:"baselineRevision"`
	ReleaseDigest       string `json:"releaseDigest"`
	ConfigurationDigest string `json:"configurationDigest"`
	StateDigest         string `json:"stateDigest"`
}

// Selector identifies server-owned facts; clients do not supply their digests.
type Selector struct {
	Kind           string `json:"kind"`
	Action         string `json:"action"`
	Environment    string `json:"environment"`
	InstallationID string `json:"installationId"`
	Resource       string `json:"resource"`
	Version        string `json:"version"`
}

func (s Selector) Validate() error {
	if (s.Kind != "ENTITY" && s.Kind != "MODULE") || (s.Action != "INSTALL" && s.Action != "UPDATE") || strings.EqualFold(s.Version, "latest") {
		return ErrInvalid
	}
	for _, value := range []string{s.Environment, s.InstallationID, s.Resource, s.Version} {
		if !ValidID(value) {
			return ErrInvalid
		}
	}
	return nil
}

func (t Target) Selector() Selector {
	return Selector{t.Kind, t.Action, t.Environment, t.InstallationID, t.Resource, t.Version}
}

func (t Target) Validate() error {
	if t.Selector().Validate() != nil || (t.Action == "INSTALL" && t.BaselineRevision != 0) || (t.Action == "UPDATE" && t.BaselineRevision == 0) {
		return ErrInvalid
	}
	for _, digest := range []string{t.ReleaseDigest, t.ConfigurationDigest, t.StateDigest} {
		if !digestPattern.MatchString(digest) {
			return ErrInvalid
		}
	}
	return nil
}

func ValidID(id string) bool { return idPattern.MatchString(id) }

// Identity retains both the original actor and the effective identity.
type Identity struct {
	Actor     string `json:"actor"`
	Effective string `json:"effective"`
}

func (i Identity) Validate() error {
	for _, value := range []string{i.Actor, i.Effective} {
		if !validText(value, 512) || value != strings.TrimSpace(value) || strings.ContainsFunc(value, unicode.IsControl) {
			return ErrInvalid
		}
	}
	return nil
}

// Flags describe requested exceptions, never executable authority by themselves.
type Flags struct {
	IgnoreRecalls            bool `json:"ignoreRecalls"`
	IgnoreSecretRequirements bool `json:"ignoreSecretRequirements"`
}

func (f Flags) includes(kind string) bool {
	return kind == Recall && f.IgnoreRecalls || kind == SecretRequirement && f.IgnoreSecretRequirements
}

// Blocker identifies one exact currently observed constraint fingerprint.
type Blocker struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	Digest      string `json:"digest"`
	Overridable bool   `json:"overridable"`
}

func (b Blocker) key() string { return b.Kind + ":" + b.ID }

// Snapshot is supplied by an authority adapter, not by a proposal's transport.
type Snapshot struct {
	Target                       Target    `json:"target"`
	AuthorityDigest              string    `json:"authorityDigest"`
	Blockers                     []Blocker `json:"blockers"`
	SecretRequirementsIntentOnly bool      `json:"secretRequirementsIntentOnly,omitempty"`
}

func (s Snapshot) Validate() error {
	if s.Target.Validate() != nil || !digestPattern.MatchString(s.AuthorityDigest) || len(s.Blockers) > MaximumBlockers || (s.SecretRequirementsIntentOnly && s.Target.Kind != "MODULE") {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, b := range s.Blockers {
		if (b.Kind != Recall && b.Kind != SecretRequirement && b.Kind != OtherConstraint) || !ValidID(b.ID) || !digestPattern.MatchString(b.Digest) || seen[b.key()] || (b.Kind == OtherConstraint && b.Overridable) || (s.SecretRequirementsIntentOnly && b.Kind == SecretRequirement) {
			return ErrInvalid
		}
		seen[b.key()] = true
	}
	return nil
}

// Proposal seals intent and observed facts; it carries no execution authority.
// CONDUIT-INVENTED: installoverride.ReviewBinding
// Displaced-by: a published or sanitized installation override contract establishing review scope, blocker identity, expiry, independent approval and last-safe-point authority.
type Proposal struct {
	ID        string    `json:"id"`
	ValueTier string    `json:"valueTier"`
	Author    Identity  `json:"author"`
	Flags     Flags     `json:"flags"`
	Snapshot  Snapshot  `json:"snapshot"`
	Rationale string    `json:"rationale"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func NewProposal(snapshot Snapshot, flags Flags, author Identity, rationale string, createdAt, expiresAt time.Time) (Proposal, error) {
	snapshot.Blockers = slices.Clone(snapshot.Blockers)
	slices.SortFunc(snapshot.Blockers, func(a, b Blocker) int { return strings.Compare(a.key(), b.key()) })
	p := Proposal{ValueTier: Native, Author: author, Flags: flags, Snapshot: snapshot, Rationale: strings.TrimSpace(rationale), CreatedAt: createdAt.UTC(), ExpiresAt: expiresAt.UTC()}
	p.ID = p.identity()
	if err := p.Validate(); err != nil {
		return Proposal{}, err
	}
	return p, nil
}

func (p Proposal) identity() string {
	p.ID = ""
	encoded, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return "iov_" + hex.EncodeToString(digest[:])
}

func (p Proposal) Validate() error {
	if p.ValueTier != Native || p.Author.Validate() != nil || p.Snapshot.Validate() != nil || !validText(p.Rationale, 16384) || p.Rationale != strings.TrimSpace(p.Rationale) || p.CreatedAt.IsZero() || !p.ExpiresAt.After(p.CreatedAt) || p.ExpiresAt.Sub(p.CreatedAt) > MaximumReviewLifetime || !proposalPattern.MatchString(p.ID) || p.ID != p.identity() {
		return ErrInvalid
	}
	if !slices.IsSortedFunc(p.Snapshot.Blockers, func(a, b Blocker) int { return strings.Compare(a.key(), b.key()) }) {
		return ErrInvalid
	}
	encoded, err := json.Marshal(p)
	if err != nil || len(encoded) > MaximumDocumentBytes {
		return ErrInvalid
	}
	counts := map[string]int{}
	for _, b := range p.Snapshot.Blockers {
		if p.Flags.includes(b.Kind) && b.Overridable {
			counts[b.Kind]++
		}
	}
	if (!p.Flags.IgnoreRecalls && !p.Flags.IgnoreSecretRequirements) || (p.Flags.IgnoreRecalls && counts[Recall] == 0) || (p.Flags.IgnoreSecretRequirements && counts[SecretRequirement] == 0 && !p.Snapshot.SecretRequirementsIntentOnly) {
		return ErrInvalid
	}
	return nil
}

func validText(value string, maximum int) bool {
	return len(value) <= maximum && utf8.ValidString(value) && strings.TrimSpace(value) != "" && !strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' })
}

func CopyProposal(p Proposal) Proposal {
	p.Snapshot.Blockers = slices.Clone(p.Snapshot.Blockers)
	return p
}

// Approval contains fresh server-owned review facts, not a caller assertion.
type Approval struct {
	ReviewID              string
	ProposalID            string
	Revision              uint64
	Status                string
	RequirementsSatisfied bool
	DoNotMerge            bool
	Reviewer              Identity
	ReviewedAt            time.Time
	AuthorityDigest       string
}

// Permit is short-lived and cannot be constructed by transport field binding.
type Permit struct {
	reviewID       string
	reviewRevision uint64
	target         Target
	issuedAt       time.Time
	expiresAt      time.Time
	blockers       map[string]string
}

func (p Permit) ReviewID() string       { return p.reviewID }
func (p Permit) ReviewRevision() uint64 { return p.reviewRevision }
func (p Permit) ExpiresAt() time.Time   { return p.expiresAt }
func (p Permit) ValidAt(now time.Time) bool {
	return p.reviewID != "" && !now.Before(p.issuedAt) && now.Before(p.expiresAt)
}

func (p Permit) Allows(now time.Time, target Target, blocker Blocker) bool {
	digest, exists := p.blockers[blocker.key()]
	return exists && p.ValidAt(now) && target == p.target && blocker.Overridable && digest == blocker.Digest
}

// Revalidate checks current facts; callers must obtain them from trusted ports
// in the same serialization boundary as the guarded effect.
func Revalidate(p Proposal, a Approval, current Snapshot, now time.Time) (Permit, error) {
	if p.Validate() != nil || current.Validate() != nil {
		return Permit{}, ErrInvalid
	}
	if !ValidID(a.ReviewID) || a.ProposalID != p.ID || a.Revision == 0 || a.Status != "APPROVED" || !a.RequirementsSatisfied || a.DoNotMerge || a.Reviewer.Validate() != nil || a.ReviewedAt.Before(p.CreatedAt) || a.ReviewedAt.After(now) || now.Before(p.CreatedAt) || !now.Before(p.ExpiresAt) {
		return Permit{}, ErrBlocked
	}
	for _, reviewer := range []string{a.Reviewer.Actor, a.Reviewer.Effective} {
		if reviewer == p.Author.Actor || reviewer == p.Author.Effective {
			return Permit{}, ErrBlocked
		}
	}
	if current.SecretRequirementsIntentOnly != p.Snapshot.SecretRequirementsIntentOnly || current.Target != p.Snapshot.Target || current.AuthorityDigest != p.Snapshot.AuthorityDigest || a.AuthorityDigest != current.AuthorityDigest {
		return Permit{}, ErrBlocked
	}
	reviewed := map[string]Blocker{}
	for _, b := range p.Snapshot.Blockers {
		reviewed[b.key()] = b
	}
	allowed := map[string]string{}
	for _, b := range current.Blockers {
		previous, exists := reviewed[b.key()]
		if !exists || previous != b || !b.Overridable || !p.Flags.includes(b.Kind) {
			return Permit{}, ErrBlocked
		}
		allowed[b.key()] = b.Digest
	}
	expires := now.Add(MaximumPermitLifetime)
	if p.ExpiresAt.Before(expires) {
		expires = p.ExpiresAt
	}
	return Permit{a.ReviewID, a.Revision, current.Target, now, expires, allowed}, nil
}

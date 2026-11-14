package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/installoverride/domain"
	"github.com/jonathanagustin/conduit/internal/installoverride/ports"
)

type authorityFixture struct {
	snapshot                                 domain.Snapshot
	identity                                 domain.Identity
	proposal                                 domain.Proposal
	approval                                 domain.Approval
	resolveErr, createErr, approvedErr       error
	ack                                      ports.ReviewAck
	resolveCount, createCount, approvedCount int
	mode                                     string
}

func (a *authorityFixture) ResolveProposal(context.Context, domain.Selector) (domain.Snapshot, domain.Identity, error) {
	a.resolveCount++
	return a.snapshot, a.identity, a.resolveErr
}
func (a *authorityFixture) CreateReview(_ context.Context, p domain.Proposal) (ports.ReviewAck, error) {
	a.createCount++
	a.proposal = p
	if a.createErr != nil {
		return ports.ReviewAck{}, a.createErr
	}
	if a.ack.ProposalID == "" {
		a.ack.ProposalID = p.ID
	}
	return a.ack, nil
}
func (a *authorityFixture) WithApproved(_ context.Context, _ string, _ domain.Selector, callback func(domain.Proposal, domain.Approval, domain.Snapshot) error) error {
	a.approvedCount++
	if a.approvedErr != nil {
		return a.approvedErr
	}
	if a.mode == "no-callback" {
		return nil
	}
	err := callback(a.proposal, a.approval, a.snapshot)
	if a.mode == "ignore-error" {
		return nil
	}
	if a.mode == "duplicate" {
		return callback(a.proposal, a.approval, a.snapshot)
	}
	return err
}

func applicationFixture(t *testing.T) (*Service, *authorityFixture, Intent, time.Time) {
	t.Helper()
	digest := func(letter string) string { return "sha256:" + strings.Repeat(letter, 64) }
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	a := &authorityFixture{snapshot: domain.Snapshot{Target: domain.Target{Kind: "MODULE", Action: "UPDATE", Environment: "prod", InstallationID: "module-install", Resource: "example:foundation", Version: "1.2.3", BaselineRevision: 1, ReleaseDigest: digest("a"), ConfigurationDigest: digest("b"), StateDigest: digest("c")}, AuthorityDigest: digest("d"), Blockers: []domain.Blocker{{Kind: domain.Recall, ID: "recalled-module", Digest: digest("e"), Overridable: true}}}, identity: domain.Identity{Actor: "publisher-service", Effective: "author"}, ack: ports.ReviewAck{ReviewID: "crq_override", Status: "PENDING_APPROVALS"}}
	s, err := New(a)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return now }
	intent := Intent{Target: a.snapshot.Target.Selector(), Flags: domain.Flags{IgnoreRecalls: true}, Rationale: "CAB-123", ReviewLifetime: time.Hour}
	return s, a, intent, now
}

func prepareReview(t *testing.T, s *Service, a *authorityFixture, intent Intent, now time.Time) ProposedReview {
	t.Helper()
	result, err := s.Propose(t.Context(), intent)
	if err != nil {
		t.Fatal(err)
	}
	a.approval = domain.Approval{ReviewID: result.ReviewID, ProposalID: result.Proposal.ID, Revision: 1, Status: "APPROVED", RequirementsSatisfied: true, Reviewer: domain.Identity{Actor: "review-service", Effective: "reviewer"}, ReviewedAt: now, AuthorityDigest: a.snapshot.AuthorityDigest}
	return result
}

func TestProposalUsesAuthenticatedFactsAndNeverReturnsApproval(t *testing.T) {
	s, a, intent, now := applicationFixture(t)
	result := prepareReview(t, s, a, intent, now)
	if result.Status != "PENDING_APPROVALS" || result.Proposal.Author != a.identity || result.Proposal.Snapshot.Target != a.snapshot.Target || a.resolveCount != 1 || a.createCount != 1 {
		t.Fatal("proposal skipped authenticated authority", result)
	}
	a.proposal.Snapshot.Blockers[0].Digest = "changed"
	if result.Proposal.Validate() != nil {
		t.Fatal("authority mutated returned review")
	}
	for _, mutate := range []func(*authorityFixture, *Intent){
		func(a *authorityFixture, _ *Intent) { a.snapshot.Target.Environment = "other" },
		func(a *authorityFixture, _ *Intent) { a.identity.Actor = "" },
		func(_ *authorityFixture, i *Intent) { i.Flags = domain.Flags{} },
		func(_ *authorityFixture, i *Intent) { i.Rationale = "" },
		func(_ *authorityFixture, i *Intent) { i.ReviewLifetime = time.Hour + time.Nanosecond },
		func(_ *authorityFixture, i *Intent) { i.Target.Kind = "UNKNOWN" },
	} {
		s, a, i, _ := applicationFixture(t)
		mutate(a, &i)
		if _, err := s.Propose(t.Context(), i); err == nil || a.createCount != 0 {
			t.Fatal("invalid request created review", err)
		}
	}
	for _, ack := range []ports.ReviewAck{{ReviewID: "crq_auto", Status: "APPROVED"}, {ReviewID: "crq_foreign", ProposalID: "foreign", Status: "PENDING_APPROVALS"}, {Status: "PENDING_APPROVALS"}} {
		s, a, i, _ := applicationFixture(t)
		a.ack = ack
		if _, err := s.Propose(t.Context(), i); !errors.Is(err, domain.ErrCorrupt) {
			t.Fatal("unreviewed acknowledgment accepted", ack, err)
		}
	}
	failure := errors.New("review persistence unavailable")
	for _, phase := range []string{"resolve", "create"} {
		s, a, i, _ := applicationFixture(t)
		if phase == "resolve" {
			a.resolveErr = failure
		} else {
			a.createErr = failure
		}
		if _, err := s.Propose(t.Context(), i); !errors.Is(err, failure) {
			t.Fatal("authority failure hidden", err)
		}
	}
	if _, err := New(nil); err == nil {
		t.Fatal("authority-free service admitted")
	}
}

func TestOnlyFreshBoundIndependentReviewCanReachEffect(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Service, *authorityFixture, *Intent, time.Time)
	}{
		{"pending", func(_ *Service, a *authorityFixture, _ *Intent, _ time.Time) { a.approval.Status = "PENDING_APPROVALS" }},
		{"self-review", func(_ *Service, a *authorityFixture, _ *Intent, _ time.Time) {
			a.approval.Reviewer.Effective = a.identity.Effective
		}},
		{"changed-target", func(_ *Service, a *authorityFixture, _ *Intent, _ time.Time) {
			a.snapshot.Target.ConfigurationDigest = "sha256:" + strings.Repeat("f", 64)
		}},
		{"new-recall", func(_ *Service, a *authorityFixture, _ *Intent, _ time.Time) {
			a.snapshot.Blockers[0].ID = "new-recall"
		}},
		{"foreign-review", func(_ *Service, a *authorityFixture, _ *Intent, _ time.Time) { a.approval.ReviewID = "crq_foreign" }},
		{"foreign-selector", func(_ *Service, _ *authorityFixture, i *Intent, _ time.Time) { i.Target.Version = "1.2.4" }},
		{"expired", func(s *Service, a *authorityFixture, _ *Intent, _ time.Time) {
			s.now = func() time.Time { return a.proposal.ExpiresAt }
		}},
		{"missing-authority", func(_ *Service, a *authorityFixture, _ *Intent, _ time.Time) {
			a.approvedErr = errors.New("review not found")
		}},
		{"ignored-guard-error", func(_ *Service, a *authorityFixture, _ *Intent, _ time.Time) {
			a.approval.Status = "CANCELLED"
			a.mode = "ignore-error"
		}},
		{"no-callback", func(_ *Service, a *authorityFixture, _ *Intent, _ time.Time) { a.mode = "no-callback" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, a, i, n := applicationFixture(t)
			result := prepareReview(t, s, a, i, n)
			test.mutate(s, a, &i, n)
			effects := 0
			err := s.WithPermit(t.Context(), result.ReviewID, i.Target, func(domain.Permit) error { effects++; return nil })
			if err == nil || effects != 0 {
				t.Fatal("guarded effect ran", effects, err)
			}
		})
	}
	s, a, i, n := applicationFixture(t)
	result := prepareReview(t, s, a, i, n)
	effects := 0
	err := s.WithPermit(t.Context(), result.ReviewID, i.Target, func(permit domain.Permit) error {
		effects++
		if !permit.Allows(n, a.snapshot.Target, a.snapshot.Blockers[0]) {
			t.Fatal("missing exact waiver")
		}
		return nil
	})
	if err != nil || effects != 1 {
		t.Fatal("reviewed effect refused", effects, err)
	}
	failure := errors.New("durable write failed")
	a.mode = "ignore-error"
	if err := s.WithPermit(t.Context(), result.ReviewID, i.Target, func(domain.Permit) error { return failure }); !errors.Is(err, failure) {
		t.Fatal("effect failure hidden", err)
	}
	a.mode = "duplicate"
	effects = 0
	if err := s.WithPermit(t.Context(), result.ReviewID, i.Target, func(domain.Permit) error { effects++; return nil }); !errors.Is(err, domain.ErrCorrupt) || effects != 1 {
		t.Fatal("callback replay", effects, err)
	}
}

func TestCancellationAndClockAdvanceCannotReachEffect(t *testing.T) {
	s, a, i, n := applicationFixture(t)
	result := prepareReview(t, s, a, i, n)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Propose(ctx, i); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	effects := 0
	if err := s.WithPermit(ctx, result.ReviewID, i.Target, func(domain.Permit) error { effects++; return nil }); !errors.Is(err, context.Canceled) || effects != 0 {
		t.Fatal("cancelled effect", effects, err)
	}
	reads := 0
	s.now = func() time.Time {
		reads++
		if reads == 1 {
			return n
		}
		return n.Add(domain.MaximumPermitLifetime)
	}
	if err := s.WithPermit(t.Context(), result.ReviewID, i.Target, func(domain.Permit) error { effects++; return nil }); !errors.Is(err, domain.ErrBlocked) || effects != 0 {
		t.Fatal("expired admission permit reached effect", effects, err)
	}
	if err := s.WithPermit(t.Context(), result.ReviewID, i.Target, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("nil effect admitted", err)
	}
}

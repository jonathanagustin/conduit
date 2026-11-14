package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func testDigest(letter string) string { return "sha256:" + strings.Repeat(letter, 64) }

func reviewFixture(t *testing.T) (Proposal, Approval, Snapshot, time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	snapshot := Snapshot{Target: Target{Kind: "ENTITY", Action: "UPDATE", Environment: "prod", InstallationID: "api", Resource: "example:api", Version: "1.2.3", BaselineRevision: 7, ReleaseDigest: testDigest("a"), ConfigurationDigest: testDigest("b"), StateDigest: testDigest("c")}, AuthorityDigest: testDigest("d"), Blockers: []Blocker{{Kind: Recall, ID: "recall-1", Digest: testDigest("e"), Overridable: true}, {Kind: SecretRequirement, ID: "database", Digest: testDigest("f"), Overridable: true}}}
	p, err := NewProposal(snapshot, Flags{true, true}, Identity{"publisher-service", "author"}, "Incident CAB-123\nReviewed target", now.Add(-time.Minute), now.Add(time.Minute*20))
	if err != nil {
		t.Fatal(err)
	}
	a := Approval{ReviewID: "crq_review", ProposalID: p.ID, Revision: 2, Status: "APPROVED", RequirementsSatisfied: true, Reviewer: Identity{"review-service", "reviewer"}, ReviewedAt: now.Add(-time.Second * 30), AuthorityDigest: snapshot.AuthorityDigest}
	return p, a, snapshot, now
}

func TestPermitBindsExactTargetBlockersAndLifetime(t *testing.T) {
	p, a, current, now := reviewFixture(t)
	permit, err := Revalidate(p, a, current, now)
	if err != nil {
		t.Fatal(err)
	}
	if permit.ReviewID() != a.ReviewID || permit.ReviewRevision() != a.Revision || !permit.ExpiresAt().Equal(now.Add(MaximumPermitLifetime)) {
		t.Fatal("wrong binding", permit)
	}
	for _, b := range current.Blockers {
		if !permit.Allows(now, current.Target, b) {
			t.Fatal("reviewed blocker refused", b)
		}
	}
	if permit.ValidAt(now.Add(-time.Nanosecond)) || permit.ValidAt(permit.ExpiresAt()) || (Permit{}).ValidAt(now) {
		t.Fatal("permit escaped its time window")
	}
	for _, b := range []Blocker{{Kind: Recall, ID: "missing", Overridable: true}, {Kind: Recall, ID: "recall-1", Digest: testDigest("a"), Overridable: true}, {Kind: Recall, ID: "recall-1", Digest: testDigest("e"), Overridable: false}} {
		if permit.Allows(now, current.Target, b) {
			t.Fatal("unreviewed blocker allowed", b)
		}
	}
	other := current.Target
	other.InstallationID = "other"
	if permit.Allows(now, other, current.Blockers[0]) {
		t.Fatal("foreign target allowed")
	}
	encoded, err := json.Marshal(permit)
	if err != nil || string(encoded) != "{}" {
		t.Fatal("permit became transport authority", string(encoded), err)
	}
	current.Blockers = nil
	resolved, err := Revalidate(p, a, current, now)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Allows(now, current.Target, p.Snapshot.Blockers[0]) {
		t.Fatal("resolved blocker retained a waiver")
	}
}

func TestReviewCannotAuthorizeDriftOrOtherConstraints(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Proposal, *Approval, *Snapshot, *time.Time)
	}{
		{"environment", func(_ *Proposal, _ *Approval, s *Snapshot, _ *time.Time) { s.Target.Environment = "other" }},
		{"installation", func(_ *Proposal, _ *Approval, s *Snapshot, _ *time.Time) { s.Target.InstallationID = "other" }},
		{"resource", func(_ *Proposal, _ *Approval, s *Snapshot, _ *time.Time) { s.Target.Resource = "example:other" }},
		{"version", func(_ *Proposal, _ *Approval, s *Snapshot, _ *time.Time) { s.Target.Version = "1.2.4" }},
		{"release-digest", func(_ *Proposal, _ *Approval, s *Snapshot, _ *time.Time) { s.Target.ReleaseDigest = testDigest("b") }},
		{"configuration", func(_ *Proposal, _ *Approval, s *Snapshot, _ *time.Time) {
			s.Target.ConfigurationDigest = testDigest("c")
		}},
		{"state", func(_ *Proposal, _ *Approval, s *Snapshot, _ *time.Time) { s.Target.StateDigest = testDigest("d") }},
		{"baseline-replay", func(_ *Proposal, _ *Approval, s *Snapshot, _ *time.Time) { s.Target.BaselineRevision++ }},
		{"authority-drift", func(_ *Proposal, _ *Approval, s *Snapshot, _ *time.Time) { s.AuthorityDigest = testDigest("e") }},
		{"review-authority", func(_ *Proposal, a *Approval, _ *Snapshot, _ *time.Time) { a.AuthorityDigest = testDigest("e") }},
		{"new-recall", func(_ *Proposal, _ *Approval, s *Snapshot, _ *time.Time) {
			s.Blockers = append(s.Blockers, Blocker{Kind: Recall, ID: "new-recall", Digest: testDigest("e"), Overridable: true})
		}},
		{"changed-recall", func(_ *Proposal, _ *Approval, s *Snapshot, _ *time.Time) { s.Blockers[0].Digest = testDigest("a") }},
		{"non-waivable", func(_ *Proposal, _ *Approval, s *Snapshot, _ *time.Time) { s.Blockers[0].Overridable = false }},
		{"other-policy", func(_ *Proposal, _ *Approval, s *Snapshot, _ *time.Time) {
			s.Blockers = append(s.Blockers, Blocker{Kind: OtherConstraint, ID: "unsigned-artifact", Digest: testDigest("a")})
		}},
		{"not-approved", func(_ *Proposal, a *Approval, _ *Snapshot, _ *time.Time) { a.Status = "PENDING_APPROVALS" }},
		{"cancelled", func(_ *Proposal, a *Approval, _ *Snapshot, _ *time.Time) { a.Status = "CANCELLED" }},
		{"unsatisfied-review", func(_ *Proposal, a *Approval, _ *Snapshot, _ *time.Time) { a.RequirementsSatisfied = false }},
		{"hold", func(_ *Proposal, a *Approval, _ *Snapshot, _ *time.Time) { a.DoNotMerge = true }},
		{"foreign-review", func(_ *Proposal, a *Approval, _ *Snapshot, _ *time.Time) {
			a.ProposalID = "iov_" + strings.Repeat("a", 64)
		}},
		{"missing-revision", func(_ *Proposal, a *Approval, _ *Snapshot, _ *time.Time) { a.Revision = 0 }},
		{"future-review", func(_ *Proposal, a *Approval, _ *Snapshot, n *time.Time) { a.ReviewedAt = n.Add(time.Second) }},
		{"review-before-proposal", func(p *Proposal, a *Approval, _ *Snapshot, _ *time.Time) {
			a.ReviewedAt = p.CreatedAt.Add(-time.Nanosecond)
		}},
		{"expiry-boundary", func(p *Proposal, _ *Approval, _ *Snapshot, n *time.Time) { *n = p.ExpiresAt }},
		{"clock-before-proposal", func(p *Proposal, _ *Approval, _ *Snapshot, n *time.Time) { *n = p.CreatedAt.Add(-time.Nanosecond) }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			p, a, s, n := reviewFixture(t)
			test.mutate(&p, &a, &s, &n)
			if _, err := Revalidate(p, a, s, n); err == nil {
				t.Fatal("unreviewed or stale authority admitted")
			}
		})
	}
	for _, reviewer := range []Identity{{"publisher-service", "reviewer"}, {"review-service", "author"}, {"author", "reviewer"}, {"review-service", "publisher-service"}} {
		p, a, s, n := reviewFixture(t)
		a.Reviewer = reviewer
		if _, err := Revalidate(p, a, s, n); !errors.Is(err, ErrBlocked) {
			t.Fatal("self/delegated self review admitted", reviewer, err)
		}
	}
}

func TestProposalSealingAndStrictCodec(t *testing.T) {
	p, _, snapshot, now := reviewFixture(t)
	original := slices.Clone(snapshot.Blockers)
	slices.Reverse(snapshot.Blockers)
	other, err := NewProposal(snapshot, p.Flags, p.Author, p.Rationale, p.CreatedAt, p.ExpiresAt)
	if err != nil || other.ID != p.ID {
		t.Fatal("blocker order changed identity", err)
	}
	snapshot.Blockers[0].Digest = testDigest("a")
	if !reflect.DeepEqual(p.Snapshot.Blockers, original) {
		t.Fatal("proposal retained caller-owned mutable data")
	}
	copy := CopyProposal(p)
	copy.Snapshot.Blockers[0].Digest = testDigest("a")
	if p.Validate() != nil || copy.Validate() == nil {
		t.Fatal("mutable copy escaped identity binding")
	}
	encoded, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil || !reflect.DeepEqual(decoded, p) {
		t.Fatal("codec lost binding", err)
	}
	for _, document := range [][]byte{nil, []byte("{}"), append(slices.Clone(encoded), []byte(" {}")...), bytes.Repeat([]byte("x"), MaximumDocumentBytes+1), bytes.Replace(encoded, []byte(`"valueTier":"CONDUIT_NATIVE"`), []byte(`"valueTier":"PUBLIC_CONTRACT"`), 1), append([]byte(`{"unknown":true,`), encoded[1:]...)} {
		if _, err := Decode(document); err == nil {
			t.Fatal("invalid stored proposal accepted")
		}
	}
	for _, mutate := range []func(*Proposal){func(p *Proposal) { p.Flags.IgnoreRecalls = false }, func(p *Proposal) { p.Rationale = "different" }, func(p *Proposal) { p.Author.Actor = "other" }, func(p *Proposal) { p.ExpiresAt = p.ExpiresAt.Add(time.Second) }, func(p *Proposal) { p.ID = "" }} {
		copy := CopyProposal(p)
		mutate(&copy)
		if copy.Validate() == nil {
			t.Fatal("changed proposal retained its identity")
		}
	}
	if _, err := NewProposal(p.Snapshot, Flags{}, p.Author, p.Rationale, p.CreatedAt, p.ExpiresAt); err == nil {
		t.Fatal("empty override admitted")
	}
	onlyRecall := p.Snapshot
	onlyRecall.Blockers = slices.Clone(p.Snapshot.Blockers[:1])
	if _, err := NewProposal(onlyRecall, Flags{true, true}, p.Author, p.Rationale, p.CreatedAt, p.ExpiresAt); err == nil {
		t.Fatal("future secret waiver admitted")
	}
	for _, reason := range []string{" ", "bad\x00control", string([]byte{0xff}), strings.Repeat("x", 16385), strings.Repeat("&", 16384)} {
		if _, err := NewProposal(p.Snapshot, p.Flags, p.Author, reason, p.CreatedAt, p.ExpiresAt); err == nil {
			t.Fatal("unbounded or invalid rationale admitted")
		}
	}
	for _, expiry := range []time.Time{p.CreatedAt, p.CreatedAt.Add(-time.Second), p.CreatedAt.Add(MaximumReviewLifetime + time.Nanosecond)} {
		if _, err := NewProposal(p.Snapshot, p.Flags, p.Author, p.Rationale, p.CreatedAt, expiry); err == nil {
			t.Fatal("invalid review window admitted")
		}
	}
	future := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := NewProposal(p.Snapshot, p.Flags, p.Author, p.Rationale, future, future.Add(time.Minute)); err == nil {
		t.Fatal("unencodable proposal admitted")
	}
	nearExpiry := p
	nearExpiry.ExpiresAt = now.Add(time.Second)
	nearExpiry.ID = nearExpiry.identity()
	a := Approval{ReviewID: "crq_short", ProposalID: nearExpiry.ID, Revision: 1, Status: "APPROVED", RequirementsSatisfied: true, Reviewer: Identity{"reviewer", "reviewer"}, ReviewedAt: now.Add(-time.Second), AuthorityDigest: p.Snapshot.AuthorityDigest}
	permit, err := Revalidate(nearExpiry, a, p.Snapshot, now)
	if err != nil || !permit.ExpiresAt().Equal(nearExpiry.ExpiresAt) {
		t.Fatal("permit outlived approval", err)
	}
}

func TestMalformedSnapshotsCannotBecomeReviewEvidence(t *testing.T) {
	for _, mutate := range []func(*Snapshot){
		func(s *Snapshot) { s.Target.Kind = "UNKNOWN" }, func(s *Snapshot) { s.Target.Action = "DELETE" }, func(s *Snapshot) { s.Target.Version = "" }, func(s *Snapshot) { s.Target.ReleaseDigest = "latest" }, func(s *Snapshot) { s.Target.BaselineRevision = 0 }, func(s *Snapshot) { s.Target.Action = "INSTALL" }, func(s *Snapshot) { s.AuthorityDigest = "" }, func(s *Snapshot) { s.Blockers = append(s.Blockers, s.Blockers[0]) }, func(s *Snapshot) { s.Blockers[0].Kind = "FUTURE" }, func(s *Snapshot) { s.Blockers[0].ID = "" }, func(s *Snapshot) { s.Blockers[0].Digest = "" }, func(s *Snapshot) { s.Blockers = make([]Blocker, MaximumBlockers+1) },
	} {
		_, _, s, _ := reviewFixture(t)
		mutate(&s)
		if s.Validate() == nil {
			t.Fatal("malformed facts admitted")
		}
	}
	_, _, s, _ := reviewFixture(t)
	s.Target.Kind = "MODULE"
	s.Target.Action = "INSTALL"
	s.Target.BaselineRevision = 0
	if s.Validate() != nil {
		t.Fatal("new Module target refused")
	}
}

func TestOneFlagCannotWaiveTheOtherConstraint(t *testing.T) {
	for _, flags := range []Flags{{IgnoreRecalls: true}, {IgnoreSecretRequirements: true}} {
		p, a, current, now := reviewFixture(t)
		restricted, err := NewProposal(p.Snapshot, flags, p.Author, p.Rationale, p.CreatedAt, p.ExpiresAt)
		if err != nil {
			t.Fatal(err)
		}
		a.ProposalID = restricted.ID
		if _, err := Revalidate(restricted, a, current, now); !errors.Is(err, ErrBlocked) {
			t.Fatal("one flag waived an unrelated blocker", flags, err)
		}
	}
	p, a, current, now := reviewFixture(t)
	current.Blockers = append(current.Blockers, Blocker{Kind: OtherConstraint, ID: "artifact-policy", Digest: testDigest("a")})
	blocked, err := NewProposal(current, p.Flags, p.Author, p.Rationale, p.CreatedAt, p.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	a.ProposalID = blocked.ID
	if _, err := Revalidate(blocked, a, current, now); !errors.Is(err, ErrBlocked) {
		t.Fatal("review waived a non-override constraint", err)
	}
}

package execution

import (
	"testing"
	"time"
)

var now = time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)

func at(offset time.Duration) *Lease {
	return &Lease{AgentID: "agent-1", Token: "tok", Attempt: 2, ExpiresAt: now.Add(offset)}
}

func TestLiveOnlyWhileTheLeaseHasTimeLeft(t *testing.T) {
	t.Parallel()
	if !Live(at(time.Minute), now) {
		t.Error("a lease with a minute left must be live")
	}
	if Live(at(-time.Minute), now) {
		t.Error("a lapsed lease must not be live")
	}
	if Live(nil, now) {
		t.Error("a Plan that was never leased has no live lease")
	}
}

// The boundary is exclusive on purpose: a lease expiring exactly now is
// released rather than held, so two Agents cannot both believe they hold it.
func TestLeaseExpiringExactlyNowIsNotLive(t *testing.T) {
	t.Parallel()
	if Live(at(0), now) {
		t.Fatal("a lease expiring at this instant must not be live")
	}
	if !Expired(at(0), now) {
		t.Fatal("a lease expiring at this instant must read as expired")
	}
}

// Expired is not the negation of Live. A Plan that was never leased is neither,
// and conflating the two makes an unleased Plan look like a lapsed one.
func TestNeverLeasedIsNeitherLiveNorExpired(t *testing.T) {
	t.Parallel()
	if Live(nil, now) || Expired(nil, now) {
		t.Fatal("a nil lease must be neither live nor expired")
	}
}

func TestTokenMustMatchToHoldTheLease(t *testing.T) {
	t.Parallel()
	if !HeldByToken(at(time.Minute), "tok") {
		t.Error("the issued token must hold the lease")
	}
	if HeldByToken(at(time.Minute), "other") {
		t.Error("a different token must not hold the lease")
	}
	if HeldByToken(nil, "tok") {
		t.Error("no lease means no token holds it")
	}
}

// A caller that sent no token is refused rather than matched against a lease
// whose token happens to be empty too.
func TestEmptyTokenNeverHolds(t *testing.T) {
	t.Parallel()
	if HeldByToken(&Lease{Token: ""}, "") {
		t.Fatal("an empty token must never hold a lease, even an empty-token one")
	}
}

func TestAgentMustMatchToHoldTheLease(t *testing.T) {
	t.Parallel()
	if !HeldByAgent(at(time.Minute), "agent-1") {
		t.Error("the holding Agent must hold the lease")
	}
	if HeldByAgent(at(time.Minute), "agent-2") {
		t.Error("another Agent must not hold the lease")
	}
	if HeldByAgent(nil, "agent-1") {
		t.Error("no lease means no Agent holds it")
	}
	if HeldByAgent(&Lease{AgentID: ""}, "") {
		t.Error("an empty Agent id must never hold a lease")
	}
}

// The count continues rather than restarting, so a Plan that was taken and lost
// still records how many times it was tried.
func TestNextAttemptContinuesTheCount(t *testing.T) {
	t.Parallel()
	if got := NextAttempt(nil); got != 1 {
		t.Errorf("a Plan never leased must start at attempt 1, got %d", got)
	}
	if got := NextAttempt(at(time.Minute)); got != 3 {
		t.Errorf("attempt 2 must be followed by 3, got %d", got)
	}
	if got := NextAttempt(at(-time.Hour)); got != 3 {
		t.Errorf("a lapsed lease must still carry its count forward, got %d", got)
	}
}

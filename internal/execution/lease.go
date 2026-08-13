// Package execution owns the lease: the claim an Agent holds while it is
// carrying out a Plan, and the rules deciding whether that claim is still good.
//
// A lease answers a small number of questions — is it live, has it expired, does
// this token hold it, does this Agent hold it, which attempt comes next — and
// none of them need to know what a Plan is. They were nonetheless spelled inline
// at each call site, three times in slightly different words, which is how the
// same rule drifts into three rules.
//
// The nil lease is deliberate here rather than a caller's problem. A Plan that
// has never been leased has no lease, and every question below has a sensible
// answer for that case: it is not live, nobody holds it, and the next attempt is
// the first. Making callers nil-check first is what produced the variations.
//
// This package imports nothing.
package execution

import "time"

// Lease is an Agent's claim on a Plan, valid until it expires or is renewed.
type Lease struct {
	AgentID   string    `json:"agentId"`
	Token     string    `json:"token"`
	Attempt   int       `json:"attempt"`
	IssuedAt  time.Time `json:"issuedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Live reports whether a lease is still in force.
//
// The boundary is exclusive: a lease expiring exactly now is not live. That is
// the safer direction — it releases the claim a moment early rather than
// letting two Agents believe they hold the same Plan at the same instant.
func Live(lease *Lease, now time.Time) bool {
	return lease != nil && lease.ExpiresAt.After(now)
}

// Expired reports whether a lease was held and has since lapsed. It is not the
// negation of Live: a Plan that was never leased is neither.
func Expired(lease *Lease, now time.Time) bool {
	return lease != nil && !lease.ExpiresAt.After(now)
}

// HeldByToken reports whether this token is the one the lease was issued with.
// An empty token never matches, so a caller that forgot to send one is refused
// rather than matching a lease whose token is also empty.
func HeldByToken(lease *Lease, token string) bool {
	return lease != nil && token != "" && lease.Token == token
}

// HeldByAgent reports whether this Agent is the one holding the lease. An empty
// agent identifier never matches, for the same reason as HeldByToken.
func HeldByAgent(lease *Lease, agentID string) bool {
	return lease != nil && agentID != "" && lease.AgentID == agentID
}

// NextAttempt is the attempt number a new lease should carry. A Plan that has
// never been leased starts at one; anything else continues the count, so a Plan
// that was taken and lost still records how many times it was tried.
func NextAttempt(lease *Lease) int {
	if lease == nil {
		return 1
	}
	return lease.Attempt + 1
}

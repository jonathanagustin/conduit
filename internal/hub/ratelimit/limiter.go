// Package ratelimit bounds how fast one caller may ask the Hub for work.
//
// The Hub serves operators and Agents on one listener. Without a bound, one
// operator script in a loop -- or one unauthenticated flood -- spends the same
// goroutines, database connections and CPU an Agent needs to take a lease and
// report a Plan. This package is the bound: a token bucket per caller, with
// callers grouped into classes that never share a bucket.
//
// It knows nothing about HTTP or identity. The caller says which class a
// request belongs to and what key identifies who is asking; the composition
// root owns both decisions.
//
// The bucket is implemented here rather than imported. It is a dozen lines of
// arithmetic over an injected clock, and this repository adds a direct
// dependency only where owning the code would be the larger risk.
package ratelimit

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

// Class is the kind of caller a bucket belongs to. The set is closed here, in
// code, so it is safe to export as a metric label: it cannot grow with a
// tenant's fleet, and it names nobody.
type Class string

const (
	// ClassOperator is an authenticated operator or service account.
	ClassOperator Class = "operator"
	// ClassAgent is an authenticated Agent on an Agent route.
	ClassAgent Class = "agent"
	// ClassAnonymous is any request no identity was resolved for.
	ClassAnonymous Class = "anonymous"
)

// Classes lists every class, in a stable order.
func Classes() []Class { return []Class{ClassOperator, ClassAgent, ClassAnonymous} }

// Limit is one token bucket's shape: Burst requests at once, refilled at
// PerSecond.
type Limit struct {
	PerSecond float64
	Burst     int
}

func (limit Limit) validate(name string) error {
	if math.IsNaN(limit.PerSecond) || math.IsInf(limit.PerSecond, 0) || limit.PerSecond <= 0 {
		return fmt.Errorf("%s rate must be a positive number of requests per second", name)
	}
	if limit.Burst < 1 {
		return fmt.Errorf("%s burst must be at least 1", name)
	}
	return nil
}

// Defaults are deliberately generous. They are a ceiling on one caller, not a
// capacity plan: see docs/operations/hub-rate-limiting.md for the arithmetic.
const (
	DefaultMaximumKeys = 10000
	DefaultIdleTimeout = 10 * time.Minute
)

// DefaultConfig is what a Hub runs with when nothing is configured.
func DefaultConfig() Config {
	return Config{
		Operator:    Limit{PerSecond: 100, Burst: 600},
		Agent:       Limit{PerSecond: 20, Burst: 60},
		Anonymous:   Limit{PerSecond: 10, Burst: 60},
		MaximumKeys: DefaultMaximumKeys,
		IdleTimeout: DefaultIdleTimeout,
	}
}

// Config shapes a Limiter.
type Config struct {
	Operator, Agent, Anonymous Limit
	// MaximumKeys bounds the callers tracked per class. Callers beyond it
	// share one overflow bucket of the class's shape, so the limiter's own
	// memory cannot be grown by inventing identities or addresses.
	MaximumKeys int
	// IdleTimeout is how long a caller's bucket is kept after its last
	// request. It is raised to the bucket's refill time when shorter, so a
	// bucket is only ever forgotten once it is full again.
	IdleTimeout time.Duration
	// Now is the clock. Nil means time.Now.
	Now func() time.Time
}

// Decision is the answer for one request.
type Decision struct {
	Allowed bool
	// RetryAfter is how long until one token is available. Zero when Allowed.
	RetryAfter time.Duration
}

// Counts is what one class has decided so far.
type Counts struct {
	Allowed, Limited uint64
	// Keys is how many callers currently hold a bucket.
	Keys int
}

// bucket is a token bucket. tokens is what was available at refilled; the
// balance at any later moment follows from the two and the class limit, so an
// idle bucket costs nothing to keep current.
type bucket struct {
	tokens   float64
	refilled time.Time
	lastSeen time.Time
}

func newBucket(limit Limit, now time.Time) *bucket {
	return &bucket{tokens: float64(limit.Burst), refilled: now, lastSeen: now}
}

// take spends one token if one is available at now, and otherwise reports how
// long until one is. A refusal spends nothing, so being refused never pushes a
// caller's next allowed request further away. A clock that steps backwards
// grants no tokens and loses none.
func (bucket *bucket) take(limit Limit, now time.Time) (bool, time.Duration) {
	if elapsed := now.Sub(bucket.refilled); elapsed > 0 {
		bucket.tokens = math.Min(float64(limit.Burst), bucket.tokens+elapsed.Seconds()*limit.PerSecond)
		bucket.refilled = now
	}
	if bucket.tokens >= 1 {
		bucket.tokens--
		return true, 0
	}
	return false, time.Duration((1 - bucket.tokens) / limit.PerSecond * float64(time.Second))
}

type classState struct {
	mu        sync.Mutex
	limit     Limit
	idle      time.Duration
	buckets   map[string]*bucket
	overflow  *bucket
	lastSweep time.Time
	allowed   uint64
	limited   uint64
}

// Limiter holds every caller's bucket. Safe for concurrent use.
type Limiter struct {
	now         func() time.Time
	maximumKeys int
	classes     map[Class]*classState
}

// New validates the configuration and builds a Limiter.
func New(config Config) (*Limiter, error) {
	if config.MaximumKeys < 1 {
		return nil, errors.New("rate limiter must track at least one caller per class")
	}
	if config.IdleTimeout <= 0 {
		return nil, errors.New("rate limiter idle timeout must be positive")
	}
	limiter := &Limiter{now: config.Now, maximumKeys: config.MaximumKeys, classes: map[Class]*classState{}}
	if limiter.now == nil {
		limiter.now = time.Now
	}
	started := limiter.now()
	for class, limit := range map[Class]Limit{
		ClassOperator: config.Operator, ClassAgent: config.Agent, ClassAnonymous: config.Anonymous,
	} {
		if err := limit.validate(string(class)); err != nil {
			return nil, err
		}
		// Forgetting a bucket before it has refilled would hand its caller a
		// fresh burst early. Never evict sooner than a full refill takes.
		idle := config.IdleTimeout
		if refill := time.Duration(float64(limit.Burst) / limit.PerSecond * float64(time.Second)); refill > idle {
			idle = refill
		}
		limiter.classes[class] = &classState{
			limit: limit, idle: idle, buckets: map[string]*bucket{},
			overflow: newBucket(limit, started), lastSweep: started,
		}
	}
	return limiter, nil
}

// Allow takes one token from the caller's bucket in its class. A request is
// counted once, when it arrives: a long-lived response such as an event stream
// costs one token however long it stays open.
//
// An unknown class is refused rather than waved through, so a caller added
// without a budget cannot become an unlimited one.
func (limiter *Limiter) Allow(class Class, key string) Decision {
	state := limiter.classes[class]
	if state == nil {
		return Decision{RetryAfter: time.Second}
	}
	now := limiter.now()
	state.mu.Lock()
	defer state.mu.Unlock()
	allowed, retryAfter := state.bucketFor(key, now, limiter.maximumKeys).take(state.limit, now)
	if !allowed {
		state.limited++
		return Decision{RetryAfter: retryAfter}
	}
	state.allowed++
	return Decision{Allowed: true}
}

func (state *classState) bucketFor(key string, now time.Time, maximumKeys int) *bucket {
	if existing := state.buckets[key]; existing != nil {
		existing.lastSeen = now
		return existing
	}
	// Sweep twice per idle period, and when the table is full at most once a
	// second: a flood of new keys against a full table must not turn every
	// request into a walk of the whole table.
	sinceSweep := now.Sub(state.lastSweep)
	if sinceSweep >= state.idle/2 || (len(state.buckets) >= maximumKeys && sinceSweep >= time.Second) {
		state.sweep(now)
	}
	if len(state.buckets) >= maximumKeys {
		return state.overflow
	}
	created := newBucket(state.limit, now)
	state.buckets[key] = created
	return created
}

func (state *classState) sweep(now time.Time) {
	state.lastSweep = now
	for key, candidate := range state.buckets {
		if now.Sub(candidate.lastSeen) >= state.idle {
			delete(state.buckets, key)
		}
	}
}

// Snapshot reports each class's decisions and tracked callers. It names no
// caller: the counts are per class only.
func (limiter *Limiter) Snapshot() map[Class]Counts {
	snapshot := make(map[Class]Counts, len(limiter.classes))
	for class, state := range limiter.classes {
		state.mu.Lock()
		snapshot[class] = Counts{Allowed: state.allowed, Limited: state.limited, Keys: len(state.buckets)}
		state.mu.Unlock()
	}
	return snapshot
}

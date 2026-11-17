package ratelimit_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/hub/ratelimit"
)

// clock is the injected time source. Nothing in these tests sleeps.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *clock { return &clock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)} }

func (clock *clock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *clock) Advance(by time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(by)
}

func limiterForTest(t *testing.T, clock *clock, change func(*ratelimit.Config)) *ratelimit.Limiter {
	t.Helper()
	config := ratelimit.Config{
		Operator:    ratelimit.Limit{PerSecond: 2, Burst: 3},
		Agent:       ratelimit.Limit{PerSecond: 1, Burst: 2},
		Anonymous:   ratelimit.Limit{PerSecond: 1, Burst: 1},
		MaximumKeys: 100, IdleTimeout: time.Minute, Now: clock.Now,
	}
	if change != nil {
		change(&config)
	}
	limiter, err := ratelimit.New(config)
	if err != nil {
		t.Fatal(err)
	}
	return limiter
}

func take(limiter *ratelimit.Limiter, class ratelimit.Class, key string, times int) (allowed int) {
	for range times {
		if limiter.Allow(class, key).Allowed {
			allowed++
		}
	}
	return allowed
}

func TestABurstIsAdmittedThenRefusedWithTheTimeUntilTheNextToken(t *testing.T) {
	t.Parallel()
	clock := newClock()
	limiter := limiterForTest(t, clock, nil)
	if got := take(limiter, ratelimit.ClassOperator, "alice", 3); got != 3 {
		t.Fatalf("burst admitted %d of 3", got)
	}
	refused := limiter.Allow(ratelimit.ClassOperator, "alice")
	// 2 tokens a second: the next one is half a second away.
	if refused.Allowed || refused.RetryAfter != 500*time.Millisecond {
		t.Fatalf("fourth request = %+v, want a refusal with 500ms to wait", refused)
	}
	// Part of the wait has passed: the answer shrinks with it.
	clock.Advance(200 * time.Millisecond)
	if again := limiter.Allow(ratelimit.ClassOperator, "alice"); again.Allowed || again.RetryAfter != 300*time.Millisecond {
		t.Fatalf("after 200ms = %+v, want a refusal with 300ms to wait", again)
	}
}

func TestTokensRefillWithTheInjectedClockAndNeverPastTheBurst(t *testing.T) {
	t.Parallel()
	clock := newClock()
	limiter := limiterForTest(t, clock, nil)
	take(limiter, ratelimit.ClassOperator, "alice", 3)
	if limiter.Allow(ratelimit.ClassOperator, "alice").Allowed {
		t.Fatal("an empty bucket admitted a request with no time passed")
	}
	// Refusals spend nothing: exactly one token is back after half a second,
	// however many times the caller was refused in between.
	take(limiter, ratelimit.ClassOperator, "alice", 50)
	clock.Advance(500 * time.Millisecond)
	if got := take(limiter, ratelimit.ClassOperator, "alice", 5); got != 1 {
		t.Fatalf("after one refill interval %d requests were admitted, want 1", got)
	}
	// A long idle period refills to the burst and no further.
	clock.Advance(time.Hour)
	if got := take(limiter, ratelimit.ClassOperator, "alice", 50); got != 3 {
		t.Fatalf("after an hour %d requests were admitted, want the burst of 3", got)
	}
	// A clock stepping backwards grants nothing.
	clock.Advance(-time.Hour)
	if limiter.Allow(ratelimit.ClassOperator, "alice").Allowed {
		t.Fatal("a backwards clock step refilled the bucket")
	}
}

func TestOneCallerExhaustingItsBucketLeavesAnotherCallerUntouched(t *testing.T) {
	t.Parallel()
	limiter := limiterForTest(t, newClock(), nil)
	take(limiter, ratelimit.ClassOperator, "alice", 100)
	if got := take(limiter, ratelimit.ClassOperator, "bob", 3); got != 3 {
		t.Fatalf("bob was admitted %d of 3 while alice was throttled", got)
	}
}

// The property the Agent class exists for. The key is the same string in both
// classes on purpose: a limiter that kept one table for every class would hand
// the Agent the operator's empty bucket.
func TestAnExhaustedOperatorCannotSpendAnAgentsBudget(t *testing.T) {
	t.Parallel()
	limiter := limiterForTest(t, newClock(), nil)
	take(limiter, ratelimit.ClassOperator, "prod/agent-1", 100)
	take(limiter, ratelimit.ClassAnonymous, "prod/agent-1", 100)
	if got := take(limiter, ratelimit.ClassAgent, "prod/agent-1", 2); got != 2 {
		t.Fatalf("the Agent was admitted %d of its burst of 2 while other classes were throttled", got)
	}
	counts := limiter.Snapshot()
	if counts[ratelimit.ClassAgent].Limited != 0 || counts[ratelimit.ClassOperator].Limited != 97 ||
		counts[ratelimit.ClassOperator].Allowed != 3 || counts[ratelimit.ClassAnonymous].Allowed != 1 {
		t.Fatalf("snapshot = %+v", counts)
	}
}

func TestIdleCallersAreEvictedAndOnlyOnceTheirBucketIsFull(t *testing.T) {
	t.Parallel()
	clock := newClock()
	limiter := limiterForTest(t, clock, nil)
	for index := range 40 {
		limiter.Allow(ratelimit.ClassOperator, fmt.Sprintf("operator-%d", index))
	}
	if keys := limiter.Snapshot()[ratelimit.ClassOperator].Keys; keys != 40 {
		t.Fatalf("tracked %d callers, want 40", keys)
	}
	// Still inside the idle timeout: a new caller evicts nobody.
	clock.Advance(59 * time.Second)
	limiter.Allow(ratelimit.ClassOperator, "recent")
	if keys := limiter.Snapshot()[ratelimit.ClassOperator].Keys; keys != 41 {
		t.Fatalf("tracked %d callers before the idle timeout, want 41", keys)
	}
	// Past it, and half an idle period after the last sweep: the next new
	// caller sweeps the forty idle ones and keeps the one seen 31s ago.
	clock.Advance(31 * time.Second)
	limiter.Allow(ratelimit.ClassOperator, "newcomer")
	if keys := limiter.Snapshot()[ratelimit.ClassOperator].Keys; keys != 2 {
		t.Fatalf("tracked %d callers after the idle timeout, want 2", keys)
	}

	// An idle timeout shorter than the refill time would let a throttled
	// caller go quiet, be forgotten, and come back to a fresh burst early.
	slow := limiterForTest(t, clock, func(config *ratelimit.Config) {
		config.Anonymous = ratelimit.Limit{PerSecond: 0.01, Burst: 2} // 200s to refill
		config.IdleTimeout = time.Second
	})
	take(slow, ratelimit.ClassAnonymous, "192.0.2.1", 5)
	clock.Advance(100 * time.Second)
	slow.Allow(ratelimit.ClassAnonymous, "198.51.100.7")
	if got := take(slow, ratelimit.ClassAnonymous, "192.0.2.1", 5); got != 1 {
		t.Fatalf("a caller idle for half its refill time was admitted %d times, want the 1 token it earned", got)
	}
}

// The limiter is itself a table keyed by something a caller chooses. It must
// not be a way to exhaust the Hub's memory.
func TestTheCallerTableIsBoundedAndTheOverflowIsStillLimited(t *testing.T) {
	t.Parallel()
	clock := newClock()
	limiter := limiterForTest(t, clock, func(config *ratelimit.Config) { config.MaximumKeys = 5 })
	admitted := 0
	for index := range 500 {
		if limiter.Allow(ratelimit.ClassAnonymous, fmt.Sprintf("2001:db8:%x::/64", index)).Allowed {
			admitted++
		}
	}
	counts := limiter.Snapshot()[ratelimit.ClassAnonymous]
	if counts.Keys != 5 {
		t.Fatalf("tracked %d callers, want the maximum of 5", counts.Keys)
	}
	// Five tracked callers with a burst of 1, plus the one shared overflow
	// bucket's burst of 1. The other 494 invented addresses got nothing.
	if admitted != 6 {
		t.Fatalf("%d of 500 invented callers were admitted, want 6", admitted)
	}
	// A tracked caller keeps its own bucket while the table is full.
	clock.Advance(time.Second)
	if !limiter.Allow(ratelimit.ClassAnonymous, "2001:db8:0::/64").Allowed {
		t.Fatal("a tracked caller lost its bucket to the overflow")
	}
}

func TestConcurrentRequestsNeverExceedTheBurst(t *testing.T) {
	t.Parallel()
	limiter := limiterForTest(t, newClock(), func(config *ratelimit.Config) {
		config.Operator = ratelimit.Limit{PerSecond: 1, Burst: 50}
	})
	var admitted atomic.Int64
	var group sync.WaitGroup
	for range 20 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 100 {
				if limiter.Allow(ratelimit.ClassOperator, "alice").Allowed {
					admitted.Add(1)
				}
			}
		}()
	}
	group.Wait()
	if admitted.Load() != 50 {
		t.Fatalf("%d concurrent requests were admitted against a burst of 50", admitted.Load())
	}
}

func TestAnUnusableConfigurationAndAnUnknownClassAreRefused(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*ratelimit.Config){
		"zero rate":      func(config *ratelimit.Config) { config.Operator.PerSecond = 0 },
		"negative rate":  func(config *ratelimit.Config) { config.Agent.PerSecond = -1 },
		"zero burst":     func(config *ratelimit.Config) { config.Anonymous.Burst = 0 },
		"no callers":     func(config *ratelimit.Config) { config.MaximumKeys = 0 },
		"no idle period": func(config *ratelimit.Config) { config.IdleTimeout = 0 },
	} {
		config := ratelimit.DefaultConfig()
		change(&config)
		if _, err := ratelimit.New(config); err == nil {
			t.Errorf("%s: the configuration was accepted", name)
		}
	}
	defaults, err := ratelimit.New(ratelimit.DefaultConfig())
	if err != nil {
		t.Fatalf("the defaults are not a usable configuration: %v", err)
	}
	// A class nobody gave a budget is not an unlimited class.
	if decision := defaults.Allow(ratelimit.Class("administrator"), "alice"); decision.Allowed || decision.RetryAfter <= 0 {
		t.Fatalf("an unknown class was answered %+v", decision)
	}
	// The anonymous default must stay the strictest: it is the only class a
	// caller can enter without proving who they are.
	config := ratelimit.DefaultConfig()
	if config.Anonymous.PerSecond >= config.Operator.PerSecond || config.Anonymous.Burst > config.Agent.Burst {
		t.Fatalf("the anonymous default %+v is not stricter than the authenticated ones", config.Anonymous)
	}
}

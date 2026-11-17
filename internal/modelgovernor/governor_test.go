package modelgovernor_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/modelgovernor"
	"github.com/jonathanagustin/conduit/internal/modelgovernor/adapters/memory"
	"github.com/jonathanagustin/conduit/internal/modelhttp"
)

// fakeClock is a clock a test moves by hand. With auto set, every After fires
// at once and advances the clock by the delay, so a retry test runs without
// waiting and can read what the waits were.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
	auto   bool
	delays []time.Duration
}

type fakeTimer struct {
	at time.Time
	ch chan time.Time
}

func newClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)}
}

func (clock *fakeClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *fakeClock) After(delay time.Duration) <-chan time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	ch := make(chan time.Time, 1)
	clock.delays = append(clock.delays, delay)
	if clock.auto {
		clock.now = clock.now.Add(delay)
		ch <- clock.now
		return ch
	}
	clock.timers = append(clock.timers, &fakeTimer{at: clock.now.Add(delay), ch: ch})
	return ch
}

func (clock *fakeClock) Advance(delay time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(delay)
	kept := clock.timers[:0]
	for _, timer := range clock.timers {
		if timer.at.After(clock.now) {
			kept = append(kept, timer)
			continue
		}
		timer.ch <- clock.now
	}
	clock.timers = kept
}

func (clock *fakeClock) Set(now time.Time) {
	clock.mu.Lock()
	clock.now = now
	clock.mu.Unlock()
}

func (clock *fakeClock) Delays() []time.Duration {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return append([]time.Duration{}, clock.delays...)
}

// eventually polls for a condition another goroutine is about to make true.
func eventually(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

type auditLog struct {
	mu     sync.Mutex
	events []modelgovernor.CallAudit
	fail   error
}

func (log *auditLog) RecordModelCall(_ context.Context, event modelgovernor.CallAudit) error {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.fail != nil {
		return log.fail
	}
	event.EventID = fmt.Sprintf("aud_%d", len(log.events)+1)
	log.events = append(log.events, event)
	return nil
}

func (log *auditLog) Events() []modelgovernor.CallAudit {
	log.mu.Lock()
	defer log.mu.Unlock()
	return append([]modelgovernor.CallAudit{}, log.events...)
}

type fixture struct {
	governor    *modelgovernor.Governor
	clock       *fakeClock
	usage       *memory.Usage
	transcripts *memory.Transcripts
	audit       *auditLog
	metrics     *modelgovernor.Metrics
}

func newFixture(config modelgovernor.Config, change ...func(*modelgovernor.Dependencies)) fixture {
	fx := fixture{
		clock: newClock(), usage: memory.NewUsage(), transcripts: memory.NewTranscripts(),
		audit: &auditLog{}, metrics: modelgovernor.NewMetrics(),
	}
	deps := modelgovernor.Dependencies{
		Usage: fx.usage, Transcripts: fx.transcripts, Audit: fx.audit, Metrics: fx.metrics, Clock: fx.clock,
		Jitter: func() float64 { return 0 },
	}
	for _, apply := range change {
		apply(&deps)
	}
	fx.governor = modelgovernor.New(config, deps)
	return fx
}

func as(owner string) context.Context {
	return modelgovernor.WithScope(context.Background(), modelgovernor.Scope{Owner: func() string { return owner }})
}

var assistantCall = modelgovernor.Call{
	Capability: modelgovernor.CapabilityAssistant, Transport: modelgovernor.TransportHTTP, ConfiguredModel: "m", Prompt: []byte("prompt"),
}

func reply(text string) func(context.Context) (modelgovernor.Result, error) {
	return func(context.Context) (modelgovernor.Result, error) {
		return modelgovernor.Result{Response: []byte(text)}, nil
	}
}

// hold starts a call that occupies a slot until the returned release is
// called, and returns once it is inside the model.
func hold(t *testing.T, governor *modelgovernor.Governor, ctx context.Context) (release func(), done <-chan error) {
	t.Helper()
	entered, gate, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := governor.Do(ctx, assistantCall, nil, func(ctx context.Context) (modelgovernor.Result, error) {
			close(entered)
			select {
			case <-gate:
				return modelgovernor.Result{Response: []byte("held")}, nil
			case <-ctx.Done():
				return modelgovernor.Result{}, ctx.Err()
			}
		})
		finished <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("a call under the limit never reached the model")
	}
	var once sync.Once
	return func() { once.Do(func() { close(gate) }) }, finished
}

func TestTheGlobalCapMakesTheNextCallWaitAndThenRefusesIt(t *testing.T) {
	fx := newFixture(modelgovernor.Config{GlobalConcurrency: 2, OwnerConcurrency: 5, QueueWait: 3 * time.Second})
	releaseA, _ := hold(t, fx.governor, as("ada"))
	releaseB, _ := hold(t, fx.governor, as("grace"))
	defer releaseA()
	defer releaseB()

	called := false
	result := make(chan error, 1)
	go func() {
		_, err := fx.governor.Do(as("linus"), assistantCall, nil, func(context.Context) (modelgovernor.Result, error) {
			called = true
			return modelgovernor.Result{}, nil
		})
		result <- err
	}()
	// It waits: nothing comes back until the queue wait has passed.
	eventually(t, "the third call to start waiting", func() bool { return len(fx.clock.Delays()) == 3 })
	fx.clock.Advance(3*time.Second - time.Nanosecond)
	select {
	case err := <-result:
		t.Fatalf("the call returned before the queue wait ended: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	fx.clock.Advance(time.Nanosecond)
	err := <-result
	var busy *modelgovernor.BusyError
	if !errors.As(err, &busy) || !errors.Is(err, modelgovernor.ErrBusy) || busy.Scope != "global" || busy.RetryAfter != 3*time.Second {
		t.Fatalf("err = %v, want a global BusyError with the queue wait as Retry-After", err)
	}
	if called {
		t.Fatal("a refused call reached the model")
	}
	if rows := usageRows(t, fx, "linus"); len(rows) != 1 || rows[0].Outcome != "throttled" || rows[0].Attempts != 0 {
		t.Fatalf("a throttled call was metered as %+v", rows)
	}
}

func TestACallWaitingForASlotGetsTheOneThatFrees(t *testing.T) {
	fx := newFixture(modelgovernor.Config{GlobalConcurrency: 1, OwnerConcurrency: 1})
	release, held := hold(t, fx.governor, as("ada"))
	result := make(chan error, 1)
	go func() {
		_, err := fx.governor.Do(as("ada"), assistantCall, nil, reply("second"))
		result <- err
	}()
	eventually(t, "the second call to start waiting", func() bool { return len(fx.clock.Delays()) == 2 })
	release()
	if err := <-held; err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatalf("a call that waited for a freed slot failed: %v", err)
	}
}

func TestTheOwnerCapRefusesOneOwnerAndNotAnother(t *testing.T) {
	fx := newFixture(modelgovernor.Config{GlobalConcurrency: 5, OwnerConcurrency: 1})
	release, _ := hold(t, fx.governor, as("ada"))
	defer release()
	result := make(chan error, 1)
	go func() {
		_, err := fx.governor.Do(as("ada"), assistantCall, nil, reply("x"))
		result <- err
	}()
	eventually(t, "ada's second call to start waiting", func() bool { return len(fx.clock.Delays()) == 2 })
	// Another owner is not held up by ada's limit.
	if _, err := fx.governor.Do(as("grace"), assistantCall, nil, reply("x")); err != nil {
		t.Fatalf("another owner was refused: %v", err)
	}
	fx.clock.Advance(modelgovernor.DefaultQueueWait)
	var busy *modelgovernor.BusyError
	if err := <-result; !errors.As(err, &busy) || busy.Scope != "owner" {
		t.Fatalf("err = %v, want an owner BusyError", err)
	}
	// A call with no operator is its own class, not ada's.
	if _, err := fx.governor.Do(context.Background(), assistantCall, nil, reply("x")); err != nil {
		t.Fatalf("an unattributed call was refused: %v", err)
	}
	if rows := usageRows(t, fx, modelgovernor.SystemOwner); len(rows) != 1 {
		t.Fatalf("an unattributed call was metered as %+v", rows)
	}
}

func TestCancellationReleasesEverySlot(t *testing.T) {
	fx := newFixture(modelgovernor.Config{GlobalConcurrency: 1, OwnerConcurrency: 1})
	running, cancelRunning := context.WithCancel(as("ada"))
	_, held := hold(t, fx.governor, running)

	// One waiter on the owner limit and one on the global limit.
	waiting, cancelWaiting := context.WithCancel(as("ada"))
	other, cancelOther := context.WithCancel(as("grace"))
	results := make(chan error, 2)
	for _, ctx := range []context.Context{waiting, other} {
		go func() {
			_, err := fx.governor.Do(ctx, assistantCall, nil, reply("x"))
			results <- err
		}()
	}
	eventually(t, "both calls to start waiting", func() bool { return len(fx.clock.Delays()) == 3 })
	cancelWaiting()
	cancelOther()
	for range 2 {
		if err := <-results; !errors.Is(err, context.Canceled) {
			t.Fatalf("a cancelled waiter returned %v", err)
		}
	}
	cancelRunning()
	if err := <-held; !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled call returned %v", err)
	}
	if calls, owners := fx.governor.InFlight(); calls != 0 || owners != 0 {
		t.Fatalf("after cancellation %d slots are held and %d owners tracked, want 0 and 0", calls, owners)
	}
	// And the slot is usable.
	if _, err := fx.governor.Do(as("ada"), assistantCall, nil, reply("x")); err != nil {
		t.Fatalf("the slot a cancelled call held was not released: %v", err)
	}
	if calls, owners := fx.governor.InFlight(); calls != 0 || owners != 0 {
		t.Fatalf("after a completed call %d slots are held and %d owners tracked", calls, owners)
	}
}

func TestOnlyRetryableFailuresAreRetried(t *testing.T) {
	startFailure := fmt.Errorf("%w: %w: fork failed", localcmd.ErrFailed, localcmd.ErrStartFailed)
	cases := []struct {
		name     string
		classify func(error) modelgovernor.Verdict
		err      error
		attempts int
		outcome  modelgovernor.Outcome
	}{
		{"http connection error", modelgovernor.ClassifyHTTP, fmt.Errorf("%w: connection error", modelhttp.ErrTransport), 3, modelgovernor.OutcomeTransport},
		{"http 429", modelgovernor.ClassifyHTTP, &modelhttp.RateLimitError{}, 3, modelgovernor.OutcomeRateLimited},
		{"http 5xx", modelgovernor.ClassifyHTTP, fmt.Errorf("%w: status 503", modelhttp.ErrUpstream), 3, modelgovernor.OutcomeUpstream},
		{"http 401", modelgovernor.ClassifyHTTP, fmt.Errorf("%w: status 401", modelhttp.ErrUnauthorized), 1, modelgovernor.OutcomeAuth},
		{"http 400", modelgovernor.ClassifyHTTP, fmt.Errorf("%w: status 400", modelhttp.ErrStatus), 1, modelgovernor.OutcomeError},
		{"http reply is not a completion", modelgovernor.ClassifyHTTP, fmt.Errorf("%w: body is not JSON", modelhttp.ErrMalformed), 1, modelgovernor.OutcomeInvalidResponse},
		{"http reply too large", modelgovernor.ClassifyHTTP, modelhttp.ErrResponseTooLarge, 1, modelgovernor.OutcomeInvalidResponse},
		{"http timeout", modelgovernor.ClassifyHTTP, modelhttp.ErrTimeout, 1, modelgovernor.OutcomeTimeout},
		{"http redirect refused", modelgovernor.ClassifyHTTP, modelhttp.ErrRedirectRefused, 1, modelgovernor.OutcomeTransport},
		{"http cancelled", modelgovernor.ClassifyHTTP, fmt.Errorf("%w: %w", modelhttp.ErrCanceled, context.Canceled), 1, modelgovernor.OutcomeCanceled},
		{"cli could not start", modelgovernor.ClassifyCLI, startFailure, 3, modelgovernor.OutcomeTransport},
		{"cli exited non-zero", modelgovernor.ClassifyCLI, fmt.Errorf("%w: exit status 1", localcmd.ErrFailed), 1, modelgovernor.OutcomeError},
		{"cli timed out", modelgovernor.ClassifyCLI, localcmd.ErrTimeout, 1, modelgovernor.OutcomeTimeout},
		{"cli not found", modelgovernor.ClassifyCLI, fmt.Errorf("%w: codex", localcmd.ErrNotFound), 1, modelgovernor.OutcomeError},
		{"unclassified", nil, errors.New("anything"), 1, modelgovernor.OutcomeError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newFixture(modelgovernor.Config{})
			fx.clock.auto = true
			attempts := 0
			_, err := fx.governor.Do(as("ada"), assistantCall, tc.classify, func(context.Context) (modelgovernor.Result, error) {
				attempts++
				return modelgovernor.Result{}, tc.err
			})
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want the model's own error", err)
			}
			if attempts != tc.attempts {
				t.Fatalf("attempts = %d, want %d", attempts, tc.attempts)
			}
			rows := usageRows(t, fx, "ada")
			if len(rows) != 1 || rows[0].Outcome != string(tc.outcome) || rows[0].Attempts != int64(tc.attempts) || rows[0].Calls != 1 {
				t.Fatalf("usage = %+v, want one %s call of %d attempts", rows, tc.outcome, tc.attempts)
			}
		})
	}
}

func TestARetrySucceedsAndBacksOffWithJitter(t *testing.T) {
	jitter := 0.0
	fx := newFixture(modelgovernor.Config{MaxAttempts: 4, BackoffBase: time.Second}, func(deps *modelgovernor.Dependencies) {
		deps.Jitter = func() float64 { return jitter }
	})
	fx.clock.auto = true
	run := func() []time.Duration {
		before := len(fx.clock.Delays())
		attempts := 0
		result, err := fx.governor.Do(as("ada"), assistantCall, modelgovernor.ClassifyHTTP, func(context.Context) (modelgovernor.Result, error) {
			attempts++
			if attempts < 4 {
				return modelgovernor.Result{}, fmt.Errorf("%w: status 502", modelhttp.ErrUpstream)
			}
			return modelgovernor.Result{Response: []byte("fine")}, nil
		})
		if err != nil || string(result.Response) != "fine" || attempts != 4 {
			t.Fatalf("result = %q, err = %v after %d attempts", result.Response, err, attempts)
		}
		// The first After of every call is the queue-wait timer.
		return fx.clock.Delays()[before+1:]
	}
	if got := run(); fmt.Sprint(got) != fmt.Sprint([]time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second}) {
		t.Fatalf("backoff with no jitter = %v, want half of 1s, 2s, 4s", got)
	}
	jitter = 0.999999
	got := run()
	for index, base := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		if got[index] <= base/2 || got[index] > base {
			t.Fatalf("backoff %d with full jitter = %s, want in (%s, %s]", index, got[index], base/2, base)
		}
	}
	if rows := usageRows(t, fx, "ada"); len(rows) != 1 || rows[0].Outcome != "ok" || rows[0].Attempts != 8 || rows[0].Calls != 2 {
		t.Fatalf("usage = %+v", rows)
	}
}

func TestRetryAfterIsHonouredAndCapped(t *testing.T) {
	fx := newFixture(modelgovernor.Config{MaxAttempts: 3, RetryDelayCap: 20 * time.Second})
	fx.clock.auto = true
	waits := []time.Duration{7 * time.Second, 10 * time.Minute}
	attempts := 0
	_, err := fx.governor.Do(as("ada"), assistantCall, modelgovernor.ClassifyHTTP, func(context.Context) (modelgovernor.Result, error) {
		attempts++
		if attempts <= len(waits) {
			return modelgovernor.Result{}, &modelhttp.RateLimitError{RetryAfter: waits[attempts-1]}
		}
		return modelgovernor.Result{Response: []byte("ok")}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := fx.clock.Delays()[1:]; fmt.Sprint(got) != fmt.Sprint([]time.Duration{7 * time.Second, 20 * time.Second}) {
		t.Fatalf("waits = %v, want the provider's 7s and then its 10m capped to 20s", got)
	}
}

func TestRetriesStopAtTheCallersDeadlineAndOnCancellation(t *testing.T) {
	t.Run("a wait that would pass the deadline is not started", func(t *testing.T) {
		fx := newFixture(modelgovernor.Config{MaxAttempts: 5})
		fx.clock.auto = true
		fx.clock.Set(time.Now())
		ctx, cancel := context.WithDeadline(as("ada"), fx.clock.Now().Add(10*time.Second))
		defer cancel()
		attempts := 0
		_, err := fx.governor.Do(ctx, assistantCall, modelgovernor.ClassifyHTTP, func(context.Context) (modelgovernor.Result, error) {
			attempts++
			return modelgovernor.Result{}, &modelhttp.RateLimitError{RetryAfter: 6 * time.Second}
		})
		if !errors.Is(err, modelhttp.ErrRateLimited) || attempts != 2 {
			t.Fatalf("err = %v after %d attempts, want 2: one 6s wait fits in 10s and a second does not", err, attempts)
		}
	})
	t.Run("a cancelled caller is not retried", func(t *testing.T) {
		fx := newFixture(modelgovernor.Config{MaxAttempts: 5})
		ctx, cancel := context.WithCancel(as("ada"))
		attempts := 0
		_, err := fx.governor.Do(ctx, assistantCall, modelgovernor.ClassifyHTTP, func(context.Context) (modelgovernor.Result, error) {
			attempts++
			cancel()
			return modelgovernor.Result{}, fmt.Errorf("%w: status 500", modelhttp.ErrUpstream)
		})
		if err == nil || attempts != 1 {
			t.Fatalf("err = %v after %d attempts, want 1", err, attempts)
		}
		if rows := usageRows(t, fx, "ada"); len(rows) != 1 || rows[0].Outcome != "canceled" {
			t.Fatalf("usage = %+v", rows)
		}
	})
	t.Run("cancellation during a backoff wait ends the call", func(t *testing.T) {
		fx := newFixture(modelgovernor.Config{MaxAttempts: 5})
		ctx, cancel := context.WithCancel(as("ada"))
		attempts := 0
		done := make(chan error, 1)
		go func() {
			_, err := fx.governor.Do(ctx, assistantCall, modelgovernor.ClassifyHTTP, func(context.Context) (modelgovernor.Result, error) {
				attempts++
				return modelgovernor.Result{}, fmt.Errorf("%w: status 500", modelhttp.ErrUpstream)
			})
			done <- err
		}()
		eventually(t, "the backoff wait", func() bool { return len(fx.clock.Delays()) == 2 })
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) || attempts != 1 {
			t.Fatalf("err = %v after %d attempts", err, attempts)
		}
		if calls, _ := fx.governor.InFlight(); calls != 0 {
			t.Fatal("a call cancelled during backoff still holds a slot")
		}
	})
}

func usageRows(t *testing.T, fx fixture, owner string) []modelgovernor.UsageRow {
	t.Helper()
	rows, err := fx.usage.Summary(context.Background(), modelgovernor.UsageQuery{Owner: owner, FromDay: "2000-01-01", ToDay: "2999-01-01"})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func tokens(n int64) *int64 { return &n }

func TestUsageIsRecordedForSuccessAndFailureAndTokensAreNeverEstimated(t *testing.T) {
	fx := newFixture(modelgovernor.Config{})
	call := assistantCall
	call.Prompt = []byte("twelve bytes")
	_, err := fx.governor.Do(as("ada"), call, nil, func(context.Context) (modelgovernor.Result, error) {
		fx.clock.Advance(250 * time.Millisecond)
		return modelgovernor.Result{Response: []byte("reply"), ProviderModel: "m-2026", PromptTokens: tokens(11), CompletionTokens: tokens(3)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cli := modelgovernor.Call{Capability: modelgovernor.CapabilityPlanning, Transport: modelgovernor.TransportCLI, ConfiguredModel: "local", Prompt: []byte("p")}
	if _, err := fx.governor.Do(as("ada"), cli, nil, reply("a long reply with no usage object")); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.governor.Do(as("ada"), call, modelgovernor.ClassifyHTTP, func(context.Context) (modelgovernor.Result, error) {
		return modelgovernor.Result{}, fmt.Errorf("%w: status 401", modelhttp.ErrUnauthorized)
	}); err == nil {
		t.Fatal("the failing call did not fail")
	}
	rows := usageRows(t, fx, "ada")
	if len(rows) != 3 {
		t.Fatalf("rows = %+v", rows)
	}
	by := map[string]modelgovernor.UsageRow{}
	for _, row := range rows {
		by[row.Capability+"/"+row.Outcome] = row
	}
	ok := by["assistant/ok"]
	if ok.Transport != "http" || ok.ConfiguredModel != "m" || ok.ProviderModel != "m-2026" || ok.Calls != 1 || ok.Attempts != 1 ||
		ok.PromptBytes != 12 || ok.ResponseBytes != 5 || ok.LatencyMillis != 250 ||
		ok.PromptTokens == nil || *ok.PromptTokens != 11 || ok.CompletionTokens == nil || *ok.CompletionTokens != 3 || ok.Day != "2026-11-17" {
		t.Fatalf("the successful call was metered as %+v", ok)
	}
	if planned := by["planning/ok"]; planned.Transport != "cli" || planned.PromptTokens != nil || planned.CompletionTokens != nil ||
		planned.CallsWithTokens != 0 || planned.ResponseBytes != int64(len("a long reply with no usage object")) {
		t.Fatalf("a call with no usage object was metered as %+v: tokens must be null, not estimated", planned)
	}
	if failed := by["assistant/auth_error"]; failed.Calls != 1 || failed.Attempts != 1 || failed.PromptBytes != 12 || failed.ResponseBytes != 0 {
		t.Fatalf("the failed call was metered as %+v", failed)
	}
}

func TestTheDailyBudgetRefusesAtTheLimitAndResetsAtUTCMidnight(t *testing.T) {
	fx := newFixture(modelgovernor.Config{DailyTokenBudget: 100})
	// 23:30 UTC on the 17th, which is already the 18th east of UTC.
	fx.clock.Set(time.Date(2026, 11, 17, 23, 30, 0, 0, time.FixedZone("east", 5*3600)).Add(5 * time.Hour))
	spend := func(owner string, n int64) error {
		_, err := fx.governor.Do(as(owner), assistantCall, nil, func(context.Context) (modelgovernor.Result, error) {
			return modelgovernor.Result{Response: []byte("r"), PromptTokens: tokens(n), CompletionTokens: tokens(0)}, nil
		})
		return err
	}
	if err := spend("ada", 60); err != nil {
		t.Fatal(err)
	}
	// 60 of 100 is under the limit, so this call runs and takes ada past it.
	if err := spend("ada", 60); err != nil {
		t.Fatalf("a call under the limit was refused: %v", err)
	}
	called := false
	_, err := fx.governor.Do(as("ada"), assistantCall, nil, func(context.Context) (modelgovernor.Result, error) {
		called = true
		return modelgovernor.Result{}, nil
	})
	var budget *modelgovernor.BudgetError
	if !errors.As(err, &budget) || !errors.Is(err, modelgovernor.ErrBudgetExceeded) || called {
		t.Fatalf("err = %v, called = %v: want a budget refusal before the call", err, called)
	}
	if budget.Unit != "tokens" || budget.Limit != 100 || budget.Used != 120 || budget.RetryAfter != 30*time.Minute {
		t.Fatalf("refusal = %+v, want 120 of 100 tokens, resetting in 30m", budget)
	}
	if err := spend("grace", 1); err != nil {
		t.Fatalf("one owner's budget refused another: %v", err)
	}
	standing, err := fx.governor.BudgetFor(context.Background(), "ada")
	if err != nil || standing.RemainingTokens == nil || *standing.RemainingTokens != 0 || standing.Used.Tokens != 120 ||
		!standing.ResetsAt.Equal(time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)) || standing.RemainingCalls != nil {
		t.Fatalf("standing = %+v, err = %v", standing, err)
	}
	// One minute past UTC midnight the budget is whole again.
	fx.clock.Advance(31 * time.Minute)
	if err := spend("ada", 10); err != nil {
		t.Fatalf("the budget did not reset at UTC midnight: %v", err)
	}
	// The refusal was metered and did not itself count toward the budget.
	refused := 0
	for _, row := range usageRows(t, fx, "ada") {
		if row.Outcome == "budget_exceeded" {
			refused += int(row.Calls)
		}
	}
	if refused != 1 {
		t.Fatalf("%d budget refusals were metered, want 1", refused)
	}
}

func TestATokenBudgetSpentExactlyRefusesTheNextCall(t *testing.T) {
	fx := newFixture(modelgovernor.Config{DailyTokenBudget: 100})
	if _, err := fx.governor.Do(as("ada"), assistantCall, nil, func(context.Context) (modelgovernor.Result, error) {
		return modelgovernor.Result{PromptTokens: tokens(70), CompletionTokens: tokens(30)}, nil
	}); err != nil {
		t.Fatal(err)
	}
	_, err := fx.governor.Do(as("ada"), assistantCall, nil, func(context.Context) (modelgovernor.Result, error) {
		t.Fatal("a call was made with exactly the budget spent")
		return modelgovernor.Result{}, nil
	})
	if !errors.Is(err, modelgovernor.ErrBudgetExceeded) {
		t.Fatalf("err = %v with 100 of 100 tokens used", err)
	}
}

func TestACallBudgetBindsWhenNoTokensAreReported(t *testing.T) {
	fx := newFixture(modelgovernor.Config{DailyCallBudget: 2, DailyTokenBudget: 1000})
	for range 2 {
		if _, err := fx.governor.Do(as("ada"), assistantCall, nil, reply("no usage object")); err != nil {
			t.Fatal(err)
		}
	}
	_, err := fx.governor.Do(as("ada"), assistantCall, nil, reply("x"))
	var budget *modelgovernor.BudgetError
	if !errors.As(err, &budget) || budget.Unit != "calls" || budget.Used != 2 {
		t.Fatalf("err = %v, want a call-budget refusal", err)
	}
}

func TestABudgetIsSharedByTwoGovernorsOverOneStore(t *testing.T) {
	first := newFixture(modelgovernor.Config{DailyCallBudget: 3})
	second := modelgovernor.New(modelgovernor.Config{DailyCallBudget: 3}, modelgovernor.Dependencies{
		Usage: first.usage, Clock: first.clock, Metrics: modelgovernor.NewMetrics(),
	})
	for _, governor := range []*modelgovernor.Governor{first.governor, second, first.governor} {
		if _, err := governor.Do(as("ada"), assistantCall, nil, reply("x")); err != nil {
			t.Fatal(err)
		}
	}
	for _, governor := range []*modelgovernor.Governor{first.governor, second} {
		if _, err := governor.Do(as("ada"), assistantCall, nil, reply("x")); !errors.Is(err, modelgovernor.ErrBudgetExceeded) {
			t.Fatalf("a replica allowed a fourth call: %v", err)
		}
	}
}

type brokenUsage struct{ modelgovernor.UsageStore }

func (brokenUsage) OwnerDay(context.Context, string, string) (modelgovernor.Totals, error) {
	return modelgovernor.Totals{}, errors.New("database is down")
}
func (brokenUsage) Record(context.Context, modelgovernor.UsageRecord) error {
	return errors.New("database is down")
}

func TestABudgetThatCannotBeReadRefusesAndAnUnbudgetedCallSurvivesAUsageOutage(t *testing.T) {
	budgeted := newFixture(modelgovernor.Config{DailyCallBudget: 5}, func(deps *modelgovernor.Dependencies) { deps.Usage = brokenUsage{} })
	if _, err := budgeted.governor.Do(as("ada"), assistantCall, nil, reply("x")); !errors.Is(err, modelgovernor.ErrUsageUnavailable) {
		t.Fatalf("a budget that could not be read returned %v", err)
	}
	if modelgovernor.RunFrom(as("ada")).Refusal() != nil {
		t.Fatal("a fresh run carries a refusal")
	}
	open := newFixture(modelgovernor.Config{}, func(deps *modelgovernor.Dependencies) { deps.Usage = brokenUsage{} })
	if _, err := open.governor.Do(as("ada"), assistantCall, nil, reply("x")); err != nil {
		t.Fatalf("a usage write failure failed the call: %v", err)
	}
	if open.metrics.UsageWriteFailures() != 1 {
		t.Fatalf("usage write failures = %d, want 1", open.metrics.UsageWriteFailures())
	}
}

func TestMetricsAreKeyedByClosedSetsOnly(t *testing.T) {
	fx := newFixture(modelgovernor.Config{GlobalConcurrency: 1})
	fx.clock.auto = true
	_, _ = fx.governor.Do(as("ada@tenant.example"), assistantCall, nil, func(context.Context) (modelgovernor.Result, error) {
		return modelgovernor.Result{Response: []byte("r"), ProviderModel: "provider-said-this", PromptTokens: tokens(5), CompletionTokens: tokens(2)}, nil
	})
	attempts := 0
	_, _ = fx.governor.Do(as("grace"), assistantCall, modelgovernor.ClassifyHTTP, func(context.Context) (modelgovernor.Result, error) {
		attempts++
		return modelgovernor.Result{}, fmt.Errorf("%w: status 500", modelhttp.ErrUpstream)
	})
	closed := map[string]bool{}
	for _, outcome := range modelgovernor.Outcomes() {
		closed[string(outcome)] = true
	}
	samples := fx.metrics.Snapshot()
	if len(samples) != 2 {
		t.Fatalf("samples = %+v", samples)
	}
	for _, sample := range samples {
		if sample.Capability != "assistant" || sample.Transport != "http" || !closed[sample.Outcome] {
			t.Fatalf("a series is keyed outside the closed sets: %+v", sample.MetricKey)
		}
		if text := fmt.Sprintf("%+v", sample); strings.Contains(text, "ada") || strings.Contains(text, "grace") || strings.Contains(text, "provider-said-this") {
			t.Fatalf("a sample carries an owner or a provider-reported model: %s", text)
		}
	}
	ok, failed := samples[0], samples[1]
	if ok.Outcome != "ok" || ok.Calls != 1 || ok.Retries != 0 || ok.PromptTokens != 5 || ok.CompletionTokens != 2 {
		t.Fatalf("ok series = %+v", ok)
	}
	if failed.Outcome != "upstream_error" || failed.Calls != 1 || failed.Retries != 2 {
		t.Fatalf("failed series = %+v", failed)
	}
}

func TestFromEnvDefaultsAndRefusals(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(name string) string { return values[name] }
	}
	config, err := modelgovernor.FromEnv(env(nil))
	if err != nil || config.Transcripts || config.DailyTokenBudget != 0 || config.DailyCallBudget != 0 {
		t.Fatalf("defaults = %+v, err = %v: transcripts off and budgets unlimited", config, err)
	}
	applied := modelgovernor.New(config, modelgovernor.Dependencies{}).Config()
	if applied.GlobalConcurrency != 8 || applied.OwnerConcurrency != 2 || applied.QueueWait != 2*time.Second || applied.MaxAttempts != 3 {
		t.Fatalf("applied defaults = %+v", applied)
	}
	config, err = modelgovernor.FromEnv(env(map[string]string{
		"CONDUIT_MODEL_MAX_CONCURRENCY": "4", "CONDUIT_MODEL_MAX_CONCURRENCY_PER_OWNER": "1", "CONDUIT_MODEL_QUEUE_WAIT": "500ms",
		"CONDUIT_MODEL_MAX_ATTEMPTS": "2", "CONDUIT_MODEL_DAILY_TOKEN_BUDGET": "1000", "CONDUIT_MODEL_DAILY_CALL_BUDGET": "50",
		"CONDUIT_AI_TRANSCRIPTS": "on", "CONDUIT_AI_TRANSCRIPT_RETENTION": "720h",
	}))
	if err != nil || config.GlobalConcurrency != 4 || config.OwnerConcurrency != 1 || config.QueueWait != 500*time.Millisecond ||
		config.MaxAttempts != 2 || config.DailyTokenBudget != 1000 || config.DailyCallBudget != 50 || !config.Transcripts ||
		config.TranscriptRetention != 720*time.Hour {
		t.Fatalf("config = %+v, err = %v", config, err)
	}
	for name, values := range map[string]map[string]string{
		"transcripts on with no retention": {"CONDUIT_AI_TRANSCRIPTS": "on"},
		"an unknown transcript mode":       {"CONDUIT_AI_TRANSCRIPTS": "yes"},
		"a zero budget":                    {"CONDUIT_MODEL_DAILY_TOKEN_BUDGET": "0"},
		"a negative concurrency":           {"CONDUIT_MODEL_MAX_CONCURRENCY": "-1"},
		"a retention that is not a time":   {"CONDUIT_AI_TRANSCRIPTS": "on", "CONDUIT_AI_TRANSCRIPT_RETENTION": "30d"},
	} {
		if _, err := modelgovernor.FromEnv(env(values)); !errors.Is(err, modelgovernor.ErrInvalidConfig) {
			t.Errorf("%s was accepted: %v", name, err)
		}
	}
}

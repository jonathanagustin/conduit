// Package modelgovernor is the one wrapper around every model call. A
// capability's port adapter still builds the prompt and parses the reply; the
// call itself passes through Governor.Do, which bounds concurrency, retries a
// failure that is safe to retry, meters usage, enforces a daily budget, writes
// one audit event per call, and keeps an optional transcript.
//
// It sits at the transport, below the port adapters, because that is the only
// place the exact prompt bytes exist for every capability. It does not approve
// a change and it does not read a model reply.
package modelgovernor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"sync"
	"time"
)

// Capability is which part of Conduit made the call. The set is closed: it is
// a metric label.
type Capability string

const (
	CapabilityAssistant    Capability = "assistant"
	CapabilityPlanning     Capability = "planning"
	CapabilityAppgen       Capability = "appgen"
	CapabilityIntelligence Capability = "intelligence"
	CapabilityPackaging    Capability = "packaging"
)

// Transport is how the model was reached. Closed, for the same reason.
type Transport string

const (
	TransportHTTP Transport = "http"
	TransportCLI  Transport = "cli"
)

// Outcome is the class a call ended in. Closed: it is a metric label and a
// usage key, so an error message never becomes one.
type Outcome string

const (
	OutcomeOK              Outcome = "ok"
	OutcomeTimeout         Outcome = "timeout"
	OutcomeRateLimited     Outcome = "rate_limited"
	OutcomeUpstream        Outcome = "upstream_error"
	OutcomeTransport       Outcome = "transport_error"
	OutcomeAuth            Outcome = "auth_error"
	OutcomeInvalidResponse Outcome = "invalid_response"
	OutcomeCanceled        Outcome = "canceled"
	OutcomeError           Outcome = "error"
	// The following outcomes are decided here, not by a provider.
	OutcomeThrottled        Outcome = "throttled"
	OutcomeBudgetExceeded   Outcome = "budget_exceeded"
	OutcomeAuditFailed      Outcome = "audit_failed"
	OutcomeUsageUnavailable Outcome = "usage_unavailable"
)

// Outcomes is every value of Outcome.
func Outcomes() []Outcome {
	return []Outcome{
		OutcomeOK, OutcomeTimeout, OutcomeRateLimited, OutcomeUpstream, OutcomeTransport, OutcomeAuth,
		OutcomeInvalidResponse, OutcomeCanceled, OutcomeError, OutcomeThrottled, OutcomeBudgetExceeded, OutcomeAuditFailed, OutcomeUsageUnavailable,
	}
}

// Capabilities is every value of Capability.
func Capabilities() []Capability {
	return []Capability{CapabilityAssistant, CapabilityPlanning, CapabilityAppgen, CapabilityIntelligence, CapabilityPackaging}
}

// SystemOwner is the owner of a call nobody is signed in for: a worker, or a
// context that carries no operator.
const SystemOwner = "system:unattributed"

var (
	// ErrBusy is a call refused because a concurrency limit stayed full for
	// the whole queue wait. *BusyError matches it.
	ErrBusy = errors.New("model concurrency limit reached")
	// ErrBudgetExceeded is a call refused because the owner's daily budget is
	// spent. *BudgetError matches it.
	ErrBudgetExceeded = errors.New("daily model budget exceeded")
	// ErrAuditUnavailable is a call whose audit event could not be written.
	// The reply is discarded: an unaudited model exchange is not returned.
	ErrAuditUnavailable = errors.New("model call audit is unavailable")
	// ErrUsageUnavailable is a call refused because a budget is configured and
	// the usage it is checked against could not be read.
	ErrUsageUnavailable = errors.New("model usage is unavailable")
)

// BusyError carries how long to wait before trying again.
type BusyError struct {
	// Scope is "owner" or "global": which limit was full.
	Scope      string
	RetryAfter time.Duration
}

func (err *BusyError) Error() string {
	return fmt.Sprintf("%v (%s); retry after %s", ErrBusy, err.Scope, err.RetryAfter)
}
func (err *BusyError) Is(target error) bool          { return target == ErrBusy }
func (err *BusyError) RetryAfterHint() time.Duration { return err.RetryAfter }

// BudgetError says which budget was spent and when it resets.
type BudgetError struct {
	// Unit is "tokens" or "calls".
	Unit       string
	Limit      int64
	Used       int64
	RetryAfter time.Duration
}

func (err *BudgetError) Error() string {
	return fmt.Sprintf("%v: %d of %d %s used today (UTC); resets in %s",
		ErrBudgetExceeded, err.Used, err.Limit, err.Unit, err.RetryAfter.Round(time.Second))
}
func (err *BudgetError) Is(target error) bool          { return target == ErrBudgetExceeded }
func (err *BudgetError) RetryAfterHint() time.Duration { return err.RetryAfter }

// Call is one model call as the transport sees it.
type Call struct {
	Capability      Capability
	Transport       Transport
	ConfiguredModel string
	// Prompt is the exact bytes that will be sent.
	Prompt []byte
}

// Result is what the transport got back. Response is returned even beside an
// error when the transport has one, so it can be digested.
type Result struct {
	Response      []byte
	ProviderModel string
	// Token counts are the provider's own; nil when it reported none.
	PromptTokens     *int64
	CompletionTokens *int64
}

// Verdict classifies one failed attempt.
type Verdict struct {
	Outcome   Outcome
	Retryable bool
	// RetryAfter is the provider's own delay, or zero.
	RetryAfter time.Duration
}

// Clock is the time source. Tests inject one.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time                         { return time.Now() }
func (systemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Config is the whole configuration. FromEnv reads it; zero values take the
// defaults named beside each field.
type Config struct {
	// GlobalConcurrency is how many calls run at once in this process.
	// Default 8.
	GlobalConcurrency int
	// OwnerConcurrency is how many of those one owner may hold. Default 2.
	OwnerConcurrency int
	// QueueWait is how long a call waits for a slot before it is refused.
	// Default 2s.
	QueueWait time.Duration
	// MaxAttempts counts the first try. Default 3.
	MaxAttempts int
	// BackoffBase doubles per attempt, with jitter. Default 500ms.
	BackoffBase time.Duration
	// RetryDelayCap bounds every wait between attempts, a provider's
	// Retry-After included. Default 30s.
	RetryDelayCap time.Duration
	// DailyTokenBudget and DailyCallBudget are per owner per UTC day. Zero is
	// unlimited.
	DailyTokenBudget int64
	DailyCallBudget  int64
	// Transcripts keeps prompt and response bytes. Off by default.
	Transcripts         bool
	TranscriptRetention time.Duration
	// TranscriptRecordBytes and TranscriptRunBytes cap what is kept. Defaults
	// 2 MiB and 16 MiB.
	TranscriptRecordBytes int64
	TranscriptRunBytes    int64
	// AuditRequired refuses a call when there is no Auditor. The Hub sets it.
	AuditRequired bool
}

const (
	DefaultGlobalConcurrency           = 8
	DefaultOwnerConcurrency            = 2
	DefaultQueueWait                   = 2 * time.Second
	DefaultMaxAttempts                 = 3
	DefaultBackoffBase                 = 500 * time.Millisecond
	DefaultRetryDelayCap               = 30 * time.Second
	DefaultTranscriptRecordBytes int64 = 2 << 20
	DefaultTranscriptRunBytes    int64 = 16 << 20
)

func (config Config) withDefaults() Config {
	if config.GlobalConcurrency <= 0 {
		config.GlobalConcurrency = DefaultGlobalConcurrency
	}
	if config.OwnerConcurrency <= 0 {
		config.OwnerConcurrency = DefaultOwnerConcurrency
	}
	if config.QueueWait <= 0 {
		config.QueueWait = DefaultQueueWait
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = DefaultMaxAttempts
	}
	if config.BackoffBase <= 0 {
		config.BackoffBase = DefaultBackoffBase
	}
	if config.RetryDelayCap <= 0 {
		config.RetryDelayCap = DefaultRetryDelayCap
	}
	if config.TranscriptRecordBytes <= 0 {
		config.TranscriptRecordBytes = DefaultTranscriptRecordBytes
	}
	if config.TranscriptRunBytes <= 0 {
		config.TranscriptRunBytes = DefaultTranscriptRunBytes
	}
	return config
}

// Dependencies are the ports. Every one may be nil: a nil store is not
// written, a nil Auditor is refused only under Config.AuditRequired.
type Dependencies struct {
	Usage       UsageStore
	Transcripts TranscriptStore
	Audit       Auditor
	Metrics     *Metrics
	Clock       Clock
	// Jitter returns a value in [0,1). Tests inject one.
	Jitter func() float64
	// Redact is applied to what a transcript stores. Nil stores bytes as sent.
	Redact func([]byte) []byte
	Logger *slog.Logger
}

// Governor is safe for concurrent use. One is shared by every capability in a
// process: the global limit is only global if they share it.
type Governor struct {
	config Config
	deps   Dependencies
	global chan struct{}

	mu             sync.Mutex
	owners         map[string]*ownerSlots
	usageMu        sync.Mutex
	unacknowledged map[usageScope]UsageRecord
}

type usageScope struct{ owner, day string }

type ownerSlots struct {
	slots chan struct{}
	users int
}

// New builds a governor.
func New(config Config, deps Dependencies) *Governor {
	config = config.withDefaults()
	if deps.Clock == nil {
		deps.Clock = systemClock{}
	}
	if deps.Jitter == nil {
		deps.Jitter = rand.Float64
	}
	if deps.Metrics == nil {
		deps.Metrics = ProcessMetrics
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &Governor{
		config: config, deps: deps,
		global: make(chan struct{}, config.GlobalConcurrency), owners: map[string]*ownerSlots{},
		unacknowledged: map[usageScope]UsageRecord{},
	}
}

// Config is the configuration in force, defaults applied.
func (governor *Governor) Config() Config { return governor.config }

// Do runs one model call. classify names the class of a failed attempt;
// invoke makes one attempt and may be called more than once.
func (governor *Governor) Do(
	ctx context.Context, call Call, classify func(error) Verdict, invoke func(context.Context) (Result, error),
) (Result, error) {
	scope := scopeFrom(ctx)
	owner := scope.owner()
	run := scope.run
	if run == nil {
		run = NewRun()
	}
	started := governor.deps.Clock.Now()
	record := UsageRecord{
		Day: Day(started), Owner: owner, Capability: string(call.Capability), Transport: string(call.Transport),
		ConfiguredModel: call.ConfiguredModel, PromptBytes: int64(len(call.Prompt)),
	}
	refuse := func(outcome Outcome, err error) (Result, error) {
		record.Outcome = string(outcome)
		governor.finish(ctx, record, started)
		run.refuse(err)
		return Result{}, err
	}
	if err := governor.retryUsage(ctx, owner, Day(started)); err != nil {
		return refuse(OutcomeUsageUnavailable, err)
	}
	if err := governor.checkBudget(ctx, owner, started); err != nil {
		if errors.Is(err, ErrBudgetExceeded) {
			return refuse(OutcomeBudgetExceeded, err)
		}
		return refuse(OutcomeUsageUnavailable, err)
	}
	if governor.config.AuditRequired && governor.deps.Audit == nil {
		return refuse(OutcomeAuditFailed, fmt.Errorf("%w: no auditor is configured", ErrAuditUnavailable))
	}
	release, err := governor.acquire(ctx, owner)
	if err != nil {
		if errors.Is(err, ErrBusy) {
			return refuse(OutcomeThrottled, err)
		}
		record.Outcome = string(OutcomeCanceled)
		governor.finish(ctx, record, started)
		return Result{}, err
	}
	// Queued calls may have read usage before an earlier call spent it. Recheck
	// only after admission and retain capacity through audit and metering, so
	// waiting callers cannot race a completed call's usage write.
	defer release()
	admittedAt := governor.deps.Clock.Now()
	record.Day = Day(admittedAt)
	if err := governor.checkBudget(ctx, owner, admittedAt); err != nil {
		if errors.Is(err, ErrBudgetExceeded) {
			return refuse(OutcomeBudgetExceeded, err)
		}
		return refuse(OutcomeUsageUnavailable, err)
	}
	if err := governor.retryPendingAudit(ctx, run); err != nil {
		return refuse(OutcomeAuditFailed, fmt.Errorf("%w: %w", ErrAuditUnavailable, err))
	}
	if governor.budgeted() {
		receiptID, err := governor.beginUsage(ctx, owner, admittedAt)
		if err != nil {
			switch {
			case errors.Is(err, ErrBusy):
				return refuse(OutcomeThrottled, err)
			case errors.Is(err, ErrBudgetExceeded):
				return refuse(OutcomeBudgetExceeded, err)
			default:
				return refuse(OutcomeUsageUnavailable, err)
			}
		}
		record.ReceiptID = receiptID
	}
	result, outcome, attempts, callErr := governor.attempt(ctx, classify, invoke)

	record.Attempts = attempts
	record.Outcome = string(outcome)
	record.ProviderModel = result.ProviderModel
	record.ResponseBytes = int64(len(result.Response))
	record.PromptTokens, record.CompletionTokens = result.PromptTokens, result.CompletionTokens

	if auditErr := governor.audit(ctx, run, owner, call, result, outcome, attempts, started); auditErr != nil {
		record.Outcome = string(OutcomeAuditFailed)
		governor.finish(ctx, record, started)
		// The audit failure is what the caller is told, beside whatever the
		// model call itself did.
		if callErr != nil {
			return Result{}, fmt.Errorf("%w: %w (the model call had also failed: %w)", ErrAuditUnavailable, auditErr, callErr)
		}
		return Result{}, fmt.Errorf("%w: %w", ErrAuditUnavailable, auditErr)
	}
	governor.finish(ctx, record, started)
	return result, callErr
}

// attempt runs invoke until it succeeds, fails in a way that is not
// retryable, runs out of attempts, or would outlive the caller's deadline.
func (governor *Governor) attempt(
	ctx context.Context, classify func(error) Verdict, invoke func(context.Context) (Result, error),
) (Result, Outcome, int, error) {
	for attempt := 1; ; attempt++ {
		result, err := invoke(ctx)
		if err == nil {
			return result, OutcomeOK, attempt, nil
		}
		if ctx.Err() != nil {
			outcome := OutcomeCanceled
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				outcome = OutcomeTimeout
			}
			return result, outcome, attempt, err
		}
		verdict := Verdict{Outcome: OutcomeError}
		if classify != nil {
			verdict = classify(err)
		}
		if !verdict.Retryable || attempt >= governor.config.MaxAttempts {
			return result, verdict.Outcome, attempt, err
		}
		delay := governor.retryDelay(attempt, verdict.RetryAfter)
		if deadline, ok := ctx.Deadline(); ok && !governor.deps.Clock.Now().Add(delay).Before(deadline) {
			// The wait alone would use up the caller's time.
			return result, verdict.Outcome, attempt, err
		}
		select {
		case <-governor.deps.Clock.After(delay):
		case <-ctx.Done():
			return result, OutcomeCanceled, attempt, fmt.Errorf("%w: %w", ctx.Err(), err)
		}
	}
}

// retryDelay is the provider's Retry-After when it sent one, otherwise an
// exponential backoff with jitter in [half, full]. Both are capped.
func (governor *Governor) retryDelay(attempt int, retryAfter time.Duration) time.Duration {
	limit := governor.config.RetryDelayCap
	if retryAfter > 0 {
		return min(retryAfter, limit)
	}
	delay := governor.config.BackoffBase
	for step := 1; step < attempt && delay < limit; step++ {
		delay *= 2
	}
	delay = min(delay, limit)
	return delay/2 + time.Duration(governor.deps.Jitter()*float64(delay/2))
}

// acquire takes the owner's slot and then a global one. The owner's is first,
// so an owner at its own limit never holds a global slot while it waits.
func (governor *Governor) acquire(ctx context.Context, owner string) (func(), error) {
	wait := governor.deps.Clock.After(governor.config.QueueWait)
	slots := governor.ownerSlots(owner)
	select {
	case slots.slots <- struct{}{}:
	default:
		select {
		case slots.slots <- struct{}{}:
		case <-ctx.Done():
			governor.dropOwner(owner, slots)
			return nil, ctx.Err()
		case <-wait:
			governor.dropOwner(owner, slots)
			return nil, &BusyError{Scope: "owner", RetryAfter: governor.config.QueueWait}
		}
	}
	releaseOwner := func() {
		<-slots.slots
		governor.dropOwner(owner, slots)
	}
	select {
	case governor.global <- struct{}{}:
	default:
		select {
		case governor.global <- struct{}{}:
		case <-ctx.Done():
			releaseOwner()
			return nil, ctx.Err()
		case <-wait:
			releaseOwner()
			return nil, &BusyError{Scope: "global", RetryAfter: governor.config.QueueWait}
		}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			<-governor.global
			releaseOwner()
		})
	}, nil
}

func (governor *Governor) ownerSlots(owner string) *ownerSlots {
	governor.mu.Lock()
	defer governor.mu.Unlock()
	slots := governor.owners[owner]
	if slots == nil {
		slots = &ownerSlots{slots: make(chan struct{}, governor.config.OwnerConcurrency)}
		governor.owners[owner] = slots
	}
	slots.users++
	return slots
}

// dropOwner forgets an owner nobody is using, so the map is bounded by the
// calls in flight rather than by every owner ever seen.
func (governor *Governor) dropOwner(owner string, slots *ownerSlots) {
	governor.mu.Lock()
	defer governor.mu.Unlock()
	slots.users--
	if slots.users <= 0 && governor.owners[owner] == slots {
		delete(governor.owners, owner)
	}
}

// InFlight is how many calls hold a global slot, and how many owners are
// tracked. It is what a leak test reads.
func (governor *Governor) InFlight() (calls, owners int) {
	governor.mu.Lock()
	defer governor.mu.Unlock()
	return len(governor.global), len(governor.owners)
}

// checkBudget cheaply refuses an already-spent budget before queueing.
// BeginUsage's atomic totals are checked again with the durable reservation.
func (governor *Governor) checkBudget(ctx context.Context, owner string, now time.Time) error {
	if !governor.budgeted() {
		return nil
	}
	if governor.deps.Usage == nil {
		return fmt.Errorf("%w: a budget is configured and there is no usage store", ErrUsageUnavailable)
	}
	used, err := governor.deps.Usage.OwnerDay(ctx, owner, Day(now))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUsageUnavailable, err)
	}
	return governor.budgetError(used, now)
}

func (governor *Governor) budgeted() bool {
	return governor.config.DailyTokenBudget > 0 || governor.config.DailyCallBudget > 0
}

func (governor *Governor) budgetError(used Totals, now time.Time) error {
	tokens, calls := governor.config.DailyTokenBudget, governor.config.DailyCallBudget
	reset := NextDay(now).Sub(now)
	if tokens > 0 && used.Tokens >= tokens {
		return &BudgetError{Unit: "tokens", Limit: tokens, Used: used.Tokens, RetryAfter: reset}
	}
	if calls > 0 && used.Calls >= calls {
		return &BudgetError{Unit: "calls", Limit: calls, Used: used.Calls, RetryAfter: reset}
	}
	return nil
}

// finish records usage and metrics. A usage write that fails is logged and
// counted; it does not fail a call that already happened.
func (governor *Governor) finish(ctx context.Context, record UsageRecord, started time.Time) {
	record.Latency = governor.deps.Clock.Now().Sub(started)
	governor.deps.Metrics.observe(record)
	if governor.deps.Usage == nil {
		return
	}
	// The caller may already be cancelled; the call still happened.
	storeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := governor.deps.Usage.Record(storeCtx, record); err != nil {
		if record.ReceiptID != "" {
			// Freeze pointer values before the caller can reuse its Result.
			if record.PromptTokens != nil {
				value := *record.PromptTokens
				record.PromptTokens = &value
			}
			if record.CompletionTokens != nil {
				value := *record.CompletionTokens
				record.CompletionTokens = &value
			}
			governor.usageMu.Lock()
			governor.unacknowledged[usageScope{record.Owner, record.Day}] = record
			governor.usageMu.Unlock()
		}
		governor.deps.Metrics.usageWriteFailed()
		governor.deps.Logger.ErrorContext(ctx, "model usage was not acknowledged", "error", err,
			"capability", record.Capability, "outcome", record.Outcome)
	}
}

// beginUsage obtains a durable accounting reservation and checks the totals
// returned with it. Checking only before BeginUsage would admit stale totals
// when another replica finishes between the read and the reservation.
func (governor *Governor) beginUsage(ctx context.Context, owner string, now time.Time) (string, error) {
	receipt, err := newUsageReceipt(owner, Day(now))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUsageUnavailable, err)
	}
	used, err := governor.deps.Usage.BeginUsage(ctx, receipt)
	if errors.Is(err, ErrUsagePending) {
		return "", &BusyError{Scope: "owner accounting", RetryAfter: governor.config.QueueWait}
	}
	if err == nil {
		err = governor.budgetError(used, now)
	}
	if err == nil {
		return receipt.ID, nil
	}
	// No provider was invoked. Even an ambiguously committed begin may be
	// safely cancelled by this exact receipt ID. Failed cleanup stays closed.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if cleanupErr := governor.deps.Usage.CancelUsage(cleanup, receipt.ID); cleanupErr != nil {
		return "", fmt.Errorf("%w: release unused receipt: %w", ErrUsageUnavailable, cleanupErr)
	}
	if errors.Is(err, ErrBudgetExceeded) {
		return "", err
	}
	return "", fmt.Errorf("%w: %w", ErrUsageUnavailable, err)
}

// retryUsage uses the exact receipt and payload, so an acknowledgement lost
// after commit cannot increment the additive daily aggregate twice.
func (governor *Governor) retryUsage(ctx context.Context, owner, day string) error {
	if !governor.budgeted() {
		return nil
	}
	key := usageScope{owner, day}
	governor.usageMu.Lock()
	record, exists := governor.unacknowledged[key]
	governor.usageMu.Unlock()
	if !exists {
		return nil
	}
	storeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := governor.deps.Usage.Record(storeCtx, record); err != nil {
		return fmt.Errorf("%w: earlier model spend is not acknowledged: %w", ErrUsageUnavailable, err)
	}
	governor.usageMu.Lock()
	if governor.unacknowledged[key].ReceiptID == record.ReceiptID {
		delete(governor.unacknowledged, key)
	}
	governor.usageMu.Unlock()
	return nil
}

// audit writes the transcript, if one is kept, and then the audit event. The
// run is locked for both, so the chain is in the order the events are written.
func (governor *Governor) audit(
	ctx context.Context, run *Run, owner string, call Call, result Result, outcome Outcome, attempts int, started time.Time,
) error {
	run.mu.Lock()
	defer run.mu.Unlock()
	round, previous := run.round+1, run.chain
	if count := len(run.pendingAudit); count > 0 {
		last := run.pendingAudit[count-1]
		round, previous = last.Round+1, last.ChainSHA256
	}
	event := CallAudit{
		Owner: owner, Capability: string(call.Capability), Transport: string(call.Transport),
		RunID: run.id, SessionID: run.session, Round: round,
		PromptSHA256: Digest(call.Prompt), ResponseSHA256: Digest(result.Response),
		PreviousChainSHA256: previous,
		ConfiguredModel:     call.ConfiguredModel, ProviderModel: result.ProviderModel,
		Outcome: string(outcome), Attempts: attempts, At: governor.deps.Clock.Now().UTC(),
		Transcript: TranscriptOff,
	}
	event.ChainSHA256 = ChainDigest(event.PreviousChainSHA256, event.Round, event.PromptSHA256, event.ResponseSHA256)
	// The audit and the transcript outlive a cancelled request: the exchange
	// happened, and that is what they record.
	storeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if governor.config.Transcripts && governor.deps.Transcripts != nil {
		governor.keepTranscript(storeCtx, run, &event, call.Prompt, result.Response)
	}
	// Reserve this coordinate even when the audit acknowledgement fails. The
	// transcript may already exist, so reusing the round would bind a later
	// exchange to the earlier text. Retry the unchanged event instead.
	run.pendingAudit = append(run.pendingAudit, event)
	return governor.flushAuditLocked(storeCtx, run)
}

// retryPendingAudit repairs an interrupted acknowledgement before another
// model is invoked. Concurrent calls that already invoked still append unique
// pending rounds under audit's lock, and are flushed in the same order.
func (governor *Governor) retryPendingAudit(ctx context.Context, run *Run) error {
	run.mu.Lock()
	defer run.mu.Unlock()
	if len(run.pendingAudit) == 0 {
		return nil
	}
	storeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return governor.flushAuditLocked(storeCtx, run)
}

func (governor *Governor) flushAuditLocked(ctx context.Context, run *Run) error {
	for len(run.pendingAudit) > 0 {
		event := run.pendingAudit[0]
		if governor.deps.Audit != nil {
			if err := governor.deps.Audit.RecordModelCall(ctx, event); err != nil {
				return err
			}
		}
		run.round, run.chain = event.Round, event.ChainSHA256
		run.pendingAudit[0] = CallAudit{}
		run.pendingAudit = run.pendingAudit[1:]
	}
	return nil
}

// keepTranscript stores the redacted exchange and notes on the audit event
// what was kept. A transcript that cannot be kept does not fail the call: the
// audit event says so, and an export reports the gap.
func (governor *Governor) keepTranscript(ctx context.Context, run *Run, event *CallAudit, prompt, response []byte) {
	storedPrompt, storedResponse := prompt, response
	if governor.deps.Redact != nil {
		storedPrompt, storedResponse = governor.deps.Redact(prompt), governor.deps.Redact(response)
	}
	size := int64(len(storedPrompt) + len(storedResponse))
	if size > governor.config.TranscriptRecordBytes {
		event.Transcript = TranscriptOmittedRecordCap
		return
	}
	if run.transcriptBytes+size > governor.config.TranscriptRunBytes {
		event.Transcript = TranscriptOmittedRunCap
		return
	}
	record := TranscriptRecord{
		RunID: event.RunID, SessionID: event.SessionID, Owner: event.Owner, Capability: event.Capability, Round: event.Round,
		SentPromptSHA256: event.PromptSHA256, ReceivedResponseSHA256: event.ResponseSHA256,
		StoredPromptSHA256: Digest(storedPrompt), StoredResponseSHA256: Digest(storedResponse),
		Prompt: storedPrompt, Response: storedResponse,
		CreatedAt: event.At, ExpiresAt: event.At.Add(governor.config.TranscriptRetention),
	}
	err := governor.deps.Transcripts.Put(ctx, record, governor.config.TranscriptRunBytes)
	switch {
	case errors.Is(err, ErrTranscriptRunFull):
		event.Transcript = TranscriptOmittedRunCap
	case err != nil:
		event.Transcript = TranscriptFailed
		governor.deps.Logger.ErrorContext(ctx, "model transcript was not stored", "error", err, "runId", event.RunID)
	default:
		run.transcriptBytes += size
		event.Transcript = TranscriptStored
		// Recorded only when redaction changed the bytes: the audit log then
		// vouches for the redacted copy as well as for what was sent.
		if record.StoredPromptSHA256 != record.SentPromptSHA256 {
			event.StoredPromptSHA256 = record.StoredPromptSHA256
		}
		if record.StoredResponseSHA256 != record.ReceivedResponseSHA256 {
			event.StoredResponseSHA256 = record.StoredResponseSHA256
		}
	}
}

// Digest is "sha256:" and the hex digest, the form the audit log already uses.
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ChainDigest links one call to the one before it in the same run. The first
// call's previous digest is empty.
func ChainDigest(previous string, round int, promptSHA256, responseSHA256 string) string {
	return Digest([]byte(previous + "\n" + strconv.Itoa(round) + "\n" + promptSHA256 + "\n" + responseSHA256))
}

// Day is the UTC calendar day a budget and a usage row belong to.
func Day(at time.Time) string { return at.UTC().Format("2006-01-02") }

// NextDay is the UTC midnight after at, when a daily budget resets.
func NextDay(at time.Time) time.Time {
	at = at.UTC()
	return time.Date(at.Year(), at.Month(), at.Day()+1, 0, 0, 0, 0, time.UTC)
}

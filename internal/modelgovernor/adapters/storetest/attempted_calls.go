package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/modelgovernor"
)

type attemptAudit struct{ fail bool }

func (audit *attemptAudit) RecordModelCall(context.Context, modelgovernor.CallAudit) error {
	if audit.fail {
		return errors.New("injected audit failure")
	}
	return nil
}

type attemptClock struct{ queued chan struct{} }

func (*attemptClock) Now() time.Time { return epoch }
func (clock *attemptClock) After(time.Duration) <-chan time.Time {
	clock.queued <- struct{}{}
	return make(chan time.Time)
}

func attemptContext(owner string) context.Context {
	return modelgovernor.WithScope(context.Background(), modelgovernor.Scope{Owner: func() string { return owner }})
}

var attemptedCall = modelgovernor.Call{Capability: modelgovernor.CapabilityAssistant, Transport: modelgovernor.TransportHTTP}

func attemptedGovernor(store modelgovernor.UsageStore, audit modelgovernor.Auditor, clock modelgovernor.Clock, budget int64) *modelgovernor.Governor {
	return modelgovernor.New(modelgovernor.Config{AuditRequired: true, DailyCallBudget: budget, GlobalConcurrency: 1, OwnerConcurrency: 1}, modelgovernor.Dependencies{Usage: store, Audit: audit, Clock: clock, Metrics: modelgovernor.NewMetrics()})
}

// RunAttemptedCalls covers the distinction before aggregation: requests
// sharing an outcome may have invoked zero, one, or several transport attempts.
func RunAttemptedCalls(t *testing.T, open func(*testing.T) modelgovernor.UsageStore) {
	t.Run("mixed identical outcomes retain request and attempted-call counts", func(t *testing.T) {
		store := open(t)
		ctx := context.Background()
		for _, outcome := range []string{"audit_failed", "canceled"} {
			for _, attempts := range []int{0, 3} {
				record := modelgovernor.UsageRecord{Owner: "ada", Day: "2026-11-17", Capability: "assistant", Transport: "http", Outcome: outcome, Attempts: attempts}
				if err := store.Record(ctx, record); err != nil {
					t.Fatal(err)
				}
			}
		}
		used, err := store.OwnerDay(ctx, "ada", "2026-11-17")
		if err != nil || used.Calls != 2 {
			t.Fatalf("four requests with two attempted calls/6 attempts counted as %+v, %v", used, err)
		}
		rows, err := store.Summary(ctx, modelgovernor.UsageQuery{Owner: "ada", FromDay: "2026-11-17", ToDay: "2026-11-17"})
		if err != nil || len(rows) != 2 {
			t.Fatalf("mixed outcome rows=%+v err=%v", rows, err)
		}
		for _, row := range rows {
			if row.Calls != 2 || row.BudgetedCalls != 1 || row.Attempts != 3 {
				t.Fatalf("request/attempt evidence was collapsed: %+v", row)
			}
		}
	})
	t.Run("auditor preflight refusal leaves budget for a healthy retry", func(t *testing.T) {
		store := open(t)
		clock := &attemptClock{queued: make(chan struct{}, 10)}
		ctx := attemptContext("ada")
		invoked := 0
		invoke := func(context.Context) (modelgovernor.Result, error) { invoked++; return modelgovernor.Result{}, nil }
		if _, err := attemptedGovernor(store, nil, clock, 1).Do(ctx, attemptedCall, nil, invoke); !errors.Is(err, modelgovernor.ErrAuditUnavailable) {
			t.Fatal(err)
		}
		if invoked != 0 {
			t.Fatal("preflight called a provider")
		}
		if _, err := attemptedGovernor(store, &attemptAudit{}, clock, 1).Do(ctx, attemptedCall, nil, invoke); err != nil || invoked != 1 {
			t.Fatalf("healthy retry after zero provider calls: invocations=%d err=%v", invoked, err)
		}
	})
	t.Run("queue cancellation leaves budget for a healthy retry", func(t *testing.T) {
		store := open(t)
		clock := &attemptClock{queued: make(chan struct{}, 10)}
		governor := attemptedGovernor(store, &attemptAudit{}, clock, 1)
		grace, cancelGrace := context.WithCancel(attemptContext("grace"))
		t.Cleanup(cancelGrace)
		entered, release, first := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		go func() {
			_, err := governor.Do(grace, attemptedCall, nil, func(ctx context.Context) (modelgovernor.Result, error) {
				close(entered)
				select {
				case <-release:
					return modelgovernor.Result{}, nil
				case <-ctx.Done():
					return modelgovernor.Result{}, ctx.Err()
				}
			})
			first <- err
		}()
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("first provider did not start")
		}
		<-clock.queued
		ada, cancelAda := context.WithCancel(attemptContext("ada"))
		t.Cleanup(cancelAda)
		invoked := 0
		invoke := func(context.Context) (modelgovernor.Result, error) { invoked++; return modelgovernor.Result{}, nil }
		done := make(chan error, 1)
		go func() { _, err := governor.Do(ada, attemptedCall, nil, invoke); done <- err }()
		select {
		case <-clock.queued:
		case <-time.After(5 * time.Second):
			t.Fatal("second caller did not queue")
		}
		cancelAda()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("queue cancellation=%v", err)
		}
		close(release)
		if err := <-first; err != nil {
			t.Fatal(err)
		}
		if invoked != 0 {
			t.Fatal("cancelled waiter invoked a provider")
		}
		if _, err := governor.Do(attemptContext("ada"), attemptedCall, nil, invoke); err != nil || invoked != 1 {
			t.Fatalf("healthy retry after cancelled waiter: invocations=%d err=%v", invoked, err)
		}
	})
	t.Run("pending audit retry does not consume the remaining call", func(t *testing.T) {
		store := open(t)
		audit := &attemptAudit{fail: true}
		governor := attemptedGovernor(store, audit, &attemptClock{queued: make(chan struct{}, 10)}, 2)
		ctx := attemptContext("ada")
		invoked := 0
		invoke := func(context.Context) (modelgovernor.Result, error) { invoked++; return modelgovernor.Result{}, nil }
		for range 2 {
			if _, err := governor.Do(ctx, attemptedCall, nil, invoke); !errors.Is(err, modelgovernor.ErrAuditUnavailable) {
				t.Fatal(err)
			}
		}
		if invoked != 1 {
			t.Fatalf("pending audit retry invoked a provider: %d", invoked)
		}
		used, err := store.OwnerDay(ctx, "ada", "2026-11-17")
		if err != nil || used.Calls != 1 {
			t.Fatalf("one invoked call plus preflight retry spent %+v, %v", used, err)
		}
		audit.fail = false
		if _, err := governor.Do(ctx, attemptedCall, nil, invoke); err != nil || invoked != 2 {
			t.Fatalf("healthy retry lost remaining budget: invocations=%d err=%v", invoked, err)
		}
	})
	for _, failure := range []string{"canceled", "audit_failed"} {
		t.Run("genuine attempted "+failure+" still spends the call budget", func(t *testing.T) {
			store := open(t)
			audit := &attemptAudit{fail: failure == "audit_failed"}
			governor := attemptedGovernor(store, audit, &attemptClock{queued: make(chan struct{}, 10)}, 1)
			ctx := attemptContext("ada")
			invoked := 0
			invoke := func(context.Context) (modelgovernor.Result, error) {
				invoked++
				if failure == "canceled" {
					return modelgovernor.Result{}, context.Canceled
				}
				return modelgovernor.Result{}, nil
			}
			if _, err := governor.Do(ctx, attemptedCall, modelgovernor.ClassifyHTTP, invoke); err == nil {
				t.Fatal("injected genuine attempt failure was ignored")
			}
			audit.fail = false
			if _, err := governor.Do(ctx, attemptedCall, nil, invoke); !errors.Is(err, modelgovernor.ErrBudgetExceeded) || invoked != 1 {
				t.Fatalf("actual failed attempt was free: invocations=%d err=%v", invoked, err)
			}
		})
	}
}

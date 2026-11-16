package composition

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
)

func TestComputeGuardRunsOneExecutionAndReplaysTheReceipt(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	guard := &computeGuard{
		store: repository,
		lease: time.Hour,
		now:   func() time.Time { return now },
		inner: func(ctx context.Context, _ *planningdomain.Session) error {
			calls.Add(1)
			once.Do(func() { close(entered) })
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	session := acceptedComputeSession()
	done := make(chan error, 1)
	go func() {
		done <- guard.run(t.Context(), planningdomain.CloneSession(session))
	}()
	<-entered
	err := guard.run(t.Context(), planningdomain.CloneSession(session))
	if !errors.Is(err, planningdomain.ErrComputeInProgress) {
		t.Fatalf("second compute error = %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("compute executions = %d", calls.Load())
	}
	replayed := planningdomain.CloneSession(session)
	if err := guard.run(t.Context(), replayed); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("repeat rebuilt the session, executions = %d", calls.Load())
	}
	if replayed.ComputeReceipt == nil || replayed.ComputeReceipt.PlanDigest != session.Binding.Plan.SHA256 || replayed.ComputeReceipt.Outcome != "completed" {
		t.Fatalf("repeat did not return the receipt: %+v", replayed.ComputeReceipt)
	}
}

func TestCrashedComputeReservationCanBeReclaimed(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	session := acceptedComputeSession()
	if _, err := repository.ReserveComputeExecution(t.Context(), session.ID, now, time.Minute); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	guard := &computeGuard{
		store: repository,
		lease: time.Minute,
		now:   func() time.Time { return now.Add(2 * time.Minute) },
		inner: func(context.Context, *planningdomain.Session) error {
			calls.Add(1)
			return nil
		},
	}
	replayed := planningdomain.CloneSession(session)
	if err := guard.run(t.Context(), replayed); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || replayed.ComputeReceipt == nil {
		t.Fatalf("reclaim calls = %d receipt = %+v", calls.Load(), replayed.ComputeReceipt)
	}
}

func TestFailedComputeReleasesTheReservation(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	var calls atomic.Int32
	guard := &computeGuard{
		store: repository,
		lease: time.Minute,
		now:   time.Now,
		inner: func(context.Context, *planningdomain.Session) error {
			calls.Add(1)
			if calls.Load() == 1 {
				return errors.New("build failed")
			}
			return nil
		},
	}
	session := acceptedComputeSession()
	if err := guard.run(t.Context(), planningdomain.CloneSession(session)); err == nil {
		t.Fatal("expected the build to fail")
	}
	replayed := planningdomain.CloneSession(session)
	if err := guard.run(t.Context(), replayed); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || replayed.ComputeReceipt == nil {
		t.Fatalf("calls = %d receipt = %+v", calls.Load(), replayed.ComputeReceipt)
	}
}

func acceptedComputeSession() *planningdomain.Session {
	return &planningdomain.Session{
		ID:     "ps_accepted",
		Status: planningdomain.StatusAccepted,
		Binding: &planningdomain.Binding{
			Plan: planningdomain.Plan{Text: `{"summary":"catalog export"}`, SHA256: "digest-1"},
		},
	}
}

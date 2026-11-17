// Package casetest is the conformance suite every application.CaseStore
// runs. The memory store runs it on every `go test`; the PostgreSQL adapter
// runs it when CONDUIT_TEST_POSTGRES_DSN names a server.
package casetest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/jonathanagustin/conduit/internal/intelligence/application"
)

func item(n int) application.Case {
	return application.Case{
		ID: fmt.Sprintf("case-%04d", n), Scenario: fmt.Sprintf("scenario %d", n), BoundObject: "sample-app-1",
		Value: "512Mi", ExpectedOutcome: "PASS", Source: "operator feedback",
	}
}

// Run exercises one adapter. open returns a handle on the same underlying
// store every time it is called within one subtest, the way a restarted Hub
// reopens its database. A store with nothing durable behind it reports
// durable=false and is not asked to survive a reopen.
func Run(t *testing.T, open func(t *testing.T) (reopen func() application.CaseStore, durable bool)) {
	ctx := context.Background()

	t.Run("cases list in the order they were added", func(t *testing.T) {
		reopen, _ := open(t)
		store := reopen()
		for _, n := range []int{3, 1, 2} {
			if _, err := store.Add(ctx, item(n), 10); err != nil {
				t.Fatal(err)
			}
		}
		got, err := store.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if want := []application.Case{item(3), item(1), item(2)}; !reflect.DeepEqual(got, want) {
			t.Fatalf("list = %+v, want %+v", got, want)
		}
	})

	t.Run("an empty store lists nothing and no error", func(t *testing.T) {
		reopen, _ := open(t)
		got, err := reopen().List(ctx)
		if err != nil || len(got) != 0 {
			t.Fatalf("list = %+v, %v", got, err)
		}
	})

	t.Run("adding a stored id returns the stored case unchanged", func(t *testing.T) {
		reopen, _ := open(t)
		store := reopen()
		if _, err := store.Add(ctx, item(1), 10); err != nil {
			t.Fatal(err)
		}
		changed := item(1)
		changed.Scenario = "a different scenario under the same id"
		got, err := store.Add(ctx, changed, 10)
		if err != nil || got != item(1) {
			t.Fatalf("second add = %+v, %v; want the first case back", got, err)
		}
		if listed, _ := store.List(ctx); len(listed) != 1 || listed[0] != item(1) {
			t.Fatalf("list after a repeated add = %+v", listed)
		}
	})

	t.Run("the limit refuses a new id and still returns a stored one", func(t *testing.T) {
		reopen, _ := open(t)
		store := reopen()
		for n := 0; n < 3; n++ {
			if _, err := store.Add(ctx, item(n), 3); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := store.Add(ctx, item(99), 3); !errors.Is(err, application.ErrCaseLimit) {
			t.Fatalf("add past the limit = %v, want ErrCaseLimit", err)
		}
		if got, err := store.Add(ctx, item(1), 3); err != nil || got != item(1) {
			t.Fatalf("re-adding a stored id at the limit = %+v, %v", got, err)
		}
		if listed, _ := store.List(ctx); len(listed) != 3 {
			t.Fatalf("%d cases are stored, want 3", len(listed))
		}
	})

	t.Run("concurrent adds never exceed the limit", func(t *testing.T) {
		reopen, _ := open(t)
		store := reopen()
		const limit, writers = 5, 20
		var wait sync.WaitGroup
		refused := make(chan error, writers)
		for n := 0; n < writers; n++ {
			wait.Add(1)
			go func(n int) {
				defer wait.Done()
				if _, err := store.Add(ctx, item(n), limit); err != nil {
					refused <- err
				}
			}(n)
		}
		wait.Wait()
		close(refused)
		count := 0
		for err := range refused {
			if !errors.Is(err, application.ErrCaseLimit) {
				t.Fatalf("unexpected error: %v", err)
			}
			count++
		}
		listed, err := store.List(ctx)
		if err != nil || len(listed) != limit || count != writers-limit {
			t.Fatalf("%d stored and %d refused, want %d and %d (%v)", len(listed), count, limit, writers-limit, err)
		}
	})

	t.Run("cases survive a reopen", func(t *testing.T) {
		reopen, durable := open(t)
		if !durable {
			t.Skip("this store keeps nothing outside the process")
		}
		first := reopen()
		for _, n := range []int{2, 1} {
			if _, err := first.Add(ctx, item(n), 10); err != nil {
				t.Fatal(err)
			}
		}
		if closer, ok := first.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				t.Fatal(err)
			}
		}
		second := reopen()
		got, err := second.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if want := []application.Case{item(2), item(1)}; !reflect.DeepEqual(got, want) {
			t.Fatalf("after reopen list = %+v, want %+v", got, want)
		}
		// The limit counts what was stored before the reopen.
		if _, err := second.Add(ctx, item(3), 2); !errors.Is(err, application.ErrCaseLimit) {
			t.Fatalf("add past the limit after reopen = %v", err)
		}
	})
}

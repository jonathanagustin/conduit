// Package storetest is the contract every generation store must meet. The
// memory adapter and the PostgreSQL adapter both run it.
package storetest

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jonathanagustin/conduit/internal/appgen/application"
	"github.com/jonathanagustin/conduit/internal/appgen/domain"
)

// Run checks one empty store. open is called once per subtest and must
// return a store that holds no result.
func Run(t *testing.T, open func(t *testing.T) application.Store) {
	t.Helper()
	ctx := context.Background()
	full := application.Result{
		ID: "gen_0000000000000001", Label: domain.LabelLiveModel, Name: "hello-http", Description: "a small page",
		Owner: "ada", RecordedDemo: true, AttachmentSHA256: []string{"ab"},
		PlanRevisionID: "ps_accepted", PlanDigest: "0123abcd",
		Spec:       &domain.Spec{Name: "hello-http", Port: 8080, Files: []domain.SourceFile{{Path: "main.go", Language: "go", Content: "package main\n"}}},
		Gates:      []domain.Gate{{Name: application.GateBuild, Outcome: domain.OutcomeFail, Reason: "compile failed"}},
		Files:      []application.File{{Path: "main.go", Kind: "source", SHA256: "cd", Content: "package main\n"}},
		Provenance: domain.Provenance{Adapter: "fixture-model", PromptSHA256: "p", ResponseSHA256: "r"},
		Attempts: []application.Attempt{
			{Number: 1, Provenance: domain.Provenance{Adapter: "fixture-model"}, Error: "compile failed"},
			{Number: 2, Provenance: domain.Provenance{Adapter: "fixture-model"}},
		},
		Error: "compile failed",
	}

	t.Run("a saved result is read back whole", func(t *testing.T) {
		store := open(t)
		if err := store.Save(ctx, full); err != nil {
			t.Fatal(err)
		}
		got, ok, err := store.Get(ctx, full.ID)
		if err != nil || !ok {
			t.Fatalf("get = %v %v", ok, err)
		}
		if !reflect.DeepEqual(got, full) {
			t.Fatalf("read back\n%+v\nwant\n%+v", got, full)
		}
		if _, ok, err := store.Get(ctx, "gen_missing"); err != nil || ok {
			t.Fatalf("missing id: get = %v %v", ok, err)
		}
	})

	t.Run("a list holds one owner's results in the order they were first saved", func(t *testing.T) {
		store := open(t)
		for _, item := range []application.Result{
			{ID: "gen_a1", Owner: "ada", Name: "first"},
			{ID: "gen_b1", Owner: "bea", Name: "other"},
			{ID: "gen_a2", Owner: "ada", Name: "second"},
			{ID: "gen_legacy", Owner: "", Name: "unowned"},
		} {
			if err := store.Save(ctx, item); err != nil {
				t.Fatal(err)
			}
		}
		// Saving again replaces the result and keeps its place.
		if err := store.Save(ctx, application.Result{ID: "gen_a1", Owner: "ada", Name: "first again"}); err != nil {
			t.Fatal(err)
		}
		ada, err := store.List(ctx, "ada")
		if err != nil {
			t.Fatal(err)
		}
		if len(ada) != 2 || ada[0].ID != "gen_a1" || ada[0].Name != "first again" || ada[1].ID != "gen_a2" {
			t.Fatalf("ada list = %+v", ada)
		}
		bea, err := store.List(ctx, "bea")
		if err != nil || len(bea) != 1 || bea[0].ID != "gen_b1" {
			t.Fatalf("bea list = %+v %v", bea, err)
		}
		for _, item := range append(ada, bea...) {
			if item.ID == "gen_legacy" {
				t.Fatal("an unowned result was listed")
			}
		}
		for _, nobody := range []string{"", "  ", "cy", "ADA", "ada "} {
			listed, err := store.List(ctx, nobody)
			if err != nil || len(listed) != 0 {
				t.Fatalf("list for %q = %+v %v, want none", nobody, listed, err)
			}
		}
	})

	t.Run("another owner cannot replace a result", func(t *testing.T) {
		store := open(t)
		if err := store.Save(ctx, application.Result{ID: "gen_a1", Owner: "ada", Name: "ada's"}); err != nil {
			t.Fatal(err)
		}
		err := store.Save(ctx, application.Result{ID: "gen_a1", Owner: "bea", Name: "bea's"})
		if !errors.Is(err, application.ErrOwnerConflict) {
			t.Fatalf("save over another owner's id: err = %v, want ErrOwnerConflict", err)
		}
		got, ok, err := store.Get(ctx, "gen_a1")
		if err != nil || !ok || got.Owner != "ada" || got.Name != "ada's" {
			t.Fatalf("after the refused save: %+v %v %v", got, ok, err)
		}
		if listed, _ := store.List(ctx, "bea"); len(listed) != 0 {
			t.Fatalf("bea lists %+v", listed)
		}
	})
}

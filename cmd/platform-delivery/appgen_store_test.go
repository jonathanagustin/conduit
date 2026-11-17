package main

import (
	"context"
	"os"
	"testing"

	appgenapp "github.com/jonathanagustin/conduit/internal/appgen/application"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// The generation store is opened beside the other Hub stores, on the same
// DSN, and the one closer the executable defers closes it. A pool the Hub
// opened for itself would outlive that closer.
func TestPostgresAppGenerationStoreOpensAndClosesWithTheHubStores(t *testing.T) {
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is required for the PostgreSQL integration gate")
	}
	ctx := t.Context()
	base, err := controlplane.NewPostgresRepository(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	configuration := map[string]string{
		"REPOSITORY_KIND":   "postgres",
		"POSTGRES_DSN_FILE": writeFile(t, "postgres-dsn", []byte(dsn+"\n")),
	}
	_, closeStores, _, _, component, err := configureReconciliation(ctx,
		func(name string) string { return configuration[name] }, base)
	if err != nil {
		closeStores()
		t.Fatal(err)
	}
	store := appGenerationsFor(component)
	if store == nil {
		closeStores()
		t.Fatal("PostgreSQL mode handed the Hub no generation store, so results would stay in memory")
	}
	id := "gen_wiring_" + t.Name()
	if err := store.Save(ctx, appgenapp.Result{ID: id, Owner: "ada", Name: "hello-http", PlanRevisionID: "ps_1", PlanDigest: "d"}); err != nil {
		closeStores()
		t.Fatal(err)
	}
	got, ok, err := store.Get(ctx, id)
	if err != nil || !ok || got.Owner != "ada" || got.PlanRevisionID != "ps_1" || got.PlanDigest != "d" {
		closeStores()
		t.Fatalf("read back %+v %v %v", got, ok, err)
	}
	if listed, err := store.List(ctx, "bea"); err != nil || len(listed) != 0 {
		closeStores()
		t.Fatalf("another owner lists %+v %v", listed, err)
	}
	closeStores()
	if _, _, err := store.Get(context.Background(), id); err == nil {
		t.Fatal("the generation store still answers after the Hub's closer ran, so its pool was not closed")
	}
}

// File mode has no database. The Hub must get an untyped nil, which it reads
// as "keep results in memory", and not a typed nil it would call.
func TestAppGenerationStoreIsAbsentInFileMode(t *testing.T) {
	if store := appGenerationsFor(notificationComponent{}); store != nil {
		t.Fatalf("file mode produced a store: %#v", store)
	}
}

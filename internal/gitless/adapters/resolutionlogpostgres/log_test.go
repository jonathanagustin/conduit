package resolutionlogpostgres_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"sync"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/resolutionlogpostgres"
	"github.com/jonathanagustin/conduit/internal/gitless/domain"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

func testResolution(environment, digest string) domain.Resolution {
	return domain.Resolution{
		Environment: environment,
		Selector: domain.Selector{
			Kind: domain.ByChannel, Repository: "registry.example/foundry/runtime", Channel: "stable",
		},
		Pin:       domain.Pin{Repository: "registry.example/foundry/runtime", Digest: digest},
		Publisher: "https://github.com/example/foundry/.github/workflows/release.yml@refs/tags/v1.2.3",
	}
}

func TestMigrationContractIsValid(t *testing.T) {
	if err := pgmigrate.Validate(resolutionlogpostgres.Component(), resolutionlogpostgres.Migrations()); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyDataSourceNameIsRefused(t *testing.T) {
	if _, err := resolutionlogpostgres.New(t.Context(), " "); err == nil {
		t.Fatal("an empty data source name was accepted")
	}
}

func TestPostgresReplicasAppendToOneHistory(t *testing.T) {
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is not set")
	}
	first, err := resolutionlogpostgres.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := resolutionlogpostgres.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	environment := "postgres-shared-history"
	digestA := "sha256:aa11" + strings.Repeat("0", 60)
	digestB := "sha256:bb22" + strings.Repeat("0", 60)
	resolutions := []domain.Resolution{
		testResolution(environment, digestA),
		testResolution(environment, digestB),
	}
	resolutions[1].Superseded = digestA

	var wait sync.WaitGroup
	errors := make(chan error, 2)
	for index, log := range []*resolutionlogpostgres.Log{first, second} {
		wait.Add(1)
		go func(log *resolutionlogpostgres.Log, resolution domain.Resolution) {
			defer wait.Done()
			errors <- log.Record(context.Background(), resolution)
		}(log, resolutions[index])
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}

	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var count, moves int
	if err := database.QueryRowContext(t.Context(), `
SELECT count(*), count(*) FILTER (WHERE moved AND superseded <> '')
FROM conduit_gitless_resolutions
WHERE environment = $1`, environment).Scan(&count, &moves); err != nil {
		t.Fatal(err)
	}
	if count != 2 || moves != 1 {
		t.Fatalf("shared history has count=%d moves=%d, want 2 and 1", count, moves)
	}
}

func TestPostgresInvalidResolutionIsRefusedBeforeInsert(t *testing.T) {
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is not set")
	}
	log, err := resolutionlogpostgres.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	broken := testResolution("", "sha256:aa11"+strings.Repeat("0", 60))
	if err := log.Record(t.Context(), broken); err == nil {
		t.Fatal("an invalid resolution reached the database")
	}
}

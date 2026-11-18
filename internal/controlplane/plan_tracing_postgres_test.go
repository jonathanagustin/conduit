package controlplane

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/jonathanagustin/conduit/internal/observability/ports"
	"github.com/jonathanagustin/conduit/internal/pgtest"
)

func TestPostgresDecisionTraceSurvivesPoolReopen(t *testing.T) {
	dsn := os.Getenv(pgtest.DataSourceNameEnvironment)
	if dsn == "" {
		t.Skip(pgtest.DataSourceNameEnvironment + " is required")
	}
	ctx := context.Background()
	shared, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	fresh, drop, err := pgtest.Fresh(ctx, shared, dsn, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer drop()
	first, err := NewPostgresRepository(ctx, fresh)
	if err != nil {
		t.Fatal(err)
	}
	traced := ports.ContextWithTracer(ctx, &decisionTestTracer{})
	defer first.Close()
	record, err := NewLifecycle(first).CreatePlan(traced, foundation())
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := NewPostgresRepository(ctx, fresh)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	read, err := second.GetPlan(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if read.DecisionTraceparent == "" || read.DecisionTraceparent != record.DecisionTraceparent {
		t.Fatal("durable decision trace lost across pools")
	}
}

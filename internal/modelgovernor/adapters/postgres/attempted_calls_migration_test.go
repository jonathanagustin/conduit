package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/jonathanagustin/conduit/internal/modelgovernor"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

func TestAttemptedCallMigrationPreservesAmbiguousHistoricalChargesAndCompletedReceipts(t *testing.T) {
	dsn := freshDSN(t)
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := pgmigrate.Apply(ctx, db, Component, Migrations()[:3]); err != nil {
		t.Fatal(err)
	}
	// These aggregate rows cannot reveal how many individual requests had
	// positive attempts. Preserve their old conservative charge (3+4), not
	// the attempt sum (1+2) or an invented reconstructed invocation count.
	for _, row := range []struct {
		outcome         string
		calls, attempts int
	}{
		{"audit_failed", 3, 1}, {"canceled", 4, 2}, {"throttled", 5, 0}, {"usage_unavailable", 2, 0},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO conduit_model_usage_daily (day,owner,capability,transport,configured_model,provider_model,outcome,calls,attempts) VALUES ('2026-11-17','ada','assistant','http','','',$1,$2,$3)`, row.outcome, row.calls, row.attempts); err != nil {
			t.Fatal(err)
		}
	}
	completed := modelgovernor.UsageRecord{ReceiptID: "legacy-completed", Owner: "receipt-owner", Day: "2026-11-17", Capability: "assistant", Transport: "http", Outcome: "ok", Attempts: 1}
	payload, err := json.Marshal(completed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO conduit_model_usage_receipts (receipt_id,owner,day,payload) VALUES ($1,$2,$3::date,$4)`, completed.ReceiptID, completed.Owner, completed.Day, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO conduit_model_usage_daily (day,owner,capability,transport,configured_model,provider_model,outcome,calls,attempts) VALUES ('2026-11-17','receipt-owner','assistant','http','','','ok',1,1)`); err != nil {
		t.Fatal(err)
	}
	store, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	used, err := store.OwnerDay(ctx, "ada", "2026-11-17")
	if err != nil || used.Calls != 7 {
		t.Fatalf("migration guessed historical invocations or removed prior charges: %+v, %v", used, err)
	}
	// New requests sharing the historical outcome key carry exact per-request
	// evidence: one refusal adds no charge and a three-attempt request adds 1.
	for _, attempts := range []int{0, 3} {
		if err := store.Record(ctx, modelgovernor.UsageRecord{Owner: "ada", Day: "2026-11-17", Capability: "assistant", Transport: "http", Outcome: "audit_failed", Attempts: attempts}); err != nil {
			t.Fatal(err)
		}
	}
	used, err = store.OwnerDay(ctx, "ada", "2026-11-17")
	if err != nil || used.Calls != 8 {
		t.Fatalf("new exact requests changed the historical ambiguity: %+v, %v", used, err)
	}
	rows, err := store.Summary(ctx, modelgovernor.UsageQuery{Owner: "ada", FromDay: "2026-11-17", ToDay: "2026-11-17"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Outcome == "audit_failed" && (row.Calls != 5 || row.BudgetedCalls != 4 || row.Attempts != 4) {
			t.Fatalf("historical/new evidence was collapsed: %+v", row)
		}
	}
	if err := store.Record(ctx, completed); err != nil {
		t.Fatalf("migration changed exact receipt retry semantics: %v", err)
	}
	used, err = store.OwnerDay(ctx, completed.Owner, completed.Day)
	if err != nil || used.Calls != 1 {
		t.Fatalf("old completed receipt was counted twice: %+v, %v", used, err)
	}
	var retained []byte
	if err := db.QueryRowContext(ctx, `SELECT payload FROM conduit_model_usage_receipts WHERE receipt_id=$1`, completed.ReceiptID).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, retained) {
		t.Fatal("migration rewrote immutable completed receipt evidence")
	}
}

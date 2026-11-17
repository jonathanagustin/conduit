package auditpostgres

import (
	"database/sql"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/governance/domain"
)

func pageForTest(t *testing.T, database *sql.DB, query PageQuery) ([]PageRow, int) {
	t.Helper()
	transaction, err := database.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	store := New(transaction)
	rows, err := store.PageAuthorizationAudit(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	total, err := store.CountAuthorizationAudit(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	return rows, total
}

func rowIDs(rows []PageRow) []string {
	ids := []string{}
	for _, row := range rows {
		ids = append(ids, row.Event.ID)
	}
	return ids
}

// The page is cut by the database. Events are appended with event times that
// run backwards, so chain order and time order disagree: the page must follow
// the chain, and the chain must still verify once the trail spans pages.
func TestAuditPageIsBoundedSeekableAndInChainOrder(t *testing.T) {
	database, _ := databaseForTest(t)
	epoch := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	appended := []string{}
	for index := range 11 {
		resource := "ri.audit"
		if index%3 == 2 {
			resource = "ri.other"
		}
		event := domain.NewAuthorizationCheckEvent(epoch.Add(-time.Duration(index)*time.Minute), "operator", "read", resource, domain.DecisionAllow, "seeded", nil)
		event.ID = fmt.Sprintf("event-%02d", index)
		appendEvent(t, database, event)
		appended = append(appended, event.ID)
	}

	walked := []string{}
	after := uint64(0)
	for pages := 0; ; pages++ {
		rows, total := pageForTest(t, database, PageQuery{AfterSequence: after, Limit: 4})
		if len(rows) > 4 || total != 11 {
			t.Fatalf("page %d = %d rows, total %d", pages, len(rows), total)
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			if row.Sequence != after+1 {
				t.Fatalf("sequence %d follows %d", row.Sequence, after)
			}
			after = row.Sequence
		}
		walked = append(walked, rowIDs(rows)...)
	}
	if !reflect.DeepEqual(walked, appended) {
		t.Fatalf("walked %v, want append order %v", walked, appended)
	}

	// since is inclusive and until exclusive, evaluated on the stored payload.
	rows, total := pageForTest(t, database, PageQuery{Since: epoch.Add(-5 * time.Minute), Until: epoch.Add(-2 * time.Minute), Limit: 200})
	if want := []string{"event-03", "event-04", "event-05"}; !reflect.DeepEqual(rowIDs(rows), want) || total != 3 {
		t.Fatalf("window = %v total %d", rowIDs(rows), total)
	}
	rows, total = pageForTest(t, database, PageQuery{Resource: "ri.other", Limit: 2})
	if want := []string{"event-02", "event-05"}; !reflect.DeepEqual(rowIDs(rows), want) || total != 3 {
		t.Fatalf("resource page = %v total %d", rowIDs(rows), total)
	}
	rows, _ = pageForTest(t, database, PageQuery{Resource: "ri.other", AfterSequence: rows[1].Sequence, Until: epoch, Limit: 2})
	if want := []string{"event-08"}; !reflect.DeepEqual(rowIDs(rows), want) {
		t.Fatalf("combined seek = %v", rowIDs(rows))
	}

	transaction, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	store := New(transaction)
	if _, err := store.PageAuthorizationAudit(t.Context(), PageQuery{}); err == nil {
		t.Fatal("a page without a limit was served")
	}
	if found, err := store.HasAuthorizationAuditEvent(t.Context(), "event-07"); err != nil || !found {
		t.Fatalf("chained event not found: %v", err)
	}
	if found, err := store.HasAuthorizationAuditEvent(t.Context(), "absent"); err != nil || found {
		t.Fatalf("absent event found: %v", err)
	}
	if tip, err := verify(t, database, nil); err != nil || tip.Sequence != 11 {
		t.Fatalf("chain verification over the paged trail = %#v, %v", tip, err)
	}
}

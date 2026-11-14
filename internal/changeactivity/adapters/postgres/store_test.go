package postgres

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/jonathanagustin/conduit/internal/changeactivity/domain"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/pgtest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestIndependentPoolsPreserveDiscussionThroughLegacySnapshotWrites(t *testing.T) {
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is not set")
	}
	shared, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shared.Close() })
	fresh, drop, err := pgtest.Fresh(t.Context(), shared, dsn, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(drop)
	first, err := New(t.Context(), fresh)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := New(t.Context(), fresh)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	record := domain.Comment{ID: "cmt_" + strings.Repeat("1", 32), RequestID: "crq_thread", Author: "author", Actor: "carrier", Text: "original", CreatedAt: time.Now().UTC(), ValueTier: domain.ValueTier}
	replacement := record
	replacement.Text = "replacement"
	var wins atomic.Int32
	var group sync.WaitGroup
	for i := range 20 {
		group.Go(func() {
			store, c := first, record
			if i%2 != 0 {
				store, c = second, replacement
			}
			if err := store.Append(t.Context(), c); err == nil {
				wins.Add(1)
			} else if !errors.Is(err, domain.ErrConflict) {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if wins.Load() != 1 {
		t.Fatal("duplicate append acknowledged", wins.Load())
	}
	rows, err := first.List(t.Context(), record.RequestID)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	saved := rows[0]
	core, err := controlplane.NewPostgresRepository(t.Context(), fresh)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.PutTeam(t.Context(), controlplane.Team{Name: "discussion-legacy-writer", DisplayName: "Legacy writer", AdminUsers: []string{"operator"}, Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if err := core.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(t.Context(), fresh)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	rows, err = reopened.List(t.Context(), record.RequestID)
	if err != nil || len(rows) != 1 || rows[0] != saved {
		t.Fatal("legacy snapshot erased discussion", rows, err)
	}
	other := record
	other.RequestID = "crq_other"
	if err := reopened.Append(t.Context(), other); err != nil {
		t.Fatal("comment identity is not request-scoped", err)
	}
	forged := saved
	forged.RequestID = "crq_other"
	document, _ := json.Marshal(forged)
	if _, err := reopened.db.ExecContext(t.Context(), `UPDATE conduit_change_discussion SET document=$1 WHERE request_id=$2 AND comment_id=$3`, document, record.RequestID, record.ID); err != nil {
		t.Fatal(err)
	}
	if rows, err := reopened.List(t.Context(), record.RequestID); !errors.Is(err, domain.ErrCorrupt) || rows != nil {
		t.Fatal("foreign indexed data became an empty thread", rows, err)
	}
}

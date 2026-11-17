package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	governancedomain "github.com/jonathanagustin/conduit/internal/governance/domain"
)

func seedPendingAuditForPage(t *testing.T, repository *PostgresRepository, count int) []string {
	t.Helper()
	legacy := map[string]AuthorizationAuditEvent{}
	ids := []string{}
	for index := range count {
		event := repositoryAuditEvent(fmt.Sprintf("legacy-%d", index))
		event.CreatedAt = auditPageEpoch.Add(time.Duration(index) * time.Minute)
		legacy[event.ID] = event
		ids = append(ids, event.ID)
	}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.database.ExecContext(t.Context(), "UPDATE conduit_repository_state SET state=jsonb_set(state,'{authorizationAudit}',$1::jsonb)", encoded); err != nil {
		t.Fatal(err)
	}
	return ids
}

// The native SELECT runs after the compatibility read has selected pending
// legacy records. Pause it at the driver boundary so another independent pool
// can adopt those records before the SELECT begins, without timing assumptions.
type pauseAuditPageSelect struct {
	once    sync.Once
	reached chan struct{}
	release chan struct{}
}

func (pause *pauseAuditPageSelect) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, "SELECT sequence, previous_digest,") && strings.Contains(data.SQL, " ORDER BY sequence LIMIT ") {
		pause.once.Do(func() {
			close(pause.reached)
			select {
			case <-pause.release:
			case <-ctx.Done():
			}
		})
	}
	return ctx
}

func (*pauseAuditPageSelect) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestPostgresAuthorizationAuditPageUsesOneSnapshotDuringLegacyAdoption(t *testing.T) {
	writer, dsn := auditRepositoryForTest(t)
	if _, err := writer.AppendAuthorizationAudit(t.Context(), repositoryAuditEvent("native")); err != nil {
		t.Fatal(err)
	}
	legacyIDs := seedPendingAuditForPage(t, writer, 2)
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pause := &pauseAuditPageSelect{reached: make(chan struct{}), release: make(chan struct{})}
	config.Tracer = pause
	reader := &PostgresRepository{database: stdlib.OpenDB(*config)}
	t.Cleanup(func() { _ = reader.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(pause.release) }) }
	defer release()
	type result struct {
		page AuthorizationAuditPage
		err  error
	}
	completed := make(chan result, 1)
	go func() {
		page, err := reader.PageAuthorizationAudit(ctx, AuthorizationAuditPageQuery{Limit: 10})
		completed <- result{page: page, err: err}
	}()
	select {
	case <-pause.reached:
	case <-ctx.Done():
		t.Fatal("audit page never reached the native SELECT")
	}
	if err := writer.Transaction(ctx, func(Repository) error { return nil }); err != nil {
		t.Fatal(err)
	}
	// Adoption committed in the writer pool. The blocked reader must continue
	// seeing its original snapshot: one native row and two pending legacy rows.
	release()
	select {
	case actual := <-completed:
		if actual.err != nil {
			t.Fatal(actual.err)
		}
		ids := make([]string, 0, len(actual.page.Events))
		for _, event := range actual.page.Events {
			ids = append(ids, event.ID)
		}
		want := append([]string{"native"}, legacyIDs...)
		if !reflect.DeepEqual(ids, want) || actual.page.Total != len(want) || actual.page.Next != "" {
			t.Fatalf("snapshot page = %v total %d next %q; want %v exactly once", ids, actual.page.Total, actual.page.Next, want)
		}
	case <-ctx.Done():
		t.Fatal("audit page did not complete after releasing the SELECT")
	}
	if tip := auditTip(t, writer); tip.Sequence != 3 {
		t.Fatalf("writer did not commit all adopted events: %#v", tip)
	}
}

func TestPostgresAuthorizationAuditLegacyCursorRefusesAdoptionBetweenPages(t *testing.T) {
	for _, limit := range []int{1, 2} {
		t.Run(fmt.Sprintf("limit-%d", limit), func(t *testing.T) {
			reader, dsn := auditRepositoryForTest(t)
			writer, err := NewPostgresRepository(t.Context(), dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()
			if _, err := reader.AppendAuthorizationAudit(t.Context(), repositoryAuditEvent("native")); err != nil {
				t.Fatal(err)
			}
			legacyIDs := seedPendingAuditForPage(t, reader, 3)
			first, err := reader.PageAuthorizationAudit(t.Context(), AuthorizationAuditPageQuery{Limit: limit})
			if err != nil || len(first.Events) != limit || first.Next == "" {
				t.Fatalf("first page = %#v, %v", first, err)
			}
			if next, err := reader.PageAuthorizationAudit(t.Context(), AuthorizationAuditPageQuery{Limit: limit, After: first.Next}); err != nil || len(next.Events) == 0 {
				t.Fatalf("unchanged legacy cursor did not continue: %#v, %v", next, err)
			}
			if _, err := reader.PageAuthorizationAudit(t.Context(), AuthorizationAuditPageQuery{Limit: limit, After: (auditPosition{timed: true}).String()}); !errors.Is(err, ErrInvalidAuthorizationAuditPosition) {
				t.Fatalf("unbound pre-upgrade legacy position accepted: %v", err)
			}
			if err := writer.Transaction(t.Context(), func(Repository) error { return nil }); err != nil {
				t.Fatal(err)
			}
			if page, err := reader.PageAuthorizationAudit(t.Context(), AuthorizationAuditPageQuery{Limit: limit, After: first.Next}); !errors.Is(err, ErrInvalidAuthorizationAuditPosition) {
				t.Fatalf("legacy cursor silently continued after adoption: %#v, %v", page, err)
			}
			// The HTTP surface maps this stale position to its existing invalid
			// cursor response, rather than a successful empty page or a 500.
			fingerprint := fingerprintJSON(authorizationAuditFilter{View: "events"})
			status, _ := auditPageRequest(t, auditPageHandler(reader), url.Values{
				"cursor": {encodeOpaqueSeekCursor(first.Next, fingerprint)}, "limit": {fmt.Sprint(limit)},
			})
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("adopted legacy cursor status = %d, want 422", status)
			}
			// Restarting finds all events at their authoritative chain positions.
			restarted, err := reader.PageAuthorizationAudit(t.Context(), AuthorizationAuditPageQuery{Limit: 20})
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for _, event := range restarted.Events {
				if event.ID == "native" || strings.HasPrefix(event.ID, "legacy-") {
					ids = append(ids, event.ID)
				}
			}
			if want := append([]string{"native"}, legacyIDs...); !reflect.DeepEqual(ids, want) {
				t.Fatalf("restart = %v, want %v", ids, want)
			}
		})
	}
}

func TestPostgresAuthorizationAuditFilteredChainRestartsAfterLegacyAdoption(t *testing.T) {
	reader, dsn := auditRepositoryForTest(t)
	writer, err := NewPostgresRepository(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	for _, id := range []string{"native", "excluded"} {
		event := repositoryAuditEvent(id)
		event.CreatedAt = auditPageEpoch.Add(-time.Minute)
		if id == "excluded" {
			event.ResourceRID = "different-resource"
		}
		if _, err := reader.AppendAuthorizationAudit(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	legacyIDs := seedPendingAuditForPage(t, reader, 3)
	handler := auditPageHandler(reader)
	queries := map[string]url.Values{}
	for _, view := range []string{"events", "chain"} {
		query := url.Values{
			"view": {view}, "resourceRid": {"resource"}, "limit": {"2"},
			"since": {auditPageEpoch.Add(-time.Hour).Format(time.RFC3339)},
			"until": {auditPageEpoch.Add(time.Hour).Format(time.RFC3339)},
		}
		status, first := auditPageRequest(t, handler, query)
		if status != http.StatusOK || first.Total != 4 || first.NextCursor == "" {
			t.Fatalf("%s first page = %d, total %d, next %q", view, status, first.Total, first.NextCursor)
		}
		query.Set("cursor", first.NextCursor)
		queries[view] = query
	}
	if err := writer.Transaction(t.Context(), func(Repository) error { return nil }); err != nil {
		t.Fatal(err)
	}
	for view, query := range queries {
		if status, _ := auditPageRequest(t, handler, query); status != http.StatusUnprocessableEntity {
			t.Fatalf("%s stale filtered cursor status = %d", view, status)
		}
		query.Set("resourceRid", "different-resource")
		if status, _ := auditPageRequest(t, handler, query); status != http.StatusUnprocessableEntity {
			t.Fatalf("%s cursor accepted changed filters: %d", view, status)
		}
		query.Set("resourceRid", "resource")
		query.Del("cursor")
		ids := []string{}
		previous := governancedomain.EmptyAuditCheckpoint()
		for pages := 0; ; pages++ {
			status, page := auditPageRequest(t, handler, query)
			if status != http.StatusOK || page.Total != 4 || pages > 10 {
				t.Fatalf("%s restart page %d status %d, total %d", view, pages, status, page.Total)
			}
			if view == "chain" {
				for _, entry := range page.Chain {
					ids = append(ids, entry.ID)
					var err error
					previous, err = governancedomain.VerifyAuditChainRecord(previous, governancedomain.AuditChainRecord{
						Sequence: entry.Sequence, PreviousDigest: entry.PreviousDigest, EventID: entry.ID,
						ResourceRID: entry.ResourceRID, Origin: entry.Origin, Payload: entry.Payload, Digest: entry.Digest,
					})
					if err != nil {
						t.Fatalf("restarted chain projection does not verify: %v", err)
					}
				}
			} else {
				for _, event := range page.Events {
					ids = append(ids, event.ID)
				}
			}
			if page.NextCursor == "" {
				break
			}
			query.Set("cursor", page.NextCursor)
		}
		if want := append([]string{"native"}, legacyIDs...); !reflect.DeepEqual(ids, want) {
			t.Fatalf("%s filtered restart = %v, want %v", view, ids, want)
		}
	}
}

// A read-only audit page is the only repository operation whose isolation is
// changed. Keep an explicit control for ordinary writes at READ COMMITTED.
func TestPostgresOrdinaryRepositoryWriteRetainsReadCommittedIsolation(t *testing.T) {
	repository, _ := auditRepositoryForTest(t)
	err := repository.withStateTransaction(t.Context(), true, func(_ *MemoryRepository, transaction *sql.Tx) error {
		var isolation string
		if err := transaction.QueryRowContext(t.Context(), "SHOW transaction_isolation").Scan(&isolation); err != nil {
			return err
		}
		if isolation != "read committed" {
			t.Fatalf("ordinary write isolation changed to %s", isolation)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

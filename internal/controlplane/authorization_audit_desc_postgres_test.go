package controlplane

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
)

func seedDescendingAuditCompatibility(t *testing.T, repository *PostgresRepository) {
	t.Helper()
	for index := range 3 {
		event := repositoryAuditEvent(fmt.Sprintf("native-%d", index))
		// Deliberately disagree with commit order: descending means most
		// recently recorded, not sorting trusted event timestamps afterwards.
		event.CreatedAt = auditPageEpoch.Add(-time.Duration(index) * time.Minute)
		if _, err := repository.AppendAuthorizationAudit(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	seedPendingAuditForPage(t, repository, 3)
}

func TestPostgresAuthorizationAuditDescendingReversesCompatibilityOrder(t *testing.T) {
	repository, _ := auditRepositoryForTest(t)
	seedDescendingAuditCompatibility(t, repository)
	want := []string{"legacy-2", "legacy-1", "legacy-0", "native-2", "native-1", "native-0"}
	for _, limit := range []int{1, 2, 3, 4, 5, 6, 7} {
		for _, view := range []string{"events", "chain"} {
			query := url.Values{"order": {"desc"}, "view": {view}, "limit": {fmt.Sprint(limit)}, "resourceRid": {"resource"}}
			ids := []string{}
			previous := governance.EmptyAuditCheckpoint()
			for pages := 0; ; pages++ {
				status, page := auditPageRequest(t, auditPageHandler(repository), query)
				if status != http.StatusOK || page.Total != 6 || page.Order != "desc" || pages > 10 {
					t.Fatalf("limit %d %s page %d = %d, total %d, order %q", limit, view, pages, status, page.Total, page.Order)
				}
				if view == "chain" {
					for _, entry := range page.Chain {
						ids = append(ids, entry.ID)
						var err error
						previous, err = governance.VerifyAuditChainRecord(previous, governance.AuditChainRecord{
							Sequence: entry.Sequence, PreviousDigest: entry.PreviousDigest, EventID: entry.ID,
							ResourceRID: entry.ResourceRID, Origin: entry.Origin, Payload: entry.Payload, Digest: entry.Digest,
						})
						if err != nil {
							t.Fatalf("descending projection does not verify across pages: %v", err)
						}
					}
				} else {
					ids = append(ids, auditReadIDs(page.Events)...)
				}
				if page.NextCursor == "" {
					break
				}
				query.Set("cursor", page.NextCursor)
			}
			if !reflect.DeepEqual(ids, want) {
				t.Fatalf("limit %d %s descending = %v, want %v", limit, view, ids, want)
			}
		}
	}
	if err := repository.Transaction(t.Context(), func(Repository) error { return nil }); err != nil {
		t.Fatal(err)
	}
	// Once adoption is complete, the same order is now entirely native and the
	// first descending page must start at its head without a compatibility key.
	query := AuthorizationAuditPageQuery{Desc: true, Limit: 2}
	ids := []string{}
	for pages := 0; ; pages++ {
		page, err := repository.PageAuthorizationAudit(t.Context(), query)
		if err != nil || pages > 10 {
			t.Fatalf("native descending page %d: %v", pages, err)
		}
		ids = append(ids, auditReadIDs(page.Events)...)
		if page.Next == "" {
			break
		}
		query.After = page.Next
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("fully native descending order = %v, want %v", ids, want)
	}
}

func TestPostgresAuthorizationAuditDescendingHandlesOnlyPendingOrEmptyLog(t *testing.T) {
	for _, count := range []int{0, 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			repository, _ := auditRepositoryForTest(t)
			seedPendingAuditForPage(t, repository, count)
			want := []string{}
			if count > 0 {
				want = []string{"legacy-2", "legacy-1", "legacy-0"}
			}
			for _, limit := range []int{1, 2, 3, 4} {
				query := AuthorizationAuditPageQuery{Desc: true, Limit: limit}
				ids := []string{}
				for pages := 0; ; pages++ {
					page, err := repository.PageAuthorizationAudit(t.Context(), query)
					if err != nil || page.Total != count || pages > 5 {
						t.Fatalf("pending-only page %d = %#v, %v", pages, page, err)
					}
					ids = append(ids, auditReadIDs(page.Events)...)
					if page.Next == "" {
						break
					}
					query.After = page.Next
				}
				if !reflect.DeepEqual(ids, want) {
					t.Fatalf("pending-only limit %d = %v, want %v", limit, ids, want)
				}
			}
		})
	}
}

func TestPostgresAuthorizationAuditDescendingLegacyCursorRefusesAdoption(t *testing.T) {
	for _, limit := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			reader, dsn := auditRepositoryForTest(t)
			writer, err := NewPostgresRepository(t.Context(), dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()
			seedDescendingAuditCompatibility(t, reader)
			first, err := reader.PageAuthorizationAudit(t.Context(), AuthorizationAuditPageQuery{Desc: true, Limit: limit})
			if err != nil || first.Next == "" {
				t.Fatalf("first descending page = %#v, %v", first, err)
			}
			if err := writer.Transaction(t.Context(), func(Repository) error { return nil }); err != nil {
				t.Fatal(err)
			}
			if _, err := reader.PageAuthorizationAudit(t.Context(), AuthorizationAuditPageQuery{Desc: true, Limit: limit, After: first.Next}); !errors.Is(err, ErrInvalidAuthorizationAuditPosition) {
				t.Fatalf("descending legacy cursor silently continued after adoption: %v", err)
			}
		})
	}
}

func TestPostgresAuthorizationAuditDescendingNativeCursorDoesNotReplayAdoptedEvents(t *testing.T) {
	reader, dsn := auditRepositoryForTest(t)
	writer, err := NewPostgresRepository(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	seedDescendingAuditCompatibility(t, reader)
	first, err := reader.PageAuthorizationAudit(t.Context(), AuthorizationAuditPageQuery{Desc: true, Limit: 4})
	if err != nil || first.Next == "" {
		t.Fatalf("first descending page = %#v, %v", first, err)
	}
	if want := []string{"legacy-2", "legacy-1", "legacy-0", "native-2"}; !reflect.DeepEqual(auditReadIDs(first.Events), want) {
		t.Fatalf("first descending page = %v", auditReadIDs(first.Events))
	}
	if err := writer.Transaction(t.Context(), func(Repository) error { return nil }); err != nil {
		t.Fatal(err)
	}
	second, err := reader.PageAuthorizationAudit(t.Context(), AuthorizationAuditPageQuery{Desc: true, Limit: 4, After: first.Next})
	if err != nil || second.Next != "" {
		t.Fatalf("second descending page = %#v, %v", second, err)
	}
	if want := []string{"native-1", "native-0"}; !reflect.DeepEqual(auditReadIDs(second.Events), want) {
		t.Fatalf("adoption replayed or omitted events: %v", auditReadIDs(second.Events))
	}
}

package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	governancedomain "github.com/jonathanagustin/conduit/internal/governance/domain"
)

// The HTTP read over PostgreSQL pages by chain sequence. Events are seeded
// with event times running backwards so that a page ordered or cut anywhere
// but in the sequence-ordered query shows up as a wrong order, and the ledger
// verifier is run over the same multi-page trail.
func TestPostgresAuthorizationAuditReadPagesInChainOrder(t *testing.T) {
	repository, _ := auditRepositoryForTest(t)
	seeded := []string{}
	for index := range 23 {
		event := repositoryAuditEvent(fmt.Sprintf("seed-%03d", index))
		event.Subject = "seed"
		event.CreatedAt = auditPageEpoch.Add(-time.Duration(index) * time.Minute)
		if _, err := repository.AppendAuthorizationAudit(t.Context(), event); err != nil {
			t.Fatal(err)
		}
		seeded = append(seeded, event.ID)
	}
	handler := auditPageHandler(repository)
	until := auditPageEpoch.Add(time.Hour).Format(time.RFC3339)

	_, bounded := auditPageRequest(t, handler, url.Values{"until": {until}, "limit": {"5"}})
	if len(bounded.Events) != 5 || bounded.Total != 23 || bounded.NextCursor == "" {
		t.Fatalf("first page = %d events total %d next %q", len(bounded.Events), bounded.Total, bounded.NextCursor)
	}
	for _, view := range []string{"events", "chain"} {
		query := url.Values{"until": {until}, "limit": {"5"}, "view": {view}}
		walked := []string{}
		var entries []AuthorizationAuditChainEntry
		for pages := 0; ; pages++ {
			status, page := auditPageRequest(t, handler, query)
			if status != http.StatusOK || len(page.Events) > 5 || len(page.Chain) > 5 {
				t.Fatalf("%s page %d = status %d: %s", view, pages, status, page.Error)
			}
			walked = append(walked, seededIDs(page.Events)...)
			entries = append(entries, page.Chain...)
			if page.NextCursor == "" {
				break
			}
			if pages > 100 {
				t.Fatal("the cursor never reached the end of the trail")
			}
			query.Set("cursor", page.NextCursor)
		}
		if view == "events" && !reflect.DeepEqual(walked, seeded) {
			t.Fatalf("walked %v, want chain order %v", walked, seeded)
		}
		if view == "chain" {
			if len(entries) != 23 {
				t.Fatalf("chain entries = %d", len(entries))
			}
			previous := governancedomain.EmptyAuditCheckpoint()
			for index, entry := range entries {
				if entry.ID != seeded[index] {
					t.Fatalf("chain entry %d = %s, want %s", index, entry.ID, seeded[index])
				}
				next, err := governancedomain.VerifyAuditChainRecord(previous, governancedomain.AuditChainRecord{
					Sequence: entry.Sequence, PreviousDigest: entry.PreviousDigest, EventID: entry.ID,
					ResourceRID: entry.ResourceRID, Origin: entry.Origin, Payload: entry.Payload, Digest: entry.Digest,
				})
				if err != nil {
					t.Fatalf("chain entry %d: %v", index, err)
				}
				previous = next
			}
		}
	}
	// since/until reach the SQL predicate.
	_, window := auditPageRequest(t, handler, url.Values{
		"since": {auditPageEpoch.Add(-4 * time.Minute).Format(time.RFC3339)}, "until": {auditPageEpoch.Add(-1 * time.Minute).Format(time.RFC3339)},
	})
	if want := []string{"seed-002", "seed-003", "seed-004"}; !reflect.DeepEqual(seededIDs(window.Events), want) || window.Total != 3 {
		t.Fatalf("window = %v total %d", seededIDs(window.Events), window.Total)
	}
	// A position this store never issues is a refused cursor, not a 500.
	fingerprint := fingerprintJSON(authorizationAuditFilter{View: "events"})
	if status, _ := auditPageRequest(t, handler, url.Values{"cursor": {encodeOpaqueSeekCursor("q:1", fingerprint)}}); status != http.StatusUnprocessableEntity {
		t.Fatalf("unknown position status = %d", status)
	}
	// The ledger verifier walks the whole trail, however it is paged for HTTP.
	if tip := auditTip(t, repository); tip.Sequence < 23 {
		t.Fatalf("verified tip = %#v", tip)
	}
}

// During a mixed rollout an older Hub can leave records in the legacy
// snapshot. They follow the chained records, once each, across the page
// boundary, including when the chained log ends exactly on it.
func TestPostgresAuthorizationAuditPageIncludesPendingLegacyRecords(t *testing.T) {
	repository, _ := auditRepositoryForTest(t)
	chained := []string{}
	for index := range 4 {
		event := repositoryAuditEvent(fmt.Sprintf("chained-%d", index))
		if _, err := repository.AppendAuthorizationAudit(t.Context(), event); err != nil {
			t.Fatal(err)
		}
		chained = append(chained, event.ID)
	}
	legacy := map[string]AuthorizationAuditEvent{"chained-0": repositoryAuditEvent("chained-0")}
	for index := range 3 {
		event := repositoryAuditEvent(fmt.Sprintf("legacy-%d", index))
		event.CreatedAt = event.CreatedAt.Add(-time.Duration(index) * time.Hour)
		legacy[event.ID] = event
	}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.database.ExecContext(t.Context(), "UPDATE conduit_repository_state SET state=jsonb_set(state,'{authorizationAudit}',$1::jsonb)", encoded); err != nil {
		t.Fatal(err)
	}
	want := append(append([]string{}, chained...), "legacy-2", "legacy-1", "legacy-0")
	for _, limit := range []int{1, 2, 3, 4, 5, 6, 7, 50} {
		walked := []string{}
		query := AuthorizationAuditPageQuery{Limit: limit}
		for pages := 0; ; pages++ {
			page, err := repository.PageAuthorizationAudit(t.Context(), query)
			if err != nil || len(page.Events) > limit || page.Total != 7 {
				t.Fatalf("limit %d: %d events total %d err %v", limit, len(page.Events), page.Total, err)
			}
			for _, event := range page.Events {
				walked = append(walked, event.ID)
			}
			if page.Next == "" {
				break
			}
			if pages > 100 {
				t.Fatal("the position never reached the end of the trail")
			}
			query.After = page.Next
		}
		if !reflect.DeepEqual(walked, want) {
			t.Fatalf("limit %d walked %v, want %v", limit, walked, want)
		}
	}
}

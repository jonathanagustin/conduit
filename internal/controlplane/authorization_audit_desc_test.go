package controlplane

import (
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
)

func auditReadIDs(events []AuthorizationAuditEvent) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.ID)
	}
	return ids
}

func TestAuthorizationAuditOrderPreservesAscendingCursorCompatibility(t *testing.T) {
	implicit, err := parseAuthorizationAuditRead(url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	// SHA-256 of the exact pre-order filter JSON. Omitting order and explicitly
	// selecting asc must keep existing opaque cursors usable.
	const oldFingerprint = "09bb465d28bf19a1e515d6faa81fba3f199f95871e536ac69b4592b95599e5b3"
	if implicit.fingerprint != oldFingerprint {
		t.Fatalf("default filter fingerprint changed: %s", implicit.fingerprint)
	}
	position := auditPosition{timed: true, at: auditPageEpoch, id: "event"}.String()
	cursor := encodeOpaqueSeekCursor(position, oldFingerprint)
	explicit, err := parseAuthorizationAuditRead(url.Values{"order": {"asc"}, "cursor": {cursor}})
	if err != nil || explicit.fingerprint != oldFingerprint || explicit.query.Desc {
		t.Fatalf("old ascending cursor refused: %#v, %v", explicit, err)
	}
	if _, err := parseAuthorizationAuditRead(url.Values{"order": {"desc"}, "cursor": {cursor}}); err == nil {
		t.Fatal("ascending cursor accepted with descending order")
	}
	for _, order := range []string{"DESC", "newest", "ascending"} {
		if _, err := parseAuthorizationAuditRead(url.Values{"order": {order}}); err == nil {
			t.Fatalf("invalid order %q accepted", order)
		}
	}
}

func TestUnchainedAuthorizationAuditDescendingPagesUseTimeAndID(t *testing.T) {
	for name, repository := range auditRepositoriesForTest(t) {
		t.Run(name, func(t *testing.T) {
			for _, item := range []struct {
				id      string
				minutes int
			}{{"b", 0}, {"a", 0}, {"c", 1}, {"d", -1}} {
				event := repositoryAuditEvent(item.id)
				event.Subject = "seed"
				event.CreatedAt = auditPageEpoch.Add(time.Duration(item.minutes) * time.Minute)
				if _, err := repository.AppendAuthorizationAudit(t.Context(), event); err != nil {
					t.Fatal(err)
				}
			}
			handler := auditPageHandler(repository)
			for _, view := range []string{"events", "chain"} {
				query := url.Values{"order": {"desc"}, "view": {view}, "limit": {"2"}, "resourceRid": {"resource"}}
				ids := []string{}
				previous := governance.EmptyAuditCheckpoint()
				for pages := 0; ; pages++ {
					status, page := auditPageRequest(t, handler, query)
					if status != http.StatusOK || page.Order != "desc" || page.Total != 4 || pages > 5 {
						t.Fatalf("%s page %d = %d, order %q, total %d", view, pages, status, page.Order, page.Total)
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
								t.Fatalf("descending projection does not verify: %v", err)
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
				if want := []string{"c", "b", "a", "d"}; !reflect.DeepEqual(ids, want) {
					t.Fatalf("%s descending IDs = %v, want %v", view, ids, want)
				}
			}
		})
	}
}

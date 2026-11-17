package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	governancedomain "github.com/jonathanagustin/conduit/internal/governance/domain"
)

var auditPageEpoch = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// seedAuditTrail appends count events one minute apart. Every third event
// targets "ri.other", so a resource filter has something to exclude.
func seedAuditTrail(t *testing.T, repository Repository, count int) []string {
	t.Helper()
	ids := make([]string, 0, count)
	for index := range count {
		resource := "ri.audit"
		if index%3 == 2 {
			resource = "ri.other"
		}
		event := governancedomain.NewAuthorizationCheckEvent(
			auditPageEpoch.Add(time.Duration(index)*time.Minute), "seed", "read", resource, governancedomain.DecisionAllow, "seeded", nil,
		)
		event.ID = fmt.Sprintf("seed-%03d", index)
		if _, err := repository.AppendAuthorizationAudit(t.Context(), event); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, event.ID)
	}
	return ids
}

type auditPageBody struct {
	Events []AuthorizationAuditEvent      `json:"events"`
	Chain  []AuthorizationAuditChainEntry `json:"entries"`
	Order  string                         `json:"order"`
	PageMetadata
	Total          int             `json:"total"`
	RawPrevious    json.RawMessage `json:"previousCursor"`
	Error          string          `json:"error"`
	rawEventsField bool
}

func auditPageRequest(t *testing.T, handler http.Handler, query url.Values) (int, auditPageBody) {
	t.Helper()
	response := governanceRequest(t, handler, http.MethodGet, "/v1/authorization-audit?"+query.Encode(), "admin", "", nil)
	var body auditPageBody
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", response.Body.String(), err)
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(response.Body.Bytes(), &fields)
	_, body.rawEventsField = fields["events"]
	return response.Code, body
}

func auditPageHandler(repository Repository) http.Handler {
	return NewHandler(repository, HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"},
	})
}

// seededIDs keeps only seeded events: the read itself is authorized, and an
// authorization decision may be audited too.
func seededIDs(events []AuthorizationAuditEvent) []string {
	ids := []string{}
	for _, event := range events {
		if event.Subject == "seed" {
			ids = append(ids, event.ID)
		}
	}
	return ids
}

func auditRepositoriesForTest(t *testing.T) map[string]Repository {
	t.Helper()
	file, err := NewFileRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return map[string]Repository{"memory": NewMemoryRepository(), "file": file}
}

// A read without a limit used to return the whole log. It now returns the
// default page and a cursor, and the cursor walks the rest in a stable order
// without repeating or dropping an event.
func TestAuthorizationAuditReadIsBoundedAndCursorWalksEveryEventOnce(t *testing.T) {
	t.Parallel()
	for name, repository := range auditRepositoriesForTest(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			seeded := seedAuditTrail(t, repository, 250)
			handler := auditPageHandler(repository)
			window := url.Values{"until": {auditPageEpoch.Add(24 * time.Hour).Format(time.RFC3339)}}

			status, first := auditPageRequest(t, handler, window)
			if status != http.StatusOK || len(first.Events) != defaultPageLimit || first.Limit != defaultPageLimit ||
				first.PageSize != defaultPageLimit || first.NextCursor == "" || first.HasPrevious || first.Total != 250 {
				t.Fatalf("default page = status %d, %d events, metadata %+v, total %d", status, len(first.Events), first.PageMetadata, first.Total)
			}

			walked := []string{}
			query := url.Values{"until": window["until"], "limit": {"40"}}
			pages := 0
			for {
				status, page := auditPageRequest(t, handler, query)
				if status != http.StatusOK {
					t.Fatalf("page %d status = %d: %s", pages, status, page.Error)
				}
				if len(page.Events) > 40 || page.PageSize != len(page.Events) || page.HasPrevious != (pages > 0) || len(page.RawPrevious) != 0 {
					t.Fatalf("page %d = %d events, metadata %+v", pages, len(page.Events), page.PageMetadata)
				}
				walked = append(walked, seededIDs(page.Events)...)
				pages++
				if page.NextCursor == "" {
					break
				}
				if pages > 100 {
					t.Fatal("the cursor never reached the end of the trail")
				}
				query.Set("cursor", page.NextCursor)
			}
			if pages != 7 || !reflect.DeepEqual(walked, seeded) {
				t.Fatalf("walked %d events over %d pages; want the %d seeded events in order over 7", len(walked), pages, len(seeded))
			}
		})
	}
}

func TestAuthorizationAuditReadAppliesTimeAndResourceBounds(t *testing.T) {
	t.Parallel()
	for name, repository := range auditRepositoriesForTest(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			seeded := seedAuditTrail(t, repository, 30)
			handler := auditPageHandler(repository)
			since, until := auditPageEpoch.Add(10*time.Minute), auditPageEpoch.Add(20*time.Minute)

			// since is inclusive, until exclusive.
			status, page := auditPageRequest(t, handler, url.Values{
				"since": {since.Format(time.RFC3339)}, "until": {until.Format(time.RFC3339)},
			})
			if status != http.StatusOK || !reflect.DeepEqual(seededIDs(page.Events), seeded[10:20]) || page.Total != 10 || page.NextCursor != "" {
				t.Fatalf("window = status %d ids %v total %d next %q", status, seededIDs(page.Events), page.Total, page.NextCursor)
			}
			// An offset timestamp names the same instant.
			zone := time.FixedZone("plus2", 2*60*60)
			_, shifted := auditPageRequest(t, handler, url.Values{
				"since": {since.In(zone).Format(time.RFC3339)}, "until": {until.In(zone).Format(time.RFC3339)},
			})
			if !reflect.DeepEqual(seededIDs(shifted.Events), seeded[10:20]) {
				t.Fatalf("offset window ids = %v", seededIDs(shifted.Events))
			}
			status, scoped := auditPageRequest(t, handler, url.Values{
				"resourceRid": {"ri.other"}, "until": {until.Format(time.RFC3339)}, "limit": {"4"},
			})
			want := []string{"seed-002", "seed-005", "seed-008", "seed-011"}
			if status != http.StatusOK || !reflect.DeepEqual(seededIDs(scoped.Events), want) || scoped.Total != 6 || scoped.NextCursor == "" {
				t.Fatalf("scoped = status %d ids %v total %d next %q", status, seededIDs(scoped.Events), scoped.Total, scoped.NextCursor)
			}
			// An empty window is an empty array, not null and not an error.
			status, empty := auditPageRequest(t, handler, url.Values{"until": {auditPageEpoch.Add(-time.Hour).Format(time.RFC3339)}})
			if status != http.StatusOK || !empty.rawEventsField || empty.Events == nil || len(empty.Events) != 0 || empty.Total != 0 {
				t.Fatalf("empty window = status %d body %+v", status, empty)
			}
		})
	}
}

func TestAuthorizationAuditReadRefusesInvalidPageParameters(t *testing.T) {
	t.Parallel()
	repository := NewMemoryRepository()
	seedAuditTrail(t, repository, 12)
	handler := auditPageHandler(repository)
	until := auditPageEpoch.Add(time.Hour).Format(time.RFC3339)
	_, first := auditPageRequest(t, handler, url.Values{"limit": {"5"}, "until": {until}})
	if first.NextCursor == "" {
		t.Fatal("first page issued no cursor")
	}
	_, chain := auditPageRequest(t, handler, url.Values{"limit": {"5"}, "until": {until}, "view": {"chain"}})
	for name, query := range map[string]url.Values{
		"zero limit":              {"limit": {"0"}},
		"negative limit":          {"limit": {"-1"}},
		"limit above maximum":     {"limit": {"201"}},
		"non-numeric limit":       {"limit": {"many"}},
		"malformed cursor":        {"cursor": {"not-a-cursor"}, "until": {until}},
		"oversized cursor":        {"cursor": {string(make([]byte, maxCursorBytes+1))}},
		"cursor other filter":     {"cursor": {first.NextCursor}, "until": {until}, "resourceRid": {"ri.other"}},
		"cursor other window":     {"cursor": {first.NextCursor}},
		"cursor other view":       {"cursor": {first.NextCursor}, "until": {until}, "view": {"chain"}},
		"chain cursor as events":  {"cursor": {chain.NextCursor}, "until": {until}},
		"unknown position":        {"cursor": {encodeOpaqueSeekCursor("x:1", fingerprintJSON(authorizationAuditFilter{View: "events"}))}},
		"chain cursor no digest":  {"cursor": {encodeOpaqueSeekCursor("t:2020-01-01T00:00:00Z|a", fingerprintJSON(authorizationAuditFilter{View: "chain"}))}, "view": {"chain"}},
		"chain cursor bad digest": {"cursor": {encodeOpaqueSeekCursor("3:beef:t:2020-01-01T00:00:00Z|a", fingerprintJSON(authorizationAuditFilter{View: "chain"}))}, "view": {"chain"}},
		"since not a time":        {"since": {"yesterday"}},
		"until not RFC 3339":      {"until": {"2020-01-01"}},
		"since after until":       {"since": {until}, "until": {auditPageEpoch.Format(time.RFC3339)}},
		"unknown view":            {"view": {"everything"}},
	} {
		status, body := auditPageRequest(t, handler, query)
		if status != http.StatusUnprocessableEntity || body.Error == "" {
			t.Errorf("%s: status = %d error = %q, want 422 with a reason", name, status, body.Error)
		}
	}
	// The contract ties a cursor to its filters, not to the page size.
	status, resized := auditPageRequest(t, handler, url.Values{"cursor": {first.NextCursor}, "until": {until}, "limit": {"3"}})
	if status != http.StatusOK || len(resized.Events) != 3 {
		t.Fatalf("cursor reused with another limit = status %d, %d events", status, len(resized.Events))
	}
	// A well-formed cursor whose position this store never issues is refused.
	fingerprint := fingerprintJSON(authorizationAuditFilter{View: "events"})
	status, foreign := auditPageRequest(t, handler, url.Values{"cursor": {encodeOpaqueSeekCursor("s:7", fingerprint)}})
	if status != http.StatusUnprocessableEntity || foreign.Error == "" {
		t.Fatalf("foreign position = status %d error %q", status, foreign.Error)
	}
}

// The chain view folds each page from the checkpoint its cursor carries. The
// concatenated pages must be the same projection as one unpaginated fold: the
// verifier a client runs from genesis has to keep passing over a multi-page
// trail.
func TestAuthorizationAuditChainViewVerifiesAcrossPages(t *testing.T) {
	t.Parallel()
	for name, repository := range auditRepositoriesForTest(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			seedAuditTrail(t, repository, 23)
			handler := auditPageHandler(repository)
			query := url.Values{"view": {"chain"}, "limit": {"5"}, "until": {auditPageEpoch.Add(time.Hour).Format(time.RFC3339)}}
			entries := []AuthorizationAuditChainEntry{}
			for pages := 0; ; pages++ {
				status, page := auditPageRequest(t, handler, query)
				if status != http.StatusOK || len(page.Chain) > 5 {
					t.Fatalf("chain page %d = status %d, %d entries: %s", pages, status, len(page.Chain), page.Error)
				}
				entries = append(entries, page.Chain...)
				if page.NextCursor == "" {
					break
				}
				if pages > 100 {
					t.Fatal("the cursor never reached the end of the trail")
				}
				query.Set("cursor", page.NextCursor)
			}
			if len(entries) != 23 {
				t.Fatalf("chain entries = %d, want 23", len(entries))
			}
			previous := governancedomain.EmptyAuditCheckpoint()
			for index, entry := range entries {
				record := governancedomain.AuditChainRecord{
					Sequence: entry.Sequence, PreviousDigest: entry.PreviousDigest, EventID: entry.ID,
					ResourceRID: entry.ResourceRID, Origin: entry.Origin, Payload: entry.Payload, Digest: entry.Digest,
				}
				if entry.Sequence != uint64(index+1) {
					t.Fatalf("entry %d has sequence %d", index, entry.Sequence)
				}
				next, err := governancedomain.VerifyAuditChainRecord(previous, record)
				if err != nil {
					t.Fatalf("entry %d does not verify from its predecessor: %v", index, err)
				}
				previous = next
			}
			all, err := repository.ListAuthorizationAudit(context.Background(), "")
			if err != nil {
				t.Fatal(err)
			}
			seeded := all[:0]
			for _, event := range all {
				if event.Subject == "seed" {
					seeded = append(seeded, event)
				}
			}
			whole, err := ChainAuthorizationAudit(seeded)
			if err != nil {
				t.Fatal(err)
			}
			if whole[len(whole)-1].Digest != entries[len(entries)-1].Digest {
				t.Fatal("paged projection tip differs from the unpaginated projection tip")
			}
		})
	}
}

// The bound belongs to the repository, not to the handler: a repository asked
// for one page hands back one page.
func TestRepositoryAuditPageHoldsOnlyTheRequestedWindow(t *testing.T) {
	t.Parallel()
	for name, repository := range auditRepositoriesForTest(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			seeded := seedAuditTrail(t, repository, 9)
			first, err := repository.PageAuthorizationAudit(t.Context(), AuthorizationAuditPageQuery{Limit: 4})
			if err != nil || len(first.Events) != 4 || first.Next == "" || first.Total != 9 {
				t.Fatalf("first = %d events next %q total %d err %v", len(first.Events), first.Next, first.Total, err)
			}
			rest, err := repository.PageAuthorizationAudit(t.Context(), AuthorizationAuditPageQuery{Limit: 200, After: first.Next})
			if err != nil || rest.Next != "" {
				t.Fatalf("rest = next %q err %v", rest.Next, err)
			}
			if got := append(seededIDs(first.Events), seededIDs(rest.Events)...); !reflect.DeepEqual(got, seeded) {
				t.Fatalf("pages = %v", got)
			}
			if _, err := repository.PageAuthorizationAudit(t.Context(), AuthorizationAuditPageQuery{}); err == nil {
				t.Fatal("a page without a limit was served")
			}
		})
	}
}

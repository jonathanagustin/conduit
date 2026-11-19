package composition

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	governancev1 "github.com/jonathanagustin/conduit/gen/go/conduit/governance/v1"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/governance/domain"
	"google.golang.org/protobuf/encoding/protojson"
)

func auditConnectRequest(handler http.Handler, actor, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/conduit.governance.v1.AuthorizationAuditReadService/ListAuthorizationAuditEvents", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Connect-Protocol-Version", "1")
	if actor != "" {
		r.Header.Set("X-Platform-Subject", actor)
	}
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, r)
	return out
}

func TestAuditConnectMatchesRESTPagesAndRejectsCursorRebinding(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	epoch := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 7 {
		resource := "ri.audit"
		if i == 3 {
			resource = "ri.other"
		}
		event := domain.NewAuthorizationCheckEvent(epoch.Add(time.Duration(i)*time.Minute), "seed", "read", resource, domain.DecisionAllow, "seeded", nil)
		event.ID = fmt.Sprintf("event-%d", i)
		if _, err := repository.AppendAuthorizationAudit(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}})
	for _, descending := range []bool{false, true} {
		cursor := ""
		seen := map[string]bool{}
		for pageNumber := 0; pageNumber < 10; pageNumber++ {
			q := url.Values{"resourceRid": {"ri.audit"}, "limit": {"2"}, "since": {epoch.Format(time.RFC3339)}, "until": {epoch.Add(7 * time.Minute).Format(time.RFC3339)}, "cursor": {cursor}}
			if descending {
				q.Set("order", "desc")
			}
			req := httptest.NewRequest("GET", "/v1/authorization-audit?"+q.Encode(), nil)
			req.Header.Set("X-Platform-Subject", "admin")
			native := httptest.NewRecorder()
			handler.ServeHTTP(native, req)
			body, _ := json.Marshal(map[string]any{"resourceRid": "ri.audit", "limit": 2, "since": q.Get("since"), "until": q.Get("until"), "cursor": cursor, "descending": descending})
			wire := auditConnectRequest(handler, "admin", string(body))
			if native.Code != 200 || wire.Code != 200 {
				t.Fatalf("REST %d %s RPC %d %s", native.Code, native.Body, wire.Code, wire.Body)
			}
			var expected struct {
				Events      []controlplane.AuthorizationAuditEvent `json:"events"`
				Total       int                                    `json:"total"`
				PageSize    int                                    `json:"pageSize"`
				Limit       int                                    `json:"limit"`
				NextCursor  string                                 `json:"nextCursor"`
				HasPrevious bool                                   `json:"hasPrevious"`
			}
			actual := &governancev1.ListAuthorizationAuditEventsResponse{}
			if json.Unmarshal(native.Body.Bytes(), &expected) != nil || protojson.Unmarshal(wire.Body.Bytes(), actual) != nil {
				t.Fatal("decode page")
			}
			if actual.Total != int64(expected.Total) || actual.Total != 6 || actual.PageSize != int32(expected.PageSize) || actual.Limit != int32(expected.Limit) || actual.NextCursor != expected.NextCursor || actual.HasPrevious != expected.HasPrevious || len(actual.Events) != len(expected.Events) {
				t.Fatalf("page mismatch: %+v %v", expected, actual)
			}
			for i, event := range actual.Events {
				if event.Id != expected.Events[i].ID || seen[event.Id] {
					t.Fatalf("changed or repeated event: %s", event.Id)
				}
				seen[event.Id] = true
			}
			if actual.NextCursor == "" {
				break
			}
			if pageNumber == 0 {
				rebound, _ := json.Marshal(map[string]any{"resourceRid": "ri.other", "cursor": actual.NextCursor})
				if refused := auditConnectRequest(handler, "admin", string(rebound)); refused.Code != 400 {
					t.Fatalf("cursor rebound: %d %s", refused.Code, refused.Body)
				}
			}
			cursor = actual.NextCursor
		}
		if len(seen) != 6 {
			t.Fatalf("incomplete walk: %v", seen)
		}
	}
	for _, actor := range []string{"viewer"} {
		if out := auditConnectRequest(handler, actor, "{broken"); out.Code != 403 {
			t.Fatalf("nonadmin decoded: %d %s", out.Code, out.Body)
		}
	}
	secure := NewHubHandler(repository, controlplane.HandlerOptions{})
	if out := auditConnectRequest(secure, "", "{broken"); out.Code != 401 {
		t.Fatalf("anonymous decoded: %d %s", out.Code, out.Body)
	}
}

func TestAuditConnectPreservesDelegatingAdministratorCarrier(t *testing.T) {
	_, repository, _ := delegationHub(t)
	handler := NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"alice"}})
	for _, tc := range []struct {
		actor, represented string
		status             int
	}{
		{"alice", "", 200}, {"alice", "bob", 200}, {"robot", "alice", 403}, {"bob", "alice", 403}, {"bob", "", 403},
	} {
		native := as(t, handler, "/v1/authorization-audit", tc.actor, tc.represented)
		req := httptest.NewRequest("POST", "/conduit.governance.v1.AuthorizationAuditReadService/ListAuthorizationAuditEvents", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Connect-Protocol-Version", "1")
		req.Header.Set("X-Platform-Subject", tc.actor)
		if tc.represented != "" {
			req.Header.Set("X-Platform-Service-Account", "true")
			req.Header.Set("X-Platform-On-Behalf-Of", tc.represented)
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if native.Code != tc.status || out.Code != tc.status {
			t.Fatalf("%s as %s: REST %d RPC %d %s", tc.actor, tc.represented, native.Code, out.Code, out.Body)
		}
	}
	// Production mode ignores the development subject, including a bootstrap name.
	secure := NewHubHandler(repository, controlplane.HandlerOptions{BootstrapAdministrators: []string{"alice"}})
	if out := auditConnectRequest(secure, "alice", `{}`); out.Code != 401 {
		t.Fatalf("spoofed bootstrap accepted: %d %s", out.Code, out.Body)
	}
}

func auditChainConnectRequest(handler http.Handler, actor, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/conduit.governance.v1.AuthorizationAuditChainReadService/ListAuthorizationAuditChain", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Connect-Protocol-Version", "1")
	if actor != "" {
		r.Header.Set("X-Platform-Subject", actor)
	}
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, r)
	return out
}
func bodyWithAuditCursor(t *testing.T, body []byte, cursor string) []byte {
	t.Helper()
	var q map[string]any
	if err := json.Unmarshal(body, &q); err != nil {
		t.Fatal(err)
	}
	q["cursor"] = cursor
	out, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestAuditChainConnectMatchesRESTPagesAndRejectsCursorRebinding(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	epoch := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 7 {
		resource := "ri.audit"
		if i == 3 {
			resource = "ri.other"
		}
		event := domain.NewAuthorizationCheckEvent(epoch.Add(time.Duration(i)*time.Minute), "seed", "read", resource, domain.DecisionAllow, "seeded", nil)
		event.ID = fmt.Sprintf("event-%d", i)
		if _, err := repository.AppendAuthorizationAudit(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}})
	for _, descending := range []bool{false, true} {
		cursor := ""
		seen := map[string]bool{}
		for pageNumber := 0; pageNumber < 10; pageNumber++ {
			q := url.Values{"view": {"chain"}, "resourceRid": {"ri.audit"}, "limit": {"2"}, "since": {epoch.Format(time.RFC3339)}, "until": {epoch.Add(7 * time.Minute).Format(time.RFC3339)}, "cursor": {cursor}}
			if descending {
				q.Set("order", "desc")
			}
			req := httptest.NewRequest("GET", "/v1/authorization-audit?"+q.Encode(), nil)
			req.Header.Set("X-Platform-Subject", "admin")
			native := httptest.NewRecorder()
			handler.ServeHTTP(native, req)
			body, _ := json.Marshal(map[string]any{"resourceRid": "ri.audit", "limit": 2, "since": q.Get("since"), "until": q.Get("until"), "cursor": cursor, "descending": descending})
			wire := auditChainConnectRequest(handler, "admin", string(body))
			if native.Code != 200 || wire.Code != 200 {
				t.Fatalf("REST %d %s RPC %d %s", native.Code, native.Body, wire.Code, wire.Body)
			}
			var expected struct {
				Entries     []domain.AuthorizationAuditChainEntry `json:"entries"`
				Total       int                                   `json:"total"`
				PageSize    int                                   `json:"pageSize"`
				Limit       int                                   `json:"limit"`
				NextCursor  string                                `json:"nextCursor"`
				HasPrevious bool                                  `json:"hasPrevious"`
			}
			actual := &governancev1.ListAuthorizationAuditChainResponse{}
			if json.Unmarshal(native.Body.Bytes(), &expected) != nil || protojson.Unmarshal(wire.Body.Bytes(), actual) != nil {
				t.Fatal("decode page")
			}
			if actual.Total != int64(expected.Total) || actual.Total != 6 || actual.PageSize != int32(expected.PageSize) || actual.Limit != int32(expected.Limit) || actual.NextCursor != expected.NextCursor || actual.HasPrevious != expected.HasPrevious || len(actual.Entries) != len(expected.Entries) {
				t.Fatalf("page mismatch: %+v %v", expected, actual)
			}
			for i, event := range actual.Entries {
				if event.Id != expected.Entries[i].ID || seen[event.Id] {
					t.Fatalf("changed or repeated event: %s", event.Id)
				}
				e := expected.Entries[i]
				if event.Sequence != e.Sequence || event.Actor != e.Actor || event.Action != e.Action || event.Decision != e.Decision || event.PreviousDigest != e.PreviousDigest || event.Digest != e.Digest || event.ResourceRid != e.ResourceRID || event.Origin != e.Origin || !bytes.Equal(event.Payload, e.Payload) || event.CreatedAt == nil || !event.CreatedAt.AsTime().Equal(e.CreatedAt) {
					t.Fatalf("projection material changed: native=%+v RPC=%v", e, event)
				}
				seen[event.Id] = true
			}
			if actual.NextCursor == "" {
				break
			}
			if pageNumber == 0 {
				rebound, _ := json.Marshal(map[string]any{"resourceRid": "ri.other", "cursor": actual.NextCursor})
				if refused := auditChainConnectRequest(handler, "admin", string(rebound)); refused.Code != 400 {
					t.Fatalf("cursor rebound: %d %s", refused.Code, refused.Body)
				}
			}
			if pageNumber == 0 {
				for _, changed := range []map[string]any{
					{"resourceRid": "ri.audit", "cursor": actual.NextCursor, "descending": !descending, "since": q.Get("since"), "until": q.Get("until")},
					{"resourceRid": "ri.audit", "cursor": actual.NextCursor, "descending": descending, "since": epoch.Add(time.Minute).Format(time.RFC3339), "until": q.Get("until")},
				} {
					b, _ := json.Marshal(changed)
					if refused := auditChainConnectRequest(handler, "admin", string(b)); refused.Code != 400 {
						t.Fatalf("projection cursor rebound: %d %s", refused.Code, refused.Body)
					}
				}
				// A chain cursor must never be accepted by the event procedure.
				if refused := auditConnectRequest(handler, "admin", string(bodyWithAuditCursor(t, body, actual.NextCursor))); refused.Code != 400 {
					t.Fatalf("chain cursor accepted by events: %d %s", refused.Code, refused.Body)
				}
			}
			cursor = actual.NextCursor
		}
		if len(seen) != 6 {
			t.Fatalf("incomplete walk: %v", seen)
		}
	}
	for _, actor := range []string{"viewer"} {
		if out := auditChainConnectRequest(handler, actor, "{broken"); out.Code != 403 {
			t.Fatalf("nonadmin decoded: %d %s", out.Code, out.Body)
		}
	}
	secure := NewHubHandler(repository, controlplane.HandlerOptions{})
	if out := auditChainConnectRequest(secure, "", "{broken"); out.Code != 401 {
		t.Fatalf("anonymous decoded: %d %s", out.Code, out.Body)
	}
}

func TestAuditChainConnectPreservesDelegatingAdministratorCarrier(t *testing.T) {
	_, repository, _ := delegationHub(t)
	handler := NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"alice"}})
	for _, tc := range []struct {
		actor, represented string
		status             int
	}{
		{"alice", "", 200}, {"alice", "bob", 200}, {"robot", "alice", 403}, {"bob", "alice", 403}, {"bob", "", 403},
	} {
		native := as(t, handler, "/v1/authorization-audit?view=chain", tc.actor, tc.represented)
		req := httptest.NewRequest("POST", "/conduit.governance.v1.AuthorizationAuditChainReadService/ListAuthorizationAuditChain", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Connect-Protocol-Version", "1")
		req.Header.Set("X-Platform-Subject", tc.actor)
		if tc.represented != "" {
			req.Header.Set("X-Platform-Service-Account", "true")
			req.Header.Set("X-Platform-On-Behalf-Of", tc.represented)
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if native.Code != tc.status || out.Code != tc.status {
			t.Fatalf("%s as %s: REST %d RPC %d %s", tc.actor, tc.represented, native.Code, out.Code, out.Body)
		}
	}
	// Production mode ignores the development subject, including a bootstrap name.
	secure := NewHubHandler(repository, controlplane.HandlerOptions{BootstrapAdministrators: []string{"alice"}})
	if out := auditChainConnectRequest(secure, "alice", `{}`); out.Code != 401 {
		t.Fatalf("spoofed bootstrap accepted: %d %s", out.Code, out.Body)
	}
}

func TestAuditChainConnectRejectsInvalidBoundsAndEventCursor(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	for i := range 2 {
		event := domain.NewAuthorizationCheckEvent(time.Date(2020, 1, 1, 0, i, 0, 0, time.UTC), "seed", "read", "ri.audit", domain.DecisionAllow, "seeded", nil)
		event.ID = fmt.Sprintf("bounded-%d", i)
		if _, err := repository.AppendAuthorizationAudit(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}})
	for _, body := range []string{`{"limit":0}`, `{"limit":201}`, `{"cursor":"broken"}`, `{"since":"2020-01-02T00:00:00Z","until":"2020-01-01T00:00:00Z"}`, `{"since":"2020-01-01T00:00:00Z","until":"2020-01-01T00:00:00Z"}`} {
		if out := auditChainConnectRequest(handler, "admin", body); out.Code != 400 {
			t.Fatalf("invalid bounds accepted %s: %d %s", body, out.Code, out.Body)
		}
	}
	page := auditConnectRequest(handler, "admin", `{"limit":1}`)
	events := &governancev1.ListAuthorizationAuditEventsResponse{}
	if page.Code != 200 || protojson.Unmarshal(page.Body.Bytes(), events) != nil || events.NextCursor == "" {
		t.Fatalf("event cursor fixture: %d %s", page.Code, page.Body)
	}
	body, _ := json.Marshal(map[string]any{"limit": 1, "cursor": events.NextCursor})
	if out := auditChainConnectRequest(handler, "admin", string(body)); out.Code != 400 {
		t.Fatalf("event cursor accepted as chain: %d %s", out.Code, out.Body)
	}
	out := auditChainConnectRequest(handler, "admin", `{}`)
	chain := &governancev1.ListAuthorizationAuditChainResponse{}
	if out.Code != 200 || protojson.Unmarshal(out.Body.Bytes(), chain) != nil || chain.Limit != 100 || len(chain.Entries) != 2 || chain.Entries[0].Sequence != 1 || chain.Entries[1].Sequence != 2 || chain.Entries[1].PreviousDigest != chain.Entries[0].Digest {
		t.Fatalf("default projection: %d %s", out.Code, out.Body)
	}
}

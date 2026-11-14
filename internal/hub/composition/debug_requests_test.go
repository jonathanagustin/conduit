package composition

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	debugmemory "github.com/jonathanagustin/conduit/internal/debugsessions/adapters/memory"
	debugdomain "github.com/jonathanagustin/conduit/internal/debugsessions/domain"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
)

func debugHTTP(handler http.Handler, subject, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("X-Platform-Subject", subject)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
func debugBody(id, environment, namespace string) string {
	b, _ := json.Marshal(map[string]any{"sessionId": id, "target": map[string]string{"environment": environment, "namespace": namespace, "profile": "requested-profile"}})
	return string(b)
}
func debugHub(t *testing.T) (http.Handler, *controlplane.GovernanceService, *controlplane.MemoryRepository) {
	t.Helper()
	repository := controlplane.NewMemoryRepository()
	service := controlplane.NewGovernanceService(repository)
	for _, name := range []string{"alice", "bob", "automation"} {
		if _, err := service.PutTeam(t.Context(), controlplane.Team{Name: name, DisplayName: name, AdminUsers: []string{name}, MemberUsers: []string{name}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, environment := range []string{"env", "other-env"} {
		state := desiredState(environment, "io.conduit:probe")
		state.Environment.Namespaces = []controlplane.NamespaceTarget{{RID: "ri.namespace.app", Name: "app"}, {RID: "ri.namespace.other", Name: "other"}}
		state.Entities[0].Namespace = "app"
		if _, err := controlplane.NewLifecycle(repository).CreatePlan(t.Context(), state); err != nil {
			t.Fatal(err)
		}
	}
	handler, err := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"}}, Options{DebugSessionJournal: debugmemory.New()})
	if err != nil {
		t.Fatal(err)
	}
	return handler, service, repository
}
func debugGrant(t *testing.T, service *controlplane.GovernanceService, team, resource string, operations ...string) controlplane.ResourceGrant {
	t.Helper()
	grant, err := service.PutGrant(t.Context(), controlplane.ResourceGrant{ResourceRID: resource, Team: team, Operations: operations})
	if err != nil {
		t.Fatal(err)
	}
	return grant
}
func TestDebugRequestRoutesRequireBothEnvironmentAndNamespaceGrants(t *testing.T) {
	handler, service, _ := debugHub(t)
	id := "dsr_abcdefghijklmnopqrstuvwxyz"
	body := debugBody(id, "env", "app")
	debugGrant(t, service, "alice", "environment/env", governance.OperationCreateDebugRequest, governance.OperationReadDebugRequest)
	if r := debugHTTP(handler, "alice", "POST", "/v1/debug-session-requests", body); r.Code != 403 {
		t.Fatal("Environment grant escaped namespace", r.Code, r.Body)
	}
	debugGrant(t, service, "alice", "environment/env/namespace/other", governance.OperationCreateDebugRequest, governance.OperationReadDebugRequest)
	if r := debugHTTP(handler, "alice", "POST", "/v1/debug-session-requests", body); r.Code != 403 {
		t.Fatal("wrong namespace grant", r.Code, r.Body)
	}
	grant := debugGrant(t, service, "alice", "environment/env/namespace/app", governance.OperationCreateDebugRequest, governance.OperationReadDebugRequest)
	r := debugHTTP(handler, "alice", "POST", "/v1/debug-session-requests", body)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body)
	}
	var record debugdomain.Event
	if err := json.Unmarshal(r.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.Owner != "alice" || record.Actor != "alice" || record.State != debugdomain.Requested || record.ValueTier != debugdomain.ValueTier {
		t.Fatal(record)
	}
	if r.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private metadata is cacheable")
	}
	if r := debugHTTP(handler, "alice", "POST", "/v1/debug-session-requests", debugBody("dsr_bcdefghijklmnopqrstuvwxyz2", "other-env", "app")); r.Code != 403 {
		t.Fatal("wrong Environment", r.Code, r.Body)
	}
	grant.Operations = []string{governance.OperationReadResource}
	if _, err := service.PutGrant(t.Context(), grant); err != nil {
		t.Fatal(err)
	}
	if r := debugHTTP(handler, "alice", "GET", "/v1/debug-session-requests/"+id, ""); r.Code != 403 {
		t.Fatal("revoked namespace grant", r.Code, r.Body)
	}
}
func TestDebugRequestOwnershipAndExplicitManagerPermission(t *testing.T) {
	handler, service, repository := debugHub(t)
	for _, who := range []string{"alice", "bob"} {
		for _, resource := range []string{"environment/env", "environment/env/namespace/app"} {
			debugGrant(t, service, who, resource, governance.OperationCreateDebugRequest, governance.OperationReadDebugRequest)
		}
	}
	id := "dsr_abcdefghijklmnopqrstuvwxyz"
	if r := debugHTTP(handler, "alice", "POST", "/v1/debug-session-requests", debugBody(id, "env", "app")); r.Code != 200 {
		t.Fatal(r.Code, r.Body)
	}
	for _, action := range []struct{ method, path, body string }{{"GET", "/v1/debug-session-requests/" + id, ""}, {"GET", "/v1/debug-session-requests/" + id + "/history", ""}, {"POST", "/v1/debug-session-requests/" + id + "/revoke", "{}"}, {"DELETE", "/v1/debug-session-requests/" + id, ""}} {
		if r := debugHTTP(handler, "bob", action.method, action.path, action.body); r.Code != 404 {
			t.Fatal(action, r.Code, r.Body)
		}
	}
	for _, resource := range []string{"environment/env", "environment/env/namespace/app"} {
		grants, err := repository.ListResourceGrants(t.Context(), resource)
		if err != nil {
			t.Fatal(err)
		}
		for _, grant := range grants {
			if grant.Team != "bob" {
				continue
			}
			grant.Operations = append(grant.Operations, governance.OperationManageDebugRequest)
			if _, err := service.PutGrant(t.Context(), grant); err != nil {
				t.Fatal(err)
			}
		}
	}
	if r := debugHTTP(handler, "bob", "POST", "/v1/debug-session-requests/"+id+"/revoke", "{}"); r.Code != 200 {
		t.Fatal(r.Code, r.Body)
	}
	r := debugHTTP(handler, "alice", "GET", "/v1/debug-session-requests/"+id+"/history", "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"actor":"bob"`) {
		t.Fatal(r.Code, r.Body)
	}
}
func TestDebugRequestDelegationRetainsOriginalAndEffectiveIdentities(t *testing.T) {
	handler, service, _ := debugHub(t)
	for _, resource := range []string{"environment/env", "environment/env/namespace/app"} {
		debugGrant(t, service, "alice", resource, governance.OperationCreateDebugRequest, governance.OperationReadDebugRequest)
	}
	body := debugBody("dsr_abcdefghijklmnopqrstuvwxyz", "env", "app")
	send := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/v1/debug-session-requests", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Platform-Subject", "automation")
		r.Header.Set("X-Platform-Service-Account", "true")
		r.Header.Set("X-Platform-On-Behalf-Of", "alice")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if r := send(); r.Code != 403 {
		t.Fatal("delegation without authority", r.Code, r.Body)
	}
	debugGrant(t, service, "automation", "platform/root", governance.OperationActOnBehalf)
	r := send()
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body)
	}
	var record debugdomain.Event
	if err := json.Unmarshal(r.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.Actor != "automation" || record.Owner != "alice" {
		t.Fatal(record)
	}
}
func TestNativeRequestCreationNeedsDeclaredNamespaceAndCannotAcceptActorFields(t *testing.T) {
	handler, _, _ := debugHub(t)
	id := "dsr_abcdefghijklmnopqrstuvwxyz"
	if r := debugHTTP(handler, "root", "POST", "/v1/debug-session-requests", debugBody(id, "env", "missing")); r.Code != 404 {
		t.Fatal(r.Code, r.Body)
	}
	body := debugBody(id, "env", "app")
	body = strings.Replace(body, `"sessionId":`, `"owner":"root","sessionId":`, 1)
	if r := debugHTTP(handler, "root", "POST", "/v1/debug-session-requests", body); r.Code != 400 {
		t.Fatal("accepted forged owner", r.Code, r.Body)
	}
	r := httptest.NewRequest("POST", "/v1/debug-session-requests", strings.NewReader(debugBody(id, "env", "app")))
	r.Header.Set("X-Platform-Subject", "root")
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatal("accepted simple cross-origin content type", w.Code, w.Body)
	}
}

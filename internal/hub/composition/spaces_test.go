package composition

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	spacememory "github.com/jonathanagustin/conduit/internal/spaces/adapters/memory"
	spaces "github.com/jonathanagustin/conduit/internal/spaces/domain"
)

func spaceRequest(handler http.Handler, subject, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("X-Platform-Subject", subject)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
func createNativeSpace(t *testing.T, handler http.Handler, name string) spaces.Space {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{"name": name})
	response := spaceRequest(handler, "admin", http.MethodPost, "/v1/spaces", string(payload))
	if response.Code != http.StatusCreated {
		t.Fatalf("create Space: %d %s", response.Code, response.Body)
	}
	var value spaces.Space
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if !spaces.ValidID(value.ID) || value.CreatedBy != "admin" || value.ValueTier != "CONDUIT_NATIVE" {
		t.Fatalf("creation provenance: %#v", value)
	}
	return value
}
func TestSpaceHTTPSeparatesGroupingVisibilityFromResourcePermissions(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	repository := controlplane.NewMemoryRepository()
	governance := controlplane.NewGovernanceService(repository)
	store := spacememory.New()
	if _, err := governance.PutTeam(ctx, controlplane.Team{Name: "tenant-a", DisplayName: "Tenant A", AdminUsers: []string{"alice"}, MemberUsers: []string{"alice"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(ctx, controlplane.ResourceGrant{ResourceRID: "environment/prod-a", Team: "tenant-a", Operations: []string{controlplane.OperationReadResource}}); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}}, Options{Spaces: store})
	if err != nil {
		t.Fatal(err)
	}
	first := createNativeSpace(t, handler, "First")
	second := createNativeSpace(t, handler, "Second")
	for _, entry := range []struct{ space, kind, id string }{{first.ID, spaces.Environment, "prod-a"}, {first.ID, spaces.Product, "com.private:other"}, {second.ID, spaces.Environment, "prod-b"}} {
		body, _ := json.Marshal(spaces.Resource{Kind: entry.kind, ID: entry.id})
		response := spaceRequest(handler, "admin", http.MethodPut, "/v1/spaces/"+entry.space+"/bindings", string(body))
		if response.Code != http.StatusOK {
			t.Fatalf("bind: %d %s", response.Code, response.Body)
		}
	}
	response := spaceRequest(handler, "alice", http.MethodGet, "/v1/spaces", "")
	if response.Code != http.StatusOK {
		t.Fatalf("list: %d %s", response.Code, response.Body)
	}
	var listed struct {
		Spaces []spaces.Space `json:"spaces"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Spaces) != 1 || listed.Spaces[0].ID != first.ID {
		t.Fatalf("cross-tenant Space disclosure: %s", response.Body)
	}
	response = spaceRequest(handler, "alice", http.MethodGet, "/v1/spaces/"+first.ID+"/bindings", "")
	var roster struct {
		Bindings []spaces.Binding `json:"bindings"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &roster); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || len(roster.Bindings) != 1 || roster.Bindings[0].Resource.ID != "prod-a" {
		t.Fatalf("Space roster granted unrelated resource visibility: %d %s", response.Code, response.Body)
	}
	for _, path := range []string{"/v1/spaces/" + second.ID, "/v1/spaces/" + second.ID + "/bindings", "/v1/spaces/space_ffffffffffffffffffffffffffffffff"} {
		response = spaceRequest(handler, "alice", http.MethodGet, path, "")
		if response.Code != http.StatusForbidden {
			t.Fatalf("unreadable Space answered %d: %s", response.Code, response.Body)
		}
	}
	response = spaceRequest(handler, "alice", http.MethodPost, "/v1/spaces", `{"name":"unapproved"}`)
	if response.Code != http.StatusForbidden {
		t.Fatalf("reader created a Space: %d", response.Code)
	}
	response = spaceRequest(handler, "alice", http.MethodPut, "/v1/spaces/"+second.ID+"/bindings", `{"kind":"ENVIRONMENT","id":"prod-a"}`)
	if response.Code != http.StatusForbidden {
		t.Fatalf("reader assigned a resource: %d", response.Code)
	}
	response = spaceRequest(handler, "admin", http.MethodPut, "/v1/spaces/"+second.ID+"/bindings", `{"kind":"ENVIRONMENT","id":"prod-a"}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("Space assignment moved implicitly: %d %s", response.Code, response.Body)
	}
	binding, err := store.GetBinding(ctx, spaces.Resource{Kind: spaces.Environment, ID: "prod-a"})
	if err != nil || binding.SpaceID != first.ID || binding.CreatedBy != "admin" {
		t.Fatalf("assignment changed: %#v, %v", binding, err)
	}
	for _, body := range []string{`{"name":"spoof","createdBy":"someone-else"}`, `{"name":"spoof","valueTier":"OBSERVED"}`, `{"name":"first"} {"name":"second"}`} {
		response = spaceRequest(handler, "admin", http.MethodPost, "/v1/spaces", body)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("untrusted creation metadata accepted: %d %s", response.Code, response.Body)
		}
	}
	all, err := store.ListSpaces(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("invalid requests mutated grouping: %#v, %v", all, err)
	}
}
func TestSpaceHTTPDoesNotExposeIdentityProviderDiagnostics(t *testing.T) {
	t.Parallel()
	handler, err := NewHub(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{OperatorIdentityResolver: func(*http.Request) (controlplane.IdentityClaims, error) {
		return controlplane.IdentityClaims{}, errors.New("private-provider-diagnostic")
	}}, Options{Spaces: spacememory.New()})
	if err != nil {
		t.Fatal(err)
	}
	response := spaceRequest(handler, "", http.MethodGet, "/v1/spaces", "")
	if response.Code < 400 || strings.Contains(response.Body.String(), "private-provider-diagnostic") {
		t.Fatalf("identity failure disclosed provider diagnostics: %d %s", response.Code, response.Body)
	}
}

func TestSpaceHTTPDelegationDoesNotInheritBootstrapAdministration(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	repository := controlplane.NewMemoryRepository()
	service := controlplane.NewGovernanceService(repository)
	store := spacememory.New()
	if _, err := service.PutTeam(ctx, controlplane.Team{Name: "automation", DisplayName: "Automation", AdminUsers: []string{"robot"}, MemberUsers: []string{"robot", "admin"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutGrant(ctx, controlplane.ResourceGrant{ResourceRID: "platform/root", Team: "automation", Operations: []string{controlplane.OperationActOnBehalf}}); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}}, Options{Spaces: store})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/spaces", strings.NewReader(`{"name":"escalation"}`))
	request.Header.Set("X-Platform-Subject", "robot")
	request.Header.Set("X-Platform-Service-Account", "true")
	request.Header.Set("X-Platform-On-Behalf-Of", "admin")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("delegation inherited bootstrap administration: %d %s", response.Code, response.Body)
	}
	values, err := store.ListSpaces(ctx)
	if err != nil || len(values) != 0 {
		t.Fatalf("denied delegation created grouping: %#v, %v", values, err)
	}
}

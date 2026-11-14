package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/ontology/domain"
	"github.com/jonathanagustin/conduit/internal/ontology/ports"
)

type fakeSource struct {
	catalog domain.Catalog
	applied []domain.ApplyRequest
	result  domain.ApplyResult
}

func (f *fakeSource) Catalog(context.Context, func(string) bool) (domain.Catalog, error) {
	return f.catalog, nil
}

func (f *fakeSource) Apply(_ context.Context, _ ports.Caller, request domain.ApplyRequest) (domain.ApplyResult, error) {
	f.applied = append(f.applied, request)
	return f.result, nil
}

func TestAnonymousCallerIsRefused(t *testing.T) {
	t.Parallel()
	handler := mux(Config{Source: &fakeSource{}})
	for _, target := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v2/ontologies", ""},
		{http.MethodGet, "/api/v2/ontologies/conduit/fullMetadata", ""},
		{http.MethodGet, "/api/v2/ontologies/conduit/objectTypes", ""},
		{http.MethodGet, "/api/v2/ontologies/conduit/interfaceTypes", ""},
		{http.MethodGet, "/api/v2/ontologies/conduit/actionTypes", ""},
		{http.MethodPost, "/api/v2/ontologies/conduit/objectSets/loadObjects", `{}`},
		{http.MethodPost, "/api/v2/ontologies/conduit/actions/createChangeRequest/apply", `{}`},
	} {
		response := request(handler, target.method, target.path, target.body, false)
		if response.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", target.method, target.path, response.Code)
		}
	}
}

func TestReadsAndLoadObjectsUseThePublishedShape(t *testing.T) {
	t.Parallel()
	source := &fakeSource{catalog: domain.Catalog{Objects: []domain.Object{
		{Type: "Environment", PrimaryKey: "east", Properties: map[string]string{"id": "east", "generation": "1"}},
		{Type: "Plan", PrimaryKey: "plan-east", Properties: map[string]string{"id": "plan-east", "environment": "east"}},
	}, Links: []domain.Link{
		{APIName: "plans", FromType: "Environment", FromKey: "east", ToType: "Plan", ToKey: "plan-east"},
	}}}
	handler := mux(Config{Source: source, Authorize: allowAll})
	metadata := request(handler, http.MethodGet, "/api/v2/ontologies/conduit/fullMetadata", "", true)
	if metadata.Code != http.StatusOK || !strings.Contains(metadata.Body.String(), `"objectTypes"`) ||
		!strings.Contains(metadata.Body.String(), `"interfaceTypes":{}`) {
		t.Fatalf("metadata %d %s", metadata.Code, metadata.Body.String())
	}
	if response := request(handler, http.MethodGet, "/api/v2/ontologies/other", "", true); response.Code != http.StatusNotFound {
		t.Fatalf("unknown ontology %d", response.Code)
	}
	loaded := request(handler, http.MethodPost, "/api/v2/ontologies/conduit/objectSets/loadObjects", `{
		"objectSet":{"type":"searchAround","link":"plans","objectSet":{"type":"filter","objectSet":{"type":"base","objectType":"Environment"},"where":{"type":"eq","field":"id","value":"east"}}},
		"select":["environment"],"pageSize":1
	}`, true)
	var page struct {
		Data []struct {
			PrimaryKey  string `json:"__primaryKey"`
			APIName     string `json:"__apiName"`
			Environment string `json:"environment"`
		} `json:"data"`
		TotalCount string `json:"totalCount"`
	}
	if loaded.Code != http.StatusOK || json.Unmarshal(loaded.Body.Bytes(), &page) != nil ||
		page.TotalCount != "1" || len(page.Data) != 1 || page.Data[0].PrimaryKey != "plan-east" || page.Data[0].APIName != "Plan" {
		t.Fatalf("load %d %s", loaded.Code, loaded.Body.String())
	}
}

func TestApplyReturnsValidationAndDoesNotInventApproval(t *testing.T) {
	t.Parallel()
	source := &fakeSource{result: domain.ApplyResult{
		Valid: true, ChangeRequestID: "crq-1", ChangeRequestState: "PENDING_APPROVALS", Stored: true,
	}}
	handler := mux(Config{Source: source, Authorize: allowAll, Caller: func(*http.Request) (ports.Caller, error) {
		return ports.Caller{Username: "author"}, nil
	}})
	response := request(handler, http.MethodPost, "/api/v2/ontologies/conduit/actions/createChangeRequest/apply", `{
		"parameters":{"environment":"east","targetKind":"ENVIRONMENT","targetId":"east","changeType":"UPDATE_ENVIRONMENT_SETTINGS"},
		"options":{"mode":"VALIDATE_AND_EXECUTE"}
	}`, true)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"result":"VALID"`) ||
		!strings.Contains(response.Body.String(), `"primaryKey":"crq-1"`) {
		t.Fatalf("apply %d %s", response.Code, response.Body.String())
	}
	if len(source.applied) != 1 || source.applied[0].Mode != domain.ModeValidateAndExecute {
		t.Fatalf("applied %#v", source.applied)
	}
	denied := mux(Config{Source: source, Authorize: allowAll, Permit: func(*http.Request, string) error {
		return StatusError{Status: http.StatusForbidden, Message: "forbidden"}
	}, Caller: func(*http.Request) (ports.Caller, error) {
		return ports.Caller{Username: "author"}, nil
	}})
	if response := request(denied, http.MethodPost, "/api/v2/ontologies/conduit/actions/createChangeRequest/apply", `{
		"parameters":{"environment":"west"},"options":{"mode":"VALIDATE_ONLY"}
	}`, true); response.Code != http.StatusForbidden {
		t.Fatalf("permit %d %s", response.Code, response.Body.String())
	}
}

func mux(cfg Config) http.Handler {
	router := http.NewServeMux()
	Register(router, cfg)
	return router
}

func allowAll(*http.Request) error { return nil }

func request(handler http.Handler, method, path, body string, _ bool) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(method, path, reader))
	return recorder
}

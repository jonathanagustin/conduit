// Package conformance tests only publicly sourced contracts against a local Hub.
package conformance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
)

const snapshot = "../../../docs/reference-platform/snapshots/reference-openapi-c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml"
const publicSource = "https://github.com/palantir/apollo-openapi/blob/c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml"

type operationCase struct{ Operation, Method, Path, Source string }
type inventory struct {
	OpenAPISHA256 string
	Operations    []operationCase
	GraphQL       []struct{ Name, Query, Source, Coverage string }
}

func sources(t *testing.T) inventory {
	t.Helper()
	data, err := os.ReadFile("testdata/public-sources.json")
	if err != nil {
		t.Fatal(err)
	}
	var result inventory
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func publicRouter(t *testing.T, manifest inventory) (routers.Router, *openapi3.T) {
	t.Helper()
	data, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != manifest.OpenAPISHA256 {
		t.Fatal("public snapshot changed without reviewing the source inventory")
	}
	lines := strings.Split(string(data), "\n")
	for _, tc := range manifest.Operations {
		prefix := publicSource + "#L"
		if !strings.HasPrefix(tc.Source, prefix) {
			t.Fatal("non-public source citation")
		}
		line, err := strconv.Atoi(strings.TrimPrefix(tc.Source, prefix))
		if err != nil || line < 1 || line > len(lines) || strings.TrimSpace(lines[line-1]) != "operationId: "+tc.Operation {
			t.Fatalf("source citation does not identify %s", tc.Operation)
		}
	}
	loader := openapi3.NewLoader() // External references are disabled; no remote loading.
	doc, err := loader.LoadFromData(data)
	if err != nil {
		t.Fatal(err)
	}
	router, err := legacy.NewRouter(doc, openapi3.AllowExtraSiblingFields("description"), openapi3.DisableExamplesValidation())
	if err != nil {
		t.Fatal(err)
	}
	return router, doc
}

// The seeded records are invented Conduit data, not a reference-platform fixture.
// No approval, execution, publication to a registry, or network dial is involved.
func localHub(t *testing.T) http.Handler {
	t.Helper()
	repository := controlplane.NewMemoryRepository()
	digest := "sha256:" + strings.Repeat("a", 64)
	state := controlplane.DesiredState{
		APIVersion: "platform.delivery/v1alpha1", Kind: "EnvironmentDeployment",
		Environment: controlplane.Environment{ID: "conformance-env", Generation: "synthetic-1", Namespaces: []controlplane.NamespaceTarget{{Name: "conformance", RID: "ri.namespace.main.namespace.conformance"}}},
		Releases:    []controlplane.Release{{Product: "org.conformance:app", Version: "1.0.0", Chart: controlplane.Artifact{Repository: "registry.invalid/app", Tag: "1.0.0", Digest: digest}, Images: []controlplane.Artifact{{Repository: "registry.invalid/app", Tag: "1.0.0", Digest: digest}}}},
		Entities:    []controlplane.Entity{{Name: "app", Product: "org.conformance:app", Version: "1.0.0", Namespace: "conformance"}},
	}
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	_, err := repository.CreatePlan(context.Background(), controlplane.PlanRecord{ID: "synthetic-plan", Status: controlplane.PlanRecorded, DesiredState: &state, CreatedAt: now, UpdatedAt: now, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	return composition.NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, AllowInsecureAgentIdentityHeaders: true, BootstrapAdministrators: []string{"conformance-operator"}})
}

func request(t *testing.T, handler http.Handler, method, path string, body any, agent bool) (*http.Request, *httptest.ResponseRecorder) {
	t.Helper()
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	if agent {
		req.Header.Set("X-Platform-Environment-ID", "conformance-env")
		req.Header.Set("X-Platform-Agent-ID", "conformance-agent")
	} else {
		req.Header.Set("X-Platform-Subject", "conformance-operator")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	// Request validation needs the original body, not the stream consumed by Hub.
	req.Body = io.NopCloser(bytes.NewReader(encoded))
	return req, response
}

func validate(t *testing.T, router routers.Router, operation string, req *http.Request, response *httptest.ResponseRecorder) error {
	t.Helper()
	route, params, err := router.FindRoute(req)
	if err != nil {
		return err
	}
	if route.Operation.OperationID != operation {
		return fmt.Errorf("wrong operation: %s", route.Operation.OperationID)
	}
	input := &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route, Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}}
	// Fixture identity is local-development-only. This validates public transport
	// data, not OAuth or reference-platform token acceptance.
	if err = openapi3filter.ValidateRequest(context.Background(), input); err != nil {
		return fmt.Errorf("request: %w", err)
	}
	output := &openapi3filter.ResponseValidationInput{RequestValidationInput: input, Status: response.Code, Header: response.Header(), Options: &openapi3filter.Options{IncludeResponseStatus: true}}
	output.SetBodyBytes(response.Body.Bytes())
	return openapi3filter.ValidateResponse(context.Background(), output)
}

func TestPublicRESTConformance(t *testing.T) {
	manifest := sources(t)
	router, doc := publicRouter(t, manifest)
	handler := localHub(t)
	available := map[string]bool{}
	for _, item := range doc.Paths.Map() {
		for _, op := range item.Operations() {
			available[op.OperationID] = true
		}
	}
	if len(available) != len(manifest.Operations) {
		t.Fatalf("coverage inventory drift: public=%d inventoried=%d", len(available), len(manifest.Operations))
	}
	seen := map[string]bool{}
	environmentRID, installationRID := "", ""
	for _, tc := range manifest.Operations {
		t.Run(tc.Operation, func(t *testing.T) {
			if !available[tc.Operation] || seen[tc.Operation] || !strings.HasPrefix(tc.Source, publicSource+"#L") {
				t.Fatal("missing, duplicate or non-public source citation")
			}
			seen[tc.Operation] = true
			t.Log(tc.Source)
			path := strings.ReplaceAll(strings.ReplaceAll(tc.Path, "{environmentRid}", environmentRID), "{installationRid}", installationRID)
			var body any
			switch tc.Operation {
			case "publishHelmChart":
				body = map[string]any{"extensions": map[string]any{"helmChart": map[string]any{"helmChartName": "app", "helmChartVersion": "1.0.0", "helmRepositoryUrl": "https://charts.invalid"}, "artifacts": []any{}, "productDependencies": []any{}}}
			case "publishService":
				body = map[string]any{"extensions": map[string]any{}}
			case "putStatusEntitiesInfo":
				body = map[string]any{"entitiesInfo": []any{}}
			case "putStatusEntitiesHealth":
				body = map[string]any{"entitiesHealth": []any{}}
			}
			req, response := request(t, handler, tc.Method, path, body, strings.HasPrefix(tc.Operation, "putStatus"))
			if err := validate(t, router, tc.Operation, req, response); err != nil {
				t.Fatalf("%s: %v; response %d %s", tc.Operation, err, response.Code, response.Body.String())
			}
			var document map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
				t.Fatal(err)
			}
			// Read generated IDs from local responses. Their values are not public
			// expectations and are never copied from a tenant or observed schema.
			switch tc.Operation {
			case "listEnvironments":
				rows, ok := document["environments"].([]any)
				if !ok || len(rows) != 1 {
					t.Fatal("synthetic environment not seeded")
				}
				environmentRID = rows[0].(map[string]any)["rid"].(string)
			case "listInstallations":
				rows, ok := document["installations"].([]any)
				if !ok || len(rows) != 1 {
					t.Fatal("synthetic installation not seeded")
				}
				installationRID = rows[0].(map[string]any)["rid"].(string)
			case "getProduct":
				// Harness control: a missing public-required property must be detected.
				delete(document, "id")
				invalid, _ := json.Marshal(document)
				bad := httptest.NewRecorder()
				bad.Code = response.Code
				bad.Header().Set("Content-Type", "application/json")
				_, _ = bad.Write(invalid)
				if validate(t, router, tc.Operation, req, bad) == nil {
					t.Fatal("validator accepted a missing required Product.id")
				}
			}
		})
	}
	for id := range available {
		if !seen[id] {
			t.Errorf("uncovered public operation: %s", id)
		}
	}
}

func TestPublicGraphQLExamples(t *testing.T) {
	manifest := sources(t)
	handler := localHub(t)
	if len(manifest.GraphQL) != 2 {
		t.Fatal("public example inventory drift")
	}
	for _, tc := range manifest.GraphQL {
		t.Run(tc.Name, func(t *testing.T) {
			if tc.Source != "https://www.palantir.com/docs/apollo/apollo-references/apollo-api-documentation/" {
				t.Fatal("non-public GraphQL source")
			}
			t.Log(tc.Source)
			_, response := request(t, handler, http.MethodPost, "/v1/apollo/graphql", map[string]any{"query": tc.Query}, false)
			if response.Code != http.StatusOK {
				t.Fatalf("GraphQL=%d %s", response.Code, response.Body.String())
			}
			var result struct {
				Data   map[string]json.RawMessage
				Errors []any
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if tc.Coverage == "UNCOVERED" {
				if len(result.Errors) == 0 {
					t.Fatal("known gap is now served; review and promote its coverage instead of leaving a stale exclusion")
				}
				if len(result.Errors) != 1 {
					t.Fatalf("unexpected failure instead of known coverage gap: %s", response.Body.String())
				}
				message, ok := result.Errors[0].(map[string]any)
				if !ok || message["message"] != `Cannot query field "me" on type "Query".` {
					t.Fatalf("unexpected failure instead of known coverage gap: %s", response.Body.String())
				}
				t.Skip("public GetCurrentUser is not implemented; explicitly UNCOVERED in the source inventory and report")
			}
			if tc.Coverage != "COVERED" {
				t.Fatal("unknown coverage state")
			}
			if len(result.Errors) > 0 {
				t.Fatalf("public selection rejected: %s", response.Body.String())
			}
			if tc.Name == "GetCurrentUser" {
				var me map[string]json.RawMessage
				if err := json.Unmarshal(result.Data["me"], &me); err != nil {
					t.Fatal(err)
				}
				for _, field := range []string{"id", "fullName"} {
					if _, ok := me[field]; !ok {
						t.Fatalf("public field missing: me.%s", field)
					}
				}
			} else {
				var apollo struct {
					Environments struct{ Environments []map[string]json.RawMessage }
				}
				if err := json.Unmarshal(result.Data["apollo"], &apollo); err != nil {
					t.Fatal(err)
				}
				if len(apollo.Environments.Environments) != 1 {
					t.Fatal("public environment selection did not reach synthetic record")
				}
				if _, ok := apollo.Environments.Environments[0]["id"]; !ok {
					t.Fatal("public field missing: environment.id")
				}
			}
		})
	}
}

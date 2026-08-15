package httpgraphql_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/apollographql/adapters/httpgraphql"
	"github.com/jonathanagustin/conduit/internal/apollographql/application"
	"github.com/jonathanagustin/conduit/internal/apollographql/ports"
	"github.com/jonathanagustin/conduit/internal/apollographql/schema"
)

func newHandler(t *testing.T) *httpgraphql.Handler {
	t.Helper()
	resolver, err := application.New()
	if err != nil {
		t.Fatalf("build resolver: %v", err)
	}
	return httpgraphql.New(resolver, func(http.ResponseWriter, *http.Request) (ports.Sources, bool) { return ports.Sources{}, true })
}

// The disclosure has to be reachable without the repository. A client fetching
// the schema must get the provenance annotations, not a stripped copy.
func TestTheServedSchemaCarriesItsProvenance(t *testing.T) {
	recorder := httptest.NewRecorder()
	newHandler(t).Schema(recorder, httptest.NewRequest(http.MethodGet, "/v1/apollo/graphql/schema", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d", recorder.Code)
	}
	body := recorder.Body.String()
	if body != schema.SDL() {
		t.Fatal("the served schema is not the embedded one")
	}
	for _, want := range []string{"@provenance(tier: OBSERVED)", "@provenance(tier: DOCUMENTED)"} {
		if !strings.Contains(body, want) {
			t.Errorf("the served schema does not carry %s", want)
		}
	}
	// CONDUIT_NATIVE is checked as a declared tier rather than as an annotation
	// in use: no served field currently carries it, because every one of them
	// traced to an Apollo source. The tier must still reach a client, so that an
	// interface which later does invent a coordinate can say so in a vocabulary
	// the client already understands.
	if !strings.Contains(body, "  CONDUIT_NATIVE\n") {
		t.Error("the served schema does not declare the CONDUIT_NATIVE tier")
	}
}

// A refused field is not a broken server. This pins the status code, because
// the natural instinct is to make it 500 and that would misreport a deliberate
// refusal as a fault.
func TestARefusedFieldStillAnswersWithOK(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/apollo/graphql",
		strings.NewReader(`{"query":"{ apollo { bundle(rid: \"ri.apollo.main.bundle.a\") { rid } } }"}`))
	newHandler(t).Query(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("a refusal was reported as HTTP %d", recorder.Code)
	}
	var response struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Extensions map[string]any `json:"extensions"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(response.Errors) == 0 {
		t.Fatal("the refusal did not reach the body")
	}
	if response.Extensions["provenance"] == nil {
		t.Fatal("the response carries no provenance disclosure")
	}
}

func TestAMalformedBodyIsRejected(t *testing.T) {
	recorder := httptest.NewRecorder()
	newHandler(t).Query(recorder, httptest.NewRequest(http.MethodPost, "/v1/apollo/graphql", strings.NewReader("not json")))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status %d", recorder.Code)
	}
}

func TestAnEmptyQueryIsRejected(t *testing.T) {
	recorder := httptest.NewRecorder()
	newHandler(t).Query(recorder, httptest.NewRequest(http.MethodPost, "/v1/apollo/graphql", strings.NewReader(`{}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status %d", recorder.Code)
	}
}

// An unbounded read on an authenticated endpoint is a way to spend memory
// rather than a way to ask a question.
//
// The padding sits in operationName rather than in the query, deliberately. A
// giant document would also be refused, but by exhausting the parser rather
// than by the limit -- and a check that passes because the server bogged down
// proves nothing about the limit. This body is trivial to parse once truncated,
// so a 400 can only come from the bound.
func TestAnOversizedBodyIsRejectedRatherThanRead(t *testing.T) {
	recorder := httptest.NewRecorder()
	oversized := `{"query":"{ apollo { environments(pageSize: 1) { nextPageToken } } }","operationName":"` +
		strings.Repeat("a", 512<<10) + `"}`
	newHandler(t).Query(recorder, httptest.NewRequest(http.MethodPost, "/v1/apollo/graphql", strings.NewReader(oversized)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("an oversized body was accepted with status %d", recorder.Code)
	}
}

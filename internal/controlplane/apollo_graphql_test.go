package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func graphqlRequest(t *testing.T, handler http.Handler, subject, query string) *httptest.ResponseRecorder {
	t.Helper()
	body := strings.NewReader(`{"query":` + mustJSON(t, query) + `}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/apollo/graphql", body)
	if subject != "" {
		request.Header.Set("X-Platform-Subject", subject)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// The GraphQL interface reads the same records the console's overview reads, so
// it has to be behind the same door. A read surface that skipped authentication
// because it was new would be a way around Governance rather than a view of it.
func TestApolloGraphQLRequiresAnOperator(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewMemoryRepository())
	response := graphqlRequest(t, handler, "", `{ apollo { plans(pageSize: 1) { nextPageToken } } }`)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated = %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "ApolloPlan") {
		t.Error("the refusal disclosed the shape of the interface to an unauthenticated caller")
	}
}

// The schema is published deliberately: a client must be able to see which
// coordinates were recovered and which Conduit invented before it writes a
// query. It describes the contract, not the fleet, so it carries no records.
func TestApolloGraphQLPublishesItsAnnotatedSchema(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewMemoryRepository())
	request := httptest.NewRequest(http.MethodGet, "/v1/apollo/graphql/schema", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("schema = %d %s", response.Code, response.Body.String())
	}
	for _, want := range []string{"@provenance(tier: OBSERVED)", "@provenance(tier: DOCUMENTED)", "CONDUIT_NATIVE"} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("the published schema does not carry %s", want)
		}
	}
}

func TestApolloGraphQLAnswersAnOperatorAndDisclosesWhatNoHubEstablished(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewMemoryRepository(), HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	response := graphqlRequest(t, handler, "local-developer", `{ apollo { plans(pageSize: 10) { plans { rid } } } }`)
	if response.Code != http.StatusOK {
		t.Fatalf("authenticated = %d %s", response.Code, response.Body.String())
	}
	var decoded struct {
		Errors     []struct{ Message string } `json:"errors"`
		Extensions struct {
			Provenance struct {
				DocumentedServed    []string `json:"documentedServed"`
				ConduitNativeServed []string `json:"conduitNativeServed"`
			} `json:"provenance"`
		} `json:"extensions"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Errors) != 0 {
		t.Fatalf("the plans panel refused for an authorized operator: %v", decoded.Errors)
	}
	documented := strings.Join(decoded.Extensions.Provenance.DocumentedServed, ",")
	// ApolloPlan.rid is absent because this repository has no plans, so nothing
	// under the collection was ever answered. The disclosure describes the
	// answer, not the document.
	if documented != "ApolloPlanPage.plans,ApolloQuery.plans" {
		t.Fatalf("the response did not disclose the unrecovered coordinates it served: %q", documented)
	}
	if len(decoded.Extensions.Provenance.ConduitNativeServed) != 0 {
		t.Fatalf(
			"coordinates Apollo names were disclosed as Conduit's invention: %v",
			decoded.Extensions.Provenance.ConduitNativeServed,
		)
	}
}

// The refusal that gives the interface its meaning, proved through the mounted
// route rather than only in the resolver's own tests.
func TestApolloGraphQLRefusesACoordinateConduitCannotAnswer(t *testing.T) {
	t.Parallel()
	repository := NewMemoryRepository()
	if _, err := NewLifecycle(repository).CreatePlan(t.Context(), foundation()); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	response := graphqlRequest(t, handler, "local-developer",
		`{ apollo { environments(pageSize: 1) { environments { namespaceId } } } }`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "Conduit holds no data behind it") {
		t.Fatalf("the unanswerable coordinate did not refuse: %s", response.Body.String())
	}
}

// The defect: authentication alone bound the whole Apollo projection, while
// every Apollo REST read requires OperationReadResource on platform/root. A
// principal holding nothing got every Environment, installation, namespace and
// recall the Hub governs by choosing GraphQL over REST -- which is the one
// thing internal/apollographql/ports says cannot happen, that a client cannot
// get a different answer by choosing a protocol.
//
// Refused rather than answered-empty on purpose. ListEnvironments and its
// siblings return a response with no error, so an unauthorized caller handed an
// empty projection would read "no environments" and could not tell that from a
// fleet with none -- the unknown-reads-as-none conflation that file exists to
// prevent.
func TestApolloGraphQLRefusesAnOperatorWhoHoldsNothing(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewMemoryRepository(), HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"},
	})

	response := graphqlRequest(t, handler, "someone", `{ apollo { environments(pageSize: 50) { environments { rid } } } }`)

	if response.Code != http.StatusForbidden {
		t.Fatalf("an operator with no grant read the Apollo projection: %d %s",
			response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "\"environments\"") {
		t.Errorf("the refusal still carried projection data: %s", response.Body.String())
	}
}

// And the administrator, who does hold platform read, still gets served -- so
// the check refuses the ungranted rather than everyone.
func TestApolloGraphQLStillAnswersAPrincipalHoldingPlatformRead(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewMemoryRepository(), HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"},
	})

	response := graphqlRequest(t, handler, "admin", `{ apollo { environments(pageSize: 50) { environments { rid } } } }`)

	if response.Code != http.StatusOK {
		t.Fatalf("an administrator was refused the Apollo projection: %d %s",
			response.Code, response.Body.String())
	}
}

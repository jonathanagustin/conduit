package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// uuidV4 is what a Conjure errorInstanceId is: the thing that makes one error
// traceable across a support boundary. A constant, or an empty string, would
// satisfy "the field is present" and none of its purpose.
var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type conjureEnvelope struct {
	ErrorCode       string            `json:"errorCode"`
	ErrorName       string            `json:"errorName"`
	ErrorInstanceID string            `json:"errorInstanceId"`
	Parameters      map[string]string `json:"parameters"`
	Error           string            `json:"error"`
}

// decodeConjureEnvelope insists the body decodes strictly. A response that
// merely happens to contain the right keys alongside others would be a
// different contract from the one the snapshot publishes, and the point of
// these envelopes is that a client generated from that snapshot can read them.
func decodeConjureEnvelope(t *testing.T, body string) conjureEnvelope {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.DisallowUnknownFields()
	var envelope conjureEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("body is not a published error envelope: %v\nbody: %s", err, body)
	}
	if !uuidV4.MatchString(envelope.ErrorInstanceID) {
		t.Errorf("errorInstanceId %q is not a UUIDv4; it cannot identify this error instance", envelope.ErrorInstanceID)
	}
	return envelope
}

// TestApolloReleaseMissServesTheReleaseNotFoundEnvelope pins the case the
// snapshot names most specifically.
//
// The contract requires parameters.mavenCoordinate on this error, so a client
// can say WHICH release was missing. Conduit previously served
// 404 {"error":"Release not found"} -- true, undeserializable against the
// published type, and silent about the coordinate.
func TestApolloReleaseMissServesTheReleaseNotFoundEnvelope(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewMemoryRepository(), HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	request := httptest.NewRequest(http.MethodGet, "/api/v0/products/com.example:absent/releases/9.9.9", nil)
	request.Header.Set("X-Platform-Subject", "local-developer")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("missing release = %d %s", response.Code, response.Body.String())
	}
	envelope := decodeConjureEnvelope(t, response.Body.String())
	if envelope.ErrorCode != "NOT_FOUND" {
		t.Errorf("errorCode = %q, want the enum the schema declares", envelope.ErrorCode)
	}
	if envelope.ErrorName != "ReleaseNotFound" {
		t.Errorf("errorName = %q", envelope.ErrorName)
	}
	// The coordinate is the whole reason this envelope has parameters.
	if got := envelope.Parameters["mavenCoordinate"]; got != "com.example:absent:9.9.9" {
		t.Errorf("parameters.mavenCoordinate = %q, want the coordinate that was asked for", got)
	}
}

// TestApolloCrossEnvironmentStatusServesTheNotEnvironmentOwnerEnvelope covers
// the refusal the snapshot describes as "You are not the environment owner of
// this Apollo environment".
func TestApolloCrossEnvironmentStatusServesTheNotEnvironmentOwnerEnvelope(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewMemoryRepository(), HandlerOptions{AllowInsecureAgentIdentityHeaders: true})
	request := httptest.NewRequest(http.MethodPost, "/api/v0/status/environments/prod/entities/health",
		strings.NewReader(`{"entitiesHealth":[]}`))
	request.Header.Set("X-Platform-Environment-ID", "dev-use2")
	request.Header.Set("X-Platform-Agent-ID", "agent-dev")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-Environment status = %d %s", response.Code, response.Body.String())
	}
	envelope := decodeConjureEnvelope(t, response.Body.String())
	if envelope.ErrorCode != "PERMISSION_DENIED" {
		t.Errorf("errorCode = %q, want the enum the schema declares", envelope.ErrorCode)
	}
	if envelope.ErrorName != "NotEnvironmentOwner" {
		t.Errorf("errorName = %q", envelope.ErrorName)
	}
	// The environment refused, not the environment the agent belongs to. A
	// client is asking "which environment am I not the owner of", and answering
	// with its own id would tell it nothing it did not send.
	if got := envelope.Parameters["environmentId"]; got != "prod" {
		t.Errorf("parameters.environmentId = %q, want the Environment the request addressed", got)
	}
}

// TestApolloUnauthenticatedRequestServesTheApiUsageDeniedEnvelope covers the
// schema whose description is about authentication -- "You have not properly
// authenticated with Apollo" -- which is why an unauthenticated 401 carries it.
func TestApolloUnauthenticatedRequestServesTheApiUsageDeniedEnvelope(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewMemoryRepository())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v0/products", nil))

	// The status is deliberately unchanged: the snapshot declares no status
	// codes at all, so it constrains the body and not this.
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated = %d %s", response.Code, response.Body.String())
	}
	envelope := decodeConjureEnvelope(t, response.Body.String())
	if envelope.ErrorCode != "PERMISSION_DENIED" {
		t.Errorf("errorCode = %q, want the enum the schema declares", envelope.ErrorCode)
	}
	if envelope.ErrorName != "ApiUsageDenied" {
		t.Errorf("errorName = %q", envelope.ErrorName)
	}
	// The helper's own message survives. It is the only description of what was
	// refused, and the envelope has nowhere else to carry it.
	if envelope.Error == "" {
		t.Error("the refusal carries no message at all")
	}
}

// TestApolloErrorInstanceIdIsPerInstance is the assertion that stops
// errorInstanceId being decoration. Two identical requests must not share one,
// or it identifies the error KIND -- which errorName already does -- rather
// than the occurrence a support conversation is about.
func TestApolloErrorInstanceIdIsPerInstance(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewMemoryRepository())
	seen := map[string]struct{}{}
	for range 8 {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v0/products", nil))
		envelope := decodeConjureEnvelope(t, response.Body.String())
		if _, repeated := seen[envelope.ErrorInstanceID]; repeated {
			t.Fatalf("errorInstanceId %s was reused across requests", envelope.ErrorInstanceID)
		}
		seen[envelope.ErrorInstanceID] = struct{}{}
	}
}

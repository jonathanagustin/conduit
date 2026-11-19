package composition

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	environmentsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"google.golang.org/protobuf/encoding/protojson"
)

const environmentObservationProcedure = "/conduit.environments.v1.EnvironmentObservationService/GetEnvironmentReportedState"

func observationRequest(handler http.Handler, actor, represented, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, environmentObservationProcedure, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connect-Protocol-Version", "1")
	if actor != "" {
		request.Header.Set("X-Platform-Subject", actor)
	}
	if represented != "" {
		request.Header.Set("X-Platform-Service-Account", "true")
		request.Header.Set("X-Platform-On-Behalf-Of", represented)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

type observationReadCounter struct {
	controlplane.Repository
	reads   int
	failure error
}

func (store *observationReadCounter) GetEnvironmentReport(ctx context.Context, id string) (controlplane.EnvironmentReport, error) {
	store.reads++
	if store.failure != nil {
		return controlplane.EnvironmentReport{Environment: "private-diagnostic"}, store.failure
	}
	return store.Repository.GetEnvironmentReport(ctx, id)
}

func TestEnvironmentObservationConnectMatchesRESTAndAuthorizesBeforeStore(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	instant := time.Date(2026, 11, 19, 1, 2, 3, 123456789, time.UTC)
	report := controlplane.EnvironmentReport{Environment: "env", AgentID: "agent", PlanID: "plan", ObservedAt: instant, Entities: []controlplane.ReportedEntity{{InstallationID: "install", Product: "example:app", Version: "1.2.3", Namespace: "ns", ReleaseDigest: "sha256:release", ConfigDigest: "sha256:config", Health: "HEALTHY", ConfigStatus: "APPLIED", RolloutStatus: "UP_TO_DATE", Lifecycle: "INSTALLED", ObservedAt: instant, HealthDetails: &controlplane.EntityHealthDetails{Quorum: "all", Nodes: []controlplane.EntityNodeHealth{{NodeID: "node", Containers: []controlplane.EntityContainerHealth{{ContainerID: "container", Liveness: true, Checks: []controlplane.HealthCheck{{ID: "check", Params: map[string]any{"literal": "<message>", "attempt": 3}}}}}}}}}}, Secrets: []controlplane.ReportedSecret{{Assignment: "assignment", Namespace: "ns", Revision: 3, ProviderRef: "vault://metadata", KubernetesSecretName: "secret-reference", Keys: []string{"key"}}}}
	if _, err := repository.PutEnvironmentReport(t.Context(), report); err != nil {
		t.Fatal(err)
	}
	governance := controlplane.NewGovernanceService(repository)
	if _, err := governance.PutTeam(t.Context(), controlplane.Team{Name: "readers", DisplayName: "Readers", AdminUsers: []string{"admin"}, MemberUsers: []string{"reader"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(t.Context(), controlplane.ResourceGrant{ResourceRID: "environment/env", Team: "readers", Operations: []string{controlplane.OperationReadResource}}); err != nil {
		t.Fatal(err)
	}
	store := &observationReadCounter{Repository: repository}
	handler := NewHubHandler(store, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}})
	for _, tc := range []struct {
		actor, environment string
		status             int
	}{{"admin", "env", 200}, {"reader", "env", 200}, {"outsider", "env", 403}, {"reader", "other", 403}, {"admin", "missing", 404}, {"", "env", 403}} {
		native := as(t, handler, "/v1/environments/"+tc.environment+"/reported-state", tc.actor, "")
		before := store.reads
		body, _ := json.Marshal(map[string]string{"environment": tc.environment})
		wire := observationRequest(handler, tc.actor, "", string(body))
		if native.Code != tc.status || wire.Code != tc.status {
			t.Fatalf("%s/%s REST %d %s RPC %d %s", tc.actor, tc.environment, native.Code, native.Body, wire.Code, wire.Body)
		}
		if (tc.status == 403 || tc.status == 401) && store.reads != before {
			t.Fatal("refused caller read report")
		}
		if wire.Code == 200 {
			var expected controlplane.EnvironmentReport
			actual := &environmentsv1.GetEnvironmentReportedStateResponse{}
			if json.Unmarshal(native.Body.Bytes(), &expected) != nil || protojson.Unmarshal(wire.Body.Bytes(), actual) != nil {
				t.Fatal("decode reported state")
			}
			got := actual.Reported
			if got == nil || got.Environment != expected.Environment || got.AgentId != expected.AgentID || got.PlanId != expected.PlanID || got.Revision != expected.Revision || !got.ObservedAt.AsTime().Equal(expected.ObservedAt) || len(got.Entities) != len(expected.Entities) || len(got.Secrets) != len(expected.Secrets) {
				t.Fatalf("report mismatch: %v %+v", got, expected)
			}
			entity := got.Entities[0]
			if entity.InstallationId != expected.Entities[0].InstallationID || entity.ConfigDigest != expected.Entities[0].ConfigDigest || entity.ReleaseDigest != expected.Entities[0].ReleaseDigest || !entity.ObservedAt.AsTime().Equal(expected.Entities[0].ObservedAt) || got.Secrets[0].ProviderRef != expected.Secrets[0].ProviderRef {
				t.Fatal("private authorized report fields changed")
			}
			params, _ := json.Marshal(expected.Entities[0].HealthDetails.Nodes[0].Containers[0].Checks[0].Params)
			if string(entity.HealthDetails.Nodes[0].Containers[0].Checks[0].ParamsJson) != string(params) {
				t.Fatal("native diagnostic parameters changed")
			}
		}
	}
	secure := NewHubHandler(store, controlplane.HandlerOptions{})
	before := store.reads
	if response := observationRequest(secure, "", "", "{"); response.Code != 401 || store.reads != before {
		t.Fatalf("anonymous parsed malformed body: %d", response.Code)
	}
	for _, body := range []string{`{}`, `{`} {
		if response := observationRequest(handler, "admin", "", body); response.Code != 400 || store.reads != before {
			t.Fatalf("invalid body reached store: %d %s", response.Code, response.Body)
		}
	}
	// The pinned Connect JSON codec ignores unknown fields for forward compatibility.
	// They cannot replace the contracted Environment or alter its authorization.
	before = store.reads
	if response := observationRequest(handler, "outsider", "", `{"environment":"env","unknown":true}`); response.Code != 403 || store.reads != before {
		t.Fatalf("unknown field bypassed resource policy: %d %s", response.Code, response.Body)
	}
	if response := observationRequest(handler, "admin", "", `{"environment":"env","unknown":true}`); response.Code != 200 {
		t.Fatalf("forward-compatible field rejected: %d %s", response.Code, response.Body)
	}
	store.failure = errors.New("private-diagnostic")
	response := observationRequest(handler, "admin", "", `{"environment":"env"}`)
	if response.Code != 500 || strings.Contains(response.Body.String(), "private-diagnostic") {
		t.Fatalf("private failure leaked: %d %s", response.Code, response.Body)
	}
}

func TestEnvironmentObservationConnectPreservesOriginalDelegatingCarrier(t *testing.T) {
	_, repository, _ := delegationHub(t)
	if _, err := repository.PutEnvironmentReport(t.Context(), controlplane.EnvironmentReport{Environment: "private-environment"}); err != nil {
		t.Fatal(err)
	}
	handler := NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"alice"}})
	for _, tc := range []struct {
		actor, represented string
		status             int
	}{{"alice", "", 200}, {"alice", "bob", 200}, {"robot", "alice", 403}, {"bob", "alice", 403}, {"bob", "", 403}} {
		native := as(t, handler, "/v1/environments/private-environment/reported-state", tc.actor, tc.represented)
		wire := observationRequest(handler, tc.actor, tc.represented, `{"environment":"private-environment"}`)
		if native.Code != tc.status || wire.Code != tc.status {
			t.Fatalf("%s as %s: REST %d RPC %d %s", tc.actor, tc.represented, native.Code, wire.Code, wire.Body)
		}
	}
}

func TestEnvironmentObservationConnectServesNoOtherMethods(t *testing.T) {
	handler := NewHubHandler(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}})
	for _, tc := range []struct{ method, path string }{{"POST", environmentObservationProcedure + "Unknown"}, {"POST", "/conduit.environments.v1.EnvironmentObservationService/PutEnvironmentReportedState"}, {"GET", environmentObservationProcedure}} {
		request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
		request.Header.Set("X-Platform-Subject", "admin")
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code < 400 || response.Code >= 500 {
			t.Fatalf("unregistered method served: %s %s %d", tc.method, tc.path, response.Code)
		}
	}
}

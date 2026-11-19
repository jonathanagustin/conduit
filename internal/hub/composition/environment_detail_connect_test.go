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

const environmentDetailProcedure = "/conduit.environments.v1.EnvironmentDetailService/GetEnvironment"

func detailRequest(handler http.Handler, actor, represented, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, environmentDetailProcedure, strings.NewReader(body))
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

type detailReadCounter struct {
	controlplane.Repository
	reads   int
	failure error
}

func (store *detailReadCounter) ListPlans(ctx context.Context, id string) ([]controlplane.PlanRecord, error) {
	store.reads++
	if store.failure != nil {
		return nil, store.failure
	}
	return store.Repository.ListPlans(ctx, id)
}
func (store *detailReadCounter) ListEnvironmentReports(ctx context.Context) ([]controlplane.EnvironmentReport, error) {
	store.reads++
	return store.Repository.ListEnvironmentReports(ctx)
}

func TestEnvironmentDetailConnectMatchesRESTAndAuthorizesBeforeStore(t *testing.T) {
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
	store := &detailReadCounter{Repository: repository}
	handler := NewHubHandler(store, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}})
	for _, tc := range []struct {
		actor, environment string
		status             int
	}{{"admin", "env", 200}, {"reader", "env", 200}, {"outsider", "env", 403}, {"reader", "other", 403}, {"admin", "missing", 404}, {"", "env", 403}} {
		native := as(t, handler, "/v1/environments/"+tc.environment+"", tc.actor, "")
		before := store.reads
		body, _ := json.Marshal(map[string]string{"environment": tc.environment})
		wire := detailRequest(handler, tc.actor, "", string(body))
		if native.Code != tc.status || wire.Code != tc.status {
			t.Fatalf("%s/%s REST %d %s RPC %d %s", tc.actor, tc.environment, native.Code, native.Body, wire.Code, wire.Body)
		}
		if (tc.status == 403 || tc.status == 401) && store.reads != before {
			t.Fatal("refused caller read report")
		}
		if wire.Code == 200 {
			var record controlplane.EnvironmentCatalogRecord
			actual := &environmentsv1.GetEnvironmentResponse{}
			if json.Unmarshal(native.Body.Bytes(), &record) != nil || protojson.Unmarshal(wire.Body.Bytes(), actual) != nil {
				t.Fatal("decode reported state")
			}
			if actual.Record == nil || actual.Record.Environment == nil || actual.Record.Environment.Id != record.Environment.ID {
				t.Fatal("missing full record")
			}
			expected := record.Reported
			got := actual.Record.Reported
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
	if response := detailRequest(secure, "", "", "{"); response.Code != 401 || store.reads != before {
		t.Fatalf("anonymous parsed malformed body: %d", response.Code)
	}
	for _, body := range []string{`{}`, `{`} {
		if response := detailRequest(handler, "admin", "", body); response.Code != 400 || store.reads != before {
			t.Fatalf("invalid body reached store: %d %s", response.Code, response.Body)
		}
	}
	// The pinned Connect JSON codec ignores unknown fields for forward compatibility.
	// They cannot replace the contracted Environment or alter its authorization.
	before = store.reads
	if response := detailRequest(handler, "outsider", "", `{"environment":"env","unknown":true}`); response.Code != 403 || store.reads != before {
		t.Fatalf("unknown field bypassed resource policy: %d %s", response.Code, response.Body)
	}
	if response := detailRequest(handler, "admin", "", `{"environment":"env","unknown":true}`); response.Code != 200 {
		t.Fatalf("forward-compatible field rejected: %d %s", response.Code, response.Body)
	}
	store.failure = errors.New("private-diagnostic")
	response := detailRequest(handler, "admin", "", `{"environment":"env"}`)
	if response.Code != 500 || strings.Contains(response.Body.String(), "private-diagnostic") {
		t.Fatalf("private failure leaked: %d %s", response.Code, response.Body)
	}
}

func TestEnvironmentDetailConnectPreservesOriginalDelegatingCarrier(t *testing.T) {
	_, repository, _ := delegationHub(t)
	if _, err := repository.PutEnvironmentReport(t.Context(), controlplane.EnvironmentReport{Environment: "private-environment"}); err != nil {
		t.Fatal(err)
	}
	handler := NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"alice"}})
	for _, tc := range []struct {
		actor, represented string
		status             int
	}{{"alice", "", 200}, {"alice", "bob", 200}, {"robot", "alice", 403}, {"bob", "alice", 403}, {"bob", "", 403}} {
		native := as(t, handler, "/v1/environments/private-environment", tc.actor, tc.represented)
		wire := detailRequest(handler, tc.actor, tc.represented, `{"environment":"private-environment"}`)
		if native.Code != tc.status || wire.Code != tc.status {
			t.Fatalf("%s as %s: REST %d RPC %d %s", tc.actor, tc.represented, native.Code, wire.Code, wire.Body)
		}
	}
}

func TestEnvironmentDetailConnectServesNoOtherMethods(t *testing.T) {
	handler := NewHubHandler(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}})
	for _, tc := range []struct{ method, path string }{{"POST", environmentDetailProcedure + "Unknown"}, {"POST", "/conduit.environments.v1.EnvironmentDetailService/PutEnvironmentReportedState"}, {"GET", environmentDetailProcedure}} {
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

func TestEnvironmentDetailConnectSpaceSelectionMatchesRESTBeforePrivateReads(t *testing.T) {
	const spaceID = "space_0123456789abcdef0123456789abcdef"
	for _, tc := range []struct {
		name, actor, query, body string
		excluded, unconfigured   bool
		selectionError           error
		status                   int
		membershipReads          int
	}{
		{name: "omitted", actor: "admin", body: `{"environment":"env"}`, status: 200},
		{name: "selected", actor: "admin", query: "?spaceId=" + spaceID, body: `{"environment":"env","spaceId":"` + spaceID + `"}`, status: 200, membershipReads: 2},
		{name: "present empty", actor: "admin", query: "?spaceId=", body: `{"environment":"env","spaceId":""}`, status: 400},
		{name: "invalid identity", actor: "admin", query: "?spaceId=invalid", body: `{"environment":"env","spaceId":"invalid"}`, status: 400},
		{name: "unconfigured", actor: "admin", query: "?spaceId=" + spaceID, body: `{"environment":"env","spaceId":"` + spaceID + `"}`, unconfigured: true, status: 503},
		{name: "callback lifecycle error", actor: "admin", query: "?spaceId=" + spaceID, body: `{"environment":"env","spaceId":"` + spaceID + `"}`, selectionError: controlplane.ErrInvalidSpaceSelection, status: 500},
		{name: "callback error is not a missing capability", actor: "admin", query: "?spaceId=" + spaceID, body: `{"environment":"env","spaceId":"` + spaceID + `"}`, selectionError: controlplane.ErrSpaceMembershipUnavailable, status: 500},
		{name: "excluded", actor: "admin", query: "?spaceId=" + spaceID, body: `{"environment":"env","spaceId":"` + spaceID + `"}`, excluded: true, status: 404, membershipReads: 2},
		{name: "carrier refusal precedes selection", actor: "outsider", query: "?spaceId=invalid", body: `{"environment":"env","spaceId":"invalid","principal":"admin"}`, status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := controlplane.NewMemoryRepository()
			if _, err := repository.PutEnvironmentReport(t.Context(), controlplane.EnvironmentReport{Environment: "env"}); err != nil {
				t.Fatal(err)
			}
			store := &detailReadCounter{Repository: repository}
			membershipReads := 0
			options := controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}, RequireSpaceSelection: func(context.Context, controlplane.Principal, string) error { return tc.selectionError }, SpaceIDLookup: func(context.Context, controlplane.Principal, string, string) (string, error) { return spaceID, nil }, LoadSpaceIndexMembership: func(context.Context, controlplane.Principal, string) (controlplane.SpaceIndexMembership, error) {
				membershipReads++
				if tc.excluded {
					return controlplane.SpaceIndexMembership{}, nil
				}
				return controlplane.SpaceIndexMembership{EnvironmentIDs: []string{"env"}}, nil
			}}
			if tc.unconfigured {
				options.RequireSpaceSelection = nil
			}
			handler := NewHubHandler(store, options)
			native := as(t, handler, "/v1/environments/env"+tc.query, tc.actor, "")
			wire := detailRequest(handler, tc.actor, "", tc.body)
			if native.Code != tc.status || wire.Code != tc.status || membershipReads != tc.membershipReads {
				t.Fatalf("REST %d %s RPC %d %s membership=%d", native.Code, native.Body, wire.Code, wire.Body, membershipReads)
			}
			if tc.status != 200 && store.reads != 0 {
				t.Fatal("refused selection or carrier reached private catalog")
			}
			if tc.status == 200 && store.reads != 4 {
				t.Fatalf("native catalog read behavior changed: %d", store.reads)
			}
		})
	}
}

func TestEnvironmentDetailConnectDesiredConfigurationAndMissingReport(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	state := controlplane.DesiredState{Environment: controlplane.Environment{ID: "configured", Generation: "opaque-generation", PackageArchitecture: "arm64", Labels: map[string]string{"key": "value"}, Config: map[string]any{"literal": "<value>", "number": 42}, Namespaces: []controlplane.NamespaceTarget{{RID: "namespace", Name: "name", DeletionProtected: new(false)}}, ArtifactSecurityPolicy: &controlplane.ArtifactSecurityPolicy{RequiredOutcome: "PASS"}, ArtifactRegistries: []controlplane.ArtifactRegistryAssignment{{ID: "registry", Revision: 1 << 40, CredentialRef: "opaque://reference"}}, HelmExecution: &controlplane.HelmExecutionDefaults{ImagePrePull: &controlplane.ImagePrePullPolicy{Enabled: false}}}}
	if _, err := repository.CreatePlan(t.Context(), controlplane.PlanRecord{ID: "plan", Plan: controlplane.Plan{Environment: "configured"}, DesiredState: &state, Status: controlplane.PlanAwaitingApproval, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	handler := NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}})
	for _, present := range []bool{false, true} {
		if present {
			if _, err := repository.PutEnvironmentReport(t.Context(), controlplane.EnvironmentReport{Environment: "configured"}); err != nil {
				t.Fatal(err)
			}
		}
		native := as(t, handler, "/v1/environments/configured", "admin", "")
		wire := detailRequest(handler, "admin", "", `{"environment":"configured"}`)
		if native.Code != 200 || wire.Code != 200 {
			t.Fatalf("REST %d %s RPC %d %s", native.Code, native.Body, wire.Code, wire.Body)
		}
		var record controlplane.EnvironmentCatalogRecord
		response := &environmentsv1.GetEnvironmentResponse{}
		if err := json.Unmarshal(native.Body.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		if err := protojson.Unmarshal(wire.Body.Bytes(), response); err != nil {
			t.Fatal(err)
		}
		got := response.Record
		if got == nil || got.Environment == nil {
			t.Fatal("missing record")
		}
		e := got.Environment
		if e.Generation != record.Environment.Generation || e.PackageArchitecture != record.Environment.PackageArchitecture || e.Labels["key"] != record.Environment.Labels["key"] || len(e.Namespaces) != 1 || e.Namespaces[0].DeletionProtected == nil || *e.Namespaces[0].DeletionProtected || e.ArtifactSecurityPolicy == nil || e.ArtifactSecurityPolicy.MaximumScanAgeSeconds != 0 || len(e.ArtifactRegistries) != 1 || e.ArtifactRegistries[0].Revision != 1<<40 || e.ArtifactRegistries[0].CredentialRef != record.Environment.ArtifactRegistries[0].CredentialRef || e.HelmExecution == nil || e.HelmExecution.ImagePrePull == nil || e.HelmExecution.ImagePrePull.Enabled {
			t.Fatalf("raw configuration changed: %v", e)
		}
		config, err := json.Marshal(record.Environment.Config)
		if err != nil {
			t.Fatal(err)
		}
		if string(e.ConfigJson) != string(config) {
			t.Fatal("configuration JSON changed")
		}
		if (got.Reported != nil) != present || (record.Reported != nil) != present || got.Spoke.State != controlplane.SpokeUnreachable || got.Spoke.ObservedAt != nil || (got.Reported != nil && got.Reported.ObservedAt != nil) {
			t.Fatal("report presence or unknown time changed")
		}
	}
}

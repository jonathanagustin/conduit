package httpapi

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	environmentsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestEnvironmentObservationMappingPreservesPresenceNumbersAndNestedHealth(t *testing.T) {
	instant := time.Date(2026, 11, 19, 1, 2, 3, 123456789, time.UTC)
	native := controlplane.EnvironmentReport{Environment: "env", AgentID: "agent", PlanID: "plan", ObservedAt: instant, Revision: math.MaxUint64,
		Entities: []controlplane.ReportedEntity{{InstallationID: "install", Product: "app", Version: "future", ReleaseDigest: "sha256:release", Namespace: "ns", ConfigDigest: "sha256:config", Health: "FUTURE_HEALTH", RolloutStatus: "FUTURE_ROLLOUT", ConfigStatus: "FUTURE_CONFIG", Lifecycle: "FUTURE_LIFECYCLE", ObservedAt: instant, HealthDetails: &controlplane.EntityHealthDetails{Quorum: "open", Nodes: []controlplane.EntityNodeHealth{{NodeID: "node", ProductVersion: "v", Result: "node-result", Containers: []controlplane.EntityContainerHealth{{ContainerID: "container", Liveness: true, Readiness: false, Result: "container-result", Checks: []controlplane.HealthCheck{{ID: "check", Message: "literal <message>", Result: "check-result", Params: map[string]any{"exact": uint64(9007199254740993), "nil": nil}}, {ID: "nil-params"}}}}}}}}},
		Secrets:  []controlplane.ReportedSecret{{Assignment: "assignment", Namespace: "ns", Revision: 17, ProviderRef: "vault://reference", KubernetesSecretName: "secret-name", Keys: []string{"first", "second"}}},
	}
	service := environmentObservationService{cfg: EnvironmentObservationConfig{Read: func(context.Context, string) (controlplane.EnvironmentReport, error) { return native, nil }}}
	result, err := service.GetEnvironmentReportedState(t.Context(), &environmentsv1.GetEnvironmentReportedStateRequest{Environment: "env"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := protojson.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	roundtrip := &environmentsv1.GetEnvironmentReportedStateResponse{}
	if err := protojson.Unmarshal(b, roundtrip); err != nil {
		t.Fatal(err)
	}
	report := roundtrip.Reported
	if report.Environment != "env" || report.AgentId != "agent" || report.PlanId != "plan" || report.Revision != math.MaxUint64 || !report.ObservedAt.AsTime().Equal(instant) {
		t.Fatalf("report changed: %v", report)
	}
	row := report.Entities[0]
	if row.InstallationId != "install" || row.Product != "app" || row.Version != "future" || row.ReleaseDigest != "sha256:release" || row.Namespace != "ns" || row.ConfigDigest != "sha256:config" || row.Health != "FUTURE_HEALTH" || row.RolloutStatus != "FUTURE_ROLLOUT" || row.ConfigStatus != "FUTURE_CONFIG" || row.Lifecycle != "FUTURE_LIFECYCLE" || !row.ObservedAt.AsTime().Equal(instant) {
		t.Fatalf("entity changed: %v", row)
	}
	node := row.HealthDetails.Nodes[0]
	container := node.Containers[0]
	check := container.Checks[0]
	if row.HealthDetails.Quorum != "open" || node.NodeId != "node" || node.ProductVersion != "v" || node.Result != "node-result" || container.ContainerId != "container" || !container.Liveness || container.Readiness || container.Result != "container-result" || check.Id != "check" || check.Message != "literal <message>" || check.Result != "check-result" || !strings.Contains(string(check.ParamsJson), "9007199254740993") || string(container.Checks[1].ParamsJson) != "null" {
		t.Fatalf("health changed: %v", row.HealthDetails)
	}
	secret := report.Secrets[0]
	if secret.Assignment != "assignment" || secret.Namespace != "ns" || secret.Revision != 17 || secret.ProviderRef != "vault://reference" || secret.KubernetesSecretName != "secret-name" || strings.Join(secret.Keys, ",") != "first,second" {
		t.Fatalf("secret references changed: %v", secret)
	}
	empty, err := environmentObservationMessage(controlplane.EnvironmentReport{Entities: []controlplane.ReportedEntity{{}, {HealthDetails: &controlplane.EntityHealthDetails{}}}})
	if err != nil || empty.ObservedAt != nil || empty.Entities[0].ObservedAt != nil || empty.Entities[0].HealthDetails != nil || empty.Entities[1].HealthDetails == nil {
		t.Fatalf("absence fabricated or lost: %v %v", empty, err)
	}
}

func TestEnvironmentObservationRefusesInvalidResultsWithoutPrivateDiagnostics(t *testing.T) {
	for _, native := range []controlplane.EnvironmentReport{
		{ObservedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Entities: []controlplane.ReportedEntity{{ObservedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}}},
		{Entities: []controlplane.ReportedEntity{{HealthDetails: &controlplane.EntityHealthDetails{Nodes: []controlplane.EntityNodeHealth{{Containers: []controlplane.EntityContainerHealth{{Checks: []controlplane.HealthCheck{{Params: map[string]any{"private-diagnostic": math.NaN()}}}}}}}}}}},
	} {
		service := environmentObservationService{cfg: EnvironmentObservationConfig{Read: func(context.Context, string) (controlplane.EnvironmentReport, error) { return native, nil }}}
		response, err := service.GetEnvironmentReportedState(t.Context(), &environmentsv1.GetEnvironmentReportedStateRequest{Environment: "env"})
		if response != nil || connect.CodeOf(err) != connect.CodeInternal || strings.Contains(err.Error(), "private-diagnostic") {
			t.Fatalf("invalid result escaped: %v %v", response, err)
		}
	}
	for _, failure := range []struct {
		err  error
		code connect.Code
	}{{controlplane.ErrForbidden, connect.CodePermissionDenied}, {controlplane.ErrNotFound, connect.CodeNotFound}, {controlplane.ErrUnauthorized, connect.CodeUnauthenticated}, {errors.New("private-diagnostic"), connect.CodeInternal}} {
		service := environmentObservationService{cfg: EnvironmentObservationConfig{Read: func(context.Context, string) (controlplane.EnvironmentReport, error) {
			return controlplane.EnvironmentReport{Environment: "private"}, failure.err
		}}}
		response, err := service.GetEnvironmentReportedState(t.Context(), &environmentsv1.GetEnvironmentReportedStateRequest{Environment: "env"})
		if response != nil || connect.CodeOf(err) != failure.code || strings.Contains(err.Error(), "private-diagnostic") {
			t.Fatalf("read error escaped: %v %v", response, err)
		}
	}
}

func TestEnvironmentObservationAuthenticatesBeforeDecodingAndReads(t *testing.T) {
	reads := 0
	mux := http.NewServeMux()
	RegisterEnvironmentObservationConnect(mux, EnvironmentObservationConfig{Read: func(context.Context, string) (controlplane.EnvironmentReport, error) {
		reads++
		return controlplane.EnvironmentReport{}, nil
	}})
	request := httptest.NewRequest(http.MethodPost, strings.TrimPrefix(EnvironmentObservationConnectRoute, "POST "), strings.NewReader("{"))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connect-Protocol-Version", "1")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || reads != 0 {
		t.Fatalf("anonymous reached decoder/store: %d reads=%d", response.Code, reads)
	}
	service := environmentObservationService{cfg: EnvironmentObservationConfig{Read: func(context.Context, string) (controlplane.EnvironmentReport, error) {
		reads++
		return controlplane.EnvironmentReport{}, nil
	}}}
	if _, err := service.GetEnvironmentReportedState(t.Context(), &environmentsv1.GetEnvironmentReportedStateRequest{}); connect.CodeOf(err) != connect.CodeInvalidArgument || reads != 0 {
		t.Fatalf("empty identifier reached store: %v reads=%d", err, reads)
	}
}

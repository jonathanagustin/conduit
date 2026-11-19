package operator

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	environmentsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func validReportedWire() *environmentsv1.ReportedEnvironmentState {
	return &environmentsv1.ReportedEnvironmentState{Environment: "env", AgentId: "agent", PlanId: "plan", Revision: math.MaxUint64, ObservedAt: timestamppb.New(time.Date(2026, 11, 19, 1, 2, 3, 123456789, time.UTC)), Entities: []*environmentsv1.ReportedEntity{{InstallationId: "install", Product: "example:app", Version: "1.2.3", Namespace: "ns", ReleaseDigest: "release", ConfigDigest: "config", Health: "FUTURE", RolloutStatus: "FUTURE_ROLLOUT", ConfigStatus: "FUTURE_CONFIG", Lifecycle: "FUTURE_LIFECYCLE", HealthDetails: &environmentsv1.EntityHealthDetails{Quorum: "all", Nodes: []*environmentsv1.EntityNodeHealth{{NodeId: "node", ProductVersion: "version", Result: "node-result", Containers: []*environmentsv1.EntityContainerHealth{{ContainerId: "container", Liveness: true, Readiness: false, Result: "container-result", Checks: []*environmentsv1.HealthCheck{{Id: "check", Message: "literal <message>", Result: "check-result", ParamsJson: []byte(`{"exact":9007199254740993,"nested":[1.25,null]}`)}}}}}}}}}, Secrets: []*environmentsv1.ReportedSecretReference{{Assignment: "assignment", Namespace: "ns", Revision: 7, ProviderRef: "vault://metadata", KubernetesSecretName: "secret-name", Keys: []string{"key"}}}}
}

func TestEnvironmentObservationReadPreservesNumbersAndNativeFields(t *testing.T) {
	wire := validReportedWire()
	report, err := environmentObservationFromWire(wire, "env")
	if err != nil {
		t.Fatal(err)
	}
	if report.Environment != "env" || report.AgentID != "agent" || report.PlanID != "plan" || report.Revision != math.MaxUint64 || !report.ObservedAt.Equal(wire.ObservedAt.AsTime()) {
		t.Fatalf("report changed: %+v", report)
	}
	entity := report.Entities[0]
	node := entity.HealthDetails.Nodes[0]
	container := node.Containers[0]
	check := container.Checks[0]
	if entity.InstallationID != "install" || entity.Product != "example:app" || entity.Version != "1.2.3" || entity.Namespace != "ns" || entity.ReleaseDigest != "release" || entity.ConfigDigest != "config" || entity.Health != "FUTURE" || entity.RolloutStatus != "FUTURE_ROLLOUT" || entity.ConfigStatus != "FUTURE_CONFIG" || entity.Lifecycle != "FUTURE_LIFECYCLE" || !entity.ObservedAt.IsZero() || entity.HealthDetails.Quorum != "all" || node.NodeID != "node" || node.ProductVersion != "version" || node.Result != "node-result" || container.ContainerID != "container" || !container.Liveness || container.Readiness || container.Result != "container-result" || check.ID != "check" || check.Message != "literal <message>" || check.Result != "check-result" {
		t.Fatalf("entity fields changed: %+v", entity)
	}
	if number, ok := check.Params["exact"].(json.Number); !ok || number.String() != "9007199254740993" {
		t.Fatalf("diagnostic number rounded: %#v", check.Params)
	}
	secret := report.Secrets[0]
	if secret.Assignment != "assignment" || secret.Namespace != "ns" || secret.Revision != 7 || secret.ProviderRef != "vault://metadata" || secret.KubernetesSecretName != "secret-name" || strings.Join(secret.Keys, ",") != "key" {
		t.Fatalf("secret metadata changed: %+v", secret)
	}
	wire.Secrets[0].Keys[0] = "mutated"
	if report.Secrets[0].Keys[0] != "key" {
		t.Fatal("result aliases wire key slice")
	}
	empty, err := environmentObservationFromWire(&environmentsv1.ReportedEnvironmentState{Environment: "env", Entities: []*environmentsv1.ReportedEntity{{}, {HealthDetails: &environmentsv1.EntityHealthDetails{}}}}, "env")
	if err != nil || !empty.ObservedAt.IsZero() || empty.Entities[0].HealthDetails != nil || empty.Entities[1].HealthDetails == nil {
		t.Fatalf("observation presence fabricated/lost: %+v %v", empty, err)
	}
}

func TestEnvironmentObservationReadRejectsMalformedWireWithoutPartialEvidence(t *testing.T) {
	mutations := []func(*environmentsv1.ReportedEnvironmentState){
		func(r *environmentsv1.ReportedEnvironmentState) { r.Environment = "other" },
		func(r *environmentsv1.ReportedEnvironmentState) {
			r.ObservedAt = &timestamppb.Timestamp{Seconds: 253402300800}
		},
		func(r *environmentsv1.ReportedEnvironmentState) {
			r.Entities[0].ObservedAt = &timestamppb.Timestamp{Nanos: 1000000000}
		},
		func(r *environmentsv1.ReportedEnvironmentState) { r.Entities[0] = nil },
		func(r *environmentsv1.ReportedEnvironmentState) { r.Entities[0].HealthDetails.Nodes[0] = nil },
		func(r *environmentsv1.ReportedEnvironmentState) {
			r.Entities[0].HealthDetails.Nodes[0].Containers[0] = nil
		},
		func(r *environmentsv1.ReportedEnvironmentState) {
			r.Entities[0].HealthDetails.Nodes[0].Containers[0].Checks[0] = nil
		},
		func(r *environmentsv1.ReportedEnvironmentState) { r.Secrets[0] = nil },
	}
	for _, mutate := range mutations {
		wire := proto.Clone(validReportedWire()).(*environmentsv1.ReportedEnvironmentState)
		mutate(wire)
		got, err := environmentObservationFromWire(wire, "env")
		if err == nil || !reflect.DeepEqual(got, controlplane.EnvironmentReport{}) {
			t.Fatalf("invalid result admitted: %+v %v", got, err)
		}
	}
	for _, params := range []string{"", `[]`, `1`, `"private-diagnostic"`, `{} {}`, `{`, `null trailing`} {
		wire := validReportedWire()
		wire.Entities[0].HealthDetails.Nodes[0].Containers[0].Checks[0].ParamsJson = []byte(params)
		got, err := environmentObservationFromWire(wire, "env")
		if err == nil || !reflect.DeepEqual(got, controlplane.EnvironmentReport{}) || strings.Contains(err.Error(), "private-diagnostic") {
			t.Fatalf("invalid diagnostic JSON admitted/leaked: %+v %v", got, err)
		}
	}
	if _, err := environmentObservationFromWire(nil, "env"); err == nil {
		t.Fatal("missing report admitted")
	}
	if _, err := environmentObservationFromWire(validReportedWire(), ""); err == nil {
		t.Fatal("unbound Environment admitted")
	}
	wire := validReportedWire()
	wire.Entities[0].HealthDetails.Nodes[0].Containers[0].Checks[0].ParamsJson = []byte("null")
	got, err := environmentObservationFromWire(wire, "env")
	if err != nil || got.Entities[0].HealthDetails.Nodes[0].Containers[0].Checks[0].Params != nil {
		t.Fatalf("native nil params lost: %+v %v", got, err)
	}
}

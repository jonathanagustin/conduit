package operator

import (
	"encoding/json"
	"math"
	"reflect"
	"strconv"
	"testing"
	"time"

	environmentsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestEnvironmentDetailClientPreservesEveryNativeConfigurationField(t *testing.T) {
	instant := time.Date(2026, 11, 19, 1, 2, 3, 456789123, time.UTC)
	wide := int64(1)
	e := controlplane.Environment{
		ID: "env", Generation: "opaque", PackageArchitecture: "arm64", ConnectionSetting: "connection", DeclaredStateSource: "source", DefaultReleaseChannel: "channel", Labels: map[string]string{"key": "value"}, Config: map[string]any{"integer": json.Number("9007199254740993"), "literal": "<value>"},
		Namespaces:             []controlplane.NamespaceTarget{{RID: "namespace-rid", Name: "name", Description: "description", DeletionProtected: new(false), EnrollmentRID: "enrollment", Organizations: []controlplane.Organization{{DisplayName: "org", PrimaryHostname: "org.example"}}, Trashed: true, OntologyCountValid: true, Installed: map[string]controlplane.InstalledTarget{"app": {Version: "version", ConfigDigest: "sha256:config"}}}},
		ArtifactSecurityPolicy: &controlplane.ArtifactSecurityPolicy{RequiredOutcome: "PASS", MaximumScanAgeSeconds: 0}, ArtifactAvailabilityPolicy: &controlplane.ArtifactAvailabilityPolicy{RequiredOutcome: "ANY"},
		ArtifactRegistries: []controlplane.ArtifactRegistryAssignment{{ID: "registry", URL: "oci://example", Type: "raw-type", Revision: int(wide << 40), Mirrors: []string{"mirror"}, Authentication: "reference", CredentialRef: "opaque://credential", ImagePullSecretName: "pull-secret"}},
		Secrets:            []controlplane.ExternalSecretAssignment{{Name: "secret", Revision: int(wide << 41), ProviderRef: "opaque://provider", KubernetesSecretName: "secret-name", Keys: []string{"key"}}}, AutomaticSecurityRecall: true, VulnerabilityUpgrades: &controlplane.VulnerabilityUpgradePolicy{AllowRecalledForVulnerabilities: true, RescanSLASeconds: int(wide << 42)}, AutoSuppressionThreshold: int(wide << 43),
		Maintenance:   controlplane.MaintenancePolicy{Downtime: &controlplane.WeeklyMaintenanceWindow{TimeZone: "UTC", Intervals: []string{"downtime"}}, NoDowntime: &controlplane.WeeklyMaintenanceWindow{TimeZone: "America/New_York", Intervals: []string{"no-downtime"}}, Overrides: []controlplane.MaintenanceOverride{{InstallationID: "installation", Type: "recorded-type", StartsAt: instant, EndsAt: instant.Add(time.Hour), Reason: "reviewed reason", ApprovedBy: "recorded-reviewer"}}},
		HelmExecution: &controlplane.HelmExecutionDefaults{DefaultRolloutStrategy: "raw-strategy", InstallResourceReadinessTimeoutSeconds: int(wide << 44), UpgradeResourceReadinessTimeoutSeconds: int(wide << 45), ImagePrePull: &controlplane.ImagePrePullPolicy{Enabled: false, TimeoutSeconds: int(wide << 46)}},
	}
	wire := &environmentsv1.Environment{}
	if err := protojson.Unmarshal([]byte(`{
 "id":"env","generation":"opaque","packageArchitecture":"arm64","connectionSetting":"connection","declaredStateSource":"source","defaultReleaseChannel":"channel","labels":{"key":"value"},
 "namespaces":[{"rid":"namespace-rid","name":"name","description":"description","deletionProtected":false,"enrollmentRid":"enrollment","organizations":[{"displayName":"org","primaryHostname":"org.example"}],"trashed":true,"ontologyCountValid":true,"installed":{"app":{"version":"version","configDigest":"sha256:config"}}}],
 "artifactSecurityPolicy":{"requiredOutcome":"PASS","maximumScanAgeSeconds":"0"},"artifactAvailabilityPolicy":{"requiredOutcome":"ANY"},
 "artifactRegistries":[{"id":"registry","url":"oci://example","type":"raw-type","revision":"1099511627776","mirrors":["mirror"],"authentication":"reference","credentialRef":"opaque://credential","imagePullSecretName":"pull-secret"}],
 "secrets":[{"name":"secret","revision":"2199023255552","providerRef":"opaque://provider","kubernetesSecretName":"secret-name","keys":["key"]}],"automaticSecurityRecall":true,"vulnerabilityUpgrades":{"allowRecalledForVulnerabilities":true,"rescanSlaSeconds":"4398046511104"},"autoSuppressionThreshold":"8796093022208",
 "maintenance":{"downtime":{"timeZone":"UTC","intervals":["downtime"]},"noDowntime":{"timeZone":"America/New_York","intervals":["no-downtime"]},"overrides":[{"installationId":"installation","type":"recorded-type","startsAt":"2026-11-19T01:02:03.456789123Z","endsAt":"2026-11-19T02:02:03.456789123Z","reason":"reviewed reason","approvedBy":"recorded-reviewer"}]},
 "helmExecution":{"defaultRolloutStrategy":"raw-strategy","installResourceReadinessTimeoutSeconds":"17592186044416","upgradeResourceReadinessTimeoutSeconds":"35184372088832","imagePrePull":{"enabled":false,"timeoutSeconds":"70368744177664"}}
 }`), wire); err != nil {
		t.Fatal(err)
	}
	wire.ConfigJson = []byte(`{"integer":9007199254740993,"literal":"<value>"}`)
	record := &environmentsv1.EnvironmentRecord{Environment: wire, Spoke: &environmentsv1.SpokeLiveness{AgentId: "agent", State: "FUTURE", ObservedAt: timestamppb.New(instant)}, Reported: &environmentsv1.ReportedEnvironmentState{Environment: "env", AgentId: "agent", PlanId: "plan", Revision: math.MaxUint64, ObservedAt: timestamppb.New(instant)}}
	got, err := environmentDetailFromWire(record, "env")
	if strconv.IntSize == 32 {
		if err == nil {
			t.Fatal("wide native integer silently narrowed")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	want := controlplane.EnvironmentCatalogRecord{Environment: e, Spoke: controlplane.SpokeLiveness{AgentID: "agent", State: "FUTURE", ObservedAt: instant}, Reported: &controlplane.EnvironmentReport{Environment: "env", AgentID: "agent", PlanID: "plan", Revision: math.MaxUint64, ObservedAt: instant}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("native detail mismatch\ngot=%+v\nwant=%+v", got, want)
	}
	got.Environment.Labels["key"] = "changed"
	*got.Environment.Namespaces[0].DeletionProtected = true
	got.Environment.ArtifactRegistries[0].Mirrors[0] = "changed"
	if wire.Labels["key"] != "value" || *wire.Namespaces[0].DeletionProtected || wire.ArtifactRegistries[0].Mirrors[0] != "mirror" {
		t.Fatal("native result aliases wire configuration")
	}
	wire.ArtifactSecurityPolicy.MaximumScanAgeSeconds = 1 << 47
	wire.HelmExecution.ImagePrePull.Enabled = true
	*wire.Namespaces[0].DeletionProtected = true
	got, err = environmentDetailFromWire(record, "env")
	if err != nil || got.Environment.ArtifactSecurityPolicy.MaximumScanAgeSeconds != int(wide<<47) || !got.Environment.HelmExecution.ImagePrePull.Enabled || !*got.Environment.Namespaces[0].DeletionProtected {
		t.Fatalf("explicit override changed: %+v %v", got, err)
	}
}

func environmentDetailMinimalWire() *environmentsv1.EnvironmentRecord {
	return &environmentsv1.EnvironmentRecord{Environment: &environmentsv1.Environment{Id: "env", ConfigJson: []byte("null")}, Spoke: &environmentsv1.SpokeLiveness{State: "unreachable"}}
}

func TestEnvironmentDetailClientPreservesUnknownTimeAndOptionalPolicies(t *testing.T) {
	wire := environmentDetailMinimalWire()
	got, err := environmentDetailFromWire(wire, "env")
	if err != nil || got.Reported != nil || !got.Spoke.ObservedAt.IsZero() || got.Environment.ArtifactSecurityPolicy != nil || got.Environment.HelmExecution != nil || got.Environment.Config != nil {
		t.Fatalf("absence changed: %+v %v", got, err)
	}
	wire.Environment.ArtifactSecurityPolicy = &environmentsv1.ArtifactSecurityPolicy{}
	wire.Environment.HelmExecution = &environmentsv1.HelmExecutionDefaults{ImagePrePull: &environmentsv1.ImagePrePullPolicy{Enabled: false}}
	wire.Environment.Namespaces = []*environmentsv1.NamespaceTarget{{Name: "ns", DeletionProtected: new(false)}}
	wire.Reported = &environmentsv1.ReportedEnvironmentState{Environment: "env"}
	got, err = environmentDetailFromWire(wire, "env")
	if err != nil || got.Reported == nil || !got.Reported.ObservedAt.IsZero() || got.Environment.ArtifactSecurityPolicy == nil || got.Environment.ArtifactSecurityPolicy.MaximumScanAgeSeconds != 0 || got.Environment.HelmExecution == nil || got.Environment.HelmExecution.ImagePrePull == nil || got.Environment.HelmExecution.ImagePrePull.Enabled || got.Environment.Namespaces[0].DeletionProtected == nil || *got.Environment.Namespaces[0].DeletionProtected {
		t.Fatalf("explicit false/zero policy changed: %+v %v", got, err)
	}
}

func TestEnvironmentDetailClientRejectsMalformedResultsWithoutPartialRecords(t *testing.T) {
	invalidTime := timestamppb.New(time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC))
	for _, tc := range []struct {
		name   string
		change func(*environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord
	}{
		{"missing record", func(*environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord { return nil }},
		{"missing Environment", func(r *environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord {
			r.Environment = nil
			return r
		}},
		{"missing liveness", func(r *environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord { r.Spoke = nil; return r }},
		{"wrong Environment", func(r *environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord {
			r.Environment.Id = "other"
			return r
		}},
		{"wrong report", func(r *environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord {
			r.Reported = &environmentsv1.ReportedEnvironmentState{Environment: "other"}
			return r
		}},
		{"missing JSON", func(r *environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord {
			r.Environment.ConfigJson = nil
			return r
		}},
		{"multiple JSON values", func(r *environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord {
			r.Environment.ConfigJson = []byte("{} {}")
			return r
		}},
		{"nonobject JSON", func(r *environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord {
			r.Environment.ConfigJson = []byte("[]")
			return r
		}},
		{"invalid observation time", func(r *environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord {
			r.Spoke.ObservedAt = invalidTime
			return r
		}},
		{"nil namespace", func(r *environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord {
			r.Environment.Namespaces = []*environmentsv1.NamespaceTarget{nil}
			return r
		}},
		{"nil organization", func(r *environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord {
			r.Environment.Namespaces = []*environmentsv1.NamespaceTarget{{Organizations: []*environmentsv1.Organization{nil}}}
			return r
		}},
		{"nil installed value", func(r *environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord {
			r.Environment.Namespaces = []*environmentsv1.NamespaceTarget{{Installed: map[string]*environmentsv1.InstalledTarget{"app": nil}}}
			return r
		}},
		{"nil registry", func(r *environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord {
			r.Environment.ArtifactRegistries = []*environmentsv1.ArtifactRegistryAssignment{nil}
			return r
		}},
		{"nil secret", func(r *environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord {
			r.Environment.Secrets = []*environmentsv1.ExternalSecretAssignment{nil}
			return r
		}},
		{"nil override", func(r *environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord {
			r.Environment.Maintenance = &environmentsv1.MaintenancePolicy{Overrides: []*environmentsv1.MaintenanceOverride{nil}}
			return r
		}},
		{"invalid override start", func(r *environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord {
			r.Environment.Maintenance = &environmentsv1.MaintenancePolicy{Overrides: []*environmentsv1.MaintenanceOverride{{StartsAt: invalidTime}}}
			return r
		}},
		{"invalid override end", func(r *environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord {
			r.Environment.Maintenance = &environmentsv1.MaintenancePolicy{Overrides: []*environmentsv1.MaintenanceOverride{{EndsAt: invalidTime}}}
			return r
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := environmentDetailFromWire(tc.change(environmentDetailMinimalWire()), "env")
			if err == nil || !reflect.DeepEqual(got, controlplane.EnvironmentCatalogRecord{}) {
				t.Fatalf("invalid record leaked partial values: %+v %v", got, err)
			}
		})
	}
}

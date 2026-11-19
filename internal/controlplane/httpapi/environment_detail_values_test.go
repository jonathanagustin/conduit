package httpapi

import (
	"encoding/json"
	"testing"
	"time"

	environmentsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestEnvironmentDetailProjectsEveryNativeConfigurationFieldWithoutDefaults(t *testing.T) {
	instant := time.Date(2026, 11, 19, 1, 2, 3, 456789123, time.UTC)
	e := controlplane.Environment{
		ID: "env", Generation: "opaque", PackageArchitecture: "arm64", ConnectionSetting: "connection", DeclaredStateSource: "source", DefaultReleaseChannel: "channel", Labels: map[string]string{"key": "value"}, Config: map[string]any{"integer": json.Number("9007199254740993"), "literal": "<value>"},
		Namespaces:             []controlplane.NamespaceTarget{{RID: "namespace-rid", Name: "name", Description: "description", DeletionProtected: new(false), EnrollmentRID: "enrollment", Organizations: []controlplane.Organization{{DisplayName: "org", PrimaryHostname: "org.example"}}, Trashed: true, OntologyCountValid: true, Installed: map[string]controlplane.InstalledTarget{"app": {Version: "version", ConfigDigest: "sha256:config"}}}},
		ArtifactSecurityPolicy: &controlplane.ArtifactSecurityPolicy{RequiredOutcome: "PASS", MaximumScanAgeSeconds: 0}, ArtifactAvailabilityPolicy: &controlplane.ArtifactAvailabilityPolicy{RequiredOutcome: "ANY"},
		ArtifactRegistries: []controlplane.ArtifactRegistryAssignment{{ID: "registry", URL: "oci://example", Type: "raw-type", Revision: 1 << 40, Mirrors: []string{"mirror"}, Authentication: "reference", CredentialRef: "opaque://credential", ImagePullSecretName: "pull-secret"}},
		Secrets:            []controlplane.ExternalSecretAssignment{{Name: "secret", Revision: 1 << 41, ProviderRef: "opaque://provider", KubernetesSecretName: "secret-name", Keys: []string{"key"}}}, AutomaticSecurityRecall: true, VulnerabilityUpgrades: &controlplane.VulnerabilityUpgradePolicy{AllowRecalledForVulnerabilities: true, RescanSLASeconds: 1 << 42}, AutoSuppressionThreshold: 1 << 43,
		Maintenance:   controlplane.MaintenancePolicy{Downtime: &controlplane.WeeklyMaintenanceWindow{TimeZone: "UTC", Intervals: []string{"downtime"}}, NoDowntime: &controlplane.WeeklyMaintenanceWindow{TimeZone: "America/New_York", Intervals: []string{"no-downtime"}}, Overrides: []controlplane.MaintenanceOverride{{InstallationID: "installation", Type: "recorded-type", StartsAt: instant, EndsAt: instant.Add(time.Hour), Reason: "reviewed reason", ApprovedBy: "recorded-reviewer"}}},
		HelmExecution: &controlplane.HelmExecutionDefaults{DefaultRolloutStrategy: "raw-strategy", InstallResourceReadinessTimeoutSeconds: 1 << 44, UpgradeResourceReadinessTimeoutSeconds: 1 << 45, ImagePrePull: &controlplane.ImagePrePullPolicy{Enabled: false, TimeoutSeconds: 1 << 46}},
	}
	got, err := environmentDetailMessage(controlplane.EnvironmentCatalogRecord{Environment: e, Reported: &controlplane.EnvironmentReport{Environment: "env", AgentID: "agent", ObservedAt: instant, Revision: 1<<63 + 1}, Spoke: controlplane.SpokeLiveness{AgentID: "agent", State: controlplane.SpokeStale, ObservedAt: instant}})
	if err != nil {
		t.Fatal(err)
	}
	expected := &environmentsv1.Environment{}
	// This independent wire fixture spells out every native configuration value.
	// ProtoJSON decimal strings make narrowing or float conversion observable.
	if err := protojson.Unmarshal([]byte(`{
 "id":"env","generation":"opaque","packageArchitecture":"arm64","connectionSetting":"connection","declaredStateSource":"source","defaultReleaseChannel":"channel","labels":{"key":"value"},
 "namespaces":[{"rid":"namespace-rid","name":"name","description":"description","deletionProtected":false,"enrollmentRid":"enrollment","organizations":[{"displayName":"org","primaryHostname":"org.example"}],"trashed":true,"ontologyCountValid":true,"installed":{"app":{"version":"version","configDigest":"sha256:config"}}}],
 "artifactSecurityPolicy":{"requiredOutcome":"PASS","maximumScanAgeSeconds":"0"},"artifactAvailabilityPolicy":{"requiredOutcome":"ANY"},
 "artifactRegistries":[{"id":"registry","url":"oci://example","type":"raw-type","revision":"1099511627776","mirrors":["mirror"],"authentication":"reference","credentialRef":"opaque://credential","imagePullSecretName":"pull-secret"}],
 "secrets":[{"name":"secret","revision":"2199023255552","providerRef":"opaque://provider","kubernetesSecretName":"secret-name","keys":["key"]}],"automaticSecurityRecall":true,"vulnerabilityUpgrades":{"allowRecalledForVulnerabilities":true,"rescanSlaSeconds":"4398046511104"},"autoSuppressionThreshold":"8796093022208",
 "maintenance":{"downtime":{"timeZone":"UTC","intervals":["downtime"]},"noDowntime":{"timeZone":"America/New_York","intervals":["no-downtime"]},"overrides":[{"installationId":"installation","type":"recorded-type","startsAt":"2026-11-19T01:02:03.456789123Z","endsAt":"2026-11-19T02:02:03.456789123Z","reason":"reviewed reason","approvedBy":"recorded-reviewer"}]},
 "helmExecution":{"defaultRolloutStrategy":"raw-strategy","installResourceReadinessTimeoutSeconds":"17592186044416","upgradeResourceReadinessTimeoutSeconds":"35184372088832","imagePrePull":{"enabled":false,"timeoutSeconds":"70368744177664"}}
 }`), expected); err != nil {
		t.Fatal(err)
	}
	expected.ConfigJson = []byte(`{"integer":9007199254740993,"literal":"\u003cvalue\u003e"}`)
	if !proto.Equal(got.Environment, expected) {
		t.Fatalf("configuration mismatch\ngot %s\nwant %s", got.Environment, expected)
	}
	if got.Reported == nil || got.Reported.Revision != 1<<63+1 || !got.Reported.ObservedAt.AsTime().Equal(instant) || got.Spoke.State != controlplane.SpokeStale || !got.Spoke.ObservedAt.AsTime().Equal(instant) {
		t.Fatal("report/liveness projection changed")
	}
	// The adapter must not return aliases into a stored native configuration.
	got.Environment.Labels["key"] = "changed"
	*got.Environment.Namespaces[0].DeletionProtected = true
	got.Environment.ArtifactRegistries[0].Mirrors[0] = "changed"
	if e.Labels["key"] != "value" || *e.Namespaces[0].DeletionProtected || e.ArtifactRegistries[0].Mirrors[0] != "mirror" {
		t.Fatal("wire result aliases native configuration")
	}
	// A raw zero must stay zero, but an explicitly configured wide value must
	// also survive; the zero fixture alone cannot catch an omitted assignment.
	e.ArtifactSecurityPolicy.MaximumScanAgeSeconds = 1 << 47
	e.HelmExecution.ImagePrePull.Enabled = true
	*e.Namespaces[0].DeletionProtected = true
	got, err = environmentDetailMessage(controlplane.EnvironmentCatalogRecord{Environment: e})
	if err != nil || got.Environment.ArtifactSecurityPolicy.MaximumScanAgeSeconds != 1<<47 ||
		!got.Environment.HelmExecution.ImagePrePull.Enabled || !*got.Environment.Namespaces[0].DeletionProtected {
		t.Fatalf("explicit configured policy changed: %v %v", got, err)
	}
	e.Namespaces[0].DeletionProtected = nil
	e.HelmExecution.ImagePrePull = nil
	got, err = environmentDetailMessage(controlplane.EnvironmentCatalogRecord{Environment: e})
	if err != nil || got.Environment.Namespaces[0].DeletionProtected != nil || got.Environment.HelmExecution.ImagePrePull != nil {
		t.Fatalf("absent nested override became configured: %v %v", got, err)
	}

}

func TestEnvironmentDetailPreservesAbsenceAndRejectsUnrepresentableValues(t *testing.T) {
	got, err := environmentDetailMessage(controlplane.EnvironmentCatalogRecord{Environment: controlplane.Environment{ID: "env"}, Spoke: controlplane.SpokeLiveness{State: controlplane.SpokeUnreachable}})
	if err != nil || got.Reported != nil || got.Spoke.ObservedAt != nil || got.Environment.HelmExecution != nil || got.Environment.ArtifactSecurityPolicy != nil || got.Environment.VulnerabilityUpgrades != nil || string(got.Environment.ConfigJson) != "null" {
		t.Fatalf("absence changed: %v %v", got, err)
	}
	for _, record := range []controlplane.EnvironmentCatalogRecord{
		{Environment: controlplane.Environment{Config: map[string]any{"unsupported": make(chan int)}}},
		{Spoke: controlplane.SpokeLiveness{ObservedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}},
		{Environment: controlplane.Environment{Maintenance: controlplane.MaintenancePolicy{Overrides: []controlplane.MaintenanceOverride{{EndsAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}}}}},
	} {
		if _, err := environmentDetailMessage(record); err == nil {
			t.Fatal("unrepresentable value silently accepted")
		}
	}
}

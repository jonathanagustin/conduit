package plan

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// A Plan is persisted and handed to Agents, so its JSON is a contract rather
// than an implementation detail. Moving these types between packages must not
// change a single key, and a type alias guarantees that only if the fields were
// carried across correctly. This pins the whole surface so a dropped tag or a
// renamed field fails here rather than at an Agent that cannot read its Plan.
func TestPlanJSONKeysAreStable(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name  string
		value any
		want  []string
	}{
		{"Plan", Plan{}, []string{
			"environment", "generation", "autoSuppressionThreshold", "steps",
		}},
		{"Step", Step{}, []string{
			"position", "action", "installationId", "entity", "product", "version",
			"releaseDigest", "namespace", "configDigest", "values", "chart", "images",
			"helmExecution", "maintenance",
		}},
		{"Access", Access{}, []string{
			"requirement", "assignment", "revision", "providerRef", "kubernetesSecretName", "keys",
		}},
		{"Mutation", Mutation{}, []string{"position", "action", "namespace"}},
		{"Override", Override{}, []string{"type", "startsAt", "endsAt", "reason", "approvedBy"}},
		{"UpgradeRationale", UpgradeRationale{}, []string{
			"reason", "currentVersion", "targetVersion", "currentRiskScore", "targetRiskScore",
		}},
		{"IgnoredNamespace", IgnoredNamespace{}, []string{"entity", "namespaceRid", "reason"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			encoded, err := json.Marshal(testCase.value)
			if err != nil {
				t.Fatal(err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			for _, key := range testCase.want {
				if _, present := decoded[key]; !present {
					t.Errorf("%s lost its %q key: %s", testCase.name, key, encoded)
				}
			}
		})
	}
}

// The optional fields must stay optional. A tag that lost its omitempty makes
// every Plan larger and, worse, makes an absent value indistinguishable from an
// explicitly empty one at the far end.
func TestOptionalPlanFieldsStayOmitted(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(Step{})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"releaseChannel", "namespaceRid", "registryAccess", "secretAccess",
		"healthQuorum", "rampedRollout", "recallRollOff", "recallIds",
		"dependsOn", "upgradeRationale", "resourceKind", "resourceName",
		"helmReleaseName",
	} {
		if strings.Contains(string(encoded), `"`+key+`"`) {
			t.Errorf("an empty Step must omit %q: %s", key, encoded)
		}
	}
}

// A Plan round-trips unchanged. This is the property a Spoke depends on: what
// the Hub wrote is what it reads back.
func TestPlanRoundTripsUnchanged(t *testing.T) {
	t.Parallel()
	original := Plan{
		Environment:              "dev-use2",
		Generation:               "g-7",
		AutoSuppressionThreshold: 3,
		SecretMutations: []Mutation{{
			Position: 1, Action: SecretReplace, Namespace: "apps",
			Current:  &Access{Requirement: "db", Assignment: "vault", Revision: 4, Keys: []string{"password"}},
			Previous: &Access{Requirement: "db", Assignment: "vault", Revision: 3, Keys: []string{"password"}},
		}},
		Steps: []Step{{
			Position: 1, Action: "INSTALL", InstallationID: "inst", Entity: "keycloak",
			Product: "keycloak", Version: "1.2.3", ConfigDigest: "sha256:aa",
			Values: json.RawMessage(`{"replicas":2}`),
			Maintenance: MaintenanceConstraint{
				RequiresDowntime: true,
				Overrides: []Override{{
					Type: "DOWNTIME", StartsAt: time.Unix(0, 0).UTC(), EndsAt: time.Unix(3600, 0).UTC(),
					Reason: "incident", ApprovedBy: "operator",
				}},
			},
			UpgradeRationale: &UpgradeRationale{Reason: "cve", CurrentVersion: "1.2.2", TargetVersion: "1.2.3"},
		}},
		IgnoredNamespaces: []IgnoredNamespace{{Entity: "keycloak", NamespaceRID: "ns/1", Reason: "denied"}},
	}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Plan
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, decoded) {
		t.Fatalf("a Plan did not survive a round trip:\n before %#v\n after  %#v", original, decoded)
	}
}

// A Plan names its Environment; it does not carry one. If that ever changes,
// every persisted Plan starts embedding a frozen copy of an Environment's
// configuration, and this is where it should be noticed.
func TestPlanReferencesItsEnvironmentByIdentifier(t *testing.T) {
	t.Parallel()
	field, found := reflect.TypeOf(Plan{}).FieldByName("Environment")
	if !found {
		t.Fatal("Plan must name its Environment")
	}
	if field.Type.Kind() != reflect.String {
		t.Fatalf("Plan.Environment must be an identifier, not %s", field.Type)
	}
}

// The default is what every Plan authored before the override existed relies
// on, and a whitespace-only override is not an override.
func TestHelmReleaseFallsBackToTheEntity(t *testing.T) {
	t.Parallel()
	if got := (Step{Entity: "keycloak"}).HelmRelease(); got != "keycloak" {
		t.Errorf("default = %s, want the Entity name", got)
	}
	if got := (Step{Entity: "keycloak", HelmReleaseName: "  "}).HelmRelease(); got != "keycloak" {
		t.Errorf("blank override = %q, want the Entity name", got)
	}
	if got := (Step{Entity: "keycloak", HelmReleaseName: "helm-keycloak"}).HelmRelease(); got != "helm-keycloak" {
		t.Errorf("override = %s", got)
	}
}

// The Hub refuses to suppress an installation a cancelled Plan never touched,
// so the answer has to be about the steps and nothing else -- an empty Plan
// contains nothing, and a step for one installation does not admit another.
func TestContainsInstallationAnswersFromTheSteps(t *testing.T) {
	t.Parallel()
	plan := Plan{Steps: []Step{{InstallationID: "example-api"}, {InstallationID: "example-worker"}}}
	if !plan.ContainsInstallation("example-worker") {
		t.Error("a Plan does not contain an installation one of its steps names")
	}
	if plan.ContainsInstallation("example-database") {
		t.Error("a Plan contains an installation no step names")
	}
	if (Plan{}).ContainsInstallation("example-api") {
		t.Error("a Plan with no steps contains an installation")
	}
}

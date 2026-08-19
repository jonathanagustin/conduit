package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func foundation() DesiredState {
	return DesiredState{
		APIVersion:  "platform.delivery/v1alpha1",
		Kind:        "EnvironmentDeployment",
		Environment: Environment{ID: "dev-use2", Generation: "0123456789abcdef"},
		Releases: []Release{
			{
				Product: "app.example:example-api", Version: "0.1.0",
				Chart:  Artifact{Repository: "registry.example/example-api", Tag: "0.1.0", Digest: digest},
				Images: []Artifact{{Repository: "registry.example/example-api", Tag: "0.1.0", Digest: digest}},
			},
			{
				Product: "app.example:example-workspace", Version: "0.1.0",
				Chart:        Artifact{Repository: "registry.example/example-workspace", Tag: "0.1.0", Digest: digest},
				Images:       []Artifact{{Repository: "registry.example/example-workspace", Tag: "0.1.0", Digest: digest}},
				Dependencies: []Dependency{{Product: "app.example:example-api", MinimumVersion: "0.1.0", MaximumVersion: "0.1.999"}},
			},
		},
		Entities: []Entity{
			{Name: "example-workspace", Product: "app.example:example-workspace", Version: "0.1.0", Namespace: "example"},
			{Name: "example-api", Product: "app.example:example-api", Version: "0.1.0", Namespace: "example"},
		},
	}
}

func assessedFoundation() DesiredState {
	state := foundation()
	state.Environment.ArtifactSecurityPolicy = &ArtifactSecurityPolicy{
		RequiredOutcome: "PASS",
	}
	for index := range state.Releases {
		state.Releases[index].VulnerabilityAssessment = &VulnerabilityAssessment{
			Provider: "test-scanner", RunID: "scan-20260801", Status: "COMPLETE", Outcome: "PASS",
			ArtifactDigests: []string{digest},
		}
	}
	return state
}

func TestVersionEndpointExposesBuildIdentity(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/version", nil)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("version = %d %s", response.Code, response.Body.String())
	}
	var info struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Version == "" || info.Commit == "" {
		t.Fatalf("version response = %#v", info)
	}
}

func TestBuildPlanOrdersDependencies(t *testing.T) {
	plan, err := BuildPlan(foundation())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(plan.Steps), 2; got != want {
		t.Fatalf("got %d steps, want %d", got, want)
	}
	if plan.Steps[0].Entity != "example-api" || plan.Steps[1].Entity != "example-workspace" {
		t.Fatalf("unexpected order: %#v", plan.Steps)
	}
	if got := plan.Steps[1].DependsOn; len(got) != 1 || got[0] != "example-api" {
		t.Fatalf("workspace dependencies = %#v", got)
	}
}

func TestOptionalDependencyMayBeAbsentAndOrdersWhenPresent(t *testing.T) {
	state := foundation()
	state.Releases[1].Dependencies[0].Optional = true

	withDependency, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	if got := withDependency.Steps[1].DependsOn; len(got) != 1 || got[0] != "example-api" {
		t.Fatalf("present optional dependency was not ordered: %#v", got)
	}

	state.Releases = state.Releases[1:]
	state.Entities = state.Entities[:1]
	state.Entities[0] = Entity{
		Name: "example-workspace", Product: "app.example:example-workspace", Version: "0.1.0", Namespace: "example",
	}
	withoutDependency, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(withoutDependency.Steps) != 1 || len(withoutDependency.Steps[0].DependsOn) != 0 {
		t.Fatalf("absent optional dependency changed plan: %#v", withoutDependency)
	}
}

func TestProductIncompatibilityBlocksOnlyMatchingVersionRange(t *testing.T) {
	state := foundation()
	state.Releases[1].Incompatibilities = []Incompatibility{{
		Product: "app.example:example-api", MinimumVersion: "0.1.0", MaximumVersion: "0.1.x",
	}}
	errs := Validate(state)
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "is incompatible with app.example:example-api") {
		t.Fatalf("incompatibility errors = %#v", errs)
	}

	state.Releases[1].Incompatibilities[0].MinimumVersion = "0.2.0"
	state.Releases[1].Incompatibilities[0].MaximumVersion = "0.2.x"
	if errs := Validate(state); len(errs) != 0 {
		t.Fatalf("non-matching incompatibility blocked state: %#v", errs)
	}
}

func TestDependencyGroupsOverridesAndIgnoredDependencies(t *testing.T) {
	state := foundation()
	state.Entities[0].DependencyGroups = []string{"frontend"}
	state.Entities[1].DependencyGroups = []string{"backend"}
	if errs := Validate(state); len(errs) != 1 || !strings.Contains(errs[0].Message, "allowed dependency group") {
		t.Fatalf("disjoint dependency groups = %#v", errs)
	}

	state.Entities[0].DependencyGroupOverrides = map[string][]string{
		"app.example:example-api": {"backend"},
	}
	plan, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Steps[1].DependsOn; len(got) != 1 || got[0] != "example-api" {
		t.Fatalf("dependency group override did not order plan: %#v", got)
	}

	state.Entities[0].DependencyGroupOverrides = nil
	state.Entities[0].IgnoredDependencies = []string{"app.example:example-api"}
	state.Releases = state.Releases[1:]
	state.Entities = state.Entities[:1]
	plan, err = BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || len(plan.Steps[0].DependsOn) != 0 {
		t.Fatalf("ignored dependency changed plan: %#v", plan)
	}
}

func TestIncompatibilityRequiresSharedDependencyGroup(t *testing.T) {
	state := foundation()
	state.Releases[1].Dependencies = nil
	state.Releases[1].Incompatibilities = []Incompatibility{{Product: "app.example:example-api"}}
	state.Entities[0].DependencyGroups = []string{"frontend"}
	state.Entities[1].DependencyGroups = []string{"backend"}
	if errs := Validate(state); len(errs) != 0 {
		t.Fatalf("disjoint-group incompatibility blocked state: %#v", errs)
	}
	state.Entities[0].DependencyGroupOverrides = map[string][]string{"app.example:example-api": {"*"}}
	if errs := Validate(state); len(errs) != 1 || !strings.Contains(errs[0].Message, "incompatible") {
		t.Fatalf("wildcard incompatibility did not apply: %#v", errs)
	}
}

func TestValidateRejectsMutableArtifacts(t *testing.T) {
	state := foundation()
	state.Releases[0].Images[0].Tag = "latest"
	state.Releases[0].Images[0].Digest = ""
	errs := Validate(state)
	if len(errs) != 2 {
		t.Fatalf("got %#v, want tag and digest errors", errs)
	}
}

func TestArtifactSecurityPolicyRequiresCompleteDigestMatchedAssessment(t *testing.T) {
	state := assessedFoundation()
	state.Releases[0].VulnerabilityAssessment = nil
	state.Releases[1].VulnerabilityAssessment.Status = "RUNNING"
	errs := Validate(state)
	if len(errs) != 2 {
		t.Fatalf("unexpected assessment errors: %#v", errs)
	}
	want := map[string]bool{
		"entities[1].release.vulnerabilityAssessment": false,
		"releases[1].vulnerabilityAssessment.status":  false,
	}
	assertValidationPaths(t, errs, want)
}

func TestArtifactSecurityPolicyRejectsUncoveredArtifactAndFailedOutcome(t *testing.T) {
	state := assessedFoundation()
	assessment := state.Releases[0].VulnerabilityAssessment
	assessment.ArtifactDigests = []string{"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	assessment.Outcome = "FAIL"
	assessment.ActiveVulnerabilities = 2
	errs := Validate(state)
	if len(errs) != 3 {
		t.Fatalf("unexpected policy errors: %#v", errs)
	}
	want := map[string]bool{
		"releases[0].vulnerabilityAssessment.artifactDigests": false,
		"entities[1].release.vulnerabilityAssessment.outcome": false,
	}
	assertValidationPaths(t, errs, want)
}

func TestArtifactSecurityPolicyAllowsNormalizedPassingAssessments(t *testing.T) {
	state := assessedFoundation()
	if errs := Validate(state); len(errs) != 0 {
		t.Fatalf("passing assessments rejected: %#v", errs)
	}
	if _, err := BuildPlan(state); err != nil {
		t.Fatal(err)
	}
}

func TestArtifactAvailabilityPolicyRequiresExactAvailableEvidence(t *testing.T) {
	t.Parallel()
	state := foundation()
	state.Environment.ArtifactAvailabilityPolicy = &ArtifactAvailabilityPolicy{RequiredOutcome: "AVAILABLE"}
	state.Environment.ArtifactRegistries = []ArtifactRegistryAssignment{{
		ID: "primary", URL: "registry.example", Revision: 3,
	}}
	for index := range state.Releases {
		state.Releases[index].ArtifactAvailability = &ArtifactAvailabilityAssessment{
			Provider: "oci-probe", RunID: "probe-20260801", Status: "COMPLETE", Outcome: "AVAILABLE",
			ArtifactDigests: []string{digest},
		}
	}
	if errs := Validate(state); len(errs) != 0 {
		t.Fatalf("available artifacts rejected: %#v", errs)
	}
	state.Releases[0].ArtifactAvailability.Outcome = "UNAVAILABLE"
	state.Releases[1].ArtifactAvailability.ArtifactDigests = nil
	errs := Validate(state)
	if len(errs) != 2 {
		t.Fatalf("availability errors = %#v", errs)
	}
	assertValidationPaths(t, errs, map[string]bool{
		"entities[1].release.artifactAvailability.outcome": false,
		"releases[1].artifactAvailability.artifactDigests": false,
	})
}

func assertValidationPaths(t *testing.T, errs []ValidationError, want map[string]bool) {
	t.Helper()
	for _, validationError := range errs {
		if _, exists := want[validationError.Path]; !exists {
			t.Fatalf("unexpected validation error: %#v", validationError)
		}
		want[validationError.Path] = true
	}
	for path, found := range want {
		if !found {
			t.Fatalf("missing validation error at %s: %#v", path, errs)
		}
	}
}

func TestArtifactRegistryAssignmentsRejectUnassignedAndAmbiguousMirrors(t *testing.T) {
	t.Parallel()
	state := foundation()
	state.Environment.ArtifactRegistries = []ArtifactRegistryAssignment{
		{ID: "primary", URL: "other.example", Revision: 1, Mirrors: []string{"registry.example"}},
		{ID: "secondary", URL: "second.example", Revision: 1, Mirrors: []string{"registry.example"}},
	}
	errs := Validate(state)
	foundOverlap := false
	for _, validationError := range errs {
		if strings.Contains(validationError.Message, "mirror selection must be deterministic") {
			foundOverlap = true
		}
	}
	if !foundOverlap {
		t.Fatalf("ambiguous mirror was not rejected: %#v", errs)
	}

	state.Environment.ArtifactRegistries = []ArtifactRegistryAssignment{{
		ID: "primary", URL: "other.example", Revision: 1,
	}}
	errs = Validate(state)
	unassigned := 0
	for _, validationError := range errs {
		if strings.Contains(validationError.Message, "is not assigned to the Environment") {
			unassigned++
		}
	}
	if unassigned != 4 {
		t.Fatalf("unassigned artifact errors = %d: %#v", unassigned, errs)
	}
}

func TestArtifactRegistryAssignmentRewritesPlanAndKeepsCredentialsExternal(t *testing.T) {
	t.Parallel()
	state := foundation()
	state.Environment.ArtifactRegistries = []ArtifactRegistryAssignment{{
		ID: "primary", URL: "mirror.internal", Revision: 4, Mirrors: []string{"registry.example"},
		Authentication: "EXTERNAL", CredentialRef: "vault://platform/registries/mirror",
		ImagePullSecretName: "mirror-pull",
	}}
	if errs := Validate(state); len(errs) != 0 {
		t.Fatalf("registry assignment rejected: %#v", errs)
	}
	plan, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range plan.Steps {
		if !strings.HasPrefix(step.Chart.Repository, "mirror.internal/") {
			t.Fatalf("chart was not rewritten: %#v", step.Chart)
		}
		for _, image := range step.Images {
			if !strings.HasPrefix(image.Repository, "mirror.internal/") || image.Tag != "0.1.0" || image.Digest != digest {
				t.Fatalf("image coordinate lost fidelity: %#v", image)
			}
		}
		if len(step.RegistryAccess) != 1 {
			t.Fatalf("registry access = %#v", step.RegistryAccess)
		}
		access := step.RegistryAccess[0]
		if access.AssignmentID != "primary" || access.Revision != 4 || access.SourceRegistry != "registry.example" ||
			access.RuntimeRegistry != "mirror.internal" || access.CredentialRef != "vault://platform/registries/mirror" ||
			access.ImagePullSecretName != "mirror-pull" {
			t.Fatalf("registry access lost assignment data: %#v", access)
		}
		var values struct {
			Apollo struct {
				DefaultArtifactStoreURI string              `json:"defaultArtifactStoreURI"`
				ImagePullSecrets        []map[string]string `json:"imagePullSecrets"`
			} `json:"apollo"`
		}
		if err := json.Unmarshal(step.Values, &values); err != nil {
			t.Fatal(err)
		}
		if values.Apollo.DefaultArtifactStoreURI != "mirror.internal" || len(values.Apollo.ImagePullSecrets) != 1 ||
			values.Apollo.ImagePullSecrets[0]["name"] != "mirror-pull" {
			t.Fatalf("Apollo registry values = %#v", values.Apollo)
		}
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"username", "password", "secret-value"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("plan disclosed secret material %q: %s", forbidden, encoded)
		}
	}
}

func TestArtifactRegistryAuthenticationValidationIsFailClosed(t *testing.T) {
	t.Parallel()
	for name, assignment := range map[string]ArtifactRegistryAssignment{
		"external without reference": {ID: "primary", URL: "registry.example", Revision: 1, Authentication: "EXTERNAL"},
		"none with reference":        {ID: "primary", URL: "registry.example", Revision: 1, Authentication: "NONE", CredentialRef: "vault://unexpected"},
	} {
		t.Run(name, func(t *testing.T) {
			state := foundation()
			state.Environment.ArtifactRegistries = []ArtifactRegistryAssignment{assignment}
			errs := Validate(state)
			found := false
			for _, validationError := range errs {
				if strings.Contains(validationError.Path, "artifactRegistries[0]") {
					found = true
				}
			}
			if !found {
				t.Fatalf("invalid authentication accepted: %#v", errs)
			}
		})
	}
}

func TestArtifactRegistryTypeValidationIsFailClosed(t *testing.T) {
	t.Parallel()
	typeError := func(assignment ArtifactRegistryAssignment) bool {
		state := foundation()
		state.Environment.ArtifactRegistries = []ArtifactRegistryAssignment{assignment}
		for _, validationError := range Validate(state) {
			if strings.Contains(validationError.Path, "artifactRegistries[0].type") {
				return true
			}
		}
		return false
	}
	base := ArtifactRegistryAssignment{ID: "primary", URL: "registry.example", Revision: 1}
	// An unknown type is rejected rather than passed through as a label.
	unknown := base
	unknown.Type = "TENANT"
	if !typeError(unknown) {
		t.Fatal("unknown Artifact Registry type accepted")
	}
	// Empty (STANDARD), STANDARD, and the Apollo-documented external-build type
	// are accepted.
	for _, accepted := range []string{"", "STANDARD", "EXTERNAL_PALANTIR_BUILD"} {
		valid := base
		valid.Type = accepted
		if typeError(valid) {
			t.Errorf("type %q rejected", accepted)
		}
	}
}

func TestExternalSecretRequirementsProduceWriteOnlyRotationCoordinates(t *testing.T) {
	t.Parallel()
	state := foundation()
	state.Environment.Secrets = []ExternalSecretAssignment{{
		Name: "database", Revision: 7, ProviderRef: "vault://platform/dev/database",
		KubernetesSecretName: "database-runtime", Keys: []string{"password", "username"},
	}}
	state.Entities[0].SecretRequirements = []SecretRequirement{{
		Name: "database-credentials", Secret: "database", Keys: []string{"username", "password"},
	}}
	plan, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	workspace := plan.Steps[1]
	if workspace.Entity != "example-workspace" || len(workspace.SecretAccess) != 1 {
		t.Fatalf("secret access plan = %#v", workspace)
	}
	access := workspace.SecretAccess[0]
	if access.Requirement != "database-credentials" || access.Assignment != "database" || access.Revision != 7 ||
		access.ProviderRef != "vault://platform/dev/database" || access.KubernetesSecretName != "database-runtime" ||
		strings.Join(access.Keys, ",") != "password,username" {
		t.Fatalf("secret access lost metadata: %#v", access)
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"actual-password", "secretValue", "plaintext"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("Plan disclosed secret material %q: %s", forbidden, encoded)
		}
	}

	state.Environment.Secrets[0].Revision++
	rotated, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Steps[1].ConfigDigest == workspace.ConfigDigest {
		t.Fatal("secret revision did not produce a new configuration coordinate")
	}
	if string(rotated.Steps[1].Values) != string(workspace.Values) {
		t.Fatal("secret rotation unexpectedly injected provider data into Helm values")
	}
}

func TestExternalSecretLifecycleProducesCreateVerifyReplaceAndDelete(t *testing.T) {
	state := foundation()
	state.Environment.Secrets = []ExternalSecretAssignment{{
		Name: "database", Revision: 7, ProviderRef: "vault://platform/dev/database/7",
		KubernetesSecretName: "database-runtime", Keys: []string{"password", "username"},
	}}
	state.Entities[0].SecretRequirements = []SecretRequirement{{
		Name: "database-credentials", Secret: "database", Keys: []string{"username", "password"},
	}}

	created, err := BuildPlanWithObserved(state, EnvironmentReport{}, time.Now())
	if err != nil || len(created.SecretMutations) != 1 || created.SecretMutations[0].Action != SecretCreate {
		t.Fatalf("create mutations=%#v err=%v", created.SecretMutations, err)
	}
	createReport := EnvironmentReport{Secrets: []ReportedSecret{{
		Assignment: "database", Namespace: "example", Revision: 7,
		ProviderRef: "vault://platform/dev/database/7", KubernetesSecretName: "database-runtime",
		Keys: []string{"password", "username"},
	}}}
	for _, step := range created.Steps {
		createReport.Entities = append(createReport.Entities, ReportedEntity{
			InstallationID: step.InstallationID, Product: step.Product, Version: step.Version,
			ReleaseDigest: step.ReleaseDigest, Namespace: step.Namespace, ConfigDigest: step.ConfigDigest,
			Health: HealthHealthy, RolloutStatus: RolloutUpToDate, ConfigStatus: ConfigApplied, Lifecycle: LifecycleInstalled,
		})
	}
	if err := validateEnvironmentReport(created, createReport, true); err != nil {
		t.Fatalf("secret report validation = %v", err)
	}
	createReport.Secrets[0].Revision = 6
	if err := validateEnvironmentReport(created, createReport, true); err == nil || !strings.Contains(err.Error(), "reported secret") {
		t.Fatalf("mismatched secret report validation = %v", err)
	}
	reported := []ReportedSecret{{
		Assignment: "database", Namespace: "example", Revision: 7,
		ProviderRef: "vault://platform/dev/database/7", KubernetesSecretName: "database-runtime",
		Keys: []string{"password", "username"},
	}}
	verified, err := BuildPlanWithObserved(state, EnvironmentReport{Secrets: reported}, time.Now())
	if err != nil || len(verified.SecretMutations) != 1 || verified.SecretMutations[0].Action != SecretVerify {
		t.Fatalf("verify mutations=%#v err=%v", verified.SecretMutations, err)
	}

	state.Environment.Secrets[0].Revision = 8
	state.Environment.Secrets[0].ProviderRef = "vault://platform/dev/database/8"
	replaced, err := BuildPlanWithObserved(state, EnvironmentReport{Secrets: reported}, time.Now())
	if err != nil || len(replaced.SecretMutations) != 1 || replaced.SecretMutations[0].Action != SecretReplace ||
		replaced.SecretMutations[0].Previous == nil || replaced.SecretMutations[0].Current == nil {
		t.Fatalf("replace mutations=%#v err=%v", replaced.SecretMutations, err)
	}

	state.Environment.Secrets = nil
	state.Entities[0].SecretRequirements = nil
	deleted, err := BuildPlanWithObserved(state, EnvironmentReport{Secrets: reported}, time.Now())
	if err != nil || len(deleted.SecretMutations) != 1 || deleted.SecretMutations[0].Action != SecretDelete || deleted.SecretMutations[0].Current != nil {
		t.Fatalf("delete mutations=%#v err=%v", deleted.SecretMutations, err)
	}
}

func TestExternalSecretLifecycleRejectsRollbackAndUnversionedMutation(t *testing.T) {
	state := foundation()
	state.Environment.Secrets = []ExternalSecretAssignment{{
		Name: "database", Revision: 7, ProviderRef: "vault://platform/dev/database/7",
		KubernetesSecretName: "database-runtime", Keys: []string{"password"},
	}}
	state.Entities[0].SecretRequirements = []SecretRequirement{{
		Name: "database-credentials", Secret: "database", Keys: []string{"password"},
	}}
	reported := EnvironmentReport{Secrets: []ReportedSecret{{
		Assignment: "database", Namespace: "example", Revision: 8,
		ProviderRef: "vault://platform/dev/database/8", KubernetesSecretName: "database-runtime", Keys: []string{"password"},
	}}}
	if _, err := BuildPlanWithObserved(state, reported, time.Now()); err == nil || !strings.Contains(err.Error(), "older than reported") {
		t.Fatalf("secret revision rollback error = %v", err)
	}

	reported.Secrets[0].Revision = 7
	if _, err := BuildPlanWithObserved(state, reported, time.Now()); err == nil || !strings.Contains(err.Error(), "without incrementing revision") {
		t.Fatalf("unversioned metadata mutation error = %v", err)
	}
}

func TestExternalSecretValidationRejectsMissingAndUnexposedMaterial(t *testing.T) {
	t.Parallel()
	state := foundation()
	state.Environment.Secrets = []ExternalSecretAssignment{{
		Name: "database", Revision: 1, ProviderRef: "vault://database",
		KubernetesSecretName: "database-runtime", Keys: []string{"username"},
	}}
	state.Entities[0].SecretRequirements = []SecretRequirement{{
		Name: "database-credentials", Secret: "database", Keys: []string{"password"},
	}, {
		Name: "missing", Secret: "absent", Keys: []string{"value"},
	}}
	errs := Validate(state)
	want := map[string]bool{
		"entities[0].secretRequirements[0].keys[0]": false,
		"entities[0].secretRequirements[1].secret":  false,
	}
	for _, validationError := range errs {
		if _, tracked := want[validationError.Path]; tracked {
			want[validationError.Path] = true
		}
	}
	for path, found := range want {
		if !found {
			t.Fatalf("missing fail-closed validation at %s: %#v", path, errs)
		}
	}
}

func TestValidateRejectsDependencyOutsideRange(t *testing.T) {
	state := foundation()
	state.Entities[1].Version = "0.0.9"
	state.Releases[0].Version = "0.0.9"
	errs := Validate(state)
	if len(errs) != 1 || errs[0].Message != "requires app.example:example-api >= 0.1.0" {
		t.Fatalf("unexpected dependency errors: %#v", errs)
	}
}

func TestPlanEndpoint(t *testing.T) {
	body, err := json.Marshal(foundation())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/plans", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	var record PlanRecord
	if err := json.Unmarshal(recorder.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.Status != PlanAwaitingApproval || record.Plan.Steps[0].Entity != "example-api" {
		t.Fatalf("unexpected record = %#v", record)
	}
}

func TestDurablePlanLifecycle(t *testing.T) {
	repository, err := NewFileRepository(filepath.Join(t.TempDir(), "delivery-state"))
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := NewLifecycle(repository)
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	lifecycle.now = func() time.Time { return now }
	lifecycle.newToken = func() (string, error) { return "lease-token", nil }

	record, err := lifecycle.CreatePlan(context.Background(), foundation())
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != PlanAwaitingApproval || record.Revision != 1 {
		t.Fatalf("created record = %#v", record)
	}
	now = now.Add(time.Minute)
	record, err = lifecycle.ApprovePlan(context.Background(), record.ID, "operator@example.test", "reviewed foundation rollout")
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != PlanApproved || len(record.Approvals) != 1 || record.Revision != 2 {
		t.Fatalf("approved record = %#v", record)
	}
	record, err = lifecycle.LeaseNextPlan(context.Background(), "dev-use2", "agent-dev-use2", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != PlanLeased || record.Lease.Token != "lease-token" || record.Lease.Attempt != 1 {
		t.Fatalf("leased record = %#v", record)
	}
	record, err = lifecycle.StartPlan(context.Background(), record.ID, record.Lease.Token)
	if err != nil {
		t.Fatal(err)
	}

	entities := make([]ReportedEntity, 0, len(record.Plan.Steps))
	for _, step := range record.Plan.Steps {
		entities = append(entities, ReportedEntity{
			InstallationID: step.InstallationID, Product: step.Product, Version: step.Version,
			ReleaseDigest: step.ReleaseDigest, Namespace: step.Namespace, ConfigDigest: step.ConfigDigest,
			Health: HealthHealthy, RolloutStatus: RolloutUpToDate,
			ConfigStatus: ConfigApplied, Lifecycle: LifecycleInstalled,
		})
	}
	record, err = lifecycle.FinishPlan(
		context.Background(), record.ID, record.Lease.Token, true, "", "", successfulPlanExecution(record), &EnvironmentReport{Entities: entities},
	)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != PlanSucceeded {
		t.Fatalf("finished record = %#v", record)
	}

	reopened, err := NewFileRepository(filepath.Join(filepath.Dir(repository.root), "delivery-state"))
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := reopened.GetPlan(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != PlanSucceeded || persisted.Revision != 5 {
		t.Fatalf("persisted record = %#v", persisted)
	}
	report, err := reopened.GetEnvironmentReport(context.Background(), "dev-use2")
	if err != nil {
		t.Fatal(err)
	}
	if report.AgentID != "agent-dev-use2" || report.PlanID != record.ID || len(report.Entities) != 2 {
		t.Fatalf("persisted report = %#v", report)
	}
}

func TestExpiredRunningPlanCanBeReLeased(t *testing.T) {
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	lifecycle.now = func() time.Time { return now }
	tokens := []string{"first-token", "second-token"}
	lifecycle.newToken = func() (string, error) {
		token := tokens[0]
		tokens = tokens[1:]
		return token, nil
	}
	record, _ := lifecycle.CreatePlan(context.Background(), foundation())
	record, _ = lifecycle.ApprovePlan(context.Background(), record.ID, "reviewer", "approved")
	record, _ = lifecycle.LeaseNextPlan(context.Background(), "dev-use2", "agent-one", time.Minute)
	record, _ = lifecycle.StartPlan(context.Background(), record.ID, record.Lease.Token)
	now = now.Add(2 * time.Minute)

	released, err := lifecycle.LeaseNextPlan(context.Background(), "dev-use2", "agent-two", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if released.Lease.Attempt != 2 || released.Lease.AgentID != "agent-two" || released.Lease.Token != "second-token" {
		t.Fatalf("re-leased record = %#v", released)
	}
	if _, err := lifecycle.StartPlan(context.Background(), record.ID, "first-token"); !errors.Is(err, ErrLeaseRejected) {
		t.Fatalf("old lease token error = %v", err)
	}
}

func TestGenerationIsImmutable(t *testing.T) {
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	if _, err := lifecycle.CreatePlan(context.Background(), foundation()); err != nil {
		t.Fatal(err)
	}
	changed := foundation()
	changed.Entities[0].Name = "workspace-renamed"
	if _, err := lifecycle.CreatePlan(context.Background(), changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed desired state at same generation error = %v", err)
	}
}

func TestOnlyOneAgentCanLeasePlan(t *testing.T) {
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	record, _ := lifecycle.CreatePlan(context.Background(), foundation())
	_, _ = lifecycle.ApprovePlan(context.Background(), record.ID, "reviewer", "approved")

	var wait sync.WaitGroup
	results := make(chan error, 8)
	for index := 0; index < 8; index++ {
		wait.Add(1)
		go func(agent int) {
			defer wait.Done()
			_, err := lifecycle.LeaseNextPlan(
				context.Background(), "dev-use2", "agent-"+string(rune('a'+agent)), time.Minute,
			)
			results <- err
		}(index)
	}
	wait.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrNoPlanAvailable) {
			t.Fatalf("unexpected lease error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful leases = %d, want 1", successes)
	}
}

func TestDesiredEntityRemovalCreatesReverseDependencyUninstallPlanAndSupersedesStalePlan(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	initial := foundation()
	stale, err := lifecycle.CreatePlan(ctx, initial)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PutEnvironmentReport(ctx, installedReport(t, initial)); err != nil {
		t.Fatal(err)
	}
	removed := initial
	removed.Environment.Generation = "fedcba9876543210"
	removed.Entities = nil
	created, err := lifecycle.CreatePlan(ctx, removed)
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Plan.Steps) != 2 || created.Plan.Steps[0].Action != "uninstall" ||
		created.Plan.Steps[0].InstallationID != "example-workspace" ||
		created.Plan.Steps[1].InstallationID != "example-api" {
		t.Fatalf("ordinary uninstall Plan = %#v", created.Plan.Steps)
	}
	old, err := repository.GetPlan(ctx, stale.ID)
	if err != nil {
		t.Fatal(err)
	}
	if old.Status != PlanCancelled || old.Cancellation == nil || !strings.Contains(old.Cancellation.Reason, created.ID) {
		t.Fatalf("stale Plan was not superseded: %#v", old)
	}
}

func TestRecallRollOffLeasesBeforeOlderRoutinePlan(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	routine := PlanRecord{
		ID: "pln_routine", Plan: Plan{Environment: "dev", Steps: []PlanStep{{InstallationID: "routine"}}},
		Status: PlanApproved, CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	recall := PlanRecord{
		ID: "pln_recall", Plan: Plan{Environment: "dev", Steps: []PlanStep{{InstallationID: "recall", RecallRollOff: true}}},
		Status: PlanApproved, CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute), Revision: 1,
	}
	if _, err := repository.CreatePlan(ctx, routine); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreatePlan(ctx, recall); err != nil {
		t.Fatal(err)
	}
	lifecycle := NewLifecycle(repository)
	lifecycle.now = func() time.Time { return now.Add(2 * time.Minute) }
	lifecycle.newToken = func() (string, error) { return "token", nil }
	leased, err := lifecycle.LeaseNextPlan(ctx, "dev", "agent", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if leased.ID != recall.ID {
		t.Fatalf("leased %s before recalled-release roll-off %s", leased.ID, recall.ID)
	}
}

func TestSuccessfulPlanRejectsIncompleteReportedState(t *testing.T) {
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	record, _ := lifecycle.CreatePlan(context.Background(), foundation())
	record, _ = lifecycle.ApprovePlan(context.Background(), record.ID, "reviewer", "approved")
	record, _ = lifecycle.LeaseNextPlan(context.Background(), "dev-use2", "agent", time.Minute)
	record, _ = lifecycle.StartPlan(context.Background(), record.ID, record.Lease.Token)

	_, err := lifecycle.FinishPlan(
		context.Background(), record.ID, record.Lease.Token, true, "", "", successfulPlanExecution(record), &EnvironmentReport{},
	)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("incomplete report error = %v", err)
	}
	persisted, _ := repository.GetPlan(context.Background(), record.ID)
	if persisted.Status != PlanRunning {
		t.Fatalf("invalid report changed plan status to %s", persisted.Status)
	}
}

func TestManagedInstallationConfigurationAcceptsTypedInputsAndTemplates(t *testing.T) {
	state := foundation()
	state.Environment.Namespaces = []NamespaceTarget{{
		RID: "ri.namespace.example", Name: "example", EnrollmentRID: "enrollment-a", OntologyCountValid: true,
		Organizations: []Organization{{DisplayName: "Example Org", PrimaryHostname: "example.example.test"}},
	}}
	state.Entities[0].Configuration = &ManagedInstallationConfiguration{
		CBACMarkings: []string{"marking-public"},
		InputMapping: map[string]json.RawMessage{
			"hostname": json.RawMessage(`{"url":"https://{{ default_hostname }}/api"}`),
		},
		PresetInputMapping: map[string]json.RawMessage{
			"organizationHost": json.RawMessage(`{"value":"{{default_hostname(organizationName:\"Example Org\")}}"}`),
		},
		SecretInputMapping: map[string]SecretResolvedInput{
			"credential": {
				Type:            "namedCredential",
				NamedCredential: &SecretName{SecretName: "example-api-runtime"},
			},
			"markings": {
				Type:     "markings",
				Markings: &SecretAndKeyName{SecretName: "example-markings", SecretKeyName: "allowed"},
			},
		},
	}
	state.Environment.Secrets = []ExternalSecretAssignment{
		{Name: "example-api-runtime", Revision: 1, ProviderRef: "vault://example/api", KubernetesSecretName: "example-api-runtime", Keys: []string{"username", "password"}},
		{Name: "example-markings", Revision: 1, ProviderRef: "vault://example/markings", KubernetesSecretName: "example-markings", Keys: []string{"allowed"}},
	}
	if errs := Validate(state); len(errs) != 0 {
		t.Fatalf("valid managed configuration rejected: %#v", errs)
	}
	encodedConfig, err := json.Marshal(state.Entities[0].Configuration)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encodedConfig, []byte(`"secretKeyName":"allowed"`)) || bytes.Contains(encodedConfig, []byte(`"keyName"`)) {
		t.Fatalf("markings input does not match the public Apollo Agent wire name: %s", encodedConfig)
	}
	plan, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Steps[1].ConfigDigest == "" {
		t.Fatal("configuration digest was not recorded")
	}
}

func TestManagedInstallationSecretInputsRequireAssignedSecretAndKey(t *testing.T) {
	state := foundation()
	state.Environment.Secrets = []ExternalSecretAssignment{{
		Name: "markings", Revision: 1, ProviderRef: "vault://markings",
		KubernetesSecretName: "markings", Keys: []string{"allowed"},
	}}
	state.Entities[0].Configuration = &ManagedInstallationConfiguration{SecretInputMapping: map[string]SecretResolvedInput{
		"markings": {
			Type: "markings", Markings: &SecretAndKeyName{SecretName: "markings", SecretKeyName: "missing"},
		},
		"credential": {
			Type: "namedCredential", NamedCredential: &SecretName{SecretName: "not-assigned"},
		},
	}}
	errs := Validate(state)
	joined, _ := json.Marshal(errs)
	if !bytes.Contains(joined, []byte("does not expose key missing")) || !bytes.Contains(joined, []byte("must reference an Environment secret assignment")) {
		t.Fatalf("secret input validation errors = %s", joined)
	}
}

func TestManagedInstallationConfigurationRejectsAmbiguousAndUnsafeInputs(t *testing.T) {
	state := foundation()
	state.Entities[0].Configuration = &ManagedInstallationConfiguration{
		InputMapping: map[string]json.RawMessage{
			"shared": json.RawMessage(`{"hostname":"{{ arbitrary_function }}"}`),
		},
		PresetInputMapping: map[string]json.RawMessage{
			"shared": json.RawMessage(`{"preset":"small"}`),
		},
		SecretInputMapping: map[string]SecretResolvedInput{
			"broken": {
				Type:            "markings",
				Markings:        &SecretAndKeyName{SecretName: "", SecretKeyName: ""},
				NamedCredential: &SecretName{SecretName: "also-populated"},
			},
		},
	}
	errs := Validate(state)
	messages := make(map[string]bool)
	for _, err := range errs {
		messages[err.Message] = true
	}
	for _, wanted := range []string{
		"contains an unsupported enrollment template: {{ arbitrary_function }}",
		"shape is already resolved by entities[0].configuration.inputMapping",
		"must populate exactly one secret-backed input variant",
	} {
		if !messages[wanted] {
			t.Fatalf("missing %q in %#v", wanted, errs)
		}
	}
}

func TestEnrollmentTemplatesResolveBeforeConfigurationDigest(t *testing.T) {
	config := &ManagedInstallationConfiguration{InputMapping: map[string]json.RawMessage{
		"endpoint": json.RawMessage(`{"url":"https://{{default_hostname}}/api"}`),
	}}
	namespaceA := &NamespaceTarget{
		RID:           "ri.namespace.a",
		Organizations: []Organization{{DisplayName: "A", PrimaryHostname: "a.example.test"}},
	}
	namespaceB := &NamespaceTarget{
		RID:           "ri.namespace.b",
		Organizations: []Organization{{DisplayName: "B", PrimaryHostname: "b.example.test"}},
	}
	digestA, err := resolvedConfigurationDigest(config, namespaceA)
	if err != nil {
		t.Fatal(err)
	}
	digestB, err := resolvedConfigurationDigest(config, namespaceB)
	if err != nil {
		t.Fatal(err)
	}
	if digestA == digestB {
		t.Fatalf("resolved namespace configurations have the same digest: %s", digestA)
	}
	if _, err := resolvedConfigurationDigest(config, nil); err == nil {
		t.Fatal("template resolution without namespace metadata succeeded")
	}
}

func TestApolloSecretKubernetesNamePreprocessingAndHelmMetadata(t *testing.T) {
	state := foundation()
	state.Environment.Secrets = []ExternalSecretAssignment{{
		Name: "keycloakadmin", Revision: 3, ProviderRef: "vault://keycloak/admin",
		KubernetesSecretName: "keycloak-admin-runtime", Keys: []string{"password"},
	}}
	state.Entities[0].ConfigOverrides = `0.1.0:
  overrides:
    auth:
      existingSecret: '{{ preprocess .Values.apollo.secrets.keycloakadmin.k8sSecretName }}'
`
	plan, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := json.Unmarshal(plan.Steps[1].Values, &values); err != nil {
		t.Fatal(err)
	}
	auth := values["auth"].(map[string]any)
	if auth["existingSecret"] != "keycloak-admin-runtime" {
		t.Fatalf("preprocessed secret name = %#v", auth["existingSecret"])
	}
	apollo := values["apollo"].(map[string]any)
	secretMetadata := apollo["secrets"].(map[string]any)["keycloakadmin"].(map[string]any)
	if secretMetadata["k8sSecretName"] != "keycloak-admin-runtime" {
		t.Fatalf("Apollo secret metadata = %#v", apollo["secrets"])
	}
	encoded, _ := json.Marshal(values)
	if bytes.Contains(encoded, []byte("vault://")) || bytes.Contains(encoded, []byte("password-value")) {
		t.Fatalf("Helm values disclosed provider material: %s", encoded)
	}

	state.Entities[0].ConfigOverrides = strings.ReplaceAll(state.Entities[0].ConfigOverrides, "keycloakadmin", "missing")
	if _, err := BuildPlan(state); err == nil || !strings.Contains(err.Error(), "deployment secret missing does not exist") {
		t.Fatalf("missing Apollo secret preprocessing error = %v", err)
	}
}

func TestEnrollmentTemplateOrganizationSelection(t *testing.T) {
	namespace := &NamespaceTarget{
		RID: "ri.namespace.multitenant",
		Organizations: []Organization{
			{DisplayName: "Primary", PrimaryHostname: "primary.example.test"},
			{DisplayName: "Secondary", PrimaryHostname: "secondary.example.test"},
		},
	}
	resolved, err := resolveTemplateString(
		`https://{{ default_hostname(organizationName: "Secondary") }}/api`, namespace,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "https://secondary.example.test/api" {
		t.Fatalf("resolved value = %q", resolved)
	}
	if _, err := resolveTemplateString("{{default_hostname}}", namespace); err == nil {
		t.Fatal("ambiguous default_hostname resolved with multiple organizations")
	}
	if _, err := resolveTemplateString(
		`{{default_hostname(organizationName:"secondary")}}`, namespace,
	); err == nil {
		t.Fatal("case-insensitive organization lookup unexpectedly succeeded")
	}
}

func TestUniversalInstallationTargetsAndIgnoreReasons(t *testing.T) {
	state := foundation()
	workspace := &state.Entities[0]
	state.Entities[1].Namespace = "target"
	workspace.Namespace = ""
	workspace.Universal = &UniversalInstallationConfiguration{
		EnrollmentAllowlist: []string{"enrollment-a"},
		NamespaceDenylist:   []string{"ri.namespace.denied"},
		ManagedConfig: ManagedInstallationConfiguration{
			CBACMarkings: []string{"marking-public"},
		},
	}
	state.Releases[1].Dependencies = nil
	digest, err := resolvedConfigurationDigest(&workspace.Universal.ManagedConfig, &NamespaceTarget{
		RID: "ri.namespace.current", Organizations: []Organization{{DisplayName: "Example Org", PrimaryHostname: "example.example.test"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	state.Environment.Namespaces = []NamespaceTarget{
		{RID: "ri.namespace.target", Name: "target", EnrollmentRID: "enrollment-a", OntologyCountValid: true},
		{RID: "ri.namespace.denied", Name: "denied", EnrollmentRID: "enrollment-a", OntologyCountValid: true},
		{RID: "ri.namespace.no-owner", Name: "no-owner", OntologyCountValid: true},
		{RID: "ri.namespace.other", Name: "other", EnrollmentRID: "enrollment-b", OntologyCountValid: true},
		{RID: "ri.namespace.trashed", Name: "trashed", EnrollmentRID: "enrollment-a", Trashed: true, OntologyCountValid: true},
		{RID: "ri.namespace.invalid-ontology", Name: "invalid-ontology", EnrollmentRID: "enrollment-a"},
		{
			RID: "ri.namespace.current", Name: "current", EnrollmentRID: "enrollment-a", OntologyCountValid: true,
			Installed: map[string]InstalledTarget{workspace.Product: {Version: workspace.Version, ConfigDigest: digest}},
		},
	}

	plan, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(plan.Steps), 2; got != want {
		t.Fatalf("steps = %d, want %d: %#v", got, want, plan.Steps)
	}
	if plan.Steps[1].InstallationID != "example-workspace@ri.namespace.target" {
		t.Fatalf("unexpected Universal target: %#v", plan.Steps[1])
	}
	reasons := make(map[string]string)
	for _, ignored := range plan.IgnoredNamespaces {
		reasons[ignored.NamespaceRID] = ignored.Reason
	}
	wantReasons := map[string]string{
		"ri.namespace.denied":           IgnoreNamespaceExplicitlyDenied,
		"ri.namespace.no-owner":         IgnoreNoOwningEnrollment,
		"ri.namespace.other":            IgnoreEnrollmentNotAllowed,
		"ri.namespace.trashed":          IgnoreNamespaceTrashed,
		"ri.namespace.invalid-ontology": IgnoreInvalidOntologyCount,
		"ri.namespace.current":          IgnoreAlreadyOnTarget,
	}
	for namespace, wanted := range wantReasons {
		if reasons[namespace] != wanted {
			t.Fatalf("reason for %s = %q, want %q; all=%#v", namespace, reasons[namespace], wanted, reasons)
		}
	}
}

func TestUniversalIgnoredNamespaceDoesNotResolveTemplates(t *testing.T) {
	state := foundation()
	workspace := &state.Entities[0]
	state.Entities[1].Namespace = "target"
	workspace.Namespace = ""
	workspace.Universal = &UniversalInstallationConfiguration{
		NamespaceDenylist: []string{"ri.namespace.denied"},
		ManagedConfig: ManagedInstallationConfiguration{InputMapping: map[string]json.RawMessage{
			"endpoint": json.RawMessage(`{"url":"https://{{default_hostname}}/api"}`),
		}},
	}
	state.Releases[1].Dependencies = nil
	state.Environment.Namespaces = []NamespaceTarget{
		{RID: "ri.namespace.denied", Name: "denied", EnrollmentRID: "enrollment-a", OntologyCountValid: true},
		{
			RID: "ri.namespace.target", Name: "target", EnrollmentRID: "enrollment-a", OntologyCountValid: true,
			Organizations: []Organization{{DisplayName: "Target", PrimaryHostname: "target.example.test"}},
		},
	}

	plan, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.IgnoredNamespaces) != 1 || plan.IgnoredNamespaces[0].Reason != IgnoreNamespaceExplicitlyDenied {
		t.Fatalf("ignored namespaces = %#v", plan.IgnoredNamespaces)
	}
}

func TestFixedInstallationMustUseInventoriedNamespace(t *testing.T) {
	state := foundation()
	state.Environment.Namespaces = []NamespaceTarget{{
		RID: "ri.namespace.other", Name: "other", EnrollmentRID: "enrollment-a", OntologyCountValid: true,
	}}
	errs := Validate(state)
	found := false
	for _, validationErr := range errs {
		if validationErr.Path == "entities[0].namespace" &&
			validationErr.Message == "must exist in environment namespace inventory" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing namespace inventory error: %#v", errs)
	}
}

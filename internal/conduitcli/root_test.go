package conduitcli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/operator"
)

type fakeService struct {
	approvedPlan string
	rationale    string
	teamName     string
	team         controlplane.Team
	environment  controlplane.EnvironmentCatalogRecord
	entities     []controlplane.Entity
	versions     []controlplane.TargetHubEntityVersion
	terraform    controlplane.TerraformProductManifest
	helmBundle   controlplane.HelmProductReleaseBundle
	// uploadedMediaType records what the CLI decided to declare for an
	// uploaded asset; the Hub stores it and cannot infer it from the bytes.
	uploadedMediaType string
	// proposed is the exact DesiredState the CLI handed to ProposePlan. It is
	// the whole payload of a Plan proposal, so a review-context test can read it
	// to see everything the command was able to transmit.
	proposed controlplane.DesiredState
}

func (*fakeService) ListArtifactRegistries(_ context.Context) ([]controlplane.ArtifactRegistryRecord, error) {
	return []controlplane.ArtifactRegistryRecord{{
		Environment: "dev",
		Assignment:  controlplane.ArtifactRegistryAssignment{ID: "primary", URL: "registry.example.test"},
	}}, nil
}
func (*fakeService) InstallModule(
	_ context.Context, environment, module, version, releaseChannel string, variables map[string]string,
) (controlplane.ModuleInstallation, error) {
	return controlplane.ModuleInstallation{
		ID: "mod_one", Environment: environment, Module: module, Version: version,
		ReleaseChannel: releaseChannel, Variables: variables,
	}, nil
}
func (service *fakeService) PublishProductRelease(
	_ context.Context, bundle controlplane.HelmProductReleaseBundle,
) (operator.ProductReleasePublication, error) {
	service.helmBundle = bundle
	manifest := bundle.Manifest
	return operator.ProductReleasePublication{
		MavenCoordinate: manifest.ProductGroup + ":" + manifest.ProductName + ":" + manifest.ProductVersion,
		Created:         true,
	}, nil
}

func TestProductReleaseCreatePublishesExactHelmConfigurationBundle(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	manifestPath := filepath.Join(directory, "manifest.yml")
	valuesPath := filepath.Join(directory, "values.yaml")
	schemaPath := filepath.Join(directory, "values.schema.json")
	digest := strings.Repeat("a", 64)
	manifest := `manifest-version: "1.0"
product-group: com.example
product-name: application
product-version: 2.0.0
product-type: helm-chart.v1
extensions:
  helm-chart:
    helm-chart-name: application
    helm-chart-version: 7.4.1
    helm-repository-url: oci://registry.example/charts/application:7.4.1@sha256:` + digest + `
  artifacts:
    - type: oci
      uri: registry.example/application:2.0.0@sha256:` + digest + `
`
	values := []byte("replicaCount: 3\n")
	schema := []byte(`{"type":"object"}`)
	for path, contents := range map[string][]byte{manifestPath: []byte(manifest), valuesPath: values, schemaPath: schema} {
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	service := &fakeService{}
	var output bytes.Buffer
	root := NewRoot(testRuntime(&output, service))
	root.SetArgs([]string{
		"--server", "http://127.0.0.1:8081", "--development-subject", "developer",
		"product-release", "create", "--manifest", manifestPath,
		"--default-config", valuesPath, "--config-schema", schemaPath,
	})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if service.helmBundle.Manifest.ProductVersion != "2.0.0" ||
		!bytes.Equal(service.helmBundle.DefaultConfig, values) || !bytes.Equal(service.helmBundle.ConfigSchema, schema) {
		t.Fatalf("bundle=%#v", service.helmBundle)
	}
}

func TestProductReleaseHelmChartInitAcceptsChartPathWithoutDuplicateMetadataFlags(t *testing.T) {
	t.Parallel()
	chart := filepath.Join(t.TempDir(), "chart")
	if err := os.MkdirAll(chart, 0o755); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	for name, contents := range map[string]string{
		"Chart.yaml":  "apiVersion: v2\nname: application\nversion: 7.4.1\n",
		"values.yaml": "image:\n  repository: registry.example/application\n  tag: 2.0.0\n  digest: sha256:" + digest + "\n",
	} {
		if err := os.WriteFile(filepath.Join(chart, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	root := NewRoot(testRuntime(&output, &fakeService{}))
	root.SetArgs([]string{
		"product-release", "helm-chart", "init",
		"--repository-url", "oci://registry.example/charts/application:7.4.1@sha256:" + digest,
		"--maven-coordinate", "com.example:application:2.0.0",
		"--chart-path", chart, "--output-dir", filepath.Join(t.TempDir(), "release"), "--as-tgz",
	})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"archivePath"`) || !strings.Contains(output.String(), "application-2.0.0.config.tgz") {
		t.Fatalf("output=%s", output.String())
	}
}
func (service *fakeService) PublishTerraformProductRelease(
	_ context.Context, manifest controlplane.TerraformProductManifest,
) (controlplane.ProductPublication, error) {
	service.terraform = manifest
	return controlplane.ProductPublication{
		MavenCoordinate: manifest.ProductGroup + ":" + manifest.ProductName + ":" + manifest.ProductVersion,
		Product:         manifest.ProductGroup + ":" + manifest.ProductName, Version: manifest.ProductVersion, Kind: "terraform",
	}, nil
}
func (*fakeService) ListProductPublications(_ context.Context, product string) ([]controlplane.ProductPublication, error) {
	return []controlplane.ProductPublication{{
		MavenCoordinate: product + ":1.2.3", Product: product, Version: "1.2.3", Kind: "helm-chart",
	}}, nil
}
func (*fakeService) ListEnvironments(context.Context) ([]controlplane.EnvironmentCatalogRecord, error) {
	return []controlplane.EnvironmentCatalogRecord{{Environment: controlplane.Environment{ID: "dev"}}}, nil
}

func (service *fakeService) GetEnvironment(_ context.Context, id string) (controlplane.EnvironmentCatalogRecord, error) {
	if service.environment.Environment.ID != "" {
		return service.environment, nil
	}
	return controlplane.EnvironmentCatalogRecord{Environment: controlplane.Environment{ID: id}}, nil
}

func (service *fakeService) ListEntities(_ context.Context, _ string) ([]controlplane.Entity, error) {
	if service.entities != nil {
		return service.entities, nil
	}
	return []controlplane.Entity{{Name: "application", Product: "com.example:application", Version: "1.2.3"}}, nil
}
func (*fakeService) ListProducts(context.Context) ([]controlplane.ProductCatalogRecord, error) {
	return []controlplane.ProductCatalogRecord{{Product: "com.example:application"}}, nil
}
func (*fakeService) ListProductReleases(_ context.Context, product string) ([]controlplane.Release, error) {
	return []controlplane.Release{{Product: product, Version: "1.2.3", Channels: []string{"RELEASE"}}}, nil
}
func (*fakeService) GetProductRelease(_ context.Context, product, version string) (controlplane.Release, error) {
	return controlplane.Release{Product: product, Version: version}, nil
}
func (*fakeService) ListModules(context.Context) ([]controlplane.ModuleRelease, error) {
	return []controlplane.ModuleRelease{{Name: "com.example:foundation", Version: "1.2.3"}}, nil
}
func (*fakeService) GetEnvironmentProfile(_ context.Context, environment string) (controlplane.EnvironmentProfile, error) {
	return controlplane.EnvironmentProfile{EnvironmentID: environment, Accreditation: "STANDARD"}, nil
}
func (*fakeService) PutEnvironmentProfile(
	_ context.Context, environment string, profile controlplane.EnvironmentProfile,
) (operator.EnvironmentProfileChangeResult, error) {
	profile.EnvironmentID = environment
	return operator.EnvironmentProfileChangeResult{Profile: &profile}, nil
}

func (service *fakeService) GetEnvironmentVersions(_ context.Context, _ string) ([]controlplane.TargetHubEntityVersion, error) {
	if service.versions != nil {
		return service.versions, nil
	}
	return []controlplane.TargetHubEntityVersion{{InstallationID: "application", Version: "1.2.3"}}, nil
}
func (*fakeService) PublishModules(_ context.Context, definitions []controlplane.ModuleDefinition) ([]controlplane.ModuleRelease, error) {
	releases := make([]controlplane.ModuleRelease, 0, len(definitions))
	for _, definition := range definitions {
		releases = append(releases, controlplane.ModuleRelease{Name: definition.Name, Version: "0.1.0"})
	}
	return releases, nil
}
func (*fakeService) LockModule(_ context.Context, definition controlplane.ModuleDefinition) (controlplane.ModuleLock, error) {
	return controlplane.ModuleLock{Module: definition.Name, ContentDigest: testDigest}, nil
}
func (*fakeService) ScanRelease(_ context.Context, _ controlplane.Release) (controlplane.ArtifactScanResult, error) {
	return controlplane.ArtifactScanResult{}, nil
}
func (*fakeService) IssueEntityCommand(_ context.Context, request controlplane.IssueCommandRequest) (controlplane.PlanRecord, error) {
	return controlplane.PlanRecord{ID: "cmd_one", Command: &controlplane.EntityCommand{Type: request.Type}}, nil
}
func (service *fakeService) UploadArtifact(_ context.Context, mediaType string, contents io.Reader) (controlplane.BlobDescriptor, error) {
	service.uploadedMediaType = mediaType
	_, _ = io.Copy(io.Discard, contents)
	return controlplane.BlobDescriptor{Digest: testDigest}, nil
}

func (*fakeService) ListChangeRequests(_ context.Context, environment string) ([]controlplane.ChangeRequest, error) {
	return []controlplane.ChangeRequest{
		{ID: "cr_one", Environment: environment, Title: "Deploy API", Status: "PENDING_APPROVALS", Author: controlplane.Principal{Username: "author"}},
		{ID: "cr_two", Environment: environment, Title: "Deploy worker", Status: "APPROVED", Author: controlplane.Principal{Username: "other"}},
	}, nil
}
func (*fakeService) GetChangeRequest(_ context.Context, id string) (controlplane.ChangeRequest, error) {
	return controlplane.ChangeRequest{ID: id, Status: "PENDING_APPROVALS"}, nil
}
func (*fakeService) ApproveChangeRequest(_ context.Context, id, rationale string) (operator.ChangeRequestActionResult, error) {
	return operator.ChangeRequestActionResult{ChangeRequest: controlplane.ChangeRequest{ID: id, Status: "APPROVED", Description: rationale}}, nil
}
func (*fakeService) RejectChangeRequest(_ context.Context, id, rationale string) (operator.ChangeRequestActionResult, error) {
	return operator.ChangeRequestActionResult{ChangeRequest: controlplane.ChangeRequest{ID: id, Status: "REJECTED", Description: rationale}}, nil
}
func (*fakeService) ReopenChangeRequest(_ context.Context, id string) (operator.ChangeRequestActionResult, error) {
	return operator.ChangeRequestActionResult{ChangeRequest: controlplane.ChangeRequest{ID: id, Status: "PENDING_APPROVALS"}}, nil
}
func (*fakeService) CancelChangeRequest(_ context.Context, id, reason string) (operator.ChangeRequestActionResult, error) {
	return operator.ChangeRequestActionResult{ChangeRequest: controlplane.ChangeRequest{ID: id, Status: "CANCELLED", Description: reason}}, nil
}

func (service *fakeService) PutTeam(_ context.Context, name string, team controlplane.Team) (controlplane.Team, error) {
	service.teamName, service.team = name, team
	team.Name = name
	return team, nil
}
func (*fakeService) CreateResourceGrant(_ context.Context, value controlplane.ResourceGrant) (controlplane.ResourceGrant, error) {
	return value, nil
}
func (*fakeService) PutApprovalPolicy(_ context.Context, id string, value controlplane.ApprovalPolicy) (controlplane.ApprovalPolicy, error) {
	value.ID = id
	return value, nil
}
func (service *fakeService) ProposePlan(_ context.Context, state controlplane.DesiredState) (controlplane.PlanRecord, error) {
	service.proposed = state
	return controlplane.PlanRecord{ID: "pln_proposed", Status: controlplane.PlanAwaitingApproval}, nil
}
func (service *fakeService) ApprovePlan(_ context.Context, id, rationale string) (controlplane.PlanRecord, error) {
	service.approvedPlan, service.rationale = id, rationale
	return controlplane.PlanRecord{ID: id, Status: controlplane.PlanApproved}, nil
}
func (*fakeService) CancelPlan(_ context.Context, id, _ string) (controlplane.PlanRecord, error) {
	return controlplane.PlanRecord{ID: id, Status: controlplane.PlanCancelled}, nil
}
func (*fakeService) GetPlan(_ context.Context, id string) (controlplane.PlanRecord, error) {
	return controlplane.PlanRecord{ID: id, Status: controlplane.PlanAwaitingApproval}, nil
}
func (*fakeService) GetReportedState(_ context.Context, environment string) (controlplane.EnvironmentReport, error) {
	return controlplane.EnvironmentReport{Environment: environment}, nil
}
func (*fakeService) ListSuppressions(_ context.Context, environment string) ([]controlplane.SuppressionRecord, error) {
	return []controlplane.SuppressionRecord{{ID: "sup_one", Environment: environment}}, nil
}
func (*fakeService) RemoveSuppression(_ context.Context, id string) (controlplane.SuppressionRecord, error) {
	return controlplane.SuppressionRecord{ID: id}, nil
}

func TestRootExposesNativeResourceCommandTree(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	root := NewRoot(Runtime{Stdout: &output, Stderr: &output, Getenv: func(string) string { return "" }})
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"approval-policy", "artifact-registry", "changerequest", "completion", "entity", "environment", "grant", "module", "plan", "product-release", "suppression", "team", "version"} {
		if !strings.Contains(output.String(), command) {
			t.Fatalf("help omitted %q:\n%s", command, output.String())
		}
	}
}

func TestArtifactRegistryListUsesResourceFirstCommandAndOutputAlias(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	root := NewRoot(testRuntime(&output, &fakeService{}))
	root.SetArgs([]string{
		"--server", "http://localhost:8081", "--development-subject", "developer", "-o", "yaml",
		"artifact-registry", "list",
	})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "environment: dev") || !strings.Contains(output.String(), "id: primary") {
		t.Fatalf("output=%s", output.String())
	}
}

// Each filter is exercised on its own. Setting all four at once and asking for
// one result cannot distinguish them: the fixture's two change requests differ
// on every field, so every filter selects the same row and any one of them
// going wrong is hidden by the other three. Dropping the "was this flag set"
// guard on a filter was invisible for exactly that reason.
func TestChangeRequestListAppliesDocumentedClientFilters(t *testing.T) {
	t.Parallel()
	// cr_one: author="author", status=PENDING_APPROVALS, title="Deploy API"
	// cr_two: author="other",  status=APPROVED,          title="Deploy worker"
	for name, test := range map[string]struct {
		flags []string
		want  []string
	}{
		"no filters returns everything": {want: []string{"cr_one", "cr_two"}},

		"author selects one":     {flags: []string{"--author", "author"}, want: []string{"cr_one"}},
		"author matches exactly": {flags: []string{"--author", "auth"}, want: nil},
		"author is not folded":   {flags: []string{"--author", "AUTHOR"}, want: nil},

		// Status is compared case-insensitively, which is the whole reason
		// EqualFold is there rather than ==.
		"status folds case":  {flags: []string{"--status", "pending_approvals"}, want: []string{"cr_one"}},
		"status selects two": {flags: []string{"--status", "APPROVED"}, want: []string{"cr_two"}},

		// Title is a case-insensitive substring, not equality.
		"title is a substring":  {flags: []string{"--title", "API"}, want: []string{"cr_one"}},
		"title folds case":      {flags: []string{"--title", "deploy"}, want: []string{"cr_one", "cr_two"}},
		"title matches nothing": {flags: []string{"--title", "absent"}, want: nil},

		// The limit truncates only when it is set; zero means unlimited rather
		// than none, which is the difference between > 0 and >= 0.
		"limit truncates":   {flags: []string{"--limit", "1"}, want: []string{"cr_one"}},
		"limit two keeps":   {flags: []string{"--limit", "2"}, want: []string{"cr_one", "cr_two"}},
		"limit zero is off": {flags: []string{"--limit", "0"}, want: []string{"cr_one", "cr_two"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			root := NewRoot(testRuntime(&output, &fakeService{}))
			root.SetArgs(append([]string{
				"--server", "http://localhost:8081", "--development-subject", "developer",
				"changerequest", "list", "--environment", "dev",
			}, test.flags...))
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			var decoded struct {
				ChangeRequests []struct {
					ID string `json:"id"`
				} `json:"changeRequests"`
			}
			if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
				t.Fatalf("decode %s: %v", output.String(), err)
			}
			var got []string
			for _, request := range decoded.ChangeRequests {
				got = append(got, request.ID)
			}
			if strings.Join(got, ",") != strings.Join(test.want, ",") {
				t.Errorf("got %v, want %v", got, test.want)
			}
		})
	}
}

func TestPlanApprovalCallsSemanticServiceAndEmitsJSON(t *testing.T) {
	t.Parallel()
	service := &fakeService{}
	var output bytes.Buffer
	root := NewRoot(testRuntime(&output, service))
	root.SetArgs([]string{
		"--server", "http://127.0.0.1:8081", "--development-subject", "reviewer",
		"plan", "approve", "pln_one", "--rationale", "reviewed exact digest",
	})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if service.approvedPlan != "pln_one" || service.rationale != "reviewed exact digest" {
		t.Fatalf("approval=%q rationale=%q", service.approvedPlan, service.rationale)
	}
	if !strings.Contains(output.String(), `"id": "pln_one"`) || !strings.Contains(output.String(), `"status": "APPROVED"`) {
		t.Fatalf("output=%s", output.String())
	}
}

func TestTeamApplyAcceptsYAMLAndEmitsYAML(t *testing.T) {
	t.Parallel()
	document := filepath.Join(t.TempDir(), "team.yaml")
	if err := os.WriteFile(document, []byte("displayName: Release reviewers\nmemberGroups: [release-reviewers]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &fakeService{}
	var output bytes.Buffer
	root := NewRoot(testRuntime(&output, service))
	root.SetArgs([]string{
		"--server", "http://localhost:8081", "--development-subject", "developer", "--output", "yaml",
		"team", "apply", "release-reviewers", "--file", document,
	})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if service.teamName != "release-reviewers" || service.team.DisplayName != "Release reviewers" {
		t.Fatalf("team name=%q value=%#v", service.teamName, service.team)
	}
	if !strings.Contains(output.String(), "name: release-reviewers") {
		t.Fatalf("output=%s", output.String())
	}
}

func TestVersionDoesNotRequireHubConfiguration(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	root := NewRoot(Runtime{Stdout: &output, Getenv: func(string) string { return "" }})
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"version": "dev"`) {
		t.Fatalf("output=%s", output.String())
	}
}

func TestRemoteDevelopmentIdentityFailsBeforeServiceConstruction(t *testing.T) {
	t.Parallel()
	called := false
	var output bytes.Buffer
	runtime := testRuntime(&output, &fakeService{})
	runtime.ServiceFactory = func(context.Context, ConnectionConfig) (Service, func(), error) {
		called = true
		return &fakeService{}, func() {}, nil
	}
	root := NewRoot(runtime)
	root.SetArgs([]string{"--server", "http://hub.example.test", "--development-subject", "developer", "plan", "get", "pln_one"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("error=%v", err)
	}
	if called {
		t.Fatal("service was constructed for an invalid identity configuration")
	}
}

func TestCompletionDoesNotRequireHubConfiguration(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	root := NewRoot(Runtime{Stdout: &output, Getenv: func(string) string { return "" }})
	root.SetArgs([]string{"completion", "bash"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "__start_conduit") {
		t.Fatalf("completion output omitted conduit entrypoint")
	}
}

func TestReleaseChannelListUsesStablePageToken(t *testing.T) {
	t.Parallel()
	document := filepath.Join(t.TempDir(), "desired.yaml")
	contents := `apiVersion: platform.delivery/v1alpha1
kind: EnvironmentDeployment
environment:
  id: dev
  generation: 0123456789abcdef
releases: []
entities: []
releaseChannels:
  - name: RELEASE
  - name: BETA
  - name: STABLE
`
	if err := os.WriteFile(document, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	root := NewRoot(testRuntime(&output, &fakeService{}))
	root.SetArgs([]string{"release-channel", "list", "--state-file", document, "--page-size", "1", "--page-token", "BETA"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"name": "RELEASE"`) ||
		!strings.Contains(output.String(), `"nextPageToken": "RELEASE"`) || strings.Contains(output.String(), `"name": "STABLE"`) {
		t.Fatalf("paginated output=%s", output.String())
	}
}

func TestEnvironmentVersionCompareFiltersResolvedReleaseChannel(t *testing.T) {
	t.Parallel()
	service := &fakeService{
		environment: controlplane.EnvironmentCatalogRecord{Environment: controlplane.Environment{ID: "dev", DefaultReleaseChannel: "STABLE"}},
		entities: []controlplane.Entity{
			{Name: "application"},
			{Name: "canary", ReleaseChannel: "RELEASE"},
		},
		versions: []controlplane.TargetHubEntityVersion{
			{InstallationID: "application", Version: "1.2.3"},
			{InstallationID: "canary", Version: "1.3.0"},
		},
	}
	var output bytes.Buffer
	root := NewRoot(testRuntime(&output, service))
	root.SetArgs([]string{
		"--server", "http://127.0.0.1:8081", "--development-subject", "developer",
		"environment", "version", "compare", "dev", "--release-channel", "STABLE",
	})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"installationId": "application"`) || strings.Contains(output.String(), `"installationId": "canary"`) {
		t.Fatalf("filtered versions=%s", output.String())
	}
}

func TestProductReleaseCreatePublishesTypedTerraformManifest(t *testing.T) {
	t.Parallel()
	manifestPath := filepath.Join(t.TempDir(), "manifest.yml")
	manifest := `manifest-version: "1.0"
product-group: com.example
product-name: network
product-version: 1.2.3
product-type: terraform.v1
extensions:
  terraform-repository-url: oci://registry.example/terraform/network:1.2.3@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &fakeService{}
	var output bytes.Buffer
	root := NewRoot(testRuntime(&output, service))
	root.SetArgs([]string{
		"--server", "http://127.0.0.1:8081", "--development-subject", "developer",
		"product-release", "create", "--manifest", manifestPath,
	})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if service.terraform.ProductType != "terraform.v1" || service.terraform.ProductName != "network" {
		t.Fatalf("manifest=%#v", service.terraform)
	}
	if !strings.Contains(output.String(), `"kind": "terraform"`) {
		t.Fatalf("output=%s", output.String())
	}
}

// An asset publishes against --maven-coordinate while citing --manifest-file as
// the manifest it belongs to. The manifest was previously echoed without being
// read, so the two could name different Products.
func TestProductReleaseAssetPublishRejectsAManifestForAnotherCoordinate(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	manifestPath := filepath.Join(directory, "manifest.yml")
	manifest := `manifest-version: "1.0"
product-group: com.example
product-name: network
product-version: 1.2.3
product-type: helm-chart.v1
extensions:
  helm-chart:
    helm-chart-name: network
    helm-chart-version: 1.2.3
    helm-repository-url: oci://registry.example/charts/network
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	assetPath := filepath.Join(directory, "asset.bin")
	if err := os.WriteFile(assetPath, []byte("asset"), 0o600); err != nil {
		t.Fatal(err)
	}

	run := func(coordinate string) error {
		var output bytes.Buffer
		root := NewRoot(testRuntime(&output, &fakeService{}))
		root.SetArgs([]string{
			"--server", "http://127.0.0.1:8081", "--development-subject", "developer",
			"product-release", "asset", "publish", "--asset-path", assetPath,
			"--maven-coordinate", coordinate, "--manifest-file", manifestPath,
		})
		root.SetOut(&output)
		root.SetErr(&output)
		return root.Execute()
	}

	if err := run("com.example:other:9.9.9"); err == nil {
		t.Fatal("a manifest naming com.example:network:1.2.3 was accepted for com.example:other:9.9.9")
	}
	if err := run("com.example:network:1.2.3"); err != nil {
		t.Fatalf("matching coordinate rejected: %v", err)
	}
}

func testRuntime(output *bytes.Buffer, service Service) Runtime {
	return Runtime{
		Stdout: output,
		Stderr: output,
		Getenv: func(string) string { return "" },
		ServiceFactory: func(context.Context, ConnectionConfig) (Service, func(), error) {
			return service, func() {}, nil
		},
	}
}

// Pagination has three cases the stable-token test does not reach: no page
// size at all, a page size exactly equal to the number of channels, and a
// negative one. Each fails differently and none is visible from a request that
// happens to paginate.
func TestReleaseChannelListPaginationBoundaries(t *testing.T) {
	t.Parallel()
	document := filepath.Join(t.TempDir(), "desired.yaml")
	contents := `apiVersion: platform.delivery/v1alpha1
kind: EnvironmentDeployment
environment:
  id: dev
  generation: 0123456789abcdef
releases: []
entities: []
releaseChannels:
  - name: RELEASE
  - name: BETA
  - name: STABLE
`
	if err := os.WriteFile(document, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	list := func(t *testing.T, flags ...string) (names []string, next string, err error) {
		t.Helper()
		var output bytes.Buffer
		root := NewRoot(testRuntime(&output, &fakeService{}))
		root.SetArgs(append([]string{"release-channel", "list", "--state-file", document}, flags...))
		if err := root.Execute(); err != nil {
			return nil, "", err
		}
		var decoded struct {
			ReleaseChannels []struct {
				Name string `json:"name"`
			} `json:"releaseChannels"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
			t.Fatalf("decode %s: %v", output.String(), err)
		}
		for _, channel := range decoded.ReleaseChannels {
			names = append(names, channel.Name)
		}
		return names, decoded.NextPageToken, nil
	}

	t.Run("no page size returns everything, sorted, with no next page", func(t *testing.T) {
		t.Parallel()
		names, next, err := list(t)
		if err != nil {
			t.Fatal(err)
		}
		// Sorted, not the order they appear in the document -- a page token
		// only means anything against a stable order.
		if strings.Join(names, ",") != "BETA,RELEASE,STABLE" {
			t.Errorf("names = %v, want BETA,RELEASE,STABLE", names)
		}
		if next != "" {
			t.Errorf("nextPageToken = %q with no page size", next)
		}
	})

	t.Run("a page size equal to the count has no next page", func(t *testing.T) {
		t.Parallel()
		// The case that separates > from >=: under >= this reports a next page
		// that does not exist, and a caller following it asks for nothing.
		names, next, err := list(t, "--page-size", "3")
		if err != nil {
			t.Fatal(err)
		}
		if len(names) != 3 || next != "" {
			t.Errorf("names = %v, nextPageToken = %q, want all three and no token", names, next)
		}
	})

	t.Run("one short of the count does have a next page", func(t *testing.T) {
		t.Parallel()
		names, next, err := list(t, "--page-size", "2")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(names, ",") != "BETA,RELEASE" || next != "RELEASE" {
			t.Errorf("names = %v, nextPageToken = %q", names, next)
		}
	})

	t.Run("a negative page size is refused", func(t *testing.T) {
		t.Parallel()
		if _, _, err := list(t, "--page-size", "-1"); err == nil {
			t.Error("a negative page size was accepted")
		}
	})
}

// Two commands are built from a shared constructor parameterised by action,
// and each registers a flag only its own action takes. Registering them on the
// wrong action, or on both, is invisible until somebody runs --help: a flag
// that exists but does nothing, and a documented one that is rejected.
func TestActionSpecificFlagsAreRegisteredOnTheRightSubcommandOnly(t *testing.T) {
	t.Parallel()
	root := NewRoot(testRuntime(&bytes.Buffer{}, &fakeService{}))

	find := func(t *testing.T, path ...string) *cobra.Command {
		t.Helper()
		command, _, err := root.Find(path)
		if err != nil {
			t.Fatalf("%v: %v", path, err)
		}
		return command
	}

	for _, test := range []struct {
		path []string
		flag string
		want bool
	}{
		// --reason is a cancellation reason; reopening does not take one, and
		// cancel refuses to run without it.
		{path: []string{"changerequest", "cancel"}, flag: "reason", want: true},
		{path: []string{"changerequest", "reopen"}, flag: "reason", want: false},
		// --latest exports the current Git-owned definition; there is nothing
		// for it to mean on the command that only reads one.
		{path: []string{"release-channel", "export"}, flag: "latest", want: true},
		{path: []string{"release-channel", "get"}, flag: "latest", want: false},
	} {
		name := strings.Join(test.path, " ") + " --" + test.flag
		command := find(t, test.path...)
		if got := command.Flags().Lookup(test.flag) != nil; got != test.want {
			t.Errorf("%s present=%v, want %v", name, got, test.want)
		}
	}
}

// firstNonEmpty picks the first value that is not blank, and returns it as
// given rather than trimmed. Both halves matter: it resolves precedence
// between a flag, an environment variable and a profile field, so inverting
// the test picks the blank one and the setting silently reverts to a default.
func TestFirstNonEmptySkipsBlanksAndPreservesTheValue(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		given []string
		want  string
	}{
		"nothing at all":           {given: nil, want: ""},
		"all blank":                {given: []string{"", "  ", "\t"}, want: ""},
		"first wins":               {given: []string{"first", "second"}, want: "first"},
		"blanks are skipped":       {given: []string{"", "  ", "third"}, want: "third"},
		"whitespace-only is blank": {given: []string{" ", "value"}, want: "value"},
		"the value is not trimmed": {given: []string{" padded "}, want: " padded "},
	} {
		if got := firstNonEmpty(test.given...); got != test.want {
			t.Errorf("%s: firstNonEmpty(%q) = %q, want %q", name, test.given, got, test.want)
		}
	}
}

// The Runtime's writers are the seam that keeps this CLI testable and keeps its
// output redirectable. A supplied writer has to be used: defaulting over one that
// was given sends diagnostics to the real stderr no matter what the caller asked
// for, and no test above notices because they all read stdout. The command is
// built with SilenceErrors, so what it writes proves nothing here -- which writer
// it was handed is the whole of the behaviour.
func TestSuppliedRuntimeWritersAreNotReplacedByDefaults(t *testing.T) {
	t.Parallel()
	var out, diagnostics bytes.Buffer
	runtime := testRuntime(&out, &fakeService{})
	runtime.Stderr = &diagnostics
	root := NewRoot(runtime)
	if root.ErrOrStderr() != io.Writer(&diagnostics) {
		t.Errorf("the supplied Stderr was replaced by %#v", root.ErrOrStderr())
	}
	if root.OutOrStdout() != io.Writer(&out) {
		t.Errorf("the supplied Stdout was replaced by %#v", root.OutOrStdout())
	}

	// And a Runtime that supplies neither still gets the process defaults, which
	// is what makes the guards worth having rather than worth deleting.
	bare := NewRoot(Runtime{ServiceFactory: runtime.ServiceFactory})
	if bare.ErrOrStderr() != io.Writer(os.Stderr) {
		t.Errorf("an unset Stderr defaulted to %#v", bare.ErrOrStderr())
	}
	if bare.OutOrStdout() != io.Writer(os.Stdout) {
		t.Errorf("an unset Stdout defaulted to %#v", bare.OutOrStdout())
	}
}

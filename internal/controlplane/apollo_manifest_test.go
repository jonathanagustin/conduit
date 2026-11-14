package controlplane

import (
	"os"
	"strings"
	"testing"
)

func TestDecodeReconstructedApolloCLIManifest(t *testing.T) {
	file, err := os.Open("../../docs/reference-platform/cli-fixtures/reconstructed/0.814.0/deployment/manifest.yml")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	manifest, errs, err := DecodeApolloProductManifest(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatalf("manifest errors = %#v", errs)
	}
	if manifest.ProductGroup != "com.example.conformance" || manifest.ProductName != "conformance-fixture" ||
		manifest.Extensions.HelmChart == nil || len(manifest.Extensions.Artifacts) != 1 {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestApolloManifestDependenciesIncompatibilitiesAndUnknownExtensions(t *testing.T) {
	input := `
manifest-version: "1.0"
product-group: com.example
product-name: application
product-version: 2.0.0
product-type: helm-chart.v1
extensions:
  helm-chart:
    helm-chart-name: application
    helm-chart-version: 2.0.0
    helm-repository-url: https://charts.example
  product-dependencies:
    - product-group: com.example
      product-name: database
      minimum-version: 1.2.3
      maximum-version: 1.x.x
      recommended-version: 1.5.0
      optional: true
  product-incompatibilities:
    - product-group: com.example
      product-name: legacy-database
      minimum-version: 0.0.0
      maximum-version: 2.x.x
  rollout-strategy: applyChangesNoWait
  resource-readiness-timeout: 1h
  image-pre-pull-config:
    timeout: 3m
`
	manifest, errs, err := DecodeApolloProductManifest(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatalf("manifest errors = %#v", errs)
	}
	if len(manifest.Extensions.ProductDependencies) != 1 || !manifest.Extensions.ProductDependencies[0].Optional ||
		len(manifest.Extensions.ProductIncompatibilities) != 1 || manifest.Extensions.RolloutStrategy != RolloutApplyChangesNoWait ||
		manifest.Extensions.ResourceReadinessTimeout != "1h" || manifest.Extensions.ImagePrePullConfig.Timeout != "3m" {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestApolloManifestRejectsUnknownTopLevelAndInvalidRanges(t *testing.T) {
	unknown := `
manifest-version: "1.0"
product-group: com.example
product-name: app
product-version: 1.0.0
product-type: helm-chart.v1
unknown: true
extensions:
  helm-chart:
    helm-chart-name: app
    helm-chart-version: 1.0.0
    helm-repository-url: https://charts.example
`
	if _, _, err := DecodeApolloProductManifest(strings.NewReader(unknown)); err == nil {
		t.Fatal("unknown top-level key was accepted")
	}

	manifest := ApolloProductManifest{
		ManifestVersion: "1.0", ProductGroup: "com.example", ProductName: "app",
		ProductVersion: "1.0.0", ProductType: "helm-chart.v1",
		Extensions: ApolloProductManifestExtensions{
			HelmChart: &ApolloManifestHelmChart{Name: "app", Version: "1.0.0", RepositoryURL: "https://charts.example"},
			ProductDependencies: []ApolloManifestDependency{{
				ProductGroup: "com.example", ProductName: "database",
				MinimumVersion: "2.0.0", MaximumVersion: "1.x.x", RecommendedVersion: "3.0.0",
			}},
		},
	}
	errs := ValidateApolloProductManifest(manifest)
	if len(errs) != 2 {
		t.Fatalf("range errors = %#v", errs)
	}
}

// artifactFromImmutableOCIReference turns a published manifest's reference into
// the digest-pinned Artifact the whole delivery path is built on, and it parses
// attacker-adjacent input: the reference comes from a Product manifest, and the
// Hub reaches it from both the Apollo and Terraform import paths.
//
// The colon <= slash guard is load-bearing against a panic, not just a
// rejection. A bare "repo@sha256:..." has no colon and no slash, so both
// LastIndex calls return -1; weakening the comparison to < lets it through to
// parts[0][:colon] and slices with a negative bound. Mutation testing moved
// that boundary and no test noticed.
func TestArtifactFromImmutableOCIReferenceRejectsWithoutPanicking(t *testing.T) {
	t.Parallel()
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, test := range []struct {
		name       string
		reference  string
		wantOK     bool
		repository string
		tag        string
	}{
		{
			name: "registry port is not mistaken for a tag", reference: "registry.example:5000/app:1.2.3@" + digest,
			wantOK: true, repository: "registry.example:5000/app", tag: "1.2.3",
		},
		{name: "plain repository and tag", reference: "registry.example/app:1.2.3@" + digest, wantOK: true, repository: "registry.example/app", tag: "1.2.3"},
		{name: "oci scheme is stripped", reference: "oci://registry.example/app:1.2.3@" + digest, wantOK: true, repository: "registry.example/app", tag: "1.2.3"},
		// The panic case: no colon and no slash puts both LastIndex results at -1.
		{name: "bare repository with no tag or slash", reference: "app@" + digest},
		{name: "no tag after the last slash", reference: "registry.example:5000/app@" + digest},
		{name: "empty tag", reference: "registry.example/app:@" + digest},
		{name: "mutable latest tag", reference: "registry.example/app:latest@" + digest},
		{name: "no digest", reference: "registry.example/app:1.2.3"},
		{name: "malformed digest", reference: "registry.example/app:1.2.3@sha256:short"},
		{name: "two digests", reference: "registry.example/app:1.2.3@" + digest + "@" + digest},
		{name: "empty", reference: ""},
		{name: "digest only", reference: "@" + digest},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// A malformed reference must be refused, never panic: it arrives from a
			// published Product manifest.
			artifact, ok := artifactFromImmutableOCIReference(test.reference)
			if ok != test.wantOK {
				t.Fatalf("accepted=%v, want %v (artifact=%#v)", ok, test.wantOK, artifact)
			}
			if !test.wantOK {
				return
			}
			if artifact.Repository != test.repository || artifact.Tag != test.tag || artifact.Digest != digest {
				t.Fatalf("artifact = %#v", artifact)
			}
		})
	}
}

// Both duration ranges in a Product manifest are operational bounds, not
// formatting rules: the readiness timeout is how long the Spoke waits for a
// rollout before failing a Plan, and the pre-pull timeout bounds an image pull
// that runs before any mutation. Their edges were unpinned, so mutation testing
// moved them freely.
func TestApolloManifestDurationRangesAreExactAtTheirEdges(t *testing.T) {
	t.Parallel()
	base := func() ApolloProductManifest {
		return ApolloProductManifest{
			ManifestVersion: "1.0", ProductGroup: "com.example", ProductName: "app",
			ProductVersion: "1.0.0", ProductType: "helm-chart.v1",
			Extensions: ApolloProductManifestExtensions{
				HelmChart: &ApolloManifestHelmChart{Name: "app", Version: "1.0.0", RepositoryURL: "https://charts.example"},
			},
		}
	}
	rejects := func(t *testing.T, manifest ApolloProductManifest, field string) bool {
		t.Helper()
		for _, err := range ValidateApolloProductManifest(manifest) {
			if strings.Contains(err.Path, field) {
				return true
			}
		}
		return false
	}

	// Readiness timeout: positive, up to and including 24h.
	for _, test := range []struct {
		timeout   string
		wantError bool
	}{
		{timeout: "1ns"},
		{timeout: "24h"},
		{timeout: "0s", wantError: true},
		{timeout: "-1s", wantError: true},
		{timeout: "24h1s", wantError: true},
		{timeout: "soon", wantError: true},
	} {
		manifest := base()
		manifest.Extensions.ResourceReadinessTimeout = test.timeout
		if got := rejects(t, manifest, "resource-readiness-timeout"); got != test.wantError {
			t.Errorf("readiness timeout %q rejected=%v, want %v", test.timeout, got, test.wantError)
		}
	}

	// Pre-pull timeout: between 1m and 90m inclusive.
	for _, test := range []struct {
		timeout   string
		wantError bool
	}{
		{timeout: "1m"},
		{timeout: "90m"},
		{timeout: "59s", wantError: true},
		{timeout: "90m1s", wantError: true},
		{timeout: "", wantError: true},
	} {
		manifest := base()
		manifest.Extensions.ImagePrePullConfig = &ApolloImagePrePullConfig{Timeout: test.timeout}
		if got := rejects(t, manifest, "image-pre-pull-config.timeout"); got != test.wantError {
			t.Errorf("pre-pull timeout %q rejected=%v, want %v", test.timeout, got, test.wantError)
		}
	}

	// An absent readiness timeout is not a zero one: the field is optional.
	if rejects(t, base(), "resource-readiness-timeout") {
		t.Error("an omitted readiness timeout was rejected")
	}
}

// The rollout strategy is a closed set, and the two members mean opposite
// things to the Spoke: one waits for the rollout and can roll it back, the
// other applies and returns. An unrecognised value must be refused rather than
// defaulted, or a Product silently gets whichever behaviour zero-value implies.
func TestApolloManifestRolloutStrategyIsAClosedSet(t *testing.T) {
	t.Parallel()
	base := func(strategy string) ApolloProductManifest {
		return ApolloProductManifest{
			ManifestVersion: "1.0", ProductGroup: "com.example", ProductName: "app",
			ProductVersion: "1.0.0", ProductType: "helm-chart.v1",
			Extensions: ApolloProductManifestExtensions{
				HelmChart:       &ApolloManifestHelmChart{Name: "app", Version: "1.0.0", RepositoryURL: "https://charts.example"},
				RolloutStrategy: strategy,
			},
		}
	}
	rejects := func(strategy string) bool {
		for _, err := range ValidateApolloProductManifest(base(strategy)) {
			if strings.Contains(err.Path, "rollout-strategy") {
				return true
			}
		}
		return false
	}
	for _, test := range []struct {
		strategy string
		want     bool
	}{
		{strategy: ""},
		{strategy: RolloutManageRollout},
		{strategy: RolloutApplyChangesNoWait},
		{strategy: "manageRollouts", want: true},
		{strategy: "MANAGEROLLOUT", want: true},
		{strategy: "applyChanges", want: true},
	} {
		if got := rejects(test.strategy); got != test.want {
			t.Errorf("strategy %q rejected=%v, want %v", test.strategy, got, test.want)
		}
	}
}

// The manifest states timeouts as durations; a Release carries them as seconds.
// Nothing asserted the conversion, so a mutant could drop either one and the
// Spoke would fall back to its own default instead of the Product's stated
// bound -- silently, because a missing timeout is indistinguishable from an
// unstated one.
func TestApolloManifestTimeoutsReachTheRelease(t *testing.T) {
	t.Parallel()
	manifest := ApolloProductManifest{
		ManifestVersion: "1.0", ProductGroup: "com.example", ProductName: "app",
		ProductVersion: "1.0.0", ProductType: "helm-chart.v1",
		Extensions: ApolloProductManifestExtensions{
			HelmChart: &ApolloManifestHelmChart{
				Name: "app", Version: "1.0.0",
				RepositoryURL: "oci://registry.example/charts/app:1.0.0@sha256:" + strings.Repeat("a", 64),
			},
			Artifacts: []ApolloManifestArtifact{{
				Type: "oci",
				URI:  "registry.example/app:1.0.0@sha256:" + strings.Repeat("b", 64),
			}},
			ResourceReadinessTimeout: "90m",
			ImagePrePullConfig:       &ApolloImagePrePullConfig{Timeout: "5m"},
		},
	}
	release, err := ExecutableReleaseFromApolloManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if release.ResourceReadinessTimeoutSeconds != 5400 {
		t.Errorf("readiness timeout = %ds, want 5400 (90m)", release.ResourceReadinessTimeoutSeconds)
	}
	if release.ImagePrePullTimeoutSeconds != 300 {
		t.Errorf("pre-pull timeout = %ds, want 300 (5m)", release.ImagePrePullTimeoutSeconds)
	}

	// Unstated stays zero, which is what lets the Spoke tell "use your default"
	// from "the Product asked for this".
	manifest.Extensions.ResourceReadinessTimeout = ""
	manifest.Extensions.ImagePrePullConfig = nil
	release, err = ExecutableReleaseFromApolloManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if release.ResourceReadinessTimeoutSeconds != 0 || release.ImagePrePullTimeoutSeconds != 0 {
		t.Errorf("unstated timeouts became %d/%d, want zero",
			release.ResourceReadinessTimeoutSeconds, release.ImagePrePullTimeoutSeconds)
	}
}

// A Product incompatibility may name no version bounds at all — "this Product is incompatible with
// that one, at every version" — which is why its range is validated with required=false. A
// dependency's range is required; an incompatibility's is not, and the two share one validator. A
// mutation sweep found the distinction unpinned: making `required` always true, on either bound,
// changed no test.
func TestApolloIncompatibilityMayOmitItsVersionBounds(t *testing.T) {
	t.Parallel()
	base := func(incompatibility ApolloManifestIncompatibility) ApolloProductManifest {
		return ApolloProductManifest{
			ManifestVersion: "1.0", ProductGroup: "com.example", ProductName: "app",
			ProductVersion: "1.0.0", ProductType: "helm-chart.v1",
			Extensions: ApolloProductManifestExtensions{
				HelmChart:                &ApolloManifestHelmChart{Name: "app", Version: "1.0.0", RepositoryURL: "https://charts.example"},
				ProductIncompatibilities: []ApolloManifestIncompatibility{incompatibility},
			},
		}
	}
	complains := func(manifest ApolloProductManifest, field string) bool {
		for _, err := range ValidateApolloProductManifest(manifest) {
			if strings.Contains(err.Path, field) {
				return true
			}
		}
		return false
	}

	unbounded := base(ApolloManifestIncompatibility{ProductGroup: "com.example", ProductName: "other"})
	for _, field := range []string{"minimum-version", "maximum-version"} {
		if complains(unbounded, field) {
			t.Errorf("an incompatibility with no %s was rejected; only a dependency's range is "+
				"required", field)
		}
	}

	// Present but malformed is still refused — the bound is optional, not unchecked.
	for _, test := range []struct {
		field           string
		incompatibility ApolloManifestIncompatibility
	}{
		{"minimum-version", ApolloManifestIncompatibility{
			ProductGroup: "com.example", ProductName: "other", MinimumVersion: "not-a-version"}},
		{"maximum-version", ApolloManifestIncompatibility{
			ProductGroup: "com.example", ProductName: "other", MaximumVersion: "not-a-matcher"}},
	} {
		if !complains(base(test.incompatibility), test.field) {
			t.Errorf("a malformed %s was accepted", test.field)
		}
	}
}

// A dependency may recommend exactly its own minimum — the commonest case, "use the oldest version
// that works". The range check reads `compareVersions(recommended, minimum) < 0`, and one boundary
// step turns that into rejecting equality.
func TestApolloDependencyMayRecommendItsMinimum(t *testing.T) {
	t.Parallel()
	manifest := ApolloProductManifest{
		ManifestVersion: "1.0", ProductGroup: "com.example", ProductName: "app",
		ProductVersion: "1.0.0", ProductType: "helm-chart.v1",
		Extensions: ApolloProductManifestExtensions{
			HelmChart: &ApolloManifestHelmChart{Name: "app", Version: "1.0.0", RepositoryURL: "https://charts.example"},
			ProductDependencies: []ApolloManifestDependency{{
				ProductGroup: "com.example", ProductName: "dep",
				MinimumVersion: "1.2.0", MaximumVersion: "2.x.x", RecommendedVersion: "1.2.0",
			}},
		},
	}
	for _, err := range ValidateApolloProductManifest(manifest) {
		if strings.Contains(err.Path, "recommended-version") {
			t.Fatalf("recommending the minimum was rejected: %s", err.Message)
		}
	}
}

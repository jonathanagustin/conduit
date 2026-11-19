package productmanifest

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	validationdomain "github.com/jonathanagustin/conduit/internal/validation/domain"
)

func executableManifestFixture() Manifest {
	return Manifest{ManifestVersion: "1.0", ProductGroup: "com.example", ProductName: "app", ProductVersion: "1.2.3", ProductType: ProductTypeHelmChart, Extensions: Extensions{
		HelmChart:           &HelmChart{Name: "app", Version: "1.2.3", RepositoryURL: "oci://registry.example/charts/app:1.2.3@sha256:" + strings.Repeat("a", 64)},
		Artifacts:           []Artifact{{Type: "oci", URI: "registry.example/app:1.2.3@sha256:" + strings.Repeat("b", 64)}},
		ProductDependencies: []Dependency{{ProductGroup: "com.example", ProductName: "dependency", MinimumVersion: "1.0.0", MaximumVersion: "2.x.x", Optional: true}}, ProductIncompatibilities: []Incompatibility{{ProductGroup: "com.example", ProductName: "incompatible"}}, RolloutStrategy: "manageRollout", ResourceReadinessTimeout: "90m", ImagePrePullConfig: &ImagePrePullConfig{Timeout: "5m"},
	}}
}

func TestExecutableReleaseRetainsNativeCoordinatesDigestsAndConfiguredTimeouts(t *testing.T) {
	manifest := executableManifestFixture()
	got, err := ExecutableRelease(manifest)
	if err != nil {
		t.Fatal(err)
	}
	want := catalog.Release{Product: "com.example:app", Version: "1.2.3", Chart: catalog.Artifact{Repository: "registry.example/charts/app", Tag: "1.2.3", Digest: "sha256:" + strings.Repeat("a", 64)}, Images: []catalog.Artifact{{Repository: "registry.example/app", Tag: "1.2.3", Digest: "sha256:" + strings.Repeat("b", 64)}}, Channels: []string{"DEV", "RELEASE_CANDIDATE", "RELEASE"}, Dependencies: []catalog.Dependency{{Product: "com.example:dependency", MinimumVersion: "1.0.0", MaximumVersion: "2.x.x", Optional: true}}, Incompatibilities: []catalog.Incompatibility{{Product: "com.example:incompatible"}}, RolloutStrategy: "manageRollout", ResourceReadinessTimeoutSeconds: 5400, ImagePrePullTimeoutSeconds: 300}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%+v\nwant=%+v", got, want)
	}
	manifest.Extensions.ResourceReadinessTimeout = ""
	manifest.Extensions.ImagePrePullConfig = nil
	got, err = ExecutableRelease(manifest)
	if err != nil || got.ResourceReadinessTimeoutSeconds != 0 || got.ImagePrePullTimeoutSeconds != 0 {
		t.Fatalf("absent timeout defaulted: %+v %v", got, err)
	}
}

func TestExecutableReleaseRetainsValidationAndImmutableReferenceRefusals(t *testing.T) {
	var validation validationdomain.Problems
	if _, err := ExecutableRelease(Manifest{}); !errors.As(err, &validation) || len(validation) == 0 || validation[0].Path != "manifest-version" {
		t.Fatalf("validation order/type changed: %v", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Manifest)
		want   string
	}{
		{name: "mutable chart", mutate: func(m *Manifest) { m.Extensions.HelmChart.RepositoryURL = "oci://registry.example/charts/app" }, want: "Helm repository URL must be an immutable oci:// reference with an explicit tag and @sha256 digest"},
		{name: "mutable image", mutate: func(m *Manifest) { m.Extensions.Artifacts[0].URI = "registry.example/app:1.2.3" }, want: "extensions.artifacts[0].uri must contain an explicit tag and @sha256 digest"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := executableManifestFixture()
			tc.mutate(&m)
			got, err := ExecutableRelease(m)
			if err == nil || err.Error() != tc.want || !reflect.DeepEqual(got, catalog.Release{}) {
				t.Fatalf("release=%+v err=%v", got, err)
			}
		})
	}
}

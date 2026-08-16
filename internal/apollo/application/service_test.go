package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/jonathanagustin/conduit/internal/apollo"
	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	environments "github.com/jonathanagustin/conduit/internal/environments/domain"
)

type catalogFixture struct{ publications []catalog.Publication }

func (fixture *catalogFixture) ListPublications(context.Context, string) ([]catalog.Publication, error) {
	return append([]catalog.Publication(nil), fixture.publications...), nil
}

func (fixture *catalogFixture) CreatePublication(_ context.Context, publication catalog.Publication) (catalog.Publication, bool, error) {
	fixture.publications = append(fixture.publications, publication)
	return publication, false, nil
}

func (*catalogFixture) CoordinateRetired(context.Context, string, string) (bool, error) {
	return false, nil
}

type reportingFixture struct{}

func (reportingFixture) LatestPlan(context.Context, string) (environments.PlanProjection, error) {
	return environments.PlanProjection{}, nil
}
func (reportingFixture) GetReport(context.Context, string) (environments.Report, error) {
	return environments.Report{}, nil
}
func (reportingFixture) PutReport(_ context.Context, report environments.Report) (environments.Report, error) {
	return report, nil
}

func TestPublicHelmPublicationDigestRetainsThePreExtractionCanonicalShape(t *testing.T) {
	fixture := &catalogFixture{}
	service, err := New(context.Background(), fixture, reportingFixture{})
	if err != nil {
		t.Fatal(err)
	}
	artifacts := []apollo.ArtifactLocator{{ArtifactUrl: "registry.example/fixture:1.2.3"}}
	dependencies := []apollo.ProductDependencyLocator{{
		ProductGroup: "com.example", ProductName: "database", MinimumVersion: "2.0.0", MaximumVersion: "2.x.x",
	}}
	body := apollo.PublishHelmChartRequest{Extensions: apollo.HelmChartProductExtensions{
		HelmChart: apollo.HelmChartReleaseLocator{
			HelmChartName: "fixture", HelmChartVersion: "1.2.3", HelmRepositoryUrl: "oci://registry.example/charts/fixture",
		},
		Artifacts: &artifacts, ProductDependencies: &dependencies,
	}}
	if err := service.PublishHelmChart(context.Background(), "com.example:fixture:1.2.3", body); err != nil {
		t.Fatal(err)
	}
	wire := `{"kind":"helm-chart","payload":{"extensions":{"helmChart":{"helmChartName":"fixture","helmChartVersion":"1.2.3","helmRepositoryUrl":"oci://registry.example/charts/fixture"},"artifacts":[{"artifactUrl":"registry.example/fixture:1.2.3"}],"productDependencies":[{"productGroup":"com.example","productName":"database","minimumVersion":"2.0.0","maximumVersion":"2.x.x"}]}}}`
	if got, want := fixture.publications[0].PublicationDigest, digest(wire); got != want {
		t.Fatalf("publication digest = %s, want canonical pre-extraction %s", got, want)
	}
}

func TestNativeHelmPublicationDigestKeepsPlatformAssetsOutOfThePublicDTO(t *testing.T) {
	fixture := &catalogFixture{}
	service, err := New(context.Background(), fixture, reportingFixture{})
	if err != nil {
		t.Fatal(err)
	}
	asset := &catalog.PublicationAsset{MediaType: "application/yaml", Digest: "sha256:abc", Size: 3}
	publication := NativeHelmPublication{
		ChartName: "fixture", ChartVersion: "1.2.3", ChartRepository: "oci://registry.example/charts/fixture",
		Artifacts: []string{"registry.example/fixture:1.2.3"},
		Dependencies: []NativeDependency{{
			ProductGroup: "com.example", ProductName: "database", MinimumVersion: "2.0.0", MaximumVersion: "2.x.x",
			RecommendedVersion: "2.4.0", Optional: true,
		}},
		Assets: PublicationAssets{DefaultConfig: asset},
	}
	if err := service.PublishNativeHelmChart(context.Background(), "com.example:fixture:1.2.3", publication); err != nil {
		t.Fatal(err)
	}
	wire := `{"kind":"helm-chart","payload":{"extensions":{"helmChart":{"helmChartName":"fixture","helmChartVersion":"1.2.3","helmRepositoryUrl":"oci://registry.example/charts/fixture"},"artifacts":[{"artifactUrl":"registry.example/fixture:1.2.3"}],"productDependencies":[{"productGroup":"com.example","productName":"database","minimumVersion":"2.0.0","maximumVersion":"2.x.x","recommendedVersion":"2.4.0","optional":true}],"platformBundle":{"defaultConfig":{"mediaType":"application/yaml","digest":"sha256:abc","size":3}}}}}`
	if got, want := fixture.publications[0].PublicationDigest, digest(wire); got != want {
		t.Fatalf("native publication digest = %s, want canonical pre-extraction %s", got, want)
	}
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// publishRelease puts a Release in the Apollo catalog the way the public
// publish route does, so the recall tests below start from a Release whose
// recallStatus was stamped false at publication.
func publishRelease(t *testing.T, service *Service, product, version string) {
	t.Helper()
	service.PublishNativeHelmChart(context.Background(), product+":"+version, NativeHelmPublication{
		ChartName: "chart", ChartVersion: version, ChartRepository: "https://charts.example.test",
	})
}

// TestARecalledReleaseIsNotServedAsNotRecalled is the defect.
//
// recallStatus was hardcoded to false at publication time, and a Release is
// published once and recalled later -- so the stamped value was necessarily
// false and stayed false however many recalls arrived afterwards. Conduit held
// the real answer the whole time, in the Environment's desired state.
//
// The direction matters: reporting a withdrawn Release as not recalled is the
// error a client cannot recover from, because it installs on the strength of
// it.
func TestARecalledReleaseIsNotServedAsNotRecalled(t *testing.T) {
	service, err := New(context.Background(), &catalogFixture{}, reportingFixture{})
	if err != nil {
		t.Fatal(err)
	}
	publishRelease(t, service, "app.example:api", "0.1.0")
	publishRelease(t, service, "app.example:api", "0.2.0")

	service.SyncEnvironment(environments.CompatibilityProjection{
		ID: "dev-use2",
		Recalls: []environments.ProjectedRecall{{
			Product: "app.example:api", StartingVersion: "0.1.0", SingleVersion: true,
			Reason: "withdrawn for cause",
		}},
	})

	recalled, err := service.GetRelease("app.example:api", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if !recalled.RecallStatus.Recalled {
		t.Error("a Release under recall was served as not recalled")
	}
	// The reason is the operator's own words and the only thing Conduit holds
	// beyond the flag; dropping it would report the fact without the cause.
	if recalled.RecallStatus.Reason == nil || *recalled.RecallStatus.Reason != "withdrawn for cause" {
		t.Errorf("the recall reason was not carried: %#v", recalled.RecallStatus.Reason)
	}
	// recalledBy and recalledAt stay absent: Conduit's Recall carries neither,
	// and inventing them is what this surface exists to avoid.
	if recalled.RecallStatus.RecalledBy != nil || recalled.RecallStatus.RecalledAt != nil {
		t.Errorf("an actor or timestamp was invented: %#v", recalled.RecallStatus)
	}

	// The fix must stay narrow. A Release the recall does not cover is still
	// not recalled, or the flag would mean nothing.
	untouched, err := service.GetRelease("app.example:api", "0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if untouched.RecallStatus.Recalled {
		t.Error("a Release outside the recall's version range was reported as recalled")
	}
}

// A reverted recall is not a recall. Reporting one would be the same error in
// the other direction -- a usable Release withheld on the strength of a
// withdrawal that was taken back.
func TestARevertedRecallDoesNotMarkAReleaseRecalled(t *testing.T) {
	service, err := New(context.Background(), &catalogFixture{}, reportingFixture{})
	if err != nil {
		t.Fatal(err)
	}
	publishRelease(t, service, "app.example:api", "0.1.0")
	service.SyncEnvironment(environments.CompatibilityProjection{
		ID: "dev-use2",
		Recalls: []environments.ProjectedRecall{{
			Product: "app.example:api", StartingVersion: "0.1.0", SingleVersion: true,
			Reason: "withdrawn then restored", Reverted: true,
		}},
	})

	release, err := service.GetRelease("app.example:api", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if release.RecallStatus.Recalled {
		t.Error("a reverted recall still marked the Release recalled")
	}
}

// Removing a recall from desired state must stop it being reported. The
// projection is the whole of what the Environment declares, so the sync
// replaces rather than accumulates -- otherwise a recall could never be undone
// by editing desired state, only by reverting it.
func TestRemovingARecallFromDesiredStateStopsReportingIt(t *testing.T) {
	service, err := New(context.Background(), &catalogFixture{}, reportingFixture{})
	if err != nil {
		t.Fatal(err)
	}
	publishRelease(t, service, "app.example:api", "0.1.0")
	withRecall := environments.CompatibilityProjection{
		ID: "dev-use2",
		Recalls: []environments.ProjectedRecall{{
			Product: "app.example:api", StartingVersion: "0.1.0", SingleVersion: true, Reason: "withdrawn",
		}},
	}
	service.SyncEnvironment(withRecall)
	if release, _ := service.GetRelease("app.example:api", "0.1.0"); !release.RecallStatus.Recalled {
		t.Fatal("the fixture did not establish a recall to remove")
	}

	service.SyncEnvironment(environments.CompatibilityProjection{ID: "dev-use2"})
	release, err := service.GetRelease("app.example:api", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if release.RecallStatus.Recalled {
		t.Error("a recall removed from desired state was still reported")
	}
}

// ListReleases has to agree with GetRelease. Two read paths that disagree
// about whether a Release is recalled is worse than either answer.
func TestListAndGetAgreeOnRecallStatus(t *testing.T) {
	service, err := New(context.Background(), &catalogFixture{}, reportingFixture{})
	if err != nil {
		t.Fatal(err)
	}
	publishRelease(t, service, "app.example:api", "0.1.0")
	service.SyncEnvironment(environments.CompatibilityProjection{
		ID: "dev-use2",
		Recalls: []environments.ProjectedRecall{{
			Product: "app.example:api", StartingVersion: "0.1.0", SingleVersion: true, Reason: "withdrawn",
		}},
	})

	listed := service.ListReleases("app.example:api")
	if listed.Releases == nil || len(*listed.Releases) != 1 {
		t.Fatalf("expected one Release, got %#v", listed.Releases)
	}
	fromList := (*listed.Releases)[0]
	fromGet, err := service.GetRelease("app.example:api", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if fromList.RecallStatus.Recalled != fromGet.RecallStatus.Recalled {
		t.Errorf("list says recalled=%v, get says recalled=%v",
			fromList.RecallStatus.Recalled, fromGet.RecallStatus.Recalled)
	}
}

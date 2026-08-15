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

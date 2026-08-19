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

// lifecycleFixture stands in for the durable desired state a Hub replica reads
// recalls and promotions FROM. It is deliberately not reachable through
// SyncEnvironment: nothing a request pushes into one process may decide these
// answers, because the next request may be served by the other replica.
type lifecycleFixture struct {
	lifecycle ReleaseLifecycle
	err       error
}

func (fixture *lifecycleFixture) ReleaseLifecycle(context.Context) (ReleaseLifecycle, error) {
	return fixture.lifecycle, fixture.err
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
	service, err := New(context.Background(), fixture, reportingFixture{}, &lifecycleFixture{})
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
	service, err := New(context.Background(), fixture, reportingFixture{}, &lifecycleFixture{})
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

// A compatibility projection is the Environment's complete current desired
// state. Keeping a Namespace omitted by the next projection serves stale data:
// the list endpoint tells clients they can still access a Namespace Conduit has
// removed.
func TestRemovedNamespaceIsNotServedFromAnOlderProjection(t *testing.T) {
	service, err := New(context.Background(), &catalogFixture{}, reportingFixture{}, &lifecycleFixture{})
	if err != nil {
		t.Fatal(err)
	}
	service.SyncEnvironment(environments.CompatibilityProjection{
		ID: "dev-use2",
		Namespaces: []environments.ProjectedNamespace{{
			RID: "ri.namespace.main.namespace.123", Name: "obsolete",
		}},
	})
	before := service.ListNamespaces()
	if before.Namespaces == nil || len(*before.Namespaces) != 1 {
		t.Fatalf("the first projection did not establish the Namespace: %#v", before.Namespaces)
	}

	service.SyncEnvironment(environments.CompatibilityProjection{ID: "dev-use2"})

	after := service.ListNamespaces()
	if after.Namespaces == nil {
		t.Fatal("the Namespace list was not answered")
	}
	if len(*after.Namespaces) != 0 {
		t.Fatalf("a removed Namespace was served from stale projection state: %#v", *after.Namespaces)
	}
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
	durable := &lifecycleFixture{}
	service, err := New(context.Background(), &catalogFixture{}, reportingFixture{}, durable)
	if err != nil {
		t.Fatal(err)
	}
	publishRelease(t, service, "app.example:api", "0.1.0")
	publishRelease(t, service, "app.example:api", "0.2.0")

	durable.lifecycle.Recalls = []environments.ProjectedRecall{{
		Product: "app.example:api", StartingVersion: "0.1.0", SingleVersion: true,
		Reason: "withdrawn for cause",
	}}

	recalled, err := service.GetRelease(context.Background(), "app.example:api", "0.1.0")
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
	untouched, err := service.GetRelease(context.Background(), "app.example:api", "0.2.0")
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
	durable := &lifecycleFixture{lifecycle: ReleaseLifecycle{
		Recalls: []environments.ProjectedRecall{{
			Product: "app.example:api", StartingVersion: "0.1.0", SingleVersion: true,
			Reason: "withdrawn then restored", Reverted: true,
		}},
	}}
	service, err := New(context.Background(), &catalogFixture{}, reportingFixture{}, durable)
	if err != nil {
		t.Fatal(err)
	}
	publishRelease(t, service, "app.example:api", "0.1.0")

	release, err := service.GetRelease(context.Background(), "app.example:api", "0.1.0")
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
	durable := &lifecycleFixture{lifecycle: ReleaseLifecycle{
		Recalls: []environments.ProjectedRecall{{
			Product: "app.example:api", StartingVersion: "0.1.0", SingleVersion: true, Reason: "withdrawn",
		}},
	}}
	service, err := New(context.Background(), &catalogFixture{}, reportingFixture{}, durable)
	if err != nil {
		t.Fatal(err)
	}
	publishRelease(t, service, "app.example:api", "0.1.0")
	if release, _ := service.GetRelease(context.Background(), "app.example:api", "0.1.0"); !release.RecallStatus.Recalled {
		t.Fatal("the fixture did not establish a recall to remove")
	}

	durable.lifecycle.Recalls = nil
	release, err := service.GetRelease(context.Background(), "app.example:api", "0.1.0")
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
	durable := &lifecycleFixture{lifecycle: ReleaseLifecycle{
		Recalls: []environments.ProjectedRecall{{
			Product: "app.example:api", StartingVersion: "0.1.0", SingleVersion: true, Reason: "withdrawn",
		}},
	}}
	service, err := New(context.Background(), &catalogFixture{}, reportingFixture{}, durable)
	if err != nil {
		t.Fatal(err)
	}
	publishRelease(t, service, "app.example:api", "0.1.0")

	listed, err := service.ListReleases(context.Background(), "app.example:api")
	if err != nil {
		t.Fatal(err)
	}
	if listed.Releases == nil || len(*listed.Releases) != 1 {
		t.Fatalf("expected one Release, got %#v", listed.Releases)
	}
	fromList := (*listed.Releases)[0]
	fromGet, err := service.GetRelease(context.Background(), "app.example:api", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if fromList.RecallStatus.Recalled != fromGet.RecallStatus.Recalled {
		t.Errorf("list says recalled=%v, get says recalled=%v",
			fromList.RecallStatus.Recalled, fromGet.RecallStatus.Recalled)
	}
}

// TestAPromotedReleaseIsNotServedOnTheChannelItStartedOn is the same defect as
// the recall one above, one field across.
//
// releaseChannels was stamped at publication with the channels the version
// implies, and a Release is published once and promoted later -- so a Release
// that reached RELEASE was served as still being on CANDIDATE. Conduit held the
// real answer the whole time, in the Environment's desired state, and
// catalog.EffectiveChannels already knew how to compute it.
//
// The direction matters here too, in the opposite way to recalls: under-
// reporting a channel hides a Release from a client selecting on it, so a
// promotion that succeeded looks like one that never happened.
func TestAPromotedReleaseIsNotServedOnTheChannelItStartedOn(t *testing.T) {
	durable := &lifecycleFixture{}
	service, err := New(context.Background(), &catalogFixture{}, reportingFixture{}, durable)
	if err != nil {
		t.Fatal(err)
	}
	// A release candidate, because a stable version already publishes onto
	// RELEASE and the promotion under test would have nowhere to go.
	publishRelease(t, service, "app.example:api", "1.0.0-rc1")
	publishRelease(t, service, "app.example:api", "1.1.0-rc1")

	before, err := service.GetRelease(context.Background(), "app.example:api", "1.0.0-rc1")
	if err != nil {
		t.Fatal(err)
	}
	// Stated rather than assumed: the fixture has to start somewhere other than
	// where the promotion lands, or the assertion below proves nothing.
	if before.ReleaseChannels == nil || containsChannel(*before.ReleaseChannels, "RELEASE") {
		t.Fatalf("the fixture already publishes onto RELEASE: %#v", before.ReleaseChannels)
	}

	durable.lifecycle.Promotions = []environments.ProjectedPromotion{{
		Product: "app.example:api", Version: "1.0.0-rc1",
		FromChannel: "RELEASE_CANDIDATE", ToChannel: "RELEASE", Passed: true,
	}}

	promoted, err := service.GetRelease(context.Background(), "app.example:api", "1.0.0-rc1")
	if err != nil {
		t.Fatal(err)
	}
	if promoted.ReleaseChannels == nil || !containsChannel(*promoted.ReleaseChannels, "RELEASE") {
		t.Errorf("a promoted Release was served on the channel it started on: %#v", promoted.ReleaseChannels)
	}
	// The channel it came from is still true of it. Promotion adds a channel
	// rather than moving between them.
	if !containsChannel(*promoted.ReleaseChannels, "RELEASE_CANDIDATE") {
		t.Errorf("promotion dropped the source channel: %#v", promoted.ReleaseChannels)
	}

	// Narrow, like the recall fix. A Release nobody promoted is unaffected, or
	// the field would say nothing about any particular Release.
	untouched, err := service.GetRelease(context.Background(), "app.example:api", "1.1.0-rc1")
	if err != nil {
		t.Fatal(err)
	}
	if untouched.ReleaseChannels == nil || containsChannel(*untouched.ReleaseChannels, "RELEASE") {
		t.Errorf("an unpromoted Release gained a channel: %#v", untouched.ReleaseChannels)
	}

	// A promotion that did NOT pass must not add its channel. This is the
	// dangerous direction: serving a Release on a channel it was blocked from
	// reaching tells a client selecting on that channel to install something
	// the promotion gate refused.
	durable.lifecycle.Promotions = []environments.ProjectedPromotion{
		{
			Product: "app.example:api", Version: "1.0.0-rc1",
			FromChannel: "RELEASE_CANDIDATE", ToChannel: "RELEASE", Passed: true,
		},
		{
			Product: "app.example:api", Version: "1.1.0-rc1",
			FromChannel: "RELEASE_CANDIDATE", ToChannel: "RELEASE", Passed: false,
		},
	}
	blocked, err := service.GetRelease(context.Background(), "app.example:api", "1.1.0-rc1")
	if err != nil {
		t.Fatal(err)
	}
	if blocked.ReleaseChannels == nil || containsChannel(*blocked.ReleaseChannels, "RELEASE") {
		t.Errorf("a blocked promotion put its Release on the target channel: %#v", blocked.ReleaseChannels)
	}

	// And the list route answers the same as the single-release route, since
	// they are separate call sites and only one of them being fixed is the
	// failure this whole class keeps producing.
	listed, err := service.ListReleases(context.Background(), "app.example:api")
	if err != nil {
		t.Fatal(err)
	}
	if listed.Releases == nil {
		t.Fatal("no releases listed")
	}
	for _, release := range *listed.Releases {
		if release.MavenCoordinate != "app.example:api:1.0.0-rc1" {
			continue
		}
		if release.ReleaseChannels == nil || !containsChannel(*release.ReleaseChannels, "RELEASE") {
			t.Errorf("ListReleases disagreed with GetRelease: %#v", release.ReleaseChannels)
		}
	}
}

func containsChannel(channels []string, wanted string) bool {
	for _, channel := range channels {
		if channel == wanted {
			return true
		}
	}
	return false
}

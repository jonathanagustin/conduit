package application

import (
	"testing"

	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	"github.com/jonathanagustin/conduit/internal/knative/domain"
	"github.com/jonathanagustin/conduit/internal/knative/schema"
)

// TestRevisionFromReleaseIsAFaithfulImmutableSnapshot pins the strongest
// mapping: a Release is Knative's immutable Revision. The image must be
// digest-pinned (Conduit identifies an image by digest, so a tag would be a
// weaker claim than it holds), and the Knative envelope must be present so a
// Knative client recognizes the object.
func TestRevisionFromReleaseIsAFaithfulImmutableSnapshot(t *testing.T) {
	t.Parallel()
	release := catalog.Release{
		Product: "com.example:application",
		Version: "2.0.0",
		Images: []catalog.Artifact{
			{Repository: "registry.example/application", Tag: "2.0.0", Digest: "sha256:" + repeat("a", 64)},
		},
		ResourceReadinessTimeoutSeconds: 600,
	}
	revision := RevisionFromRelease(release)

	if revision.APIVersion != schema.APIVersion || revision.Kind != schema.KindRevision {
		t.Fatalf("not a Knative Revision envelope: %+v", revision)
	}
	if revision.Metadata.Name != "com-example-application-2-0-0" {
		t.Fatalf("name = %q, want the DNS-normalized product+version", revision.Metadata.Name)
	}
	if revision.Metadata.Labels["serving.knative.dev/configuration"] != "com-example-application" {
		t.Fatalf("configuration label = %q", revision.Metadata.Labels["serving.knative.dev/configuration"])
	}
	if len(revision.Spec.Containers) != 1 ||
		revision.Spec.Containers[0].Image != "registry.example/application@sha256:"+repeat("a", 64) {
		t.Fatalf("image must be digest-pinned: %+v", revision.Spec.Containers)
	}
	if revision.Spec.TimeoutSeconds == nil || *revision.Spec.TimeoutSeconds != 600 {
		t.Fatalf("timeoutSeconds = %v, want 600", revision.Spec.TimeoutSeconds)
	}
}

// TestRevisionOmitsWhatConduitDoesNotKnow guards the refusal-by-omission
// discipline: an unset timeout must be absent, not a zero that reads as a chosen
// Knative default.
func TestRevisionOmitsWhatConduitDoesNotKnow(t *testing.T) {
	t.Parallel()
	revision := RevisionFromRelease(catalog.Release{Product: "p", Version: "1.0.0"})
	if revision.Spec.TimeoutSeconds != nil {
		t.Fatalf("an unset timeout was emitted as %v", *revision.Spec.TimeoutSeconds)
	}
	if len(revision.Spec.Containers) != 0 {
		t.Fatalf("a Release with no images emitted a container: %+v", revision.Spec.Containers)
	}
}

// TestConfigurationReportsNewestAsLatestCreated pins the unambiguous half:
// latestCreated is the newest Release by version order, whatever order the
// releases arrive in.
func TestConfigurationReportsNewestAsLatestCreated(t *testing.T) {
	t.Parallel()
	product := "com.example:application"
	releases := []catalog.Release{
		{Product: product, Version: "2.0.0"},
		{Product: product, Version: "1.0.0", Channels: []string{"RELEASE"}},
		{Product: "com.example:other", Version: "9.0.0"}, // different product, ignored
	}
	configuration := ConfigurationFromReleases(product, releases)

	if configuration.Status.LatestCreatedRevisionName != RevisionName(product, "2.0.0") {
		t.Fatalf("latestCreated = %q, want the newest Release", configuration.Status.LatestCreatedRevisionName)
	}
}

// TestConfigurationRefusesToGuessAReadyRevision pins the refusal: "ready" cannot
// be established from the catalog (channel membership is version-implied, not
// promotion), so latestReady stays empty rather than being guessed from the
// newest or from channel eligibility. Emitting it would route a Knative client
// to a Release no consumer was given.
func TestConfigurationRefusesToGuessAReadyRevision(t *testing.T) {
	t.Parallel()
	product := "p"
	configuration := ConfigurationFromReleases(product, []catalog.Release{
		{Product: product, Version: "1.0.0", Channels: []string{"RELEASE"}},
		{Product: product, Version: "2.0.0"},
	})
	if configuration.Status.LatestReadyRevisionName != "" {
		t.Fatalf("latestReady = %q, want empty -- readiness is not catalog-derivable",
			configuration.Status.LatestReadyRevisionName)
	}
	if _, mapped := Mapping().SourceOf(at(schema.KindConfiguration, "status.latestReadyRevisionName")); mapped {
		t.Fatal("latestReadyRevisionName is mapped, but it is refused in this slice")
	}
}

// TestMappingRefusesWhatConduitCannotBack is the analogue of the Apollo
// interface's refusal record: Knative fields with no Conduit source must not be
// in the mapping, so the projection refuses them. Route.percent is the standing
// example -- Conduit has no percentage HTTP traffic.
func TestMappingRefusesWhatConduitCannotBack(t *testing.T) {
	t.Parallel()
	for _, coordinate := range []domain.Coordinate{
		{Kind: schema.KindRoute, Field: "spec.traffic[].percent"},
		{Kind: schema.KindRevision, Field: "spec.containerConcurrency"},
		{Kind: schema.KindService, Field: "spec.template"},
	} {
		if _, mapped := Mapping().SourceOf(coordinate); mapped {
			t.Errorf("%s is mapped, but Conduit has no source for it; it must refuse", coordinate)
		}
		refusal := domain.Refuse(coordinate)
		if refusal == "" {
			t.Errorf("%s has no refusal message", coordinate)
		}
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, n)
	for range n {
		out = append(out, s[0])
	}
	return string(out)
}

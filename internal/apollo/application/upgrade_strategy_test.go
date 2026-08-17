package application

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/apollo"
	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
)

// Release.upgradeStrategy was the constant "shutdown" on every Release Conduit
// ever served. Neither pinned publish request carries the field, so no
// publisher could state it; the value was invented at publication and then
// crossed into GraphQL under ApolloProductReleaseManifest.upgradeStrategy,
// declared @provenance(tier: OBSERVED) and counted as an answered coordinate.
// That is the shape MAPPED-or-REFUSED exists to refuse.
//
// A real source was being discarded at the same time: the Conduit-native bundle
// route receives the Product manifest's upgrade-strategy extension, validated
// against the same rolling|shutdown vocabulary as the pinned enum, and dropped
// it on the floor. So the fix is not "delete the constant" -- it is to serve
// what a publisher declared and say nothing when nobody declared anything.

func nativePublication(strategy string) NativeHelmPublication {
	return NativeHelmPublication{
		ChartName: "fixture", ChartVersion: "1.2.3",
		ChartRepository: "oci://registry.example/charts/fixture", UpgradeStrategy: strategy,
	}
}

func servedRelease(t *testing.T, service *Service, product, version string) map[string]any {
	t.Helper()
	release, err := service.GetRelease(product, version)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestADeclaredUpgradeStrategyIsServedAsDeclared(t *testing.T) {
	t.Parallel()
	service, err := New(context.Background(), &catalogFixture{}, reportingFixture{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.PublishNativeHelmChart(context.Background(),
		"com.example:fixture:1.2.3", nativePublication("rolling")); err != nil {
		t.Fatal(err)
	}
	if got := servedRelease(t, service, "com.example:fixture", "1.2.3")["upgradeStrategy"]; got != "rolling" {
		t.Fatalf("a publisher declared rolling and Conduit served %#v", got)
	}
}

// The falsifier for the whole change. If publish() ever stamps a default again,
// or restorePublication resumes building a pointer to the empty string, this
// goes red -- and the two failures are distinguishable, because one serves
// "shutdown" and the other serves "".
func TestAnUndeclaredUpgradeStrategyIsOmittedRatherThanInvented(t *testing.T) {
	t.Parallel()
	service, err := New(context.Background(), &catalogFixture{}, reportingFixture{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.PublishNativeHelmChart(context.Background(),
		"com.example:fixture:1.2.3", nativePublication("")); err != nil {
		t.Fatal(err)
	}
	if value, present := servedRelease(t, service, "com.example:fixture", "1.2.3")["upgradeStrategy"]; present {
		t.Fatalf("an undeclared strategy was served as %#v", value)
	}
}

// A Publication stored before this change carries the synthesized "shutdown"
// under the old key, and nothing in the record distinguishes it from a genuine
// declaration -- no schema version, no retained wire payload, and the digest
// never covered the strategy. The persisted key moved for exactly that reason:
// the legacy key is not decoded, so those records restore as undeclared. This
// test is what keeps the old constants dead, and it fails the moment anyone
// "helpfully" adds the old spelling back as an alias.
func TestALegacyPublicationsSynthesizedStrategyIsNotServed(t *testing.T) {
	t.Parallel()
	var legacy catalog.Publication
	if err := json.Unmarshal([]byte(`{
		"mavenCoordinate":"com.example:fixture:1.2.3","product":"com.example:fixture",
		"version":"1.2.3","kind":"helm-chart","publicationDigest":"sha256:legacy",
		"releaseChannels":["RELEASE"],"upgradeStrategy":"shutdown"
	}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.UpgradeStrategy != "" {
		t.Fatalf("the legacy key is still decoded, so synthesized values survive: %q", legacy.UpgradeStrategy)
	}
	service, err := New(context.Background(),
		&catalogFixture{publications: []catalog.Publication{legacy}}, reportingFixture{})
	if err != nil {
		t.Fatal(err)
	}
	if value, present := servedRelease(t, service, "com.example:fixture", "1.2.3")["upgradeStrategy"]; present {
		t.Fatalf("a legacy record's synthesized constant was served as %#v", value)
	}
}

// The Terraform publication route stores no strategy and restorePublication
// restores every kind, so a pointer to "" reached the wire as
// `"upgradeStrategy": ""` -- not a member of the pinned enum. This was live
// before the fix and the response-shape gate never saw it, because its fixture
// only seeds a Helm publish.
func TestARestoredPublicationWithNoStrategyServesNoIllegalEnumMember(t *testing.T) {
	t.Parallel()
	service, err := New(context.Background(), &catalogFixture{publications: []catalog.Publication{{
		MavenCoordinate: "com.example:tf:1.0.0", Product: "com.example:tf", Version: "1.0.0",
		Kind: "terraform", PublicationDigest: "sha256:x", ReleaseChannels: []string{"RELEASE"},
	}}}, reportingFixture{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(service.ListReleases("com.example:tf"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"upgradeStrategy"`) {
		t.Fatalf("a Terraform publication served an upgradeStrategy key: %s", encoded)
	}
}

// The strategy has to be inside the digested payload. Left out, republishing a
// coordinate with a different declared strategy would be digest-identical, and
// the immutability check would read the second declaration as an idempotent
// retry and swallow it in silence -- the publisher would be told they had
// succeeded in changing something that did not change.
func TestRedeclaringADifferentUpgradeStrategyConflictsRatherThanBeingSwallowed(t *testing.T) {
	t.Parallel()
	fixture := &conflictingCatalogFixture{}
	service, err := New(context.Background(), fixture, reportingFixture{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := service.PublishNativeHelmChart(ctx, "com.example:fixture:1.2.3", nativePublication("rolling")); err != nil {
		t.Fatal(err)
	}
	if err := service.PublishNativeHelmChart(ctx, "com.example:fixture:1.2.3", nativePublication("rolling")); err != nil {
		t.Fatalf("an identical republication was refused: %v", err)
	}
	if err := service.PublishNativeHelmChart(ctx, "com.example:fixture:1.2.3", nativePublication("shutdown")); !IsKind(err, Conflict) {
		t.Fatalf("redeclaring a different strategy was not a conflict: %v", err)
	}
}

// The apollo application re-validates every other input it receives from the
// controlplane edge rather than trusting it, and this one is no different: a
// value outside the pinned enum must not reach storage, however it arrives.
func TestAnUnknownUpgradeStrategyIsRefusedAtPublication(t *testing.T) {
	t.Parallel()
	service, err := New(context.Background(), &catalogFixture{}, reportingFixture{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.PublishNativeHelmChart(context.Background(),
		"com.example:fixture:1.2.3", nativePublication("blue-green")); !IsKind(err, Invalid) {
		t.Fatalf("an unpinned upgrade strategy was accepted: %v", err)
	}
	if !apollo.UpgradeStrategy("rolling").Valid() || apollo.UpgradeStrategy("").Valid() {
		t.Fatal("the pinned enum no longer agrees with what this test assumes")
	}
}

// conflictingCatalogFixture enforces the immutability rule the real repository
// enforces: one coordinate, one digest. The plain fixture appends
// unconditionally, so it cannot express the conflict above.
type conflictingCatalogFixture struct{ publications []catalog.Publication }

func (fixture *conflictingCatalogFixture) ListPublications(context.Context, string) ([]catalog.Publication, error) {
	return append([]catalog.Publication(nil), fixture.publications...), nil
}

func (fixture *conflictingCatalogFixture) CreatePublication(
	_ context.Context, publication catalog.Publication,
) (catalog.Publication, bool, error) {
	for _, stored := range fixture.publications {
		if stored.MavenCoordinate != publication.MavenCoordinate {
			continue
		}
		if stored.PublicationDigest != publication.PublicationDigest {
			return catalog.Publication{}, true, nil
		}
		return stored, false, nil
	}
	fixture.publications = append(fixture.publications, publication)
	return publication, false, nil
}

func (*conflictingCatalogFixture) CoordinateRetired(context.Context, string, string) (bool, error) {
	return false, nil
}

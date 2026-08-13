package catalog

import "testing"

func release(product, version string, channels ...string) Release {
	return Release{Product: product, Version: version, Channels: channels}
}

// A recall covers from its starting version upward, and nothing below it.
func TestRecallCoversFromItsStartingVersionUpward(t *testing.T) {
	t.Parallel()
	recall := Recall{Product: "p", StartingVersion: "2.0.0"}
	for _, testCase := range []struct {
		version string
		covered bool
	}{
		{"1.9.9", false},
		{"2.0.0", true},
		{"2.0.1", true},
		{"3.0.0", true},
	} {
		if got := RecallAppliesToVersion(recall, testCase.version); got != testCase.covered {
			t.Errorf("%s covered = %v, want %v", testCase.version, got, testCase.covered)
		}
	}
}

func TestRecallBoundsAndExclusions(t *testing.T) {
	t.Parallel()
	bounded := Recall{Product: "p", StartingVersion: "2.0.0", EndingVersionExclusive: "3.0.0"}
	if RecallAppliesToVersion(bounded, "3.0.0") {
		t.Error("the ending version is exclusive and must not be covered")
	}
	if !RecallAppliesToVersion(bounded, "2.9.9") {
		t.Error("a version below the exclusive end must be covered")
	}

	single := Recall{Product: "p", StartingVersion: "2.0.0", SingleVersion: true}
	if RecallAppliesToVersion(single, "2.0.1") {
		t.Error("a single-version recall must cover only its own version")
	}

	excluded := Recall{Product: "p", StartingVersion: "2.0.0", ExcludedVersions: []string{"2.1.0"}}
	if RecallAppliesToVersion(excluded, "2.1.0") {
		t.Error("an explicitly excluded version must not be covered")
	}
}

// A reverted recall is a recall that was withdrawn. Treating it as live would
// keep a Release out of selection after somebody decided it should come back.
func TestRevertedRecallDoesNotApply(t *testing.T) {
	t.Parallel()
	recalls := []Recall{{Product: "p", StartingVersion: "2.0.0", Reverted: true}}
	if ReleaseRecalled(release("p", "2.0.0"), recalls, "dev") {
		t.Fatal("a reverted recall must not suppress a Release")
	}
	if got := RecallsForVersion(recalls, "p", "2.0.0"); len(got) != 0 {
		t.Fatalf("a reverted recall must not be listed: %#v", got)
	}
}

func TestRecallsAreScopedToTheirProduct(t *testing.T) {
	t.Parallel()
	recalls := []Recall{{Product: "other", StartingVersion: "1.0.0"}}
	if ReleaseRecalled(release("p", "2.0.0"), recalls, "dev") {
		t.Fatal("a recall on one Product must not suppress another's Release")
	}
}

// The floor is a separate question from coverage: a Release can sit outside
// every recalled range and still be below the version a recall insisted on.
func TestRecallFloorRejectsVersionsBelowIt(t *testing.T) {
	t.Parallel()
	rollForward := []Recall{{Product: "p", StartingVersion: "2.0.0", Strategy: RecallStrategy{Type: RecallRollForward, MinimumVersion: "2.5.0"}}}
	if ReleaseMeetsRecallFloor(release("p", "2.4.0"), rollForward, "dev") {
		t.Error("2.4.0 is below the 2.5.0 floor and must not meet it")
	}
	if !ReleaseMeetsRecallFloor(release("p", "2.5.0"), rollForward, "dev") {
		t.Error("2.5.0 is exactly the floor and must meet it")
	}
}

// A SELECTED_ENVIRONMENTS recall establishes no floor at all, in any
// environment. This pins current behaviour rather than intended behaviour.
//
// KNOWN GAP, recorded rather than fixed. The branch reads as though it scopes a
// floor to the named environments, but it never assigns one: outside the list
// it skips the recall, and inside the list it falls through with floor still
// empty, so both paths reach the same answer. Strategy.MinimumVersion is
// therefore ignored for this strategy type.
//
// It may be deliberate — selectReleaseForObservedEntity handles
// SELECTED_ENVIRONMENTS by freezing environments outside the list, so the floor
// may simply be somebody else's job — but nothing here says so, and changing it
// would change which Releases are selected. That is a behaviour decision, not a
// refactor, so it is left alone and written down.
func TestSelectedEnvironmentsRecallEstablishesNoFloor(t *testing.T) {
	t.Parallel()
	recalls := []Recall{{
		Product:         "p",
		StartingVersion: "2.0.0",
		Strategy:        RecallStrategy{Type: RecallSelectedEnvironments, MinimumVersion: "2.5.0", Environments: []string{"prod"}},
	}}
	for _, environment := range []string{"prod", "dev"} {
		if !ReleaseMeetsRecallFloor(release("p", "2.4.0"), recalls, environment) {
			t.Errorf("%s: MinimumVersion is not applied for this strategy type, so 2.4.0 must pass", environment)
		}
	}
}

// Promotions arrive in no particular order, so resolving them must not depend
// on that order. DEV->RC listed after RC->RELEASE still has to reach RELEASE.
func TestChannelPromotionsResolveRegardlessOfOrder(t *testing.T) {
	t.Parallel()
	forward := []Promotion{
		{Product: "p", Version: "1.0.0", FromChannel: "DEV", ToChannel: "RELEASE_CANDIDATE", Passed: true},
		{Product: "p", Version: "1.0.0", FromChannel: "RELEASE_CANDIDATE", ToChannel: "RELEASE", Passed: true},
	}
	reversed := []Promotion{forward[1], forward[0]}
	for name, promotions := range map[string][]Promotion{"in order": forward, "reversed": reversed} {
		got := EffectiveChannels(release("p", "1.0.0", "DEV"), promotions)
		if !contains(got, "RELEASE") {
			t.Errorf("%s: RELEASE not reached: %v", name, got)
		}
	}
}

func TestFailedPromotionDoesNotAdvanceAChannel(t *testing.T) {
	t.Parallel()
	promotions := []Promotion{{Product: "p", Version: "1.0.0", FromChannel: "DEV", ToChannel: "RELEASE", Passed: false}}
	if ReleaseOnChannel(release("p", "1.0.0", "DEV"), promotions, "RELEASE") {
		t.Fatal("a promotion that did not pass must not advance the Release")
	}
}

func TestPromotionIsMatchedToItsOwnReleaseOnly(t *testing.T) {
	t.Parallel()
	promotions := []Promotion{{Product: "p", Version: "9.9.9", FromChannel: "DEV", ToChannel: "RELEASE", Passed: true}}
	if ReleaseOnChannel(release("p", "1.0.0", "DEV"), promotions, "RELEASE") {
		t.Fatal("another version's promotion must not advance this Release")
	}
}

// A Release declaring no Channels falls back to what its version implies.
func TestUndeclaredChannelsFallBackToTheVersion(t *testing.T) {
	t.Parallel()
	if got := EffectiveChannels(release("p", "1.0.0"), nil); !contains(got, "RELEASE") {
		t.Fatalf("a release version implies RELEASE, got %v", got)
	}
	if got := EffectiveChannels(release("p", "1.0.0-rc1"), nil); contains(got, "RELEASE") {
		t.Fatalf("a release candidate must not imply RELEASE, got %v", got)
	}
}

func TestHasReleaseOnChannelIsScopedToItsProduct(t *testing.T) {
	t.Parallel()
	releases := []Release{release("other", "1.0.0", "RELEASE")}
	if HasReleaseOnChannel(releases, nil, "p", "RELEASE") {
		t.Fatal("another Product's Release must not answer for this one")
	}
}

// Coverage is checked rather than assumed: a scan that examined one of two
// images and passed is not evidence that the Release passed.
func TestPartialCoverageIsNotCoverage(t *testing.T) {
	t.Parallel()
	images := []Artifact{
		{Repository: "reg/a", Tag: "1", Digest: "sha256:aa"},
		{Repository: "reg/b", Tag: "1", Digest: "sha256:bb"},
	}
	if AssessmentCoversArtifacts([]string{"sha256:aa"}, images) {
		t.Fatal("a scan covering one of two images must not read as full coverage")
	}
	if !AssessmentCoversArtifacts([]string{"sha256:aa", "sha256:bb"}, images) {
		t.Fatal("a scan covering both images must read as full coverage")
	}
}

// The gap is reported as repository:tag because that is what a human reads,
// and deduplicated because one missing image referenced twice is one gap.
func TestUncoveredArtifactsNameTheGapOnce(t *testing.T) {
	t.Parallel()
	images := []Artifact{
		{Repository: "reg/b", Tag: "1", Digest: "sha256:bb"},
		{Repository: "reg/b", Tag: "1", Digest: "sha256:bb"},
	}
	got := UncoveredArtifacts([]string{"sha256:aa"}, images)
	if len(got) != 1 || got[0] != "reg/b:1" {
		t.Fatalf("want exactly [reg/b:1], got %v", got)
	}
}

package controlplane

import (
	"context"
	"strings"
	"testing"
	"time"
)

func channelFoundation(versions ...string) DesiredState {
	state := DesiredState{
		APIVersion:  "platform.delivery/v1alpha1",
		Kind:        "EnvironmentDeployment",
		Environment: Environment{ID: "dev-use2", Generation: "channel-generation"},
		Entities: []Entity{{
			Name: "example-api", Product: "app.example:example-api",
			ReleaseChannel: "RELEASE", Namespace: "example",
		}},
	}
	for _, version := range versions {
		state.Releases = append(state.Releases, Release{
			Product: "app.example:example-api", Version: version,
			Chart:  Artifact{Repository: "registry.example/example-api", Tag: version, Digest: digest},
			Images: []Artifact{{Repository: "registry.example/example-api", Tag: version, Digest: digest}},
		})
	}
	return state
}

func observedVersion(version string) EnvironmentReport {
	return EnvironmentReport{Environment: "dev-use2", Entities: []ReportedEntity{{
		InstallationID: "example-api", Product: "app.example:example-api", Version: version,
	}}}
}

func TestObservedRecallRollsForwardWaitsOrDowngrades(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	state := channelFoundation("0.1.0", "0.2.0", "0.3.0")
	state.Recalls = []Recall{{
		ID: "recall-api", Product: "app.example:example-api", StartingVersion: "0.2.0", EndingVersionExclusive: "0.3.0",
		Strategy: RecallStrategy{Type: RecallRollForward}, Reason: "regression",
	}}
	plan, err := BuildPlanWithObserved(state, observedVersion("0.2.0"), now)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Steps[0].Version != "0.3.0" || !plan.Steps[0].RecallRollOff || plan.Steps[0].RampedRollout != nil {
		t.Fatalf("roll-forward Plan = %#v", plan.Steps[0])
	}

	state.Recalls[0].EndingVersionExclusive = ""
	plan, err = BuildPlanWithObserved(state, observedVersion("0.2.0"), now)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Steps[0].Version != "0.2.0" || plan.Steps[0].RecallRollOff {
		t.Fatalf("wait-for-fix Plan = %#v", plan.Steps[0])
	}

	state = channelFoundation("0.1.0", "0.2.0")
	state.Recalls = []Recall{{
		ID: "recall-api", Product: "app.example:example-api", StartingVersion: "0.2.0", SingleVersion: true,
		Strategy: RecallStrategy{Type: RecallAllowDowngrade, MinimumVersion: "0.1.0"}, Reason: "urgent regression",
	}}
	plan, err = BuildPlanWithObserved(state, observedVersion("0.2.0"), now)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Steps[0].Version != "0.1.0" || !plan.Steps[0].RecallRollOff {
		t.Fatalf("bounded downgrade Plan = %#v", plan.Steps[0])
	}
}

func TestObservedRecallHonorsTimedAndSelectedEnvironmentFreezes(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	freezeUntil := now.Add(time.Hour)
	state := channelFoundation("0.2.0", "0.3.0")
	state.Recalls = []Recall{{
		ID: "recall-api", Product: "app.example:example-api", StartingVersion: "0.2.0", EndingVersionExclusive: "0.3.0",
		Strategy: RecallStrategy{Type: RecallFreeze, FreezeUntil: &freezeUntil}, Reason: "investigating",
	}}
	plan, err := BuildPlanWithObserved(state, observedVersion("0.2.0"), now)
	if err != nil || plan.Steps[0].Version != "0.2.0" || plan.Steps[0].RecallRollOff {
		t.Fatalf("active freeze Plan = %#v, %v", plan, err)
	}
	plan, err = BuildPlanWithObserved(state, observedVersion("0.2.0"), freezeUntil)
	if err != nil || plan.Steps[0].Version != "0.3.0" || !plan.Steps[0].RecallRollOff {
		t.Fatalf("expired freeze Plan = %#v, %v", plan, err)
	}

	state.Recalls[0].Strategy = RecallStrategy{Type: RecallSelectedEnvironments, Environments: []string{"canary-use2"}}
	plan, err = BuildPlanWithObserved(state, observedVersion("0.2.0"), now)
	if err != nil || plan.Steps[0].Version != "0.2.0" {
		t.Fatalf("non-selected Environment Plan = %#v, %v", plan, err)
	}
	state.Environment.ID = "canary-use2"
	report := observedVersion("0.2.0")
	report.Environment = "canary-use2"
	plan, err = BuildPlanWithObserved(state, report, now)
	if err != nil || plan.Steps[0].Version != "0.3.0" || !plan.Steps[0].RecallRollOff {
		t.Fatalf("selected Environment Plan = %#v, %v", plan, err)
	}
}

func TestAutomaticSecurityRecallIsSingleReleaseAndObservedStateAware(t *testing.T) {
	t.Parallel()
	state := channelFoundation("0.1.0", "0.2.0")
	state.Environment.AutomaticSecurityRecall = true
	state.Releases[0].VulnerabilityAssessment = &VulnerabilityAssessment{
		Provider: "scanner", RunID: "scan-bad", Status: "COMPLETE", Outcome: "FAIL",
		ArtifactDigests: []string{digest}, ActiveVulnerabilities: 1,
	}
	state.Releases[1].VulnerabilityAssessment = &VulnerabilityAssessment{
		Provider: "scanner", RunID: "scan-good", Status: "COMPLETE", Outcome: "PASS",
		ArtifactDigests: []string{digest}, ActiveVulnerabilities: 0,
	}
	prepared := withAutomaticSecurityRecalls(state)
	if len(prepared.Recalls) != 1 || !prepared.Recalls[0].SingleVersion || prepared.Recalls[0].Source != "SECURITY" ||
		prepared.Recalls[0].AssessmentRunID != "scan-bad" || recallAppliesToVersion(prepared.Recalls[0], "0.2.0") {
		t.Fatalf("derived security recall = %#v", prepared.Recalls)
	}
	plan, err := BuildPlanWithObserved(state, observedVersion("0.1.0"), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Steps[0].Version != "0.2.0" || !plan.Steps[0].RecallRollOff {
		t.Fatalf("security roll-off Plan = %#v", plan.Steps[0])
	}
}

func TestLifecyclePlanCreationUsesPersistedObservedVersionForRecall(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	state := channelFoundation("0.1.0", "0.2.0")
	state.Recalls = []Recall{{
		ID: "recall-api", Product: "app.example:example-api", StartingVersion: "0.2.0", SingleVersion: true,
		Strategy: RecallStrategy{Type: RecallAllowDowngrade, MinimumVersion: "0.1.0"}, Reason: "urgent regression",
	}}
	if _, err := repository.PutEnvironmentReport(ctx, observedVersion("0.2.0")); err != nil {
		t.Fatal(err)
	}
	record, err := NewLifecycle(repository).CreatePlan(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	if record.Plan.Steps[0].Version != "0.1.0" || !record.Plan.Steps[0].RecallRollOff ||
		len(record.Plan.Steps[0].RecallIDs) != 1 || record.Plan.Steps[0].RecallIDs[0] != "recall-api" {
		t.Fatalf("lifecycle recall Plan = %#v", record.Plan.Steps[0])
	}
}

func TestReleaseChannelSelectsLatestEligibleRelease(t *testing.T) {
	t.Parallel()
	state := channelFoundation("0.1.0", "0.3.0", "0.2.0")
	plan, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Version != "0.3.0" || plan.Steps[0].ReleaseChannel != "RELEASE" {
		t.Fatalf("channel plan = %#v", plan)
	}
}

func TestOpenRecallWaitsForRollForwardRelease(t *testing.T) {
	t.Parallel()
	state := channelFoundation("0.1.0", "0.2.0")
	state.Recalls = []Recall{{
		ID: "recall-api", Product: "app.example:example-api", StartingVersion: "0.2.0",
		Strategy: RecallStrategy{Type: RecallRollForward}, Reason: "regression",
	}}
	if errs := Validate(state); len(errs) != 0 {
		t.Fatalf("valid but constrained state rejected: %#v", errs)
	}
	if _, err := BuildPlan(state); err == nil || !strings.Contains(err.Error(), "no eligible Release") {
		t.Fatalf("roll-forward constraint error = %v", err)
	}
	state.Releases = append(state.Releases, Release{
		Product: "app.example:example-api", Version: "0.3.0",
		Chart:  Artifact{Repository: "registry.example/example-api", Tag: "0.3.0", Digest: digest},
		Images: []Artifact{{Repository: "registry.example/example-api", Tag: "0.3.0", Digest: digest}},
	})
	// Open-ended recalls include future Releases. Close the range at the first
	// fixed Release before it becomes eligible for roll-forward.
	state.Recalls[0].EndingVersionExclusive = "0.3.0"
	plan, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Steps[0].Version != "0.3.0" {
		t.Fatalf("roll-forward selected %s", plan.Steps[0].Version)
	}
}

func TestRecallExclusionAndBoundedDowngrade(t *testing.T) {
	t.Parallel()
	state := channelFoundation("0.1.0", "0.2.0")
	state.Recalls = []Recall{{
		ID: "recall-api", Product: "app.example:example-api", StartingVersion: "0.2.0",
		ExcludedVersions: []string{"0.2.0"}, Strategy: RecallStrategy{Type: RecallRollForward}, Reason: "regression",
	}}
	plan, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Steps[0].Version != "0.2.0" {
		t.Fatalf("excluded fixed Release selected %s", plan.Steps[0].Version)
	}

	state.Recalls[0].ExcludedVersions = nil
	state.Recalls[0].Strategy = RecallStrategy{Type: RecallAllowDowngrade, MinimumVersion: "0.1.0"}
	plan, err = BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Steps[0].Version != "0.1.0" {
		t.Fatalf("bounded downgrade selected %s", plan.Steps[0].Version)
	}
}

func TestDefaultChannelsFollowVersionType(t *testing.T) {
	t.Parallel()
	stable := Release{Version: "1.0.0"}
	candidate := Release{Version: "1.0.0-rc1"}
	snapshot := Release{Version: "1.0.0-1-gaaaaaaa"}
	if !releaseOnChannel(stable, nil, "RELEASE") || !releaseOnChannel(stable, nil, "RELEASE_CANDIDATE") {
		t.Fatalf("stable channels = %#v", effectiveReleaseChannels(stable, nil))
	}
	if releaseOnChannel(candidate, nil, "RELEASE") || !releaseOnChannel(candidate, nil, "RELEASE_CANDIDATE") {
		t.Fatalf("candidate channels = %#v", effectiveReleaseChannels(candidate, nil))
	}
	if releaseOnChannel(snapshot, nil, "RELEASE_CANDIDATE") || !releaseOnChannel(snapshot, nil, "DEV") {
		t.Fatalf("snapshot channels = %#v", effectiveReleaseChannels(snapshot, nil))
	}
}

func TestMalformedChannelCatalogReturnsValidationErrorsWithoutPanicking(t *testing.T) {
	t.Parallel()
	state := channelFoundation("not-a-version")
	errs := Validate(state)
	if len(errs) == 0 {
		t.Fatal("malformed catalog unexpectedly validated")
	}
}

// "Entity X has no eligible Release for Product Y on channel Z" is true and
// useless. Resolution has just examined every Release and knows exactly why it
// rejected each one -- including which artifact an assessment failed to cover,
// which nothing else in the system will tell an operator -- and it threw all of
// that away before returning.
func TestNoEligibleReleaseSaysWhyEachCandidateWasRejected(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)

	resolve := func(t *testing.T, mutate func(*DesiredState)) string {
		t.Helper()
		state := assessedFoundation()
		// Without a Channel the Entities are skipped outright and nothing is
		// selected for them at all, so no Release is ever judged.
		state.Environment.DefaultReleaseChannel = "RELEASE"
		mutate(&state)
		_, err := resolveDesiredStateWithObserved(state, EnvironmentReport{}, now)
		if err == nil {
			t.Fatal("resolution succeeded; the fixture no longer withholds a Release")
		}
		return err.Error()
	}

	t.Run("names the artifact an availability assessment did not cover", func(t *testing.T) {
		// The whole-Release Outcome cannot carry this. An assessment that
		// simply omits an artifact withholds the Release exactly as firmly as
		// one reporting it missing, and says nothing about which.
		message := resolve(t, func(state *DesiredState) {
			state.Environment.ArtifactAvailabilityPolicy = &ArtifactAvailabilityPolicy{RequiredOutcome: "AVAILABLE"}
			for index := range state.Releases {
				state.Releases[index].ArtifactAvailability = &ArtifactAvailabilityAssessment{
					Provider: "test", RunID: "run-1", Status: "COMPLETE", Outcome: "AVAILABLE",
					ArtifactDigests: []string{}, // covers nothing
				}
			}
		})
		if !strings.Contains(message, "does not cover every artifact") {
			t.Errorf("message = %q", message)
		}
		// Resolution fails on the first Entity it cannot satisfy, which is
		// example-workspace, so that is whose artifact must be named.
		if !strings.Contains(message, "registry.example/example-workspace:0.1.0") {
			t.Errorf("message = %q, want the uncovered artifact named", message)
		}
	})

	t.Run("distinguishes a missing assessment from an incomplete one", func(t *testing.T) {
		absent := resolve(t, func(state *DesiredState) {
			state.Environment.ArtifactAvailabilityPolicy = &ArtifactAvailabilityPolicy{RequiredOutcome: "AVAILABLE"}
		})
		if !strings.Contains(absent, "carries none") {
			t.Errorf("absent = %q", absent)
		}
		running := resolve(t, func(state *DesiredState) {
			state.Environment.ArtifactAvailabilityPolicy = &ArtifactAvailabilityPolicy{RequiredOutcome: "AVAILABLE"}
			for index := range state.Releases {
				state.Releases[index].ArtifactAvailability = &ArtifactAvailabilityAssessment{
					Provider: "test", RunID: "run-1", Status: "RUNNING", Outcome: "AVAILABLE",
				}
			}
		})
		if !strings.Contains(running, "is RUNNING, not COMPLETE") {
			t.Errorf("running = %q", running)
		}
	})

	t.Run("says a vulnerability outcome failed rather than that nothing was eligible", func(t *testing.T) {
		message := resolve(t, func(state *DesiredState) {
			for index := range state.Releases {
				state.Releases[index].VulnerabilityAssessment.Outcome = "FAIL"
			}
		})
		if !strings.Contains(message, "requires a PASS vulnerability outcome") || !strings.Contains(message, "FAIL") {
			t.Errorf("message = %q", message)
		}
	})

	t.Run("reports every policy reason, not the first", func(t *testing.T) {
		// An operator who fixes one and has to run a whole plan resolution to
		// discover the next is how a single misconfigured Release costs an
		// afternoon.
		message := resolve(t, func(state *DesiredState) {
			state.Environment.ArtifactAvailabilityPolicy = &ArtifactAvailabilityPolicy{RequiredOutcome: "AVAILABLE"}
			for index := range state.Releases {
				state.Releases[index].VulnerabilityAssessment.Outcome = "FAIL"
			}
		})
		if !strings.Contains(message, "PASS vulnerability outcome") {
			t.Errorf("message = %q, missing the security reason", message)
		}
		if !strings.Contains(message, "availability assessment") {
			t.Errorf("message = %q, missing the availability reason", message)
		}
	})

	t.Run("names the versions that share a reason together", func(t *testing.T) {
		// A Product with several Releases and one missing scanner would
		// otherwise repeat the same sentence once per Release.
		message := resolve(t, func(state *DesiredState) {
			older := cloneJSON(state.Releases[1]) // example-workspace, the Entity that fails first
			older.Version = "0.0.9"
			state.Releases = append(state.Releases, older)
			for index := range state.Releases {
				state.Releases[index].VulnerabilityAssessment.Outcome = "FAIL"
			}
		})
		if strings.Count(message, "requires a PASS vulnerability outcome") != 1 {
			t.Errorf("message = %q, want the shared reason stated once", message)
		}
		if !strings.Contains(message, "0.1.0, 0.0.9") && !strings.Contains(message, "0.0.9, 0.1.0") {
			t.Errorf("message = %q, want both versions named", message)
		}
	})

	t.Run("names an image a vulnerability assessment did not cover", func(t *testing.T) {
		// The security policy has its own coverage rule, over images rather
		// than every artifact, and it withholds the Release just as silently.
		message := resolve(t, func(state *DesiredState) {
			for index := range state.Releases {
				state.Releases[index].VulnerabilityAssessment.ArtifactDigests = nil
			}
		})
		if !strings.Contains(message, "does not cover every image") {
			t.Errorf("message = %q", message)
		}
		if !strings.Contains(message, "registry.example/example-workspace:0.1.0") {
			t.Errorf("message = %q, want the uncovered image named", message)
		}
	})

	t.Run("adds nothing when no Release was judged at all", func(t *testing.T) {
		// Nothing on the Channel means no Release ever reached a policy, so
		// there is no reason to give -- and the error must not trail a
		// separator introducing an empty list.
		message := resolve(t, func(state *DesiredState) {
			state.Environment.DefaultReleaseChannel = "NO_SUCH_CHANNEL"
		})
		if strings.Contains(message, ";") {
			t.Errorf("message = %q, want no reasons appended", message)
		}
	})

	t.Run("stays silent when a Release is eligible", func(t *testing.T) {
		state := assessedFoundation()
		state.Environment.DefaultReleaseChannel = "RELEASE"
		if _, err := resolveDesiredStateWithObserved(state, EnvironmentReport{}, now); err != nil {
			t.Fatalf("the unmodified fixture no longer resolves: %v", err)
		}
	})
}

// The two policies scope their coverage differently, and deliberately: a
// vulnerability assessment reports on images, because a Helm chart is not
// scanned for CVEs, while an availability assessment reports on every artifact
// because any of them can be missing from a registry. Collapsing the two would
// either withhold every Release for an unscannable chart or stop noticing a
// chart that is not there.
func TestPolicyCoverageIsScopedToImagesForSecurityAndAllArtifactsForAvailability(t *testing.T) {
	t.Parallel()
	const chartDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	environment := Environment{
		ID:                         "dev-use2",
		ArtifactSecurityPolicy:     &ArtifactSecurityPolicy{RequiredOutcome: "PASS"},
		ArtifactAvailabilityPolicy: &ArtifactAvailabilityPolicy{RequiredOutcome: "AVAILABLE"},
	}
	release := assessedFoundation().Releases[0]
	release.Chart.Digest = chartDigest
	// Both assessments report on the images and neither on the chart.
	release.VulnerabilityAssessment.ArtifactDigests = []string{release.Images[0].Digest}
	release.ArtifactAvailability = &ArtifactAvailabilityAssessment{
		Provider: "test", RunID: "run-1", Status: "COMPLETE", Outcome: "AVAILABLE",
		ArtifactDigests: []string{release.Images[0].Digest},
	}

	kinds := make([]string, 0)
	for _, reason := range environmentPolicyIneligibility(release, environment) {
		kinds = append(kinds, reason.Kind)
		if reason.Kind == IneligibleAvailabilityCoverageIncomplete &&
			(len(reason.Subjects) != 1 || reason.Subjects[0] != release.Chart.Repository+":"+release.Chart.Tag) {
			t.Errorf("subjects = %v, want only the chart", reason.Subjects)
		}
	}
	if len(kinds) != 1 || kinds[0] != IneligibleAvailabilityCoverageIncomplete {
		t.Fatalf("kinds = %v, want only the availability coverage gap: the chart is not an image", kinds)
	}
}

// Kind is the part a caller acts on. Message is for a human and interpolates
// live values -- a status, an outcome -- so anything grouping or filtering
// these has to read the kind, and the kinds have to be distinct.
func TestEachPolicyIneligibilityCarriesItsOwnKind(t *testing.T) {
	t.Parallel()
	both := Environment{
		ID:                         "dev-use2",
		ArtifactSecurityPolicy:     &ArtifactSecurityPolicy{RequiredOutcome: "PASS"},
		ArtifactAvailabilityPolicy: &ArtifactAvailabilityPolicy{RequiredOutcome: "AVAILABLE"},
	}
	available := func(status, outcome string, digests []string) *ArtifactAvailabilityAssessment {
		return &ArtifactAvailabilityAssessment{
			Provider: "test", RunID: "run-1", Status: status, Outcome: outcome, ArtifactDigests: digests,
		}
	}
	for _, test := range []struct {
		name   string
		mutate func(*Release)
		want   string
	}{
		{"no vulnerability assessment", func(release *Release) { release.VulnerabilityAssessment = nil },
			IneligibleSecurityAssessmentMissing},
		{"vulnerability assessment still running", func(release *Release) {
			release.VulnerabilityAssessment.Status = "RUNNING"
		}, IneligibleSecurityAssessmentIncomplete},
		{"vulnerability outcome not PASS", func(release *Release) {
			release.VulnerabilityAssessment.Outcome = "FAIL"
		}, IneligibleSecurityOutcomeNotPass},
		{"vulnerability assessment misses an image", func(release *Release) {
			release.VulnerabilityAssessment.ArtifactDigests = nil
		}, IneligibleSecurityCoverageIncomplete},
		{"no availability assessment", func(release *Release) { release.ArtifactAvailability = nil },
			IneligibleAvailabilityAssessmentMissing},
		{"availability assessment still running", func(release *Release) {
			release.ArtifactAvailability = available("RUNNING", "AVAILABLE", nil)
		}, IneligibleAvailabilityAssessmentIncomplete},
		{"availability outcome not AVAILABLE", func(release *Release) {
			release.ArtifactAvailability = available("COMPLETE", "UNAVAILABLE", releaseDigests(*release))
		}, IneligibleAvailabilityOutcomeNotAvailable},
		{"availability assessment misses an artifact", func(release *Release) {
			release.ArtifactAvailability = available("COMPLETE", "AVAILABLE", nil)
		}, IneligibleAvailabilityCoverageIncomplete},
	} {
		t.Run(test.name, func(t *testing.T) {
			release := assessedFoundation().Releases[0]
			release.ArtifactAvailability = available("COMPLETE", "AVAILABLE", releaseDigests(release))
			test.mutate(&release)
			var kinds []string
			for _, reason := range environmentPolicyIneligibility(release, both) {
				kinds = append(kinds, reason.Kind)
				if reason.Version != release.Version {
					t.Errorf("reason names version %q, want %q", reason.Version, release.Version)
				}
			}
			if !containsString(kinds, test.want) {
				t.Errorf("kinds = %v, want %s", kinds, test.want)
			}
		})
	}
}

func releaseDigests(release Release) []string {
	digests := make([]string, 0)
	for _, artifact := range releaseArtifacts(release) {
		digests = append(digests, artifact.Digest)
	}
	return digests
}

// Plan validation resolves an Entity's Release Channel to a version through
// latestEligibleRelease, so a Release that fails the Environment's policies
// must not be picked there either. Nothing covered this path.
func TestChannelResolutionDuringValidationHonoursEnvironmentPolicies(t *testing.T) {
	t.Parallel()
	state := assessedFoundation()
	environment := state.Environment
	environment.ArtifactAvailabilityPolicy = &ArtifactAvailabilityPolicy{RequiredOutcome: "AVAILABLE"}
	release := state.Releases[0]

	if _, found := latestEligibleRelease(
		[]Release{release}, nil, nil, release.Product, "RELEASE", environment,
	); found {
		t.Fatal("a Release with no availability assessment was resolved from its Channel")
	}
	release.ArtifactAvailability = &ArtifactAvailabilityAssessment{
		Provider: "test", RunID: "run-1", Status: "COMPLETE", Outcome: "AVAILABLE",
		ArtifactDigests: releaseDigests(release),
	}
	if _, found := latestEligibleRelease(
		[]Release{release}, nil, nil, release.Product, "RELEASE", environment,
	); !found {
		t.Fatal("a Release satisfying the policy was not resolved")
	}
}

// The recall FLOOR decides how far back an Entity may roll. Several recalls can be open on one
// Product at once, and the floor must be the HIGHEST of their minimums — keeping any other rolls the
// Entity onto a version a still-open recall forbids. A mutation sweep found the whole computation
// unpinned, for two reasons: every existing case has exactly ONE recall, and this path is reached
// only for an Entity with an OBSERVED version, since without one a different function answers.
//
// The fixture leaves a gap at 0.2.0 deliberately. The candidate scan runs DOWNWARD and takes the
// highest unrecalled Release at or above the floor, so a floor only changes the outcome when it
// rises past that candidate — with 0.2.0 present, every floor from 0.1.0 to 0.2.0 gives the same
// answer and the comparison is invisible.
func TestRecallFloorIsTheHighestOfEveryOpenRecall(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	downgradeFrom := func(id, minimum string) Recall {
		return Recall{
			ID: id, Product: "app.example:example-api", StartingVersion: "0.3.0", SingleVersion: true,
			Strategy: RecallStrategy{Type: RecallAllowDowngrade, MinimumVersion: minimum},
			Reason:   "regression",
		}
	}
	rollTo := func(t *testing.T, recalls ...Recall) PlanStep {
		t.Helper()
		state := channelFoundation("0.1.0", "0.3.0")
		state.Recalls = recalls
		plan, err := BuildPlanWithObserved(state, observedVersion("0.3.0"), now)
		if err != nil {
			t.Fatalf("no Plan: %v", err)
		}
		return plan.Steps[0]
	}

	// Alone, a 0.1.0 minimum lets the Entity fall to 0.1.0.
	if step := rollTo(t, downgradeFrom("low", "0.1.0")); step.Version != "0.1.0" || !step.RecallRollOff {
		t.Fatalf("a lone 0.1.0 minimum gave %#v", step)
	}
	// Alone, a 0.2.0 minimum puts 0.1.0 out of reach, so the Entity holds where it is.
	if step := rollTo(t, downgradeFrom("high", "0.2.0")); step.Version != "0.3.0" {
		t.Fatalf("a lone 0.2.0 minimum gave %#v", step)
	}
	// Both open, the higher wins — in either order.
	for _, order := range [][]Recall{
		{downgradeFrom("low", "0.1.0"), downgradeFrom("high", "0.2.0")},
		{downgradeFrom("high", "0.2.0"), downgradeFrom("low", "0.1.0")},
	} {
		if step := rollTo(t, order...); step.Version != "0.3.0" {
			t.Errorf("rolled to %s with a 0.2.0 minimum still open; the higher floor was discarded",
				step.Version)
		}
	}
}

// The two renderers an operator actually reads when nothing was eligible, and the
// automatic recall that fires without anyone asking for it. Each guard below was
// reachable and unasserted.
func TestIneligibilityRenderingAndAutomaticSecurityRecallBounds(t *testing.T) {
	t.Parallel()

	t.Run("a reason without subjects renders without empty parentheses", func(t *testing.T) {
		// Subjects are optional on the type. Rendering them unconditionally puts a
		// bare " ()" into the error an operator is meant to act on.
		got := describeIneligibility([]ReleaseIneligibility{
			{Version: "0.3.0", Kind: "SCANNER", Message: "no scanner evidence"},
		})
		if got != "; 0.3.0: no scanner evidence" {
			t.Errorf("subject-less reason rendered as %q", got)
		}
		got = describeIneligibility([]ReleaseIneligibility{
			{Version: "0.3.0", Kind: "TOPOLOGY", Message: "blocked", Subjects: []string{"dev-use2"}},
		})
		if got != "; 0.3.0: blocked (dev-use2)" {
			t.Errorf("reason with subjects rendered as %q", got)
		}
	})

	t.Run("Releases excluded for the same reason are named together", func(t *testing.T) {
		got := describeIneligibility([]ReleaseIneligibility{
			{Version: "0.1.0", Message: "no scanner evidence"},
			{Version: "0.2.0", Message: "no scanner evidence"},
			{Version: "0.3.0", Message: "recalled", Subjects: []string{"rec-1"}},
		})
		want := "; 0.1.0, 0.2.0: no scanner evidence; 0.3.0: recalled (rec-1)"
		if got != want {
			t.Errorf("grouped reasons rendered as %q, wanted %q", got, want)
		}
	})

	t.Run("an automatic security recall needs a failing assessment with live findings", func(t *testing.T) {
		// Every clause here is load-bearing: this recall is created without an
		// operator asking for it, and it makes the Release it names uninstallable.
		base := func(mutate func(*VulnerabilityAssessment)) DesiredState {
			state := channelFoundation("0.1.0")
			state.Environment.AutomaticSecurityRecall = true
			assessment := &VulnerabilityAssessment{
				Status: "COMPLETE", Outcome: "FAIL", ActiveVulnerabilities: 3, RunID: "run-1",
			}
			mutate(assessment)
			state.Releases[0].VulnerabilityAssessment = assessment
			return state
		}
		for _, testCase := range []struct {
			name    string
			mutate  func(*VulnerabilityAssessment)
			recalls int
		}{
			{"a complete failing assessment with live findings recalls", func(*VulnerabilityAssessment) {}, 1},
			{"an incomplete assessment is not yet a verdict", func(a *VulnerabilityAssessment) { a.Status = "IN_PROGRESS" }, 0},
			{"a passing assessment is not a recall", func(a *VulnerabilityAssessment) { a.Outcome = "PASS" }, 0},
			{"a failure with nothing live left is not a recall", func(a *VulnerabilityAssessment) { a.ActiveVulnerabilities = 0 }, 0},
			{"an absent assessment is not a recall", func(a *VulnerabilityAssessment) { a.RunID = "" }, 1},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				got := withAutomaticSecurityRecalls(base(testCase.mutate))
				if len(got.Recalls) != testCase.recalls {
					t.Errorf("produced %d automatic recalls, wanted %d: %#v", len(got.Recalls), testCase.recalls, got.Recalls)
				}
			})
		}
		// The switch itself still has to be on.
		off := base(func(*VulnerabilityAssessment) {})
		off.Environment.AutomaticSecurityRecall = false
		if got := withAutomaticSecurityRecalls(off); len(got.Recalls) != 0 {
			t.Errorf("recalled with the automatic switch off: %#v", got.Recalls)
		}
	})
}

// The recall floor is the version a recall says you must reach. Two edges of it
// were unasserted, and both decide whether an Entity moves or waits.
func TestRecallFloorEdgesDecideBetweenMovingAndWaiting(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)

	t.Run("a Release exactly at the floor clears it", func(t *testing.T) {
		// The floor is the lowest acceptable version, not the lowest unacceptable
		// one. Reading it exclusively strands an Entity on a recalled Release while
		// the very Release the recall names as the fix sits in the catalog.
		state := channelFoundation("0.1.0", "0.2.0", "0.3.0")
		state.Recalls = []Recall{{
			ID: "recall-api", Product: "app.example:example-api", StartingVersion: "0.1.0", SingleVersion: true,
			Strategy: RecallStrategy{Type: RecallRollForward, MinimumVersion: "0.3.0"}, Reason: "regression",
		}}
		plan, err := BuildPlanWithObserved(state, observedVersion("0.1.0"), now)
		if err != nil {
			t.Fatal(err)
		}
		if plan.Steps[0].Version != "0.3.0" || !plan.Steps[0].RecallRollOff {
			t.Errorf("Release exactly at the recall floor was refused: %#v", plan.Steps[0])
		}
	})

	t.Run("a floor above every available Release means wait, not settle", func(t *testing.T) {
		// The recall names 0.3.0 as the fix and the catalog does not have it yet.
		// 0.2.0 is higher than where the Entity sits, but it is not the fix, and
		// moving to it would report a recall as rolled off while the defect stands.
		state := channelFoundation("0.1.0", "0.2.0")
		state.Recalls = []Recall{{
			ID: "recall-api", Product: "app.example:example-api", StartingVersion: "0.1.0", SingleVersion: true,
			Strategy: RecallStrategy{Type: RecallRollForward, MinimumVersion: "0.3.0"}, Reason: "regression",
		}}
		plan, err := BuildPlanWithObserved(state, observedVersion("0.1.0"), now)
		if err != nil {
			t.Fatal(err)
		}
		if plan.Steps[0].Version != "0.1.0" || plan.Steps[0].RecallRollOff {
			t.Errorf("settled below the recall floor: %#v", plan.Steps[0])
		}
	})
}

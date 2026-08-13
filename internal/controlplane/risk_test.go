package controlplane

import (
	"testing"
	"time"
)

func TestReleaseRiskScoreReflectsWorstAndVolume(t *testing.T) {
	t.Parallel()
	score := func(findings ...VulnerabilityFinding) float64 {
		return ReleaseRiskScore(&VulnerabilityAssessment{ActiveFindings: findings})
	}

	if got := ReleaseRiskScore(nil); got != 0 {
		t.Fatalf("no assessment = %v, want 0", got)
	}
	if got := score(); got != 0 {
		t.Fatalf("no active findings = %v, want 0", got)
	}

	// One critical finding is never masked: the score cannot fall below it.
	critical := score(VulnerabilityFinding{ID: "CVE-1", CVSS: 9.8})
	if critical < 9.8 {
		t.Fatalf("single critical = %v, want at least 9.8", critical)
	}
	if critical > RiskScoreMaximum {
		t.Fatalf("single critical = %v, exceeds the bounded scale", critical)
	}

	// Accumulated low-severity findings still raise the score.
	oneLow := score(VulnerabilityFinding{ID: "CVE-1", CVSS: 3})
	manyLow := score(
		VulnerabilityFinding{ID: "CVE-1", CVSS: 3}, VulnerabilityFinding{ID: "CVE-2", CVSS: 3},
		VulnerabilityFinding{ID: "CVE-3", CVSS: 3}, VulnerabilityFinding{ID: "CVE-4", CVSS: 3},
	)
	if manyLow <= oneLow {
		t.Fatalf("accumulation is not captured: one=%v many=%v", oneLow, manyLow)
	}
	if manyLow > RiskScoreMaximum {
		t.Fatalf("accumulated score = %v, exceeds the bounded scale", manyLow)
	}

	// A finding with no numeric score still contributes, via its severity tier.
	if tiered := score(VulnerabilityFinding{ID: "CVE-1", Severity: "CRITICAL"}); tiered < cvssCritical {
		t.Fatalf("critical tier = %v, want at least %v", tiered, cvssCritical)
	}
	// An unparseable severity is scored as high, so it cannot make a Release look
	// cleaner than it is.
	unknown := score(VulnerabilityFinding{ID: "CVE-1", Severity: "wat"})
	high := score(VulnerabilityFinding{ID: "CVE-1", Severity: "HIGH"})
	if unknown != high {
		t.Fatalf("unknown tier = %v, want the high-tier score %v", unknown, high)
	}
}

// The exception exists to improve a posture already degraded by a vulnerability
// recall, and every one of its conditions must hold.
func TestVulnerabilityRecalledUpgradeEligibility(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	policy := VulnerabilityUpgradePolicy{AllowRecalledForVulnerabilities: true, RescanSLASeconds: 24 * 60 * 60}

	release := func(version string, cvss float64, assessedAt time.Time) Release {
		return Release{
			Product: "app.example:example-api", Version: version,
			Chart:  Artifact{Repository: "registry.example/api", Tag: version, Digest: digest},
			Images: []Artifact{{Repository: "registry.example/api", Tag: version, Digest: digest}},
			VulnerabilityAssessment: &VulnerabilityAssessment{
				Provider: "scanner", RunID: "run-" + version, Status: "COMPLETE", Outcome: "FAIL",
				ArtifactDigests: []string{digest}, ActiveVulnerabilities: 1, AssessedAt: assessedAt,
				ActiveFindings: []VulnerabilityFinding{{ID: "CVE-" + version, CVSS: cvss}},
			},
			MalwareAssessment: &MalwareAssessment{
				Provider: "clamav", RunID: "mal-" + version, Status: "COMPLETE", Outcome: "PASS",
				ArtifactDigests: []string{digest}, Findings: 0, AssessedAt: assessedAt,
			},
		}
	}
	securityRecall := func(version string) Recall {
		return Recall{
			ID: "security-" + version, Product: "app.example:example-api", StartingVersion: version,
			SingleVersion: true, Strategy: RecallStrategy{Type: RecallRollForward, MinimumVersion: version},
			Reason: "vulnerability", Source: RecallSourceSecurity, AssessmentRunID: "run-" + version,
		}
	}

	current := release("1.603.0", 9.0, now.Add(-time.Hour))
	better := release("1.604.0", 7.0, now.Add(-time.Hour))
	worse := release("1.605.0", 9.9, now.Add(-time.Hour))
	recalls := []Recall{securityRecall("1.603.0"), securityRecall("1.604.0"), securityRecall("1.605.0")}

	// A strictly newer, no-worse-scoring target is selected and carries both scores.
	selected, rationale, ok := selectVulnerabilityRecalledUpgrade(
		[]Release{worse, better, current}, current, recalls, policy, now)
	if !ok || selected.Version != "1.604.0" {
		t.Fatalf("selected = %#v ok = %v, want 1.604.0", selected, ok)
	}
	if rationale == nil || rationale.CurrentVersion != "1.603.0" || rationale.TargetVersion != "1.604.0" {
		t.Fatalf("rationale = %#v", rationale)
	}
	if rationale.TargetRiskScore > rationale.CurrentRiskScore {
		t.Fatalf("rationale scores = %#v, target must not be worse", rationale)
	}

	for name, mutate := range map[string]func() ([]Release, Release, []Recall, VulnerabilityUpgradePolicy){
		"disabled policy": func() ([]Release, Release, []Recall, VulnerabilityUpgradePolicy) {
			return []Release{better}, current, recalls, VulnerabilityUpgradePolicy{RescanSLASeconds: policy.RescanSLASeconds}
		},
		"target scores worse": func() ([]Release, Release, []Recall, VulnerabilityUpgradePolicy) {
			return []Release{worse}, current, recalls, policy
		},
		"target is older": func() ([]Release, Release, []Recall, VulnerabilityUpgradePolicy) {
			older := release("1.602.0", 1.0, now.Add(-time.Hour))
			return []Release{older}, current, append(recalls, securityRecall("1.602.0")), policy
		},
		"target carries a non-vulnerability recall": func() ([]Release, Release, []Recall, VulnerabilityUpgradePolicy) {
			manual := Recall{
				ID: "manual-1", Product: "app.example:example-api", StartingVersion: "1.604.0",
				SingleVersion: true, Strategy: RecallStrategy{Type: RecallRollForward},
				Reason: "operator", Source: RecallSourceManual,
			}
			return []Release{better}, current, append(recalls, manual), policy
		},
		"target scan evidence is stale": func() ([]Release, Release, []Recall, VulnerabilityUpgradePolicy) {
			stale := release("1.604.0", 7.0, now.Add(-72*time.Hour))
			return []Release{stale}, current, recalls, policy
		},
		"target has counts without per-finding risk evidence": func() ([]Release, Release, []Recall, VulnerabilityUpgradePolicy) {
			incomplete := release("1.604.0", 7.0, now.Add(-time.Hour))
			incomplete.VulnerabilityAssessment.ActiveFindings = nil
			return []Release{incomplete}, current, recalls, policy
		},
		"target has malware findings": func() ([]Release, Release, []Recall, VulnerabilityUpgradePolicy) {
			infected := release("1.604.0", 7.0, now.Add(-time.Hour))
			infected.MalwareAssessment.Findings = 1
			return []Release{infected}, current, recalls, policy
		},
		"target malware evidence is absent": func() ([]Release, Release, []Recall, VulnerabilityUpgradePolicy) {
			unscanned := release("1.604.0", 7.0, now.Add(-time.Hour))
			unscanned.MalwareAssessment = nil
			return []Release{unscanned}, current, recalls, policy
		},
		"current Release is not recalled for vulnerabilities": func() ([]Release, Release, []Recall, VulnerabilityUpgradePolicy) {
			return []Release{better}, current, []Recall{securityRecall("1.604.0")}, policy
		},
		"current scan evidence is stale": func() ([]Release, Release, []Recall, VulnerabilityUpgradePolicy) {
			strandedStale := release("1.603.0", 9.0, now.Add(-72*time.Hour))
			return []Release{better}, strandedStale, recalls, policy
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			candidates, from, activeRecalls, activePolicy := mutate()
			if _, _, ok := selectVulnerabilityRecalledUpgrade(candidates, from, activeRecalls, activePolicy, now); ok {
				t.Fatal("upgrade was permitted but every condition must hold")
			}
		})
	}
}

// A Hub default applies unless the Environment overrides it, and the override
// wins in both directions so Environments can be opted in or out independently.
func TestSpaceSettingsResolveVulnerabilityUpgrades(t *testing.T) {
	t.Parallel()
	settings := SpaceSettings{
		VulnerabilityUpgrades: VulnerabilityUpgradePolicy{AllowRecalledForVulnerabilities: true, RescanSLASeconds: 3600},
		ResourceOverrides: map[string]VulnerabilityUpgradePolicy{
			"locked-down": {AllowRecalledForVulnerabilities: false},
		},
	}
	if got := settings.ResolveVulnerabilityUpgrades("dev-use2"); !got.AllowRecalledForVulnerabilities || got.RescanSLASeconds != 3600 {
		t.Fatalf("Hub default = %#v", got)
	}
	if got := settings.ResolveVulnerabilityUpgrades("locked-down"); got.AllowRecalledForVulnerabilities {
		t.Fatalf("override = %#v, want the Environment opted out", got)
	}

	// An Environment that declares nothing does not get the exception.
	if got := effectiveVulnerabilityUpgradePolicy(Environment{ID: "dev-use2"}); got.AllowRecalledForVulnerabilities {
		t.Fatalf("undeclared policy = %#v, want the exception off", got)
	}
}

// The rationale reaches the Plan, and only via this workflow: an ordinary
// upgrade carries no risk score metadata.
func TestPlanCarriesRiskScoresOnlyForRecalledUpgrades(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	assessed := func(version string, cvss float64) Release {
		return Release{
			Product: "app.example:example-api", Version: version, Channels: []string{"RELEASE"},
			Chart:  Artifact{Repository: "registry.example/api", Tag: version, Digest: digest},
			Images: []Artifact{{Repository: "registry.example/api", Tag: version, Digest: digest}},
			VulnerabilityAssessment: &VulnerabilityAssessment{
				Provider: "scanner", RunID: "run-" + version, Status: "COMPLETE", Outcome: "FAIL",
				ArtifactDigests: []string{digest}, ActiveVulnerabilities: 1, AssessedAt: now.Add(-time.Hour),
				ActiveFindings: []VulnerabilityFinding{{ID: "CVE-" + version, CVSS: cvss}},
			},
			MalwareAssessment: &MalwareAssessment{
				Provider: "clamav", RunID: "mal-" + version, Status: "COMPLETE", Outcome: "PASS",
				ArtifactDigests: []string{digest}, AssessedAt: now.Add(-time.Hour),
			},
		}
	}
	state := DesiredState{
		APIVersion: "platform.delivery/v1alpha1", Kind: "EnvironmentDeployment",
		Environment: Environment{
			ID: "dev-use2", Generation: "0123456789abcdef",
			VulnerabilityUpgrades: &VulnerabilityUpgradePolicy{
				AllowRecalledForVulnerabilities: true, RescanSLASeconds: 24 * 60 * 60,
			},
		},
		Releases: []Release{assessed("1.603.0", 9.0), assessed("1.604.0", 7.0)},
		Recalls: []Recall{
			{
				ID: "security-603", Product: "app.example:example-api", StartingVersion: "1.603.0", SingleVersion: true,
				Strategy: RecallStrategy{Type: RecallRollForward, MinimumVersion: "1.603.0"},
				Reason:   "vulnerability", Source: RecallSourceSecurity, AssessmentRunID: "run-1.603.0",
			},
			{
				ID: "security-604", Product: "app.example:example-api", StartingVersion: "1.604.0", SingleVersion: true,
				Strategy: RecallStrategy{Type: RecallRollForward, MinimumVersion: "1.604.0"},
				Reason:   "vulnerability", Source: RecallSourceSecurity, AssessmentRunID: "run-1.604.0",
			},
		},
		Entities: []Entity{{
			Name: "example-api", Product: "app.example:example-api", ReleaseChannel: "RELEASE", Namespace: "example",
		}},
	}
	observed := EnvironmentReport{Environment: "dev-use2", Entities: []ReportedEntity{{
		InstallationID: "example-api", Product: "app.example:example-api", Version: "1.603.0",
		Namespace: "example", Health: HealthHealthy,
	}}}

	plan, err := BuildPlanWithObserved(state, observed, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(plan.Steps))
	}
	step := plan.Steps[0]
	if step.Version != "1.604.0" {
		t.Fatalf("version = %s, want the lower-risk recalled target 1.604.0", step.Version)
	}
	if step.UpgradeRationale == nil {
		t.Fatal("Plan carries no risk score metadata for a recalled-target upgrade")
	}
	if step.UpgradeRationale.CurrentRiskScore <= step.UpgradeRationale.TargetRiskScore {
		t.Fatalf("rationale = %#v, want the target to reduce risk", step.UpgradeRationale)
	}

	// With the exception disabled the Entity stays put and no rationale appears.
	stranded := cloneJSON(state)
	stranded.Environment.VulnerabilityUpgrades = nil
	strandedPlan, err := BuildPlanWithObserved(stranded, observed, now)
	if err != nil {
		t.Fatal(err)
	}
	if strandedPlan.Steps[0].Version != "1.603.0" {
		t.Fatalf("version = %s, want the Entity to stay on 1.603.0", strandedPlan.Steps[0].Version)
	}
	if strandedPlan.Steps[0].UpgradeRationale != nil {
		t.Fatalf("rationale = %#v, want none outside this workflow", strandedPlan.Steps[0].UpgradeRationale)
	}
}

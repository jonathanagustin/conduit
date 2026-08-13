package controlplane

import (
	"strings"
	"testing"
)

func TestImagePrePullDefaultsOnAndRequiresExplicitOptOut(t *testing.T) {
	state := foundation()
	plan, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range plan.Steps {
		if !step.HelmExecution.ImagePrePull || step.HelmExecution.ImagePrePullTimeoutSeconds != 15*60 {
			t.Fatalf("default image pre-pull policy = %#v", step.HelmExecution)
		}
	}

	state.Environment.HelmExecution = &HelmExecutionDefaults{
		ImagePrePull: &ImagePrePullPolicy{Enabled: false},
	}
	plan, err = BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range plan.Steps {
		if step.HelmExecution.ImagePrePull || step.HelmExecution.ImagePrePullTimeoutSeconds != 0 {
			t.Fatalf("explicit image pre-pull opt-out = %#v", step.HelmExecution)
		}
	}

	state.Releases[0].ImagePrePullTimeoutSeconds = 3 * 60
	if errs := Validate(state); len(errs) == 0 || !strings.Contains(errs[0].Message, "requires environment image pre-pull") {
		t.Fatalf("disabled pre-pull accepted Product timeout: %#v", errs)
	}
}

func TestHelmExecutionPolicyIsValidatedAndSnapshotted(t *testing.T) {
	state := foundation()
	state.Environment.HelmExecution = &HelmExecutionDefaults{
		DefaultRolloutStrategy:                 RolloutManageRollout,
		InstallResourceReadinessTimeoutSeconds: 20 * 60,
		UpgradeResourceReadinessTimeoutSeconds: 10 * 60,
		ImagePrePull:                           &ImagePrePullPolicy{Enabled: true, TimeoutSeconds: 15 * 60},
	}
	state.Releases[0].RolloutStrategy = RolloutApplyChangesNoWait
	state.Releases[0].ResourceReadinessTimeoutSeconds = 60 * 60
	state.Releases[0].ImagePrePullTimeoutSeconds = 3 * 60
	plan, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	policy := plan.Steps[0].HelmExecution
	if policy.RolloutStrategy != RolloutApplyChangesNoWait ||
		policy.InstallResourceReadinessTimeoutSeconds != 60*60 ||
		policy.UpgradeResourceReadinessTimeoutSeconds != 60*60 ||
		!policy.ImagePrePull || policy.ImagePrePullTimeoutSeconds != 3*60 {
		t.Fatalf("effective Helm execution policy = %#v", policy)
	}

	state.Releases[0].RolloutStrategy = "unsafe"
	state.Releases[0].ImagePrePullTimeoutSeconds = 30
	errs := Validate(state)
	joined := ""
	for _, validationErr := range errs {
		joined += validationErr.Path + ":" + validationErr.Message + "\n"
	}
	if !strings.Contains(joined, "rolloutStrategy") || !strings.Contains(joined, "imagePrePullTimeoutSeconds") {
		t.Fatalf("validation errors = %s", joined)
	}
}

func TestNoWaitPlanCanSucceedBeforeReadiness(t *testing.T) {
	step := PlanStep{
		InstallationID: "app", Product: "example:app", Version: "1.0.0", Namespace: "example",
		ReleaseDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ConfigDigest:  "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		HelmExecution: HelmExecutionPolicy{RolloutStrategy: RolloutApplyChangesNoWait},
	}
	report := EnvironmentReport{Entities: []ReportedEntity{{
		InstallationID: step.InstallationID, Product: step.Product, Version: step.Version, Namespace: step.Namespace,
		ReleaseDigest: step.ReleaseDigest, ConfigDigest: step.ConfigDigest,
		Health: HealthUnknown, RolloutStatus: RolloutInGracePeriod, ConfigStatus: ConfigApplied, Lifecycle: LifecycleInstalled,
	}}}
	if err := validateEnvironmentReport(Plan{Steps: []PlanStep{step}}, report, true); err != nil {
		t.Fatal(err)
	}
	step.HelmExecution.RolloutStrategy = RolloutManageRollout
	if err := validateEnvironmentReport(Plan{Steps: []PlanStep{step}}, report, true); err == nil {
		t.Fatal("managed rollout accepted a success report before readiness")
	}
}

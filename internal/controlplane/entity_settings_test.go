package controlplane

import (
	"testing"
	"time"
)

// A Helm release name override exists to adopt a release Platform did not name.
// Every Plan authored before the override existed relies on the Entity name, so
// that stays the default.
func TestHelmReleaseNameDefaultsToEntityName(t *testing.T) {
	t.Parallel()
	if got := (PlanStep{Entity: "keycloak"}).HelmRelease(); got != "keycloak" {
		t.Fatalf("default = %s, want the Entity name", got)
	}
	if got := (PlanStep{Entity: "keycloak", HelmReleaseName: "  "}).HelmRelease(); got != "keycloak" {
		t.Fatalf("blank override = %s, want the Entity name", got)
	}
	if got := (PlanStep{Entity: "keycloak", HelmReleaseName: "helm-chart-keycloak"}).HelmRelease(); got != "helm-chart-keycloak" {
		t.Fatalf("override = %s", got)
	}

	state := foundation()
	state.Entities[0].HelmReleaseName = "helm-chart-workspace"
	plan, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range plan.Steps {
		if step.Entity == "example-workspace" && step.HelmRelease() != "helm-chart-workspace" {
			t.Fatalf("step %s release = %s", step.Entity, step.HelmRelease())
		}
		if step.Entity == "example-api" && step.HelmRelease() != "example-api" {
			t.Fatalf("step %s release = %s, want the default", step.Entity, step.HelmRelease())
		}
	}
}

// A dependency is resolved on the depending Entity's own stack. An override names
// where a dependency actually lives, and because it is an explicit statement that
// the dependency is not co-located, it replaces the same-namespace requirement
// rather than adding to it.
func TestDependencyStackScopesResolution(t *testing.T) {
	t.Parallel()
	base := func() DesiredState {
		state := foundation()
		state.Entities = []Entity{
			{Name: "example-workspace", Product: "app.example:example-workspace", Version: "0.1.0", Namespace: "example"},
			{Name: "example-api", Product: "app.example:example-api", Version: "0.1.0", Namespace: "example"},
		}
		return state
	}

	// No stacks declared: one implicit stack, so nothing changes.
	if _, err := BuildPlan(base()); err != nil {
		t.Fatalf("undeclared stacks broke resolution: %v", err)
	}

	// The dependency sits on another stack with no override: unresolvable.
	split := base()
	split.Entities[0].Stack = "blue"
	split.Entities[1].Stack = "green"
	if _, err := BuildPlan(split); err == nil {
		t.Fatal("a dependency on a different stack resolved without an override")
	}

	// With an override naming the stack, it resolves.
	overridden := split
	overridden.Entities[0].DependencyStackOverrides = map[string]string{"app.example:example-api": "green"}
	if _, err := BuildPlan(overridden); err != nil {
		t.Fatalf("stack override did not resolve the dependency: %v", err)
	}

	// The override also crosses namespaces, which is the point of naming a stack.
	crossNamespace := overridden
	crossNamespace.Entities[1].Namespace = "shared"
	crossNamespace.Environment.Namespaces = []NamespaceTarget{
		{Name: "example", RID: "ri.namespace.example"}, {Name: "shared", RID: "ri.namespace.shared"},
	}
	if _, err := BuildPlan(crossNamespace); err != nil {
		t.Fatalf("stack override did not cross namespaces: %v", err)
	}

	// An override pointing at a stack nothing occupies still fails closed.
	missing := split
	missing.Entities[0].DependencyStackOverrides = map[string]string{"app.example:example-api": "violet"}
	if _, err := BuildPlan(missing); err == nil {
		t.Fatal("an override naming an empty stack resolved")
	}
}

func TestAdvancedEntityAndProductSettingsFailClosed(t *testing.T) {
	t.Parallel()
	state := foundation()
	state.Environment.VulnerabilityUpgrades = &VulnerabilityUpgradePolicy{RescanSLASeconds: -1}
	state.Entities[0].HelmReleaseName = "release-name-that-is-deliberately-longer-than-the-helm-fifty-three-character-limit"
	state.Entities[0].Stack = "not a stack"
	state.Entities[0].DependencyStackOverrides = map[string]string{"not-a-product": "also not a stack"}
	state.ProductSettings = []ProductSettings{
		{Product: "app.example:example-api", RampedRollout: &RampedRolloutSettings{Enabled: true, Windows: []ReleaseChannelRolloutWindow{
			{Channel: "release", WindowSeconds: 0}, {Channel: "release", WindowSeconds: 1},
		}}},
		{Product: "app.example:example-api"},
	}

	paths := map[string]bool{}
	for _, validationErr := range Validate(state) {
		paths[validationErr.Path] = true
	}
	for _, path := range []string{
		"environment.vulnerabilityUpgrades.rescanSlaSeconds",
		"entities[0].helmReleaseName", "entities[0].stack", "entities[0].dependencyStackOverrides",
		"productSettings[0].rampedRollout.windows[0].channel",
		"productSettings[0].rampedRollout.windows[0].windowSeconds",
		"productSettings[0].rampedRollout.windows[1].channel", "productSettings[1].product",
	} {
		if !paths[path] {
			t.Fatalf("validation paths = %#v, want %s", paths, path)
		}
	}
}

// sealPromotion stamps the content-derived identity a promotion record must carry
// to be accepted as an immutable decision.
func sealPromotion(t *testing.T, record PromotionRecord) PromotionRecord {
	t.Helper()
	record.Revision = 1
	identity, err := promotionIdentity(record)
	if err != nil {
		t.Fatal(err)
	}
	record.ID = identity
	return record
}

// A rollout window is a Product setting so it can pace a channel no promotion
// pipeline targets, which a stage-scoped window cannot express.
func TestRampedRolloutWindowComesFromProductSettings(t *testing.T) {
	t.Parallel()
	evaluated := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	state := func() DesiredState {
		base := foundation()
		base.Releases[0].Channels = []string{"RELEASE"}
		base.Releases[1].Channels = []string{"RELEASE"}
		base.Entities = []Entity{
			{Name: "example-api", Product: "app.example:example-api", Namespace: "example", ReleaseChannel: "STABLE"},
		}
		base.Promotions = []PromotionRecord{sealPromotion(t, PromotionRecord{
			Product: "app.example:example-api", Version: "0.1.0",
			FromChannel: "RELEASE", ToChannel: "STABLE", Status: PromotionPassed, EvaluatedAt: evaluated,
		})}
		return base
	}

	// No window anywhere: no pacing.
	plan, err := BuildPlanWithObserved(state(), EnvironmentReport{}, evaluated)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Steps[0].RampedRollout != nil {
		t.Fatalf("unpaced step carries a constraint: %#v", plan.Steps[0].RampedRollout)
	}

	// A Product setting for the Entity's channel paces it.
	configured := state()
	configured.ProductSettings = []ProductSettings{{
		Product: "app.example:example-api",
		RampedRollout: &RampedRolloutSettings{Enabled: true, Windows: []ReleaseChannelRolloutWindow{
			{Channel: "STABLE", WindowSeconds: 3600},
		}},
	}}
	plan, err = BuildPlanWithObserved(configured, EnvironmentReport{}, evaluated)
	if err != nil {
		t.Fatal(err)
	}
	constraint := plan.Steps[0].RampedRollout
	if constraint == nil || constraint.WindowSeconds != 3600 {
		t.Fatalf("constraint = %#v, want the Product setting window", constraint)
	}
	if !constraint.NotBefore.Before(evaluated.Add(time.Hour)) || constraint.NotBefore.Before(evaluated) {
		t.Fatalf("NotBefore %s is outside the window starting %s", constraint.NotBefore, evaluated)
	}

	// A channel outside the promotion pipeline is still paced, anchored on the
	// promotion that made the version available at all.
	offPipeline := configured
	offPipeline.Entities[0].ReleaseChannel = "PILOT"
	offPipeline.Releases[0].Channels = []string{"RELEASE", "PILOT"}
	offPipeline.ProductSettings[0].RampedRollout.Windows = []ReleaseChannelRolloutWindow{
		{Channel: "PILOT", WindowSeconds: 1800},
	}
	plan, err = BuildPlanWithObserved(offPipeline, EnvironmentReport{}, evaluated)
	if err != nil {
		t.Fatal(err)
	}
	constraint = plan.Steps[0].RampedRollout
	if constraint == nil || constraint.WindowSeconds != 1800 {
		t.Fatalf("off-pipeline channel constraint = %#v, want pacing", constraint)
	}

	// A disabled setting paces nothing.
	disabled := configured
	disabled.ProductSettings[0].RampedRollout.Enabled = false
	plan, err = BuildPlanWithObserved(disabled, EnvironmentReport{}, evaluated)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Steps[0].RampedRollout != nil {
		t.Fatalf("disabled setting still paced: %#v", plan.Steps[0].RampedRollout)
	}
}

// Pacing must be stable across Plan regeneration, otherwise every recomputation
// reshuffles which Entities go first and the ramp never converges.
func TestRampedRolloutOffsetIsStableAcrossRegeneration(t *testing.T) {
	t.Parallel()
	evaluated := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	state := foundation()
	state.Releases[0].Channels = []string{"RELEASE"}
	state.Releases[1].Channels = []string{"RELEASE"}
	state.Entities = []Entity{
		{Name: "example-api", Product: "app.example:example-api", Namespace: "example", ReleaseChannel: "RELEASE"},
	}
	// The Release starts on DEV so promoting it onto RELEASE is a legal transition.
	state.Releases[0].Channels = []string{"DEV"}
	state.Promotions = []PromotionRecord{sealPromotion(t, PromotionRecord{
		Product: "app.example:example-api", Version: "0.1.0",
		FromChannel: "DEV", ToChannel: "RELEASE", Status: PromotionPassed, EvaluatedAt: evaluated,
	})}
	state.ProductSettings = []ProductSettings{{
		Product: "app.example:example-api",
		RampedRollout: &RampedRolloutSettings{Enabled: true, Windows: []ReleaseChannelRolloutWindow{
			{Channel: "RELEASE", WindowSeconds: 7200},
		}},
	}}
	first, err := BuildPlanWithObserved(state, EnvironmentReport{}, evaluated)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildPlanWithObserved(state, EnvironmentReport{}, evaluated.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !first.Steps[0].RampedRollout.NotBefore.Equal(second.Steps[0].RampedRollout.NotBefore) {
		t.Fatalf("offset moved between regenerations: %s then %s",
			first.Steps[0].RampedRollout.NotBefore, second.Steps[0].RampedRollout.NotBefore)
	}
}

package controlplane

import "testing"

// TestExpectedSilenceEnvironmentsClassifiesFromLoadedPlans is the adapter half
// of #25: the expected-silence set is derived from the plans the overview has
// already loaded, so it costs no extra repository read, and it names exactly the
// environments whose gaps are expected -- intermittent, which expects direct
// agents but not continuous reporting. A connected environment (expects
// continuous) and a no-connection one (holds no health) are both out.
func TestExpectedSilenceEnvironmentsClassifiesFromLoadedPlans(t *testing.T) {
	t.Parallel()
	plan := func(env, setting string) PlanRecord {
		return PlanRecord{
			ID: "plan-" + env, Plan: Plan{Environment: env},
			DesiredState: &DesiredState{Environment: Environment{
				ID: env, Generation: "1", ConnectionSetting: setting, DeclaredStateSource: SourceEditOnThisHub,
			}},
		}
	}
	plans := []PlanRecord{
		plan("edge", ConnectionIntermittentAgents),
		plan("prod", ConnectionAgentsConnected),
		plan("standby", ConnectionNoConnectionExpected),
		plan("mirror", ConnectionRelayedFromAnotherHub),
	}

	set := expectedSilenceEnvironments(plans)

	if !set["edge"] {
		t.Error("an intermittent environment was not marked as expected-silence")
	}
	if set["prod"] {
		t.Error("a continuously-reporting environment was marked as expected-silence")
	}
	if set["standby"] {
		t.Error("a no-connection environment, which holds no health, was marked as expected-silence")
	}
	// A relayed environment's gap is a concern -- the bundle import that mirrors
	// the upstream state has stopped -- so its staleness must still be flagged.
	if set["mirror"] {
		t.Error("a relayed environment was marked as expected-silence; a stale mirror is a real concern")
	}
	if len(set) != 1 {
		t.Errorf("set = %v, want only the intermittent environment", set)
	}
}

package domain

import "testing"

func TestWaveDoesNotStartUntilItsGatePasses(t *testing.T) {
	rollout, err := New("roll-1", "ledger", "1.2.0", "ada", []Wave{
		{Name: "canary", Environments: []string{"east"}, Gate: GateWindow},
		{Name: "rest", Environments: []string{"west"}, Gate: GateHealth},
	})
	if err != nil {
		t.Fatal(err)
	}
	blocked, reason := Advance(rollout, Facts{})
	if reason != "time window is closed" || blocked.Waves[0].Status != WaveBlocked || blocked.Waves[1].Status != WavePending {
		t.Fatalf("closed window = %q %#v", reason, blocked.Waves)
	}
	opened, reason := Advance(blocked, Facts{Window: true})
	if reason != "wave canary started" || opened.Waves[0].Status != WaveCurrent || opened.Status != StatusRunning {
		t.Fatalf("open window = %q %#v", reason, opened)
	}
	passed, reason := Advance(opened, Facts{Window: true, Healthy: map[string]bool{"west": false}})
	if opened.Waves[0].Status != WaveCurrent {
		t.Fatal("current wave should pass on the next advance when its gate still holds")
	}
	if reason != "health gate is closed for west" || passed.Waves[0].Status != WavePassed || passed.Waves[1].Status != WaveBlocked {
		t.Fatalf("health = %q %#v", reason, passed.Waves)
	}
	done, reason := Advance(passed, Facts{Window: true, Healthy: map[string]bool{"west": true}})
	if reason != "wave rest started" {
		t.Fatalf("rest = %q", reason)
	}
	finished, reason := Advance(done, Facts{Window: true, Healthy: map[string]bool{"west": true}})
	if reason != "every wave has passed" || finished.Status != StatusCompleted {
		t.Fatalf("finished = %q %s", reason, finished.Status)
	}
}

func TestApprovalGateAndPauseRequireWhatTheySay(t *testing.T) {
	rollout, err := New("roll-2", "ledger", "1.2.0", "ada", []Wave{
		{Name: "canary", Environments: []string{"east"}, Gate: GateApproval},
	})
	if err != nil {
		t.Fatal(err)
	}
	blocked, reason := Advance(rollout, Facts{})
	if reason != "approval gate is missing" {
		t.Fatalf("approval = %q", reason)
	}
	started, _ := Advance(blocked, Facts{Approved: true})
	paused, reason := Pause(started, "ada", false)
	if reason != "confirmation is required" || paused.Status != StatusRunning {
		t.Fatalf("unconfirmed pause = %q %s", reason, paused.Status)
	}
	paused, reason = Pause(started, "ada", true)
	if reason != "rollout paused" || paused.Status != StatusPaused {
		t.Fatalf("pause = %q %s", reason, paused.Status)
	}
	held, reason := Advance(paused, Facts{Approved: true})
	if reason != "rollout is paused" || held.Status != StatusPaused {
		t.Fatalf("paused advance = %q %s", reason, held.Status)
	}
}

package domain

import "testing"

func TestActionRulesRejectTheWrongRoleAndABlocker(t *testing.T) {
	denied := Decide("execute_plan", ActionFacts{Roles: []string{"Developer"}, WindowOpen: true, ApprovalsRecorded: 2, ApprovalsRequired: 2, AllArtifactsSigned: true})
	if denied.Allowed || denied.AuditEvent != "plan.execution_dispatched" || denied.Reason == "" {
		t.Fatalf("wrong role = %#v", denied)
	}
	blocked := Decide("execute_plan", ActionFacts{Roles: []string{"Operator"}, WindowOpen: false, ApprovalsRecorded: 2, ApprovalsRequired: 2, AllArtifactsSigned: true})
	if blocked.Allowed || blocked.Reason != "maintenance window is closed" {
		t.Fatalf("blocker = %#v", blocked)
	}
	allowed := Decide("cancel_packaging_run", ActionFacts{Roles: []string{"developer"}, RunKnown: true, RunPhase: "queued"})
	if !allowed.Allowed || allowed.AuditEvent != "packaging_run.canceled" {
		t.Fatalf("cancel = %#v", allowed)
	}
	unknown := Decide("not-an-action", ActionFacts{Roles: []string{"Operator"}})
	if unknown.Known || unknown.Allowed {
		t.Fatalf("unknown = %#v", unknown)
	}
}

func TestDownloadStaysBlockedUntilTheArtifactIsKnownSigned(t *testing.T) {
	decision := Decide("download_bundle", ActionFacts{Roles: []string{"Operator"}})
	if decision.Allowed || decision.Reason != "artifact is unsigned" {
		t.Fatalf("unknown artifact = %#v", decision)
	}
	signed := Decide("download_bundle", ActionFacts{Roles: []string{"Profile Owner"}, ArtifactKnown: true, ArtifactStatus: "SIGNED"})
	if !signed.Allowed {
		t.Fatalf("signed download = %#v", signed)
	}
}

func TestAssistantWritesHaveConcreteActionRules(t *testing.T) {
	for _, test := range []struct{ action, role, status string }{
		{"add_eval_case", "Operator", ""}, {"add_eval_case", "Developer", ""},
		{"approve_change_request", "Release Approver", "PENDING_APPROVALS"},
		{"reject_change_request", "Security Admin", "PENDING_APPROVALS"},
	} {
		allowed := Decide(test.action, ActionFacts{Roles: []string{test.role}, ChangeStatus: test.status})
		if !allowed.Known || !allowed.Allowed {
			t.Fatalf("%s has no executable rule: %+v", test.action, allowed)
		}
		denied := Decide(test.action, ActionFacts{Roles: []string{"unrelated"}, ChangeStatus: test.status})
		if denied.Allowed {
			t.Fatalf("%s bypassed its role", test.action)
		}
	}
	if Decide("reject_change_request", ActionFacts{Roles: []string{"Security Admin"}, ChangeStatus: "REJECTED"}).Allowed {
		t.Fatal("terminal request admitted for rejection")
	}
	if Decide("approve_change_request", ActionFacts{Roles: []string{"Release Approver"}}).Allowed {
		t.Fatal("unknown change request admitted for approval")
	}
}

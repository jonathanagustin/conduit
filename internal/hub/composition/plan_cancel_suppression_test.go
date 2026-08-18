package composition

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// POST /v1/plans/{planID}/cancel can plant a suppression, and a suppression is
// the same durable Environment-scoped mute that
// POST /v1/environments/{environmentID}/suppressions gates at Operate. The
// cancel route's governed branch admits the Change Request's author by identity
// and asks for no operation at all, so an author who only ever held
// ProposeChange reached that effect at a lower bar than the route that exists
// for it (#150).
//
// Everything below drives the assembled Hub -- NewHubHandler, guard and all --
// rather than the facade's handler, because the register in authorization.go
// and the guard around it are part of what answers, and a test that skipped
// them would not be measuring the surface a caller reaches.
//
// Measured against planted mutants rather than assumed: deleting the Operate
// check, moving it into the ungoverned branch alone, applying it to every
// cancellation, answering 404 instead of 403, and answering a bare "forbidden"
// are each caught by one of the tests below. Moving it into the governed branch
// alone is NOT caught, and that is a property of the code rather than a gap
// here: the ungoverned branch already requires Operate on the same Environment
// for every cancellation (#155), so a suppression-specific check there can
// never be the thing that refuses. The mutant is equivalent, and
// TestAnUngovernedCancellationKeepsItsOwnBar is what would notice if that
// branch ever stopped charging its own bar.

const (
	cancelEnvironment = "cancel-bar-use2"
	cancelProduct     = "c.example:ledger"
)

// cancelHub is the Hub with two principals: an author who holds ProposeChange
// on the Environment and nothing else, and an operator who holds ProposeChange
// and Operate. Both must be able to author, because the governed branch admits
// only the Change Request's own author -- an operator who is not the author is
// refused whatever they hold, which is the branch's existing rule and not this
// change's business.
func cancelHub(t *testing.T) (http.Handler, controlplane.Repository) {
	t.Helper()
	repository := controlplane.NewMemoryRepository()
	service := controlplane.NewGovernanceService(repository)
	for _, member := range []struct {
		team, principal string
		operations      []string
	}{
		{"cancel-authors", "author", []string{controlplane.OperationReadResource, controlplane.OperationProposeChange}},
		{"cancel-operators", "operator", []string{
			controlplane.OperationReadResource, controlplane.OperationProposeChange, controlplane.OperationOperate,
		}},
	} {
		if _, err := service.PutTeam(t.Context(), controlplane.Team{
			Name: member.team, DisplayName: member.team,
			AdminUsers: []string{"root"}, MemberUsers: []string{member.principal},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.PutGrant(t.Context(), controlplane.ResourceGrant{
			ResourceRID: "environment/" + cancelEnvironment, Team: member.team, Operations: member.operations,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// A policy that asks for a review, so the Change Request lands in
	// PENDING_APPROVALS. With no enabled policy at all a proposal is stored in
	// ERROR, which governance.Cancel refuses -- the fixture would then be
	// measuring the policy engine rather than the cancel route's bar.
	if _, err := service.PutPolicy(t.Context(), controlplane.ApprovalPolicy{
		ID: "cancel-bar-review", DisplayName: "Cancel bar review", Enabled: true,
		Expression: `require_team("cancel-reviewers", "review this change")`,
	}); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHub(repository, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		// Named, so the "local-developer" bootstrap shortcut is off and every
		// decision below comes from a grant.
		BootstrapAdministrators: []string{"root"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler, repository
}

// proposePlan creates a governed Plan through POST /v1/plans, so the Change
// Request's author is the principal that asked for it -- the same way a real
// author acquires the standing the cancel route reads.
// The generation distinguishes one proposal from the next: a Plan id is a
// digest of its desired state, so two identical proposals are the same Plan.
func proposePlan(t *testing.T, handler http.Handler, subject, generation string) controlplane.PlanRecord {
	t.Helper()
	body, err := json.Marshal(stateWithGeneration(generation))
	if err != nil {
		t.Fatal(err)
	}
	response := driveAs(t, handler, http.MethodPost, "/v1/plans", subject, string(body))
	if response.Code != http.StatusCreated {
		t.Fatalf("proposing a Plan as %s: %d %s", subject, response.Code, response.Body.String())
	}
	var record controlplane.PlanRecord
	if err := json.Unmarshal(response.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.ChangeRequestID == "" {
		t.Fatalf("this fixture is meant to be governed; the Plan has no Change Request: %#v", record)
	}
	return record
}

func stateWithGeneration(generation string) controlplane.DesiredState {
	state := desiredState(cancelEnvironment, cancelProduct)
	state.Environment.Generation = generation
	return state
}

// cancelBody is the cancellation request, with and without the suppression.
func cancelBody(createSuppression bool) string {
	if createSuppression {
		return `{"reason":"incident containment","createSuppression":true}`
	}
	return `{"reason":"incident containment"}`
}

func suppressionsIn(t *testing.T, repository controlplane.Repository) []controlplane.SuppressionRecord {
	t.Helper()
	records, err := repository.ListSuppressions(t.Context(), cancelEnvironment)
	if err != nil {
		t.Fatal(err)
	}
	return records
}

// TestAnAuthorCannotMintASuppressionThroughCancellation is the defect: an
// author holding only ProposeChange planted an Environment suppression by
// asking for one alongside a cancellation.
func TestAnAuthorCannotMintASuppressionThroughCancellation(t *testing.T) {
	t.Parallel()
	handler, repository := cancelHub(t)
	record := proposePlan(t, handler, "author", "0123456789abcde1")

	response := driveAs(t, handler,
		http.MethodPost, "/v1/plans/"+record.ID+"/cancel", "author", cancelBody(true))

	// 403 exactly, and not 404: the Plan is one this author may read and may
	// cancel, so answering "no such Plan" would name the wrong fault and send
	// the author looking for a Plan that is right there.
	if response.Code != http.StatusForbidden {
		t.Fatalf("an author holding only ProposeChange minted a suppression: %d %s",
			response.Code, response.Body.String())
	}
	// The refusal must say what was refused. "forbidden" alone reads as the
	// author check above it, which this caller passed.
	if !strings.Contains(response.Body.String(), "suppression") {
		t.Errorf("the refusal does not name the suppression as the reason: %s", response.Body.String())
	}
	if stored := suppressionsIn(t, repository); len(stored) != 0 {
		t.Fatalf("the refused suppression still landed: %#v", stored)
	}
	// And the refusal has to precede the cancellation rather than follow it: a
	// check placed after CancelPlan would leave the Plan cancelled and answer
	// 403, which is a worse outcome than either allowing or refusing.
	plan, err := repository.GetPlan(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status == controlplane.PlanCancelled || plan.Cancellation != nil {
		t.Fatalf("the refused request cancelled the Plan anyway: status=%s cancellation=%#v",
			plan.Status, plan.Cancellation)
	}
}

// TestAnAuthorMayStillCancelPlainly is the half the narrow option keeps. An
// author entitled to cancel their own Change Request in an Environment they
// hold nothing on must still be able to, so the bar is on the suppression and
// not on the cancellation.
func TestAnAuthorMayStillCancelPlainly(t *testing.T) {
	t.Parallel()
	handler, repository := cancelHub(t)
	record := proposePlan(t, handler, "author", "0123456789abcde2")

	response := driveAs(t, handler,
		http.MethodPost, "/v1/plans/"+record.ID+"/cancel", "author", cancelBody(false))

	if response.Code != http.StatusOK {
		t.Fatalf("an author could not cancel their own Change Request's Plan: %d %s",
			response.Code, response.Body.String())
	}
	plan, err := repository.GetPlan(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != controlplane.PlanCancelled || plan.Cancellation == nil {
		t.Fatalf("the cancellation was not recorded: status=%s cancellation=%#v", plan.Status, plan.Cancellation)
	}
}

// TestOperateMintsTheSuppressionThroughCancellation is the other half: the bar
// admits whoever clears it, so the route keeps the capability it had for the
// principals it was meant for.
func TestOperateMintsTheSuppressionThroughCancellation(t *testing.T) {
	t.Parallel()
	handler, repository := cancelHub(t)

	plain := proposePlan(t, handler, "operator", "0123456789abcde3")
	if response := driveAs(t, handler,
		http.MethodPost, "/v1/plans/"+plain.ID+"/cancel", "operator", cancelBody(false)); response.Code != http.StatusOK {
		t.Fatalf("a principal holding Operate could not cancel plainly: %d %s", response.Code, response.Body.String())
	}

	suppressing := proposePlan(t, handler, "operator", "0123456789abcde4")
	response := driveAs(t, handler,
		http.MethodPost, "/v1/plans/"+suppressing.ID+"/cancel", "operator", cancelBody(true))
	if response.Code != http.StatusOK {
		t.Fatalf("a principal holding Operate could not cancel with a suppression: %d %s",
			response.Code, response.Body.String())
	}
	stored := suppressionsIn(t, repository)
	if len(stored) != 1 || stored[0].Source != controlplane.SuppressionManual || stored[0].CreatedBy != "operator" {
		t.Fatalf("the permitted suppression did not land: %#v", stored)
	}
}

// TestAnUngovernedCancellationKeepsItsOwnBar drives the other branch. A Plan
// with no Change Request answers to Operate for every cancellation (#155), so
// the suppression bar must take nothing away from a principal who holds it and
// must not become the only thing standing in front of one who does not.
func TestAnUngovernedCancellationKeepsItsOwnBar(t *testing.T) {
	t.Parallel()
	handler, repository := cancelHub(t)
	lifecycle := controlplane.NewLifecycle(repository)

	refused, err := lifecycle.CreatePlan(t.Context(), stateWithGeneration("0123456789abcde6"))
	if err != nil {
		t.Fatal(err)
	}
	if refused.ChangeRequestID != "" {
		t.Fatalf("this fixture is meant to be ungoverned: %#v", refused)
	}
	if response := driveAs(t, handler,
		http.MethodPost, "/v1/plans/"+refused.ID+"/cancel", "author", cancelBody(true)); response.Code != http.StatusForbidden {
		t.Fatalf("an author holding no Operate cancelled an ungoverned Plan with a suppression: %d %s",
			response.Code, response.Body.String())
	}

	allowed, err := lifecycle.CreatePlan(t.Context(), stateWithGeneration("0123456789abcde7"))
	if err != nil {
		t.Fatal(err)
	}
	if response := driveAs(t, handler,
		http.MethodPost, "/v1/plans/"+allowed.ID+"/cancel", "operator", cancelBody(true)); response.Code != http.StatusOK {
		t.Fatalf("a principal holding Operate could not cancel an ungoverned Plan with a suppression: %d %s",
			response.Code, response.Body.String())
	}
	if stored := suppressionsIn(t, repository); len(stored) != 1 {
		t.Fatalf("the ungoverned branch's suppression did not land exactly once: %#v", stored)
	}
}

// TestTheDirectSuppressionRouteKeepsItsBar is the measurement the asymmetry was
// stated against, kept runnable. If this route's own check ever relaxes, the
// cancel bar above is guarding a difference that no longer exists and the two
// have to be reconciled again rather than left to drift.
func TestTheDirectSuppressionRouteKeepsItsBar(t *testing.T) {
	t.Parallel()
	handler, _ := cancelHub(t)
	const body = `{"reason":"incident containment"}`
	target := "/v1/environments/" + cancelEnvironment + "/suppressions"

	if response := driveAs(t, handler, http.MethodPost, target, "author", body); response.Code != http.StatusForbidden {
		t.Fatalf("a principal holding only ProposeChange created a suppression directly: %d %s",
			response.Code, response.Body.String())
	}
	if response := driveAs(t, handler, http.MethodPost, target, "operator", body); response.Code != http.StatusCreated {
		t.Fatalf("a principal holding Operate could not create a suppression directly: %d %s",
			response.Code, response.Body.String())
	}
}

// TestTheCancelRouteStillRefusesAStranger keeps the branch's existing rule
// under test beside the new one. Without it, a "fix" that replaced the author
// check with the Operate check would pass everything above while handing every
// governed Plan to anyone holding the Environment.
func TestTheCancelRouteStillRefusesAStranger(t *testing.T) {
	t.Parallel()
	handler, repository := cancelHub(t)
	record := proposePlan(t, handler, "author", "0123456789abcde5")

	response := driveAs(t, handler,
		http.MethodPost, "/v1/plans/"+record.ID+"/cancel", "operator", cancelBody(false))

	if response.Code != http.StatusForbidden {
		t.Fatalf("a principal who is not the author cancelled a governed Plan: %d %s",
			response.Code, response.Body.String())
	}
	plan, err := repository.GetPlan(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status == controlplane.PlanCancelled {
		t.Fatalf("the refused cancellation still landed: %#v", plan)
	}
}

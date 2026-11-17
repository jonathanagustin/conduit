package application

import (
	"context"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/appgen/domain"
)

const acceptedPlanText = "Serve a Go HTTP page at / and a status route at /status."

func planInput() Input {
	input := generateInput()
	input.PlanRevisionID = "ps_accepted"
	input.AcceptedPlan = acceptedPlanText
	input.PlanDigest = domain.AcceptedPlanDigest(acceptedPlanText)
	input.Conversation = []domain.ConversationMessage{{Role: "operator", Text: "Add a /status route."}}
	return input
}

// htmlInstead is a reply that builds and is healthy but is a different
// application: a static page where the earlier reply was a Go server.
func htmlInstead(t *testing.T) domain.Spec {
	t.Helper()
	spec := recordedSpec(t)
	spec.Files = []domain.SourceFile{{Path: "index.html", Language: "html", Content: "<!doctype html><title>Hello</title>"}}
	if _, _, ok := domain.ShapeOf(spec); !ok {
		t.Fatal("fixture is not an allowed shape")
	}
	return spec
}

// Every exchange of a plan-bound generation is asked for under the same
// accepted plan, and the stored result and each audit record name it.
func TestRepairKeepsTheAcceptedPlanBinding(t *testing.T) {
	gen := &scriptedGen{replies: []scriptedReply{{spec: recordedSpec(t)}}}
	audit := &auditLog{}
	store := &Memory{}
	service := Service{Generator: gen, Sandbox: &scriptedSandbox{builds: []BuildResult{buildFails, buildPasses}, health: healthy}, Audit: audit, Store: store, RepairAttempts: 2}
	input := planInput()
	result, err := service.Generate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != "" || len(gen.requests) != 2 {
		t.Fatalf("result = %q after %d model calls", result.Error, len(gen.requests))
	}
	for index, request := range gen.requests {
		if request.PlanRevisionID != input.PlanRevisionID || request.PlanDigest != input.PlanDigest || request.AcceptedPlan != acceptedPlanText || len(request.Conversation) != 1 {
			t.Fatalf("request %d lost the plan binding: %+v", index+1, request)
		}
		prompt := domain.Prompt(request)
		if !strings.Contains(prompt, acceptedPlanText) || !strings.Contains(prompt, "Add a /status route.") {
			t.Fatalf("prompt %d does not carry the accepted plan and its conversation", index+1)
		}
	}
	repairPrompt := domain.Prompt(gen.requests[1])
	if !strings.Contains(repairPrompt, "must still implement accepted plan revision ps_accepted digest "+input.PlanDigest) {
		t.Fatal("the repair prompt does not say the accepted plan still governs")
	}
	if strings.Index(repairPrompt, acceptedPlanText) > strings.Index(repairPrompt, "<<<UNTRUSTED_TOOL_OUTPUT>>>") {
		t.Fatal("the accepted plan is not ahead of the untrusted failure block")
	}
	if result.PlanRevisionID != input.PlanRevisionID || result.PlanDigest != input.PlanDigest {
		t.Fatalf("result binding = %q %q", result.PlanRevisionID, result.PlanDigest)
	}
	for _, event := range audit.events {
		if event.PlanRevisionID != input.PlanRevisionID || event.PlanDigest != input.PlanDigest {
			t.Fatalf("audit for attempt %d does not name the plan: %+v", event.Attempt, event)
		}
	}
	stored, _, err := store.Get(context.Background(), result.ID)
	if err != nil || stored.PlanRevisionID != input.PlanRevisionID || stored.PlanDigest != input.PlanDigest {
		t.Fatalf("stored binding = %q %q %v", stored.PlanRevisionID, stored.PlanDigest, err)
	}
	unbound, err := Service{Generator: &scriptedGen{replies: []scriptedReply{{spec: recordedSpec(t)}}}, Sandbox: &scriptedSandbox{builds: []BuildResult{buildPasses}, health: healthy}}.
		Generate(context.Background(), generateInput())
	if err != nil || unbound.PlanRevisionID != "" || unbound.PlanDigest != "" {
		t.Fatalf("a generation with no plan claims one: %q %q %v", unbound.PlanRevisionID, unbound.PlanDigest, err)
	}
}

// A repaired reply that would build and pass health, but is a different
// application from the one the plan was accepted for, is refused. It is not
// built, and it does not become the result.
func TestARepairedReplyThatDriftsFromTheAcceptedPlanIsRefused(t *testing.T) {
	original := recordedSpec(t)
	fewerRoutes := recordedSpec(t)
	fewerRoutes.Routes = fewerRoutes.Routes[:1]
	otherPort := recordedSpec(t)
	otherPort.Port = original.Port + 1
	for name, tc := range map[string]struct {
		drifted domain.Spec
		reason  string
	}{
		"another language": {htmlInstead(t), "source shape changed from go to html"},
		"a dropped route":  {fewerRoutes, "routes changed"},
		"another port":     {otherPort, "port changed"},
	} {
		// The first reply fails to build. The drifted reply follows, and the
		// sandbox would pass anything from then on.
		gen := &scriptedGen{replies: []scriptedReply{{spec: original}, {spec: tc.drifted}}}
		sandbox := &scriptedSandbox{builds: []BuildResult{buildFails, buildPasses}, health: healthy}
		service := Service{Generator: gen, Sandbox: sandbox, Store: &Memory{}, RepairAttempts: 1}
		result, err := service.Generate(context.Background(), planInput())
		if err != nil {
			t.Fatal(err)
		}
		if result.Error == "" || !strings.Contains(result.Error, "drifted from the accepted plan revision") || !strings.Contains(result.Error, tc.reason) {
			t.Errorf("%s: result error = %q, want a drift refusal naming %q", name, result.Error, tc.reason)
		}
		if gateOutcome(result, GateSchema) != domain.OutcomeFail || gateOutcome(result, GateBuild) != domain.OutcomeSkip {
			t.Errorf("%s: gates = %+v, want the reply stopped before the build", name, result.Gates)
		}
		if sandbox.calls != 1 {
			t.Errorf("%s: the drifted reply was built (%d builds)", name, sandbox.calls)
		}
		if result.RunID != "" || result.PackagingSource != "" {
			t.Errorf("%s: the drifted reply was handed to packaging", name)
		}
	}

	// The model is told, and a reply that returns to the surface is accepted.
	gen := &scriptedGen{replies: []scriptedReply{{spec: original}, {spec: htmlInstead(t)}, {spec: original}}}
	sandbox := &scriptedSandbox{builds: []BuildResult{buildFails, buildPasses}, health: healthy}
	result, err := Service{Generator: gen, Sandbox: sandbox, RepairAttempts: 2}.Generate(context.Background(), planInput())
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != "" || len(result.Attempts) != 3 || !strings.Contains(result.Attempts[1].Error, "drifted") {
		t.Fatalf("result = %q attempts = %+v", result.Error, result.Attempts)
	}
	if repair := gen.requests[2].Repair; repair == nil || !strings.Contains(repair.Output, "source shape changed from go to html") {
		t.Fatalf("the model was not told why the reply was refused: %+v", gen.requests[2].Repair)
	}

	// With no accepted plan there is no reviewed contract to hold a reply to.
	gen = &scriptedGen{replies: []scriptedReply{{spec: original}, {spec: htmlInstead(t)}}}
	unbound, err := Service{Generator: gen, Sandbox: &scriptedSandbox{builds: []BuildResult{buildFails, buildPasses}, health: healthy}, RepairAttempts: 1}.
		Generate(context.Background(), generateInput())
	if err != nil || unbound.Error != "" {
		t.Fatalf("a generation with no plan was held to a surface: %q %v", unbound.Error, err)
	}
}

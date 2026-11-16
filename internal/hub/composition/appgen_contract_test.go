package composition

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/appgen/application"
	appdomain "github.com/jonathanagustin/conduit/internal/appgen/domain"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	planningapp "github.com/jonathanagustin/conduit/internal/planning/application"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
)

func TestAppgenComputePassesReviewRequirementAndRefusesDigestMismatch(t *testing.T) {
	const (
		problem     = "Create a service catalog portal"
		appName     = "hello-http"
		requirement = "also expose a catalog export at /catalog-export"
	)
	service := planningapp.NewService(nil)
	created, err := service.CreateWithInput(t.Context(), planningdomain.CreateInput{
		Profile: planningdomain.ProfileAppgen, ProblemStatement: problem, AppName: appName,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Turn(t.Context(), created.ID, ""); err != nil {
		t.Fatal(err)
	}
	reviewed, err := service.Turn(t.Context(), created.ID, requirement)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(reviewed.Intake.ProblemStatement, requirement) {
		t.Fatal("review requirement was written into the original problem statement")
	}
	accepted, err := service.Accept(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(accepted.Binding.Plan.Text, requirement) {
		t.Fatalf("accepted plan does not record the review requirement: %s", accepted.Binding.Plan.Text)
	}
	if appdomain.AcceptedPlanDigest(accepted.Binding.Plan.Text) != accepted.Binding.Plan.SHA256 {
		t.Fatal("generation digest does not match the planning binding hash")
	}

	captured := &captureGenerator{}
	computer := appgenPlanningComputer(application.Service{Generator: captured}, nil, controlplane.Principal{Username: "alice"}, func(string) string { return "" })
	if err := computer(t.Context(), accepted); err == nil {
		t.Fatal("expected generation to stop after the input was captured")
	}
	if captured.calls != 1 {
		t.Fatalf("generator calls = %d, want 1", captured.calls)
	}
	request := captured.request
	if request.Description != problem || strings.Contains(request.Description, requirement) {
		t.Fatalf("description = %q", request.Description)
	}
	if request.PlanRevisionID != accepted.ID || request.PlanDigest != accepted.Binding.Plan.SHA256 {
		t.Fatalf("revision = %s digest = %s", request.PlanRevisionID, request.PlanDigest)
	}
	if !strings.Contains(request.AcceptedPlan, requirement) {
		t.Fatalf("accepted plan text lost the review requirement: %s", request.AcceptedPlan)
	}
	found := false
	for _, turn := range request.Conversation {
		if turn.Role == planningdomain.RoleOperator && strings.Contains(turn.Text, requirement) {
			found = true
		}
	}
	if !found {
		t.Fatalf("planning conversation lost the review requirement: %+v", request.Conversation)
	}
	if !strings.Contains(appdomain.Prompt(request), requirement) {
		t.Fatal("generation prompt does not include the review requirement")
	}

	mismatched := planningdomain.CloneSession(accepted)
	mismatched.Binding.Plan.SHA256 = strings.Repeat("ab", 32)
	blocked := &captureGenerator{}
	refused := appgenPlanningComputer(application.Service{Generator: blocked}, nil, controlplane.Principal{Username: "alice"}, func(string) string { return "" })
	err = refused(t.Context(), mismatched)
	if !errors.Is(err, planningdomain.ErrPlanDigestMismatch) {
		t.Fatalf("hash mismatch error = %v", err)
	}
	if blocked.calls != 0 {
		t.Fatalf("generator ran after a hash mismatch: %d", blocked.calls)
	}
}

type captureGenerator struct {
	request appdomain.Request
	calls   int
}

func (generator *captureGenerator) Generate(_ context.Context, request appdomain.Request) (appdomain.Spec, appdomain.Provenance, error) {
	generator.calls++
	generator.request = request
	return appdomain.Spec{}, appdomain.Provenance{}, errors.New("stop after capturing the generation input")
}

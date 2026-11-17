package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

func TestCreatePersistsIntakeAndStartsWithoutCompute(t *testing.T) {
	t.Parallel()
	service := NewService(nil)
	session, err := service.Create(context.Background(), "https://github.com/org/app", "main")
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != domain.StatusStarted {
		t.Fatalf("status = %q", session.Status)
	}
	if session.Profile.Name != domain.ProfileOnboard {
		t.Fatalf("profile = %+v", session.Profile)
	}
	if session.Intake.RepoURL != "https://github.com/org/app" || session.Intake.Ref != "main" {
		t.Fatalf("intake = %+v", session.Intake)
	}
	if session.Binding != nil || session.Plan != nil {
		t.Fatal("create recorded a plan or binding")
	}
	if _, err := service.Compute(context.Background(), session.ID); !errors.Is(err, domain.ErrComputeBeforeAccept) {
		t.Fatalf("compute before accept: %v", err)
	}
}

func TestTurnWithoutPlannerIsRefused(t *testing.T) {
	t.Parallel()
	service := NewService(nil)
	session, err := service.Create(context.Background(), "https://github.com/org/app", "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Turn(context.Background(), session.ID, "inspect this"); !errors.Is(err, domain.ErrModelNotConfigured) {
		t.Fatalf("turn = %v", err)
	}
}

func TestAcceptRecordsBindingAndRefusesComputeUntilThen(t *testing.T) {
	t.Parallel()
	frozen := time.Date(2026, 11, 14, 16, 0, 0, 0, time.UTC)
	service := NewService(scriptedPlanner{result: &TurnResult{
		Messages:        []domain.Message{{Role: domain.RoleModel, Text: "Plan follows."}},
		Inspection:      &domain.Inspection{Language: "TypeScript", Kind: "static", ToolCalls: []string{"inspect_repo"}},
		NeedsInput:      []domain.Question{{ID: "sso", Name: "sso", Label: "Does this need SSO?", Type: domain.QuestionBoolean, Required: true}},
		Plan:            &domain.Plan{Text: "Package the static site.", SHA256: "abc"},
		ResolvedCommit:  "0123456789abcdef0123456789abcdef01234567",
		Model:           "recorded-model",
		ConfiguredModel: "configured-alias", ProviderModel: "provider-version",
		Profile: domain.ProfileRef{Name: domain.ProfileOnboard, Version: "1", Hash: "deadbeef"},
	}})
	service.now = func() time.Time { return frozen }
	session, err := service.Create(context.Background(), "https://github.com/org/app", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Accept(context.Background(), session.ID); !errors.Is(err, domain.ErrPlanRequired) {
		t.Fatalf("accept before plan: %v", err)
	}
	session, err = service.Turn(context.Background(), session.ID, "please inspect")
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != domain.StatusNeedsInput {
		t.Fatalf("status after turn = %q", session.Status)
	}
	if _, err := service.Accept(context.Background(), session.ID); !errors.Is(err, domain.ErrNeedsInputPending) {
		t.Fatalf("accept with pending input: %v", err)
	}
	session, err = service.Answer(context.Background(), session.ID, []domain.Answer{{ID: "sso", Value: "no"}})
	if err != nil {
		t.Fatal(err)
	}
	if session.Status == domain.StatusReady || session.Plan != nil {
		t.Fatal("answer changes retained a stale draft plan")
	}
	if _, err := service.Accept(context.Background(), session.ID); !errors.Is(err, domain.ErrPlanRequired) {
		t.Fatalf("accept before replanning: %v", err)
	}
	session, err = service.Turn(context.Background(), session.ID, "rebuild the plan using these answers")
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != domain.StatusReady {
		t.Fatalf("status after replanning = %q", session.Status)
	}
	if _, err := service.Compute(context.Background(), session.ID); !errors.Is(err, domain.ErrComputeBeforeAccept) {
		t.Fatalf("compute before accept: %v", err)
	}
	session, err = service.Accept(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != domain.StatusAccepted || session.Binding == nil {
		t.Fatalf("accepted = %+v", session)
	}
	binding := session.Binding
	if binding.Profile.Version != "1" || binding.Profile.Hash != "deadbeef" {
		t.Fatalf("profile = %+v", binding.Profile)
	}
	if binding.Intake.RepoURL != session.Intake.RepoURL || binding.Intake.Ref != session.Intake.Ref || binding.Intake.Message != "please inspect" {
		t.Fatalf("intake = %+v", binding.Intake)
	}
	if binding.ResolvedCommit != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("commit = %q", binding.ResolvedCommit)
	}
	if binding.ConfiguredModel != "configured-alias" || binding.ProviderModel != "provider-version" {
		t.Fatalf("separate model ids missing from binding: %+v", binding)
	}
	if binding.Model != "recorded-model" || binding.Plan.Text != "Package the static site." {
		t.Fatalf("model/plan = %+v", binding)
	}
	if !binding.AcceptedAt.Equal(frozen) {
		t.Fatalf("acceptedAt = %s", binding.AcceptedAt)
	}
	if _, err := service.Compute(context.Background(), session.ID); !errors.Is(err, domain.ErrComputeNotConfigured) {
		t.Fatalf("compute after accept: %v", err)
	}
}

func TestTurnRecordsOneInspectionEventPerToolCall(t *testing.T) {
	t.Parallel()
	first := &domain.Inspection{Language: "javascript", ToolCalls: []string{"inspect_repo"}}
	second := &domain.Inspection{Language: "javascript", ToolCalls: []string{"inspect_repo", "read_file"}}
	service := NewService(scriptedPlanner{result: &TurnResult{
		Inspection:       second,
		InspectionEvents: []*domain.Inspection{first, second},
		ResolvedCommit:   "0123456789abcdef0123456789abcdef01234567",
		Model:            "recorded-model",
	}})
	session, err := service.Create(context.Background(), "https://github.com/org/app", "main")
	if err != nil {
		t.Fatal(err)
	}
	session, err = service.Turn(context.Background(), session.ID, "inspect this")
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != domain.StatusPlanning {
		t.Fatalf("status = %q", session.Status)
	}
	if session.Inspection == nil || len(session.Inspection.ToolCalls) != 2 {
		t.Fatalf("inspection = %+v", session.Inspection)
	}
	var inspections int
	var tools [][]string
	for _, event := range session.Events {
		if event.Type == domain.EventInspection {
			inspections++
			if event.Inspection == nil {
				t.Fatal("inspection event is empty")
			}
			tools = append(tools, append([]string(nil), event.Inspection.ToolCalls...))
		}
	}
	if inspections != 2 {
		t.Fatalf("inspection events = %d, want 2: %+v", inspections, session.Events)
	}
	if len(tools[0]) != 1 || tools[0][0] != "inspect_repo" || len(tools[1]) != 2 || tools[1][1] != "read_file" {
		t.Fatalf("tool calls = %#v", tools)
	}
}

type scriptedPlanner struct{ result *TurnResult }

func (planner scriptedPlanner) Turn(context.Context, *domain.Session, string) (*TurnResult, error) {
	return planner.result, nil
}

type sequencePlanner struct {
	turns []*TurnResult
	idx   int
}

func (p *sequencePlanner) Turn(context.Context, *domain.Session, string) (*TurnResult, error) {
	if p.idx >= len(p.turns) {
		return p.turns[len(p.turns)-1], nil
	}
	res := p.turns[p.idx]
	p.idx++
	return res, nil
}

func TestMultiTurnKeepsEarlierMessagesAfterPlanArrivesAndDefaultModelLabel(t *testing.T) {
	t.Parallel()
	planner := &sequencePlanner{
		turns: []*TurnResult{
			{
				Messages:       []domain.Message{{Role: domain.RoleModel, Text: "Inspected repository: TypeScript static app."}},
				Inspection:     &domain.Inspection{Language: "TypeScript", Kind: "static", ToolCalls: []string{"inspect_repo"}},
				NeedsInput:     []domain.Question{{ID: "port", Name: "port", Label: "Which port?", Type: domain.QuestionString, Required: true}},
				ResolvedCommit: "0123456789abcdef0123456789abcdef01234567",
			},
			{
				Messages: []domain.Message{{Role: domain.RoleModel, Text: "Generated initial packaging plan."}},
				Plan:     &domain.Plan{Text: `{"summary":"Initial plan","baseImage":"node:20-slim","buildSteps":["npm ci"],"ports":[8080],"networkAllow":[],"sso":{"required":false,"notes":""},"risks":[]}`},
			},
			{
				Messages: []domain.Message{{Role: domain.RoleModel, Text: "Updated plan to expose port 3000."}},
				Plan:     &domain.Plan{Text: `{"summary":"Revised plan","baseImage":"node:20-slim","buildSteps":["npm ci"],"ports":[3000],"networkAllow":[],"sso":{"required":false,"notes":""},"risks":[]}`},
			},
		},
	}
	service := NewService(planner)
	ctx := context.Background()
	session, err := service.Create(ctx, "https://github.com/org/app", "main")
	if err != nil {
		t.Fatal(err)
	}
	session, err = service.Turn(ctx, session.ID, "First operator message")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Answer(ctx, session.ID, []domain.Answer{{ID: "port", Value: "8080"}}); err != nil {
		t.Fatal(err)
	}
	session, err = service.Turn(ctx, session.ID, "Second operator message")
	if err != nil {
		t.Fatal(err)
	}
	session, err = service.Turn(ctx, session.ID, "Third operator message")
	if err != nil {
		t.Fatal(err)
	}
	wantTexts := []string{
		"First operator message",
		"Inspected repository: TypeScript static app.",
		"Second operator message",
		"Generated initial packaging plan.",
		"Third operator message",
		"Updated plan to expose port 3000.",
	}
	if len(session.Messages) != len(wantTexts) {
		t.Fatalf("len(messages) = %d, want %d: %+v", len(session.Messages), len(wantTexts), session.Messages)
	}
	for i, want := range wantTexts {
		if session.Messages[i].Text != want {
			t.Fatalf("messages[%d] = %q, want %q", i, session.Messages[i].Text, want)
		}
	}
	if session.Model != domain.DefaultModelLabel {
		t.Fatalf("session.Model = %q, want %q", session.Model, domain.DefaultModelLabel)
	}
	accepted, err := service.Accept(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Binding.Model != domain.DefaultModelLabel {
		t.Fatalf("binding.Model = %q, want %q", accepted.Binding.Model, domain.DefaultModelLabel)
	}
}

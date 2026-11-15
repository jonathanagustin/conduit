package application

import (
	"context"
	"errors"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
	"strings"
	"testing"
)

func TestRejectedAnswerReopensOptionalQuestionAndInvalidatesDraft(t *testing.T) {
	result := &TurnResult{NeedsInput: []domain.Question{{ID: "port", Name: "Port", Type: domain.QuestionString, Required: false}}, ResolvedCommit: strings.Repeat("a", 40), Plan: &domain.Plan{Text: "Old reviewed draft"}}
	service := NewService(planningFunc(func(context.Context, *domain.Session, string) (*TurnResult, error) { return result, nil }))
	session, err := service.Create(t.Context(), "https://github.com/org/app", "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Turn(t.Context(), session.ID, "inspect"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Answer(t.Context(), session.ID, []domain.Answer{{ID: "port", Value: "8080"}}); err != nil {
		t.Fatal(err)
	}
	result = &TurnResult{NeedsInput: []domain.Question{{ID: "port", Name: "Port", Type: domain.QuestionString, ValidationError: "Choose another port."}}, Plan: &domain.Plan{Text: "Must not remain eligible"}}
	rejected, err := service.Turn(t.Context(), session.ID, "review")
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Plan != nil || len(rejected.Answers) != 0 || rejected.Status != domain.StatusNeedsInput {
		t.Fatalf("rejected answer kept a draft or answer: %+v", rejected)
	}
	if _, err := service.Accept(t.Context(), session.ID); !errors.Is(err, domain.ErrPlanRequired) {
		t.Fatalf("rejected draft accepted: %v", err)
	}
	if _, err := service.Answer(t.Context(), session.ID, []domain.Answer{{ID: "port", Value: ""}}); !errors.Is(err, domain.ErrInvalidAnswer) {
		t.Fatalf("empty rejected-field correction admitted: %v", err)
	}
	corrected, err := service.Answer(t.Context(), session.ID, []domain.Answer{{ID: "port", Value: "9090"}})
	if err != nil {
		t.Fatal(err)
	}
	if corrected.NeedsInput[0].ValidationError != "" || domain.PendingRequired(corrected.NeedsInput, corrected.Answers) || corrected.Plan != nil {
		t.Fatalf("correction retained error or stale plan: %+v", corrected)
	}
}

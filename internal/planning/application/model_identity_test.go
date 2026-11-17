package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

type identityAudit struct{ events []Audit }

func (audit *identityAudit) Record(_ context.Context, event Audit) error {
	audit.events = append(audit.events, event)
	return nil
}

func TestAcceptBindsAndAuditsSeparateModelSources(t *testing.T) {
	audit := &identityAudit{}
	service := NewService(scriptedPlanner{result: &TurnResult{
		Model: "requested (provider reported actual-version)", ConfiguredModel: "requested", ProviderModel: "actual-version",
		ResolvedCommit: strings.Repeat("a", 40), Plan: &domain.Plan{Text: "Reviewed plan"},
	}}).WithAuditor(audit)
	session, err := service.Create(t.Context(), "https://example.test/source.git", "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Turn(t.Context(), session.ID, "Plan"); err != nil {
		t.Fatal(err)
	}
	accepted, err := service.Accept(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Binding.ConfiguredModel != "requested" || accepted.Binding.ProviderModel != "actual-version" ||
		len(audit.events) != 1 || audit.events[0].ConfiguredModel != "requested" || audit.events[0].ProviderModel != "actual-version" {
		t.Fatalf("Accept lost model sources: binding=%+v audit=%+v", accepted.Binding, audit.events)
	}
	accepted.Binding.ProviderModel = "caller mutation"
	stored, err := service.Get(t.Context(), session.ID)
	if err != nil || stored.Binding.ProviderModel != "actual-version" {
		t.Fatalf("immutable binding changed: %+v / %v", stored, err)
	}
	if _, err := service.Accept(t.Context(), session.ID); !errors.Is(err, domain.ErrAlreadyAccepted) || len(audit.events) != 1 {
		t.Fatalf("second Accept rewrote provenance or audit: %v / %+v", err, audit.events)
	}
}

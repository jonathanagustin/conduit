package application

import (
	"context"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestDeclareRequiresConfirmationAndClearRefusesTheAuthor(t *testing.T) {
	ctx := context.Background()
	repository := controlplane.NewMemoryRepository()
	now := time.Date(2026, 11, 14, 18, 0, 0, 0, time.UTC)
	service := &Service{Store: repository, Now: func() time.Time { return now }}
	start, end := now, now.Add(time.Hour)
	denied, err := service.Declare(ctx, "ada", "east", "change freeze for the rehearsal", start, end, false)
	if err != nil || denied.Allowed || denied.Reason != "confirmation is required" {
		t.Fatalf("confirm = %#v %v", denied, err)
	}
	declared, err := service.Declare(ctx, "ada", "east", "change freeze for the rehearsal", start, end, true)
	if err != nil || !declared.Allowed || service.Active("east", start) != "change freeze for the rehearsal" {
		t.Fatalf("declare = %#v %v", declared, err)
	}
	if service.Active("east", end) != "" {
		t.Fatal("outside the blackout")
	}
	proposed, err := service.ProposeClear(ctx, controlplane.Principal{Username: "ada"}, "east")
	if err != nil || proposed.ChangeRequestID == "" {
		t.Fatalf("propose = %#v %v", proposed, err)
	}
	self, err := service.ApproveClear(ctx, controlplane.Principal{Username: "ada"}, proposed.ChangeRequestID, "mine")
	if err != nil || self.Allowed || service.Active("east", start) == "" {
		t.Fatalf("self = %#v active %q %v", self, service.Active("east", start), err)
	}
	cleared, err := service.ApproveClear(ctx, controlplane.Principal{Username: "bea", Teams: []string{"release-reviewers"}}, proposed.ChangeRequestID, "rehearsal is over")
	if err != nil || !cleared.Allowed || service.Active("east", start) != "" {
		t.Fatalf("clear = %#v active %q %v", cleared, service.Active("east", start), err)
	}
}

package application

import (
	"context"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

type verifier struct{ fail bool }

func (v verifier) Verify(context.Context, string) (string, error) {
	if v.fail {
		return "", errVerify
	}
	return "sha256:bundle", nil
}

type errVerifyT struct{}

func (errVerifyT) Error() string { return "bundle signature did not verify" }

var errVerify = errVerifyT{}

type presence struct{ online bool }

func (p presence) Online(context.Context, string) (bool, error) { return p.online, nil }

func TestHandOffRefusesAnUnverifiedBundleAndWaitsForCheckIn(t *testing.T) {
	ctx := context.Background()
	repository := controlplane.NewMemoryRepository()
	now := time.Date(2026, 11, 14, 17, 0, 0, 0, time.UTC)
	service := &Service{Store: repository, Verifier: verifier{fail: true}, Presence: presence{}, Now: func() time.Time { return now }}
	if _, reason, err := service.HandOff(ctx, "ada", "bnd_1", "north", true); err == nil || reason != "" {
		t.Fatalf("unverified = %q %v", reason, err)
	}
	service.Verifier = verifier{}
	if _, reason, err := service.HandOff(ctx, "ada", "bnd_1", "north", false); err != nil || reason != "confirmation is required" {
		t.Fatalf("confirm = %q %v", reason, err)
	}
	handoff, reason, err := service.HandOff(ctx, "ada", "bnd_1", "north", true)
	if err != nil || handoff.Digest != "sha256:bundle" || reason != "offline bundle handed off" {
		t.Fatalf("handoff = %#v %q %v", handoff, reason, err)
	}
	pending, reason, err := service.ProposeReport(ctx, controlplane.Principal{Username: "ada"}, handoff.ID, "installed")
	if err != nil || pending.Status != "pending_check_in" || reason != "report-back is pending until the site checks in" {
		t.Fatalf("pending = %#v %q %v", pending, reason, err)
	}
	service.Presence = presence{online: true}
	proposed, changeID, err := service.ProposeReport(ctx, controlplane.Principal{Username: "ada"}, handoff.ID, "installed")
	if err != nil || changeID == "" || proposed.Status == "installed" {
		t.Fatalf("propose = %#v %q %v", proposed, changeID, err)
	}
	if _, reason, err := service.ApproveInstall(ctx, controlplane.Principal{Username: "ada"}, changeID, "mine"); err != nil || reason != "the author cannot approve their own change request" {
		t.Fatalf("self = %q %v", reason, err)
	}
	installed, reason, err := service.ApproveInstall(ctx, controlplane.Principal{Username: "bea", Teams: []string{"release-reviewers"}}, changeID, "site checked in")
	if err != nil || installed.Status != "installed" || reason != "install report recorded" {
		t.Fatalf("install = %#v %q %v", installed, reason, err)
	}
}

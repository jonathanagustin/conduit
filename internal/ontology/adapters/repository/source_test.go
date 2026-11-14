package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/ontology/domain"
	"github.com/jonathanagustin/conduit/internal/ontology/ports"
)

func TestCatalogProjectsVisibleDomainAndHidesTheRest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := controlplane.NewMemoryRepository()
	seedProjection(t, ctx, store)
	source := New(store, controlplane.NewGovernanceService(store))
	catalog, err := source.Catalog(ctx, func(environmentID string) bool { return environmentID == "east" })
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for _, object := range catalog.Objects {
		got[object.Type] = append(got[object.Type], object.PrimaryKey)
		if object.Properties["environment"] == "west" || object.PrimaryKey == "west" || object.Properties["id"] == "west" {
			t.Fatalf("west leaked in %#v", object)
		}
	}
	for _, key := range []string{"east", "agent-east", "plan-east", "cr-east", "east/stable"} {
		found := false
		for _, keys := range got {
			for _, candidate := range keys {
				if candidate == key {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("missing %s in %#v", key, got)
		}
	}
	plans, err := domain.Resolve(catalog, domain.ObjectSet{
		Type: "searchAround", Link: "plans",
		Inner: &domain.ObjectSet{Type: "base", ObjectType: "Environment"},
	})
	if err != nil || len(plans) != 1 || plans[0].PrimaryKey != "plan-east" {
		t.Fatalf("plans = %#v err=%v", plans, err)
	}
	artifacts, err := domain.Resolve(catalog, domain.ObjectSet{
		Type: "searchAround", Link: "artifacts",
		Inner: &domain.ObjectSet{Type: "base", ObjectType: "Plan"},
	})
	if err != nil || len(artifacts) != 1 || artifacts[0].Properties["digest"] == "" {
		t.Fatalf("artifacts = %#v err=%v", artifacts, err)
	}
}

func TestApplyValidatesWithoutStoringAndExecutesThroughPolicy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := controlplane.NewMemoryRepository()
	service := controlplane.NewGovernanceService(store)
	if _, err := service.PutPolicy(ctx, controlplane.ApprovalPolicy{
		ID: "review", DisplayName: "Review", Enabled: true,
		Expression: `require_team("release-managers", "review the change")`,
	}); err != nil {
		t.Fatal(err)
	}
	source := New(store, service)
	caller := ports.Caller{Username: "author"}
	parameters := map[string]string{
		"environment": "east", "targetKind": "ENVIRONMENT", "targetId": "east",
		"changeType": controlplane.ChangeTypeUpdateEnvironmentSettings, "title": "Tune east",
	}
	preview, err := source.Apply(ctx, caller, domain.ApplyRequest{
		Action: domain.ActionCreateChange, Parameters: parameters, Mode: domain.ModeValidateOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Valid || preview.Stored || preview.ChangeRequestState != controlplane.ChangePendingApprovals {
		t.Fatalf("preview = %#v", preview)
	}
	stored, err := store.ListChangeRequests(ctx, "")
	if err != nil || len(stored) != 0 {
		t.Fatalf("validate-only stored %#v err=%v", stored, err)
	}
	executed, err := source.Apply(ctx, caller, domain.ApplyRequest{
		Action: domain.ActionCreateChange, Parameters: parameters, Mode: domain.ModeValidateAndExecute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !executed.Valid || !executed.Stored || executed.ChangeRequestState != controlplane.ChangePendingApprovals {
		t.Fatalf("executed = %#v", executed)
	}
	stored, err = store.ListChangeRequests(ctx, "east")
	if err != nil || len(stored) != 1 || stored[0].ID != executed.ChangeRequestID || stored[0].Status != controlplane.ChangePendingApprovals {
		t.Fatalf("stored = %#v err=%v", stored, err)
	}
	rejected, err := source.Apply(ctx, caller, domain.ApplyRequest{
		Action: domain.ActionCreateChange, Mode: domain.ModeValidateAndExecute,
		Parameters: map[string]string{"environment": "east", "targetKind": "ENVIRONMENT", "targetId": "east", "changeType": "NOT_A_TYPE"},
	})
	if err != nil || rejected.Valid || rejected.Stored || rejected.ParameterMessages["changeType"] == "" {
		t.Fatalf("rejected = %#v err=%v", rejected, err)
	}
	stored, err = store.ListChangeRequests(ctx, "")
	if err != nil || len(stored) != 1 {
		t.Fatalf("bad type stored %#v err=%v", stored, err)
	}
}

func seedProjection(t *testing.T, ctx context.Context, store *controlplane.MemoryRepository) {
	t.Helper()
	digest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	for _, environmentID := range []string{"east", "west"} {
		if _, err := store.CreatePlan(ctx, controlplane.PlanRecord{
			ID: "plan-" + environmentID, DesiredStateDigest: environmentID, Status: controlplane.PlanRecorded,
			Plan: controlplane.Plan{Environment: environmentID, Generation: "0123456789abcdef"},
			DesiredState: &controlplane.DesiredState{
				Environment: controlplane.Environment{
					ID: environmentID, Generation: "0123456789abcdef", DefaultReleaseChannel: "stable",
				},
				ReleaseChannels: []controlplane.ReleaseChannelDefinition{{Name: "stable", Description: "steady"}},
				Releases: []controlplane.Release{{
					Product: "ledger", Version: "1.0.0",
					Chart: controlplane.Artifact{Repository: "registry.example/ledger", Tag: "1.0.0", Digest: digest},
				}},
			},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.CreateChangeRequest(ctx, controlplane.ChangeRequest{
			ID: "cr-" + environmentID, PlanID: "plan-" + environmentID, Environment: environmentID,
			Title: "Review " + environmentID, Status: controlplane.ChangePendingApprovals,
			Author: controlplane.Principal{Username: "author"},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.PutEnvironmentReport(ctx, controlplane.EnvironmentReport{
			Environment: environmentID, AgentID: "agent-" + environmentID, ObservedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	if _, err := store.PutEnvironmentProfile(ctx, controlplane.EnvironmentProfile{
		EnvironmentID: "east", DisplayName: "East", Accreditation: "RESTRICTED", OwnerTeam: "owners",
		Source: "OPERATOR_API", UpdatedBy: "author", Rationale: "opened",
		CreatedAt: now, UpdatedAt: now, Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
}

package main

import (
	"context"
	"strings"
	"testing"
	"time"

	apollo "github.com/jonathanagustin/conduit/internal/apollo/application"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	notificationpostgres "github.com/jonathanagustin/conduit/internal/notifications/adapters/postgres"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	packagingadapters "github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/planlifecycle"
	packagingconfig "github.com/jonathanagustin/conduit/internal/packagingworkflow/config"
	packaging "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

func TestPostgresPackagingWorkerConfiguresDurableAdmissionBeforeItsFirstProposal(t *testing.T) {
	dsn := notificationRuntimeSchema(t)
	repository, err := controlplane.NewPostgresRepository(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	settings := map[string]string{
		"REPOSITORY_KIND": "postgres", "POSTGRES_DSN_FILE": writeFile(t, "packaging-dsn", []byte(dsn+"\n")),
		"PLATFORM_BOOTSTRAP_ADMINISTRATORS": "subscriber",
	}
	getenv := func(name string) string { return settings[name] }
	resource := apollo.EnvironmentRID("packaging-dev")
	var submitted string
	var child context.Context
	err = runPackagingWorker(t.Context(), getenv, packagingconfig.Config{}, repository, func(ctx context.Context, _ packagingconfig.Config, received controlplane.Repository) error {
		child = ctx
		if _, configured := received.TransactionAnnouncements(); !configured {
			t.Fatal("packaging runner entered before its source intent factory was configured")
		}
		governance := controlplane.NewGovernanceService(received)
		if _, err := governance.PutTeam(ctx, controlplane.Team{
			Name: "packaging-reviewers", DisplayName: "Packaging reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"},
		}); err != nil {
			return err
		}
		if _, err := governance.PutPolicy(ctx, controlplane.ApprovalPolicy{
			ID: "packaging-review", DisplayName: "Packaging review", Enabled: true, Expression: `require_team("packaging-reviewers", "review")`,
		}); err != nil {
			return err
		}
		// Open an independent notification pool only after production startup.
		// This also proves that the worker installed the component migrations.
		observer, err := notificationpostgres.New(ctx, dsn)
		if err != nil {
			return err
		}
		defer observer.Close()
		if err := observer.SavePreference(ctx, domain.Preference{
			ID: "packaging-source-consent", Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "subscriber"},
			ResourceRID: resource, Channel: domain.ChannelInApp, AllFutureTypes: true,
		}); err != nil {
			return err
		}
		ledger, err := packagingadapters.Open(t.TempDir())
		if err != nil {
			return err
		}
		defer ledger.Close()
		receipt, err := planlifecycle.New(received, ledger).Submit(ctx, packaging.HandoffRequest{
			RunID: "runtime-notification", ProposalRevision: "revision-1", ArtifactDigest: "sha256:" + strings.Repeat("a", 64),
			TargetPlanRef: "packaging-dev/sample-app",
		})
		if err != nil {
			return err
		}
		submitted = receipt.PlanID
		// Admission belongs to the packaging runtime: no Hub, Temporal server
		// or external transport runs in this regression.
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			entries, err := observer.Inbox(ctx, domain.Recipient{Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "subscriber"}, Channel: domain.ChannelInApp}, 10)
			if err != nil {
				return err
			}
			if len(entries) == 1 {
				if entries[0].Event.Type != domain.EventChangeRequestPendingApproval || entries[0].Event.ResourceRID != resource || entries[0].PreferenceID != "packaging-source-consent" {
					t.Fatalf("packaging source admitted the wrong notification: %#v", entries)
				}
				return nil
			}
			select {
			case <-deadline.C:
				stats, statsErr := observer.Intents(ctx)
				t.Fatalf("packaging source committed without durable IN_APP admission: intents=%#v statsError=%v", stats, statsErr)
			case <-tick.C:
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if submitted == "" || child == nil || child.Err() == nil {
		t.Fatalf("packaging invocation lost its proposal or context cleanup: plan=%s context=%v", submitted, child)
	}
	stored, err := repository.GetPlan(t.Context(), submitted)
	if err != nil || stored.Status != controlplane.PlanAwaitingApproval || stored.Lease != nil || len(stored.Approvals) != 0 {
		t.Fatalf("notification startup changed packaging approval or execution: %#v %v", stored, err)
	}
}

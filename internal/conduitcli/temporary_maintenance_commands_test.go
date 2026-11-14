package conduitcli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/agent"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/delivery/maintenance"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
)

func TestTemporaryMaintenanceCLIRealHubPartialResultsAndRestart(t *testing.T) {
	directory := t.TempDir()
	repository, err := controlplane.NewFileRepository(filepath.Join(directory, "core"))
	if err != nil {
		t.Fatal(err)
	}
	governance := controlplane.NewGovernanceService(repository)
	for _, team := range []controlplane.Team{{Name: "writers", DisplayName: "Writers", AdminUsers: []string{"writer"}, MemberUsers: []string{"writer"}}, {Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"}}} {
		if _, err := governance.PutTeam(t.Context(), team); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := governance.PutPolicy(t.Context(), controlplane.ApprovalPolicy{ID: "maintenance", DisplayName: "Maintenance", Enabled: true, Expression: `type == "CREATE_ENTITY_MAINTENANCE_WINDOW_OVERRIDE" ? require_team("reviewers", "review maintenance") : approve("ordinary")`}); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile("../../examples/example-foundation.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, environment := range []string{"env-a", "env-b"} {
		if _, err := governance.PutGrant(t.Context(), controlplane.ResourceGrant{ResourceRID: "environment/" + environment, Team: "writers", Roles: []string{"environment:editor"}}); err != nil {
			t.Fatal(err)
		}
		var state controlplane.DesiredState
		if err := json.Unmarshal(contents, &state); err != nil {
			t.Fatal(err)
		}
		state.Environment.ID = environment
		state.Entities = state.Entities[:1]
		// Both recurring windows are closed now; only the scoped grant opens
		// the schedule during this test.
		closedDay := strings.ToUpper(time.Now().UTC().Add(72 * time.Hour).Weekday().String())
		interval := closedDay + "/10:00-" + closedDay + "/11:00"
		state.Environment.Maintenance.NoDowntime = &controlplane.WeeklyMaintenanceWindow{TimeZone: "UTC", Intervals: []string{interval}}
		state.Environment.Maintenance.Downtime = &controlplane.WeeklyMaintenanceWindow{TimeZone: "UTC", Intervals: []string{interval}}
		author, err := governance.ResolvePrincipal(t.Context(), controlplane.IdentityClaims{Username: "writer"})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := governance.ProposePlan(t.Context(), state, author, "baseline", ""); err != nil {
			t.Fatal(err)
		}
	}
	handler, err := composition.NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, AllowInsecureAgentIdentityHeaders: true})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	run := func(subject string, args ...string) (string, error) {
		return runMembershipCLI(t, server.URL, "", subject, args...)
	}
	base := []string{"entity", "maintenancewindowoverride", "create", "aeid:env-a:helm-chart:example-api", "aeid:env-a:helm-chart:example-api", "aeid:env-b:helm-chart:example-api", "--duration", "2h", "--reason", "CAB-364", "--compliance-ticket", "FRCCB-364"}
	preview := append(append([]string{}, base...), "--dry-run")
	if output, err := run("writer", preview...); err != nil {
		t.Fatal(output, err)
	}
	requests, err := repository.ListChangeRequests(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range requests {
		if request.TargetKind == controlplane.TemporaryMaintenanceTarget {
			t.Fatal("dry run created request")
		}
	}
	invalid := append(append([]string{}, base...), "--output", "table")
	if _, err := run("writer", invalid...); err == nil {
		t.Fatal("unsupported output admitted")
	}
	if _, err := run("outsider", base...); err == nil {
		t.Fatal("ungranted proposal")
	}
	// Inserting an absent Entity after the two unique valid targets must return
	// already-created requests, fail the command and skip the remaining target.
	partial := append([]string{}, base[:6]...)
	partial = append(partial, "aeid:env-b:helm-chart:missing", "aeid:env-b:helm-chart:never-submitted")
	partial = append(partial, base[6:]...)
	output, err := run("writer", partial...)
	if err == nil {
		t.Fatal("partial submission reported success", output)
	}
	var result maintenance.OverrideBatch
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal("created requests not printed", output, err)
	}
	if len(result.Requests) != 2 || result.Failure == "" || result.FailedTarget == nil || result.FailedTarget.Entity != "missing" {
		t.Fatal(output)
	}
	if !result.Requests[0].StartsAt.Equal(result.Requests[1].StartsAt) || !result.Requests[0].EndsAt.Equal(result.Requests[1].EndsAt) {
		t.Fatal("common interval lost")
	}
	for _, receipt := range result.Requests {
		if receipt.Status != controlplane.ChangePendingApprovals || receipt.Type != "NO_DOWNTIME" {
			t.Fatal(receipt)
		}
		if output, err := run("reviewer", "change-request", "approve", receipt.ChangeRequestID, "--rationale", "Review CAB-364"); err != nil {
			t.Fatal(output, err)
		}
	}
	reopened, err := controlplane.NewFileRepository(filepath.Join(directory, "core"))
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := composition.NewHub(reopened, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, AllowInsecureAgentIdentityHeaders: true})
	if err != nil {
		t.Fatal(err)
	}
	restartedServer := httptest.NewServer(restarted)
	defer restartedServer.Close()
	// The legacy route has no temporary authority, even after restart.
	response, err := http.Post(restartedServer.URL+"/v1/environments/env-a/leases", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("unauthenticated lease", response.StatusCode)
	}
	client, err := agent.NewHTTPControlPlane(restartedServer.URL, restartedServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	leased, err := client.Lease(t.Context(), "env-a", "agent-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(leased.Plan.Steps[0].Maintenance.Overrides) != 1 {
		t.Fatal("restart lost admission")
	}
	if err := client.Start(t.Context(), "env-b", "agent-a", leased.ID, leased.Lease.Token); err == nil {
		t.Fatal("cross-Environment start")
	}
	if err := client.Start(t.Context(), "env-a", "other-agent", leased.ID, leased.Lease.Token); err == nil {
		t.Fatal("foreign Agent start")
	}
	if err := client.Start(t.Context(), "env-a", "agent-a", leased.ID, "wrong-token"); err == nil {
		t.Fatal("wrong token start")
	}
	if err := client.Start(t.Context(), "env-a", "agent-a", leased.ID, leased.Lease.Token); err != nil {
		t.Fatal(err)
	}
	persisted, err := reopened.GetPlan(t.Context(), leased.ID)
	if err != nil || len(persisted.Plan.Steps[0].Maintenance.Overrides) != 0 {
		t.Fatal("temporary authority persisted in Plan", err)
	}
}

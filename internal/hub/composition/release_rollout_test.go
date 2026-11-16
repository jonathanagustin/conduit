package composition

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	releasedomain "github.com/jonathanagustin/conduit/internal/delivery/releasestatus/domain"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReleaseRolloutNativeFactsWavesAndAuthorization(t *testing.T) {
	t.Parallel()
	repo := controlplane.NewMemoryRepository()
	ctx := t.Context()
	gov := controlplane.NewGovernanceService(repo)
	for _, team := range []struct{ name, user string }{{"viewers", "alice"}, {"other", "bob"}} {
		if _, err := gov.PutTeam(ctx, controlplane.Team{Name: team.name, DisplayName: team.name, AdminUsers: []string{team.user}, MemberUsers: []string{team.user}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, resource := range []string{"product/example:app", "environment/visible"} {
		if _, err := gov.PutGrant(ctx, controlplane.ResourceGrant{ResourceRID: resource, Team: "viewers", Operations: []string{controlplane.OperationReadResource}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, environment := range []string{"visible", "private"} {
		state := desiredState(environment, "example:app")
		state.ProductSettings = []controlplane.ProductSettings{{Product: "example:app", PromotionPipeline: &controlplane.ProductPromotionPipeline{Enabled: true, Stages: []controlplane.ProductPromotionStage{{FromChannel: "DEV", ToChannel: "STAGING"}}}}}
		if _, err := controlplane.NewLifecycle(repo).CreatePlan(ctx, state); err != nil {
			t.Fatal(err)
		}
	}
	latest := desiredState("visible", "example:app")
	latest.Environment.Generation = "fedcba9876543210"
	latest.ProductSettings = []controlplane.ProductSettings{{Product: "example:app", PromotionPipeline: &controlplane.ProductPromotionPipeline{Enabled: true, Stages: []controlplane.ProductPromotionStage{{FromChannel: "DEV", ToChannel: "PROD"}}}}}
	latestRecord, err := controlplane.NewLifecycle(repo).CreatePlan(ctx, latest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PutEnvironmentReport(ctx, controlplane.EnvironmentReport{Environment: "visible", AgentID: "visible-agent", ObservedAt: time.Now().Add(-time.Minute), Entities: []controlplane.ReportedEntity{{InstallationID: "thing", Product: "example:app", Version: "0.0.9", Namespace: "example", ReleaseDigest: "sha256:observed", ConfigDigest: "sha256:observed-config", Health: "UNHEALTHY", ConfigStatus: "DRIFTED", Lifecycle: "INSTALLED"}}}); err != nil {
		t.Fatal(err)
	}
	options := controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"}}
	handler, err := NewHub(repo, options)
	if err != nil {
		t.Fatal(err)
	}
	send := func(subject, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if subject != "" {
			r.Header.Set("X-Platform-Subject", subject)
		}
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, wave := range []struct{ id, environments string }{{"mixed", `["visible","private"]`}, {"visible-wave", `["visible"]`}} {
		body := `{"action":"create","id":"` + wave.id + `","product":"example:app","version":"0.1.0","waves":[{"name":"first","environments":` + wave.environments + `,"gate":"health"}]}`
		if result := send("root", "POST", "/v1/rollouts", body); result.Code != 200 {
			t.Fatalf("create rollout: %d %s", result.Code, result.Body.String())
		}
	}
	path := "/v1/products/example:app/releases/0.1.0/rollout"
	if result := send("", "GET", path, ""); result.Code != 401 {
		t.Fatalf("anonymous localhost: %d", result.Code)
	}
	if result := send("bob", "GET", path, ""); result.Code != 403 {
		t.Fatalf("no product grant: %d %s", result.Code, result.Body.String())
	}
	result := send("alice", "GET", path, "")
	if result.Code != 200 {
		t.Fatalf("read: %d %s", result.Code, result.Body.String())
	}
	if strings.Contains(result.Body.String(), "private") || strings.Contains(result.Body.String(), "mixed") {
		t.Fatalf("hidden rollout leaked: %s", result.Body.String())
	}
	var view releasedomain.View
	if err := json.Unmarshal(result.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Environments) != 1 || view.Environments[0].ID != "visible" || len(view.Environments[0].Installations) != 1 || len(view.Rollouts) != 1 || view.Rollouts[0].ID != "visible-wave" {
		t.Fatalf("joined view: %#v", view)
	}
	row := view.Environments[0].Installations[0]
	if row.DesiredVersion != "0.1.0" || row.ObservedVersion != "0.0.9" || row.ObservedReleaseDigest != "sha256:observed" || row.Health != "UNHEALTHY" || row.ConfigStatus != "DRIFTED" {
		t.Fatalf("changed reported facts: %#v", row)
	}
	if len(view.Environments[0].PromotionGraphs) != 1 || view.Environments[0].PromotionGraphs[0].Edges[0].ToChannel != "PROD" || view.Environments[0].PromotionGraphs[0].PlanID != latestRecord.ID {
		t.Fatalf("missing promotion graph: %#v", view)
	}
	root := send("root", "GET", path, "")
	if root.Code != 200 || !strings.Contains(root.Body.String(), "mixed") {
		t.Fatalf("authorized aggregate missing: %d %s", root.Code, root.Body.String())
	}
	if result := send("alice", "GET", "/v1/products/example:app/releases/9.9.9/rollout", ""); result.Code != 404 {
		t.Fatalf("missing release: %d", result.Code)
	}
	// The same existing rollout service is read without advancing a wave.
	if view.Rollouts[0].Status != "draft" || view.Rollouts[0].Waves[0].Status != "pending" {
		t.Fatal("read advanced rollout")
	}
	calls := 0
	options.OperatorIdentityResolver = func(*http.Request) (controlplane.IdentityClaims, error) {
		calls++
		return controlplane.IdentityClaims{Username: "alice"}, nil
	}
	handler, err = NewHub(repo, options)
	if err != nil {
		t.Fatal(err)
	}
	if result := send("alice", "GET", path, ""); result.Code != 200 || calls != 1 {
		t.Fatalf("identity changed during read: status=%d calls=%d", result.Code, calls)
	}
}

type failedReleaseRead struct{ controlplane.Repository }

func (failedReleaseRead) ListEnvironmentReports(context.Context) ([]controlplane.EnvironmentReport, error) {
	return nil, errors.New("PRIVATE_STORAGE_ERROR")
}
func TestReleaseRolloutSourceFailureIsUnavailable(t *testing.T) {
	repo := controlplane.NewMemoryRepository()
	state := desiredState("dev", "example:app")
	if _, err := controlplane.NewLifecycle(repo).CreatePlan(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHub(failedReleaseRead{Repository: repo}, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"}})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/v1/products/example:app/releases/0.1.0/rollout", nil)
	r.Header.Set("X-Platform-Subject", "root")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 503 || strings.Contains(w.Body.String(), "PRIVATE_STORAGE_ERROR") {
		t.Fatalf("false empty or private error: %d %s", w.Code, w.Body.String())
	}
}

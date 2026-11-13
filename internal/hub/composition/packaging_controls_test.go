package composition

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	packaginghttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/httpapi"
)

type governedRuns struct {
	runs    []domain.RunStatus
	cancels int
	started []domain.RunIdentity
}

func (runs *governedRuns) Configured() bool { return true }

func (runs *governedRuns) Read(_ context.Context, runID string) (domain.RunStatus, error) {
	for _, run := range runs.runs {
		if run.RunID == runID {
			return run, nil
		}
	}
	return domain.RunStatus{}, packaginghttp.ErrNotFound
}

func (runs *governedRuns) List(context.Context) ([]domain.RunStatus, error) {
	return append([]domain.RunStatus(nil), runs.runs...), nil
}

func (runs *governedRuns) Events(context.Context, string) ([]domain.StepEvent, error) {
	return nil, packaginghttp.ErrNotFound
}

func (runs *governedRuns) Cancel(_ context.Context, runID string) error {
	runs.cancels++
	for i := range runs.runs {
		if runs.runs[i].RunID == runID {
			runs.runs[i].Phase = domain.PhaseBlocked
			runs.runs[i].Outcome = string(domain.OutcomeCancelled)
			return nil
		}
	}
	return packaginghttp.ErrNotFound
}

func (runs *governedRuns) Start(_ context.Context, id domain.RunIdentity) (domain.RunStatus, error) {
	runs.started = append(runs.started, id)
	return domain.RunStatus{RunID: id.RunID, Phase: domain.PhaseAssess, SourceDigest: id.SourceDigest, TargetPlanRef: id.TargetPlanRef}, nil
}

func TestPackagingControlsAuthorizeAuditAndLeavePlansAlone(t *testing.T) {
	t.Parallel()
	repository := controlplane.NewMemoryRepository()
	service := controlplane.NewGovernanceService(repository)
	if _, err := service.PutTeam(t.Context(), controlplane.Team{
		Name: "operators", DisplayName: "Operators", AdminUsers: []string{"alice"}, MemberUsers: []string{"alice"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutGrant(t.Context(), controlplane.ResourceGrant{
		ResourceRID: "environment/env-a", Team: "operators",
		Operations: []string{controlplane.OperationReadResource, controlplane.OperationOperate},
	}); err != nil {
		t.Fatal(err)
	}
	runs := &governedRuns{runs: []domain.RunStatus{{
		RunID: "run-review", Phase: domain.PhaseReview, SourceDigest: "sha256:abc",
		ProposalRevision: "rev-1", TargetPlanRef: "env-a/app",
	}}}
	options := controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		BootstrapAdministrators:              []string{"root"},
	}
	handler, err := NewHub(repository, options, Options{PackagingStatus: runs, PackagingControls: runs})
	if err != nil {
		t.Fatal(err)
	}
	production, err := NewHub(repository, controlplane.HandlerOptions{}, Options{PackagingStatus: runs, PackagingControls: runs})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"decisionId":"d-1","sourceDigest":"sha256:abc","proposalRevision":"rev-1","targetPlanRef":"env-a/app","templateVersion":"fixture-1","toolVersions":"lint=fixture-1"}`
	anonymous := httptest.NewRecorder()
	production.ServeHTTP(anonymous, httptest.NewRequest(http.MethodPost, "/v1/packaging-runs/run-review/cancel", strings.NewReader(body)))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d %s", anonymous.Code, anonymous.Body.String())
	}
	nobody := driveAs(t, handler, http.MethodPost, "/v1/packaging-runs/run-review/cancel", "nobody", body)
	if nobody.Code != http.StatusForbidden || runs.cancels != 0 {
		t.Fatalf("nobody = %d %s", nobody.Code, nobody.Body.String())
	}
	cancelled := driveAs(t, handler, http.MethodPost, "/v1/packaging-runs/run-review/cancel", "alice", body)
	if cancelled.Code != http.StatusOK || runs.cancels != 1 || !strings.Contains(cancelled.Body.String(), `"planCancelled":false`) || strings.Contains(cancelled.Body.String(), "/v1/plans/") {
		t.Fatalf("alice = %d %s", cancelled.Code, cancelled.Body.String())
	}
	events, err := repository.ListAuthorizationAudit(t.Context(), "packaging-run/run-review")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.Subject == "alice" && event.Action == "packaging-run.cancel" && event.Decision == "ALLOW" &&
			event.Context["decisionId"] == "d-1" && event.Context["sourceDigest"] == "sha256:abc" && !event.CreatedAt.IsZero() {
			found = true
		}
	}
	if !found {
		t.Fatalf("audit = %#v", events)
	}
	seen := driveAs(t, handler, http.MethodGet, "/v1/packaging-runs/run-review", "alice", "")
	if seen.Code != http.StatusOK || !strings.Contains(seen.Body.String(), "CANCELLED") {
		t.Fatalf("read back = %d %s", seen.Code, seen.Body.String())
	}

	unconfigured, err := NewHub(repository, options, Options{})
	if err != nil {
		t.Fatal(err)
	}
	refused := driveAs(t, unconfigured, http.MethodPost, "/v1/packaging-runs/run-review/rerun", "alice",
		`{"decisionId":"d-off","sourceDigest":"sha256:abc","proposalRevision":"rev-1","targetPlanRef":"env-a/app","templateVersion":"fixture-1","toolVersions":"lint=fixture-1"}`)
	if refused.Code != http.StatusConflict || !strings.Contains(refused.Body.String(), "not configured") || len(runs.started) != 0 {
		t.Fatalf("unconfigured = %d %s", refused.Code, refused.Body.String())
	}
	refusedEvents, err := repository.ListAuthorizationAudit(t.Context(), "packaging-run/run-review")
	if err != nil {
		t.Fatal(err)
	}
	auditedRefusal := false
	for _, event := range refusedEvents {
		if event.Subject == "alice" && event.Action == "packaging-run.rerun" && event.Decision == "DENY" && event.Context["decisionId"] == "d-off" {
			auditedRefusal = true
		}
	}
	if !auditedRefusal {
		t.Fatalf("unconfigured audit = %#v", refusedEvents)
	}
}

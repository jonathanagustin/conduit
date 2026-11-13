package composition

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	packaginghttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/httpapi"
)

type packagingCatalog struct {
	runs   []domain.RunStatus
	events map[string][]domain.StepEvent
}

func (c packagingCatalog) Read(_ context.Context, runID string) (domain.RunStatus, error) {
	for _, run := range c.runs {
		if run.RunID == runID {
			return run, nil
		}
	}
	return domain.RunStatus{}, packaginghttp.ErrNotFound
}

func (c packagingCatalog) List(context.Context) ([]domain.RunStatus, error) {
	return append([]domain.RunStatus(nil), c.runs...), nil
}

func (c packagingCatalog) Events(_ context.Context, runID string) ([]domain.StepEvent, error) {
	events, ok := c.events[runID]
	if !ok {
		return nil, packaginghttp.ErrNotFound
	}
	return append([]domain.StepEvent(nil), events...), nil
}

func TestPackagingIndexAndHistoryFollowEnvironmentRead(t *testing.T) {
	t.Parallel()
	repository := controlplane.NewMemoryRepository()
	service := controlplane.NewGovernanceService(repository)
	ctx := t.Context()
	for _, tenant := range []struct{ team, principal string }{
		{"tenant-a", "alice"},
		{"tenant-b", "bob"},
	} {
		if _, err := service.PutTeam(ctx, controlplane.Team{
			Name: tenant.team, DisplayName: tenant.team,
			AdminUsers: []string{tenant.principal}, MemberUsers: []string{tenant.principal},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.PutGrant(ctx, controlplane.ResourceGrant{
		ResourceRID: "environment/tenant-a", Team: "tenant-a",
		Operations: []string{controlplane.OperationReadResource},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutGrant(ctx, controlplane.ResourceGrant{
		ResourceRID: "environment/tenant-b", Team: "tenant-b",
		Operations: []string{controlplane.OperationReadResource},
	}); err != nil {
		t.Fatal(err)
	}
	catalog := packagingCatalog{
		runs: []domain.RunStatus{
			{RunID: "run-a", Phase: domain.PhaseComplete, TargetPlanRef: "tenant-a/app", PlanID: "plan_a"},
			{RunID: "run-b", Phase: domain.PhaseReview, TargetPlanRef: "tenant-b/app", PlanID: "plan_b"},
		},
		events: map[string][]domain.StepEvent{
			"run-a": {{Sequence: 1, Step: domain.StepHandoff, PlanID: "plan_a", Outcome: string(domain.OutcomeHandoffRecorded)}},
			"run-b": {{Sequence: 1, Step: domain.StepReview, Outcome: domain.EventWaiting}},
		},
	}
	handler, err := NewHub(repository, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		BootstrapAdministrators:              []string{"root"},
	}, Options{PackagingStatus: catalog})
	if err != nil {
		t.Fatal(err)
	}
	production, err := NewHub(repository, controlplane.HandlerOptions{}, Options{PackagingStatus: catalog})
	if err != nil {
		t.Fatal(err)
	}

	anonymous := httptest.NewRecorder()
	production.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/v1/packaging-runs", nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d %s", anonymous.Code, anonymous.Body.String())
	}
	anonymousEvents := httptest.NewRecorder()
	production.ServeHTTP(anonymousEvents, httptest.NewRequest(http.MethodGet, "/v1/packaging-runs/run-a/events", nil))
	if anonymousEvents.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous events = %d %s", anonymousEvents.Code, anonymousEvents.Body.String())
	}

	alice := driveAs(t, handler, http.MethodGet, "/v1/packaging-runs", "alice", "")
	if alice.Code != http.StatusOK || !strings.Contains(alice.Body.String(), "run-a") || strings.Contains(alice.Body.String(), "run-b") || strings.Contains(alice.Body.String(), "plan_b") {
		t.Fatalf("alice index = %d %s", alice.Code, alice.Body.String())
	}
	var alicePage struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(alice.Body.Bytes(), &alicePage); err != nil {
		t.Fatal(err)
	}
	if alicePage.Total != 1 {
		t.Fatalf("alice total = %d", alicePage.Total)
	}
	bob := driveAs(t, handler, http.MethodGet, "/v1/packaging-runs", "bob", "")
	if !strings.Contains(bob.Body.String(), "run-b") || strings.Contains(bob.Body.String(), "run-a") {
		t.Fatalf("bob index = %s", bob.Body.String())
	}
	nobody := driveAs(t, handler, http.MethodGet, "/v1/packaging-runs", "nobody", "")
	if nobody.Code != http.StatusOK || strings.Contains(nobody.Body.String(), "run-a") || strings.Contains(nobody.Body.String(), "run-b") {
		t.Fatalf("nobody = %d %s", nobody.Code, nobody.Body.String())
	}
	root := driveAs(t, handler, http.MethodGet, "/v1/packaging-runs", "root", "")
	if !strings.Contains(root.Body.String(), "run-a") || !strings.Contains(root.Body.String(), "run-b") {
		t.Fatalf("root index = %s", root.Body.String())
	}

	own := driveAs(t, handler, http.MethodGet, "/v1/packaging-runs/run-a/events", "alice", "")
	if own.Code != http.StatusOK || !strings.Contains(own.Body.String(), "plan_a") || strings.Contains(own.Body.String(), "AWAITING_APPROVAL") {
		t.Fatalf("alice events = %d %s", own.Code, own.Body.String())
	}
	other := driveAs(t, handler, http.MethodGet, "/v1/packaging-runs/run-b/events", "alice", "")
	if other.Code != http.StatusNotFound || strings.Contains(other.Body.String(), "plan_b") {
		t.Fatalf("alice reading bob = %d %s", other.Code, other.Body.String())
	}
	posted := driveAs(t, handler, http.MethodPost, "/v1/packaging-runs/run-a/events", "alice", `{}`)
	if posted.Code == http.StatusOK {
		t.Fatal("event history accepted a write")
	}
}

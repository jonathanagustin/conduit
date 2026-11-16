package composition

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	packagingv1 "github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	packaginghttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/httpapi"
	"google.golang.org/protobuf/encoding/protojson"
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

	for _, actor := range []string{"", "alice", "bob", "nobody", "root"} {
		native := driveAs(t, handler, http.MethodGet, "/v1/packaging-runs", actor, "")
		request := httptest.NewRequest(http.MethodPost, "/conduit.packaging.v1.PackagingRunService/ListPackagingRuns", strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Connect-Protocol-Version", "1")
		if actor != "" {
			request.Header.Set("X-Platform-Subject", actor)
		}
		wire := httptest.NewRecorder()
		handler.ServeHTTP(wire, request)
		if wire.Code != native.Code {
			t.Fatalf("actor=%s REST=%d RPC=%d %s", actor, native.Code, wire.Code, wire.Body.String())
		}
		if wire.Code != 200 {
			continue
		}
		var expected struct {
			Total int
			Items []domain.RunStatus
		}
		if err := json.Unmarshal(native.Body.Bytes(), &expected); err != nil {
			t.Fatal(err)
		}
		actual := &packagingv1.ListPackagingRunsResponse{}
		if err := protojson.Unmarshal(wire.Body.Bytes(), actual); err != nil {
			t.Fatal(err)
		}
		if actual.Total != int64(expected.Total) || len(actual.Items) != len(expected.Items) {
			t.Fatalf("actor=%s visibility diverged", actor)
		}
		for i, row := range actual.Items {
			if row.RunId != expected.Items[i].RunID {
				t.Fatalf("actor=%s foreign row", actor)
			}
		}
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

func TestPackagingListConnectRetainsDelegatingCarrier(t *testing.T) {
	_, repository, _ := delegationHub(t)
	handler, err := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"alice"}}, Options{PackagingStatus: packagingCatalog{runs: []domain.RunStatus{{RunID: "private-run", TargetPlanRef: "secret-env/app"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		actor, represented string
		total              int64
	}{{"alice", "", 1}, {"alice", "bob", 1}, {"robot", "alice", 0}, {"bob", "alice", 0}, {"bob", "", 0}} {
		native := as(t, handler, "/v1/packaging-runs", test.actor, test.represented)
		request := httptest.NewRequest(http.MethodPost, "/conduit.packaging.v1.PackagingRunService/ListPackagingRuns", strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Connect-Protocol-Version", "1")
		request.Header.Set("X-Platform-Subject", test.actor)
		if test.represented != "" {
			request.Header.Set("X-Platform-Service-Account", "true")
			request.Header.Set("X-Platform-On-Behalf-Of", test.represented)
		}
		wire := httptest.NewRecorder()
		handler.ServeHTTP(wire, request)
		if native.Code != 200 || wire.Code != native.Code {
			t.Fatalf("%s/%s REST=%d RPC=%d", test.actor, test.represented, native.Code, wire.Code)
		}
		actual := &packagingv1.ListPackagingRunsResponse{}
		if err := protojson.Unmarshal(wire.Body.Bytes(), actual); err != nil {
			t.Fatal(err)
		}
		var expected struct{ Total int64 }
		if err := json.Unmarshal(native.Body.Bytes(), &expected); err != nil {
			t.Fatal(err)
		}
		if actual.Total != test.total || actual.Total != expected.Total {
			t.Fatalf("%s/%s got %d native %d expected %d", test.actor, test.represented, actual.Total, expected.Total, test.total)
		}
	}
}

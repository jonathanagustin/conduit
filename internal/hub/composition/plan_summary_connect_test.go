package composition

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	plansv1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"google.golang.org/protobuf/encoding/protojson"
)

type planSummaryCountingRepository struct {
	controlplane.Repository
	reads atomic.Int64
	err   error
}

func (repository *planSummaryCountingRepository) ListPlans(ctx context.Context, environment string) ([]controlplane.PlanRecord, error) {
	repository.reads.Add(1)
	if repository.err != nil {
		return nil, repository.err
	}
	return repository.Repository.ListPlans(ctx, environment)
}

const planSummaryProcedure = "/conduit.plans.v1.PlanReadService/ListPlanSummaries"

func postPlanSummaries(t *testing.T, handler http.Handler, actor, represented, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, planSummaryProcedure, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Connect-Protocol-Version", "1")
	r.Header.Set("X-Platform-Subject", actor)
	if represented != "" {
		r.Header.Set("X-Platform-Service-Account", "true")
		r.Header.Set("X-Platform-On-Behalf-Of", represented)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestPlanSummaryConnectSharesNativeVisibilityFiltersAndCursors(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	governance := controlplane.NewGovernanceService(repository)
	if _, err := governance.PutTeam(t.Context(), controlplane.Team{Name: "east-readers", DisplayName: "East", AdminUsers: []string{"owner"}, MemberUsers: []string{"alice"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(t.Context(), controlplane.ResourceGrant{ResourceRID: "environment/east", Team: "east-readers", Operations: []string{controlplane.OperationReadResource}}); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, time.November, 16, 12, 0, 0, 123456789, time.FixedZone("native", 3600))
	for i, row := range []struct{ id, environment string }{{"plan-a", "east"}, {"plan-b", "east"}, {"plan-private", "west"}} {
		_, err := repository.CreatePlan(t.Context(), controlplane.PlanRecord{
			ID: row.id, DesiredStateDigest: row.id,
			Plan:   controlplane.Plan{Environment: row.environment, Generation: row.id, Steps: []controlplane.PlanStep{{InstallationID: "frontend", Product: "app"}}},
			Status: controlplane.PlanRunning, RequiredApprovals: 3, Lease: &controlplane.PlanLease{Token: "PRIVATE-LEASE-TOKEN"},
			CreatedAt: base.Add(time.Duration(i) * time.Hour), UpdatedAt: base.Add(time.Duration(i)*time.Hour + time.Second), Revision: uint64(9007199254740993),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	handler, err := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// No request filter may accidentally hide a broken authorization check.
	unfiltered := postPlanSummaries(t, handler, "alice", "", `{}`)
	own := &plansv1.ListPlanSummariesResponse{}
	if unfiltered.Code != http.StatusOK || protojson.Unmarshal(unfiltered.Body.Bytes(), own) != nil || own.Total != 2 || len(own.Plans) != 2 {
		t.Fatalf("unfiltered authorized index: %d %s", unfiltered.Code, unfiltered.Body.String())
	}
	for _, plan := range own.Plans {
		if plan.Environment != "east" {
			t.Fatalf("authorized principal received foreign row: %v", plan)
		}
	}
	values := url.Values{"view": {"summary"}, "limit": {"1"}, "environment": {" east "}, "status": {" running "}, "installationId": {" frontend "}, "search": {" plan "}}
	var ids []string
	for pageNumber := 0; pageNumber < 2; pageNumber++ {
		native := driveAs(t, handler, http.MethodGet, "/v1/plans?"+values.Encode(), "alice", "")
		if native.Code != http.StatusOK {
			t.Fatal(native.Body.String())
		}
		var expected controlplane.PlanSummaryPage
		if err := json.Unmarshal(native.Body.Bytes(), &expected); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]any{"limit": 1, "cursor": values.Get("cursor"), "environment": values.Get("environment"), "status": values.Get("status"), "installationId": values.Get("installationId"), "search": values.Get("search")})
		wire := postPlanSummaries(t, handler, "alice", "", string(body))
		if wire.Code != http.StatusOK {
			t.Fatalf("RPC=%d %s", wire.Code, wire.Body.String())
		}
		actual := &plansv1.ListPlanSummariesResponse{}
		if err := protojson.Unmarshal(wire.Body.Bytes(), actual); err != nil {
			t.Fatal(err)
		}
		if expected.Total != 2 || actual.Total != 2 || actual.PageSize != int32(expected.PageSize) || actual.Limit != int32(expected.Limit) || actual.HasPrevious != expected.HasPrevious || actual.NextCursor != expected.NextCursor || actual.PreviousCursor != expected.PreviousCursor || len(actual.Plans) != 1 || len(expected.Plans) != 1 {
			t.Fatalf("native=%+v RPC=%v", expected, actual)
		}
		nativePlan, rpcPlan := expected.Plans[0], actual.Plans[0]
		if rpcPlan.Id != nativePlan.ID || rpcPlan.Environment != "east" || rpcPlan.Status != string(nativePlan.Status) || rpcPlan.StepCount != int64(nativePlan.StepCount) || rpcPlan.ApprovalCount != int64(nativePlan.ApprovalCount) || rpcPlan.RequiredApprovals != int64(nativePlan.RequiredApprovals) || rpcPlan.Revision != nativePlan.Revision || !rpcPlan.CreatedAt.AsTime().Equal(nativePlan.CreatedAt) || !rpcPlan.UpdatedAt.AsTime().Equal(nativePlan.UpdatedAt) {
			t.Fatalf("summary changed: native=%+v RPC=%v", nativePlan, rpcPlan)
		}
		if strings.Contains(wire.Body.String(), "private") || strings.Contains(wire.Body.String(), "west") || strings.Contains(wire.Body.String(), "lease") || strings.Contains(wire.Body.String(), "PRIVATE") {
			t.Fatal("summary disclosed foreign or execution evidence")
		}
		ids = append(ids, rpcPlan.Id)
		values.Set("cursor", expected.NextCursor)
	}
	if ids[0] != "plan-b" || ids[1] != "plan-a" || values.Get("cursor") != "" {
		t.Fatalf("native order or terminal cursor changed: %v %v", ids, values)
	}
	for _, actor := range []string{"outsider", "root"} {
		wire := postPlanSummaries(t, handler, actor, "", `{}`)
		actual := &plansv1.ListPlanSummariesResponse{}
		if wire.Code != http.StatusOK || protojson.Unmarshal(wire.Body.Bytes(), actual) != nil {
			t.Fatalf("%s: %d %s", actor, wire.Code, wire.Body.String())
		}
		count := int64(0)
		if actor == "root" {
			count = 3
		}
		if actual.Total != count || int64(len(actual.Plans)) != count {
			t.Fatalf("%s: %v", actor, actual)
		}
	}
	for _, bad := range []string{`{"limit":0}`, `{"limit":201}`, `{"cursor":"malformed"}`} {
		wire := postPlanSummaries(t, handler, "alice", "", bad)
		if wire.Code != http.StatusBadRequest || !strings.Contains(wire.Body.String(), "invalid_argument") {
			t.Fatalf("input=%s status=%d %s", bad, wire.Code, wire.Body.String())
		}
	}
	for _, id := range []string{"plan-a", "plan-b", "plan-private"} {
		record, err := repository.GetPlan(t.Context(), id)
		if err != nil || record.Revision != uint64(9007199254740993) || record.Lease == nil || record.Lease.Token != "PRIVATE-LEASE-TOKEN" {
			t.Fatalf("read changed stored Plan: %+v %v", record, err)
		}
	}
}

func TestPlanSummaryConnectRetainsOriginalDelegatingCarrier(t *testing.T) {
	_, repository, _ := delegationHub(t)
	if _, err := repository.CreatePlan(t.Context(), controlplane.PlanRecord{ID: "secret-plan", DesiredStateDigest: "secret", Plan: controlplane.Plan{Environment: "secret-env"}, Status: controlplane.PlanRunning}); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"alice"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		actor, represented string
		count              int64
		status             int
	}{{"alice", "", 1, http.StatusOK}, {"alice", "bob", 1, http.StatusOK}, {"robot", "alice", 0, http.StatusOK}, {"bob", "alice", 0, http.StatusForbidden}, {"bob", "", 0, http.StatusOK}} {
		native := as(t, handler, "/v1/plans?view=summary", tc.actor, tc.represented)
		wire := postPlanSummaries(t, handler, tc.actor, tc.represented, `{}`)
		if native.Code != tc.status || wire.Code != tc.status {
			t.Fatalf("%s as %s: REST=%d %s RPC=%d %s", tc.actor, tc.represented, native.Code, native.Body.String(), wire.Code, wire.Body.String())
		}
		if tc.status != http.StatusOK {
			continue
		}
		var expected controlplane.PlanSummaryPage
		actual := &plansv1.ListPlanSummariesResponse{}
		if native.Code != http.StatusOK || wire.Code != http.StatusOK || json.Unmarshal(native.Body.Bytes(), &expected) != nil || protojson.Unmarshal(wire.Body.Bytes(), actual) != nil || actual.Total != tc.count || int64(expected.Total) != tc.count {
			t.Fatalf("%s as %s: REST=%d %s RPC=%d %s", tc.actor, tc.represented, native.Code, native.Body.String(), wire.Code, wire.Body.String())
		}
	}
}

func TestPlanSummaryConnectUsesChangeRequestVisibilityBeforeEnvironment(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	governance := controlplane.NewGovernanceService(repository)
	if _, err := governance.PutTeam(t.Context(), controlplane.Team{Name: "east", DisplayName: "East", AdminUsers: []string{"owner"}, MemberUsers: []string{"alice"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(t.Context(), controlplane.ResourceGrant{ResourceRID: "environment/east", Team: "east", Operations: []string{controlplane.OperationReadResource}}); err != nil {
		t.Fatal(err)
	}
	for _, cr := range []controlplane.ChangeRequest{
		{ID: "cr-owned", Author: controlplane.Principal{Username: "alice"}, Environment: "west", Status: controlplane.ChangePendingApprovals, Revision: 1},
		{ID: "cr-foreign", Author: controlplane.Principal{Username: "bob"}, Environment: "west", Status: controlplane.ChangePendingApprovals, Revision: 1},
	} {
		if _, err := repository.CreateChangeRequest(t.Context(), cr); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct{ id, environment, cr string }{
		{"plan-owned-cr", "west", "cr-owned"},
		{"plan-foreign-cr", "east", "cr-foreign"},
		{"plan-missing-cr", "east", "cr-missing"},
	} {
		if _, err := repository.CreatePlan(t.Context(), controlplane.PlanRecord{ID: row.id, DesiredStateDigest: "fixture", ChangeRequestID: row.cr, Plan: controlplane.Plan{Environment: row.environment}, Status: controlplane.PlanRunning}); err != nil {
			t.Fatal(err)
		}
	}
	handler, err := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	native := driveAs(t, handler, http.MethodGet, "/v1/plans?view=summary", "alice", "")
	var expected controlplane.PlanSummaryPage
	if native.Code != http.StatusOK || json.Unmarshal(native.Body.Bytes(), &expected) != nil {
		t.Fatalf("native: %d %s", native.Code, native.Body.String())
	}
	wire := postPlanSummaries(t, handler, "alice", "", `{}`)
	actual := &plansv1.ListPlanSummariesResponse{}
	if wire.Code != http.StatusOK || protojson.Unmarshal(wire.Body.Bytes(), actual) != nil {
		t.Fatalf("RPC: %d %s", wire.Code, wire.Body.String())
	}
	if expected.Total != 1 || len(expected.Plans) != 1 || expected.Plans[0].ID != "plan-owned-cr" || actual.Total != 1 || len(actual.Plans) != 1 || actual.Plans[0].Id != "plan-owned-cr" {
		t.Fatalf("CR visibility changed: native=%+v RPC=%v", expected, actual)
	}
}

func TestPlanSummaryConnectAuthenticatesBeforeTheNativeStore(t *testing.T) {
	repository := &planSummaryCountingRepository{Repository: controlplane.NewMemoryRepository()}
	handler, err := NewHub(repository, controlplane.HandlerOptions{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	before := repository.reads.Load()
	wire := postPlanSummaries(t, handler, "forged-dev-header", "", `{"limit":0,"actor":"root"}`)
	if wire.Code != http.StatusUnauthorized || repository.reads.Load() != before {
		t.Fatalf("production refusal read Plans: status=%d reads=%d body=%s", wire.Code, repository.reads.Load()-before, wire.Body.String())
	}
}

func TestPlanSummaryConnectSanitizesNativeStoreFailure(t *testing.T) {
	repository := &planSummaryCountingRepository{Repository: controlplane.NewMemoryRepository(), err: errors.New("PRIVATE database address and password")}
	handler, err := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	native := driveAs(t, handler, http.MethodGet, "/v1/plans?view=summary", "root", "")
	wire := postPlanSummaries(t, handler, "root", "", `{}`)
	if native.Code != http.StatusInternalServerError || wire.Code != http.StatusInternalServerError || strings.Contains(wire.Body.String(), "PRIVATE") || strings.Contains(wire.Body.String(), "password") || !strings.Contains(wire.Body.String(), "Plan summary index unavailable") {
		t.Fatalf("native=%d RPC=%d %s", native.Code, wire.Code, wire.Body.String())
	}
}

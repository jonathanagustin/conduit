package conduitcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	plansv1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1/plansv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	"github.com/jonathanagustin/conduit/internal/operator"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func executePlanTransport(t *testing.T, server, subject, mode string, args ...string) (string, error) {
	t.Helper()
	var output bytes.Buffer
	root := NewRoot(Runtime{Stdout: &output, Stderr: &output, Getenv: func(string) string { return "" }})
	argv := []string{"--server", server, "--development-subject", subject, "plan", "list"}
	if mode != "" {
		argv = append(argv, "--read-transport", mode)
	}
	root.SetArgs(append(argv, args...))
	err := root.Execute()
	return output.String(), err
}

func planReadPage() *plansv1.ListPlanSummariesResponse {
	at := time.Date(2026, time.November, 16, 10, 0, 0, 123456789, time.UTC)
	return &plansv1.ListPlanSummariesResponse{Total: 1, PageSize: 1, Limit: 200, Plans: []*plansv1.PlanSummary{{
		Id: "plan-a", Environment: "east", Status: "FUTURE_STATUS", StepCount: 101, ApprovalCount: 2, RequiredApprovals: 3,
		CreatedAt: timestamppb.New(at), UpdatedAt: timestamppb.New(at.Add(time.Second)), Revision: 9007199254740993,
	}}}
}

func writePlanReadPage(t *testing.T, w http.ResponseWriter, page *plansv1.ListPlanSummariesResponse) {
	t.Helper()
	wire, err := proto.Marshal(page)
	if err != nil {
		t.Error(err)
		return
	}
	w.Header().Set("Content-Type", "application/proto")
	_, _ = w.Write(wire)
}

func TestPlanListConnectPreservesFiltersCursorsAndNativeDTO(t *testing.T) {
	var requests []*plansv1.ListPlanSummariesRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != plansv1connect.PlanReadServiceListPlanSummariesProcedure || r.Header.Get("X-Platform-Subject") != "developer" {
			t.Error("wrong procedure or configured identity")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		query := &plansv1.ListPlanSummariesRequest{}
		if err := proto.Unmarshal(body, query); err != nil {
			t.Error(err)
			return
		}
		requests = append(requests, query)
		page := planReadPage()
		page.Total = 2
		if query.Cursor == "" {
			page.NextCursor = "opaque/+=%"
		} else {
			if query.Cursor != "opaque/+=%" {
				t.Error("opaque cursor changed")
			}
			page.Plans[0].Id = "plan-b"
		}
		writePlanReadPage(t, w, page)
	}))
	defer server.Close()
	output, err := executePlanTransport(t, server.URL, "developer", "connect", "--environment", "east", "--status", "FUTURE_STATUS", "--installation-id", "frontend", "--search", " app ")
	if err != nil {
		t.Fatal(err, output)
	}
	var result operator.PlanListResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err, output)
	}
	if result.Total != 2 || len(result.Plans) != 2 || result.Plans[0].ID != "plan-a" || result.Plans[1].ID != "plan-b" {
		t.Fatal(output)
	}
	p := result.Plans[0]
	expected := planReadPage().Plans[0]
	if p.Environment != expected.Environment || string(p.Status) != expected.Status || p.StepCount != 101 || p.ApprovalCount != 2 || p.RequiredApprovals != 3 || p.Revision != expected.Revision || !p.CreatedAt.Equal(expected.CreatedAt.AsTime()) || !p.UpdatedAt.Equal(expected.UpdatedAt.AsTime()) {
		t.Fatal("native DTO or exact evidence changed", output)
	}
	if len(requests) != 2 {
		t.Fatal("unexpected page count", len(requests))
	}
	for _, q := range requests {
		if q.Limit == nil || *q.Limit != 200 || q.Environment != "east" || q.Status != "FUTURE_STATUS" || q.InstallationId != "frontend" || q.Search != " app " {
			t.Fatal(q)
		}
	}
}

func TestPlanListConnectMatchesNativeHubAcrossAuthorizedPages(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	governance := controlplane.NewGovernanceService(repository)
	if _, err := governance.PutTeam(t.Context(), controlplane.Team{Name: "readers", DisplayName: "Readers", AdminUsers: []string{"owner"}, MemberUsers: []string{"alice"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(t.Context(), controlplane.ResourceGrant{ResourceRID: "environment/east", Team: "readers", Operations: []string{controlplane.OperationReadResource}}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, time.November, 16, 10, 0, 0, 123456789, time.UTC)
	for i := 0; i < 202; i++ {
		environment := "east"
		if i == 201 {
			environment = "west"
		}
		id := fmt.Sprintf("plan-%03d", i)
		if _, err := repository.CreatePlan(t.Context(), controlplane.PlanRecord{ID: id, DesiredStateDigest: id, Plan: controlplane.Plan{Environment: environment, Generation: id, Steps: []controlplane.PlanStep{{InstallationID: "frontend", Product: "app"}}}, Status: controlplane.PlanRunning, CreatedAt: at.Add(time.Duration(i) * time.Second), UpdatedAt: at, Revision: 9007199254740993, Lease: &controlplane.PlanLease{Token: "PRIVATE-LEASE"}}); err != nil {
			t.Fatal(err)
		}
	}
	handler, err := composition.NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	for _, actor := range []struct {
		name  string
		count int
	}{{"alice", 201}, {"outsider", 0}, {"root", 202}} {
		t.Run(actor.name, func(t *testing.T) {
			var reads []operator.PlanListResult
			for _, mode := range []string{"rest", "connect"} {
				output, err := executePlanTransport(t, server.URL, actor.name, mode)
				if err != nil {
					t.Fatal(err, output)
				}
				var result operator.PlanListResult
				if err := json.Unmarshal([]byte(output), &result); err != nil {
					t.Fatal(err)
				}
				if result.Total != actor.count || len(result.Plans) != actor.count || strings.Contains(output, "PRIVATE-LEASE") {
					t.Fatal("visibility or count changed", output)
				}
				reads = append(reads, result)
			}
			if !reflect.DeepEqual(reads[0], reads[1]) {
				t.Fatal("REST and Connect summaries differ")
			}
		})
	}
}

func TestPlanListTransportSelectionAndRefusals(t *testing.T) {
	for _, mode := range []string{"", "rest", "unknown", "connect"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if mode == "connect" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"code":"permission_denied","message":"refused"}`))
					return
				}
				if r.Method != http.MethodGet || r.URL.Path != "/v1/plans" || r.URL.Query().Get("view") != "summary" || r.URL.Query().Get("limit") != "200" {
					t.Error("REST default/rollback changed")
				}
				_, _ = w.Write([]byte(`{"plans":[],"total":0}`))
			}))
			defer server.Close()
			_, err := executePlanTransport(t, server.URL, "developer", mode)
			if mode == "unknown" {
				if err == nil || calls != 0 {
					t.Fatal(err, calls)
				}
				return
			}
			if calls != 1 || ((mode == "connect") != (err != nil)) {
				t.Fatal("refusal replayed or transport changed", err, calls)
			}
		})
	}
}

func TestPlanListConnectRejectsInvalidEvidenceAndCursorCycles(t *testing.T) {
	for _, kind := range []string{"page-size", "total", "limit", "missing-id", "missing-time", "invalid-time", "oversized", "cursor-cycle"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				page := planReadPage()
				switch kind {
				case "page-size":
					page.PageSize = 2
				case "total":
					page.Total = 0
				case "limit":
					page.Limit = 201
				case "missing-id":
					page.Plans[0].Id = ""
				case "missing-time":
					page.Plans[0].UpdatedAt = nil
				case "invalid-time":
					page.Plans[0].CreatedAt = &timestamppb.Timestamp{Seconds: 253402300800}
				case "oversized":
					page.Plans[0].Environment = strings.Repeat("x", (1<<20)+1)
				case "cursor-cycle":
					page.NextCursor = "repeat"
				}
				if calls > 3 {
					http.Error(w, "failed to bound cursor cycle", http.StatusInternalServerError)
					return
				}
				writePlanReadPage(t, w, page)
			}))
			defer server.Close()
			_, err := executePlanTransport(t, server.URL, "developer", "connect")
			want := 1
			if kind == "cursor-cycle" {
				want = 2
			}
			if err == nil || calls != want {
				t.Fatal("invalid evidence accepted or replayed", err, calls)
			}
		})
	}
}

func TestPlanListConnectCancelsTheActualRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	canceled := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		cancel()
		select {
		case <-r.Context().Done():
			canceled <- true
		case <-time.After(2 * time.Second):
			canceled <- false
		}
	}))
	defer server.Close()
	var output bytes.Buffer
	root := NewRoot(Runtime{Stdout: &output, Stderr: &output, Getenv: func(string) string { return "" }})
	root.SetArgs([]string{"--server", server.URL, "--development-subject", "developer", "plan", "list", "--read-transport", "connect"})
	if err := root.ExecuteContext(ctx); err == nil {
		t.Fatal("cancellation became success")
	}
	select {
	case ok := <-canceled:
		if !ok {
			t.Fatal("cancellation did not reach the HTTP request")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation was not observed")
	}
}

func TestPlanListConnectUsesConfiguredTLSIdentity(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != plansv1connect.PlanReadServiceListPlanSummariesProcedure || r.Header.Get("Authorization") != "Bearer fixture-access" {
			t.Error("configured identity or procedure lost")
		}
		writePlanReadPage(t, w, planReadPage())
	}))
	defer server.Close()
	token := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(token, []byte("fixture-access\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	root := NewRoot(Runtime{Stdout: &output, Stderr: &output, Getenv: func(string) string { return "" }})
	root.SetArgs([]string{"--server", server.URL, "--ca-file", certificatePEM(t, "hub", server), "--bearer-token-file", token, "plan", "list", "--read-transport", "connect"})
	if err := root.Execute(); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
}

func TestPlanListConnectUsesConfiguredTimeout(t *testing.T) {
	canceled := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
			canceled <- true
		case <-time.After(2 * time.Second):
			canceled <- false
		}
	}))
	defer server.Close()
	_, err := executePlanTransport(t, server.URL, "developer", "connect", "--timeout", "100ms")
	if err == nil {
		t.Fatal("timeout became success")
	}
	select {
	case ok := <-canceled:
		if !ok {
			t.Fatal("configured timeout did not cancel the actual request", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout cancellation was not observed")
	}
}

func TestPlanListConnectDoesNotReplayTypedRefusals(t *testing.T) {
	for _, failure := range []struct {
		status int
		code   string
	}{{401, "unauthenticated"}, {403, "permission_denied"}, {400, "invalid_argument"}, {404, "not_found"}, {500, "internal"}, {501, "unimplemented"}} {
		t.Run(failure.code, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != plansv1connect.PlanReadServiceListPlanSummariesProcedure {
					t.Error("refusal replayed through another route")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(failure.status)
				_ = json.NewEncoder(w).Encode(map[string]string{"code": failure.code, "message": "refused"})
			}))
			defer server.Close()
			_, err := executePlanTransport(t, server.URL, "developer", "connect")
			if err == nil || calls != 1 || connect.CodeOf(err).String() != failure.code {
				t.Fatal("refusal category changed, accepted or replayed", err, calls)
			}
		})
	}
}

func observedPlanRequestCancellation(t *testing.T, canceled <-chan bool) bool {
	t.Helper()
	select {
	case observed := <-canceled:
		return observed
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP request was not observed before the test deadline")
		return false
	}
}

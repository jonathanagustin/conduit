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
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"

	environmentsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1/environmentsv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	"github.com/jonathanagustin/conduit/internal/operator"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func executeEnvironmentRead(t *testing.T, server, view, mode, format string, extra ...string) (string, error) {
	t.Helper()
	var output bytes.Buffer
	root := NewRoot(Runtime{Stdout: &output, Stderr: io.Discard, Getenv: func(string) string { return "" }})
	args := []string{"--server", server, "--development-subject", "reader", "--output", format, "environment", "list"}
	if view != "" {
		args = append(args, "--view", view)
	}
	if mode != "" {
		args = append(args, "--read-transport", mode)
	}
	root.SetArgs(append(args, extra...))
	err := root.Execute()
	return output.String(), err
}

func validEnvironmentReadPage() *environmentsv1.ListEnvironmentSummariesResponse {
	return &environmentsv1.ListEnvironmentSummariesResponse{Total: 1, PageSize: 1, Limit: 200, Environments: []*environmentsv1.EnvironmentSummary{{Environment: &environmentsv1.EnvironmentIdentity{Id: "dev", Generation: "generation:opaque"}, Spoke: &environmentsv1.SpokeLiveness{State: "unreachable"}}}}
}

func writeEnvironmentReadPage(t *testing.T, w http.ResponseWriter, page *environmentsv1.ListEnvironmentSummariesResponse) {
	t.Helper()
	body, err := proto.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/proto")
	_, _ = w.Write(body)
}

func TestEnvironmentSummaryCLIUsesRealHubAndKeepsOutputParity(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	for i := 0; i < 201; i++ {
		at := time.Date(2025, 1, 1, 0, 0, i, 123, time.UTC)
		if i == 0 {
			at = time.Time{}
		}
		if _, err := repository.PutEnvironmentReport(t.Context(), controlplane.EnvironmentReport{Environment: fmt.Sprintf("env-%03d", i), AgentID: "agent", ObservedAt: at, Entities: []controlplane.ReportedEntity{{InstallationID: "private-entity-body"}}}); err != nil {
			t.Fatal(err)
		}
	}
	state := controlplane.DesiredState{Environment: controlplane.Environment{ID: "env-000", Generation: "generation:opaque", Config: map[string]any{"private": "private-configuration"}}}
	if _, err := repository.CreatePlan(t.Context(), controlplane.PlanRecord{ID: "desired", DesiredState: &state, Plan: controlplane.Plan{Environment: "env-000"}}); err != nil {
		t.Fatal(err)
	}
	handler := composition.NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"reader"}})
	server := httptest.NewServer(handler)
	defer server.Close()
	for _, format := range []string{"json", "yaml", "table", "csv"} {
		rest, err := executeEnvironmentRead(t, server.URL, "summary", "rest", format)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := executeEnvironmentRead(t, server.URL, "summary", "connect", format)
		if err != nil || wire != rest {
			t.Fatalf("%s parity: %v\nREST %s\nRPC %s", format, err, rest, wire)
		}
		if strings.Contains(wire, "private-configuration") || strings.Contains(wire, "private-entity-body") {
			t.Fatal("summary disclosed full fields")
		}
		if format == "json" {
			var list operator.EnvironmentSummaryList
			if json.Unmarshal([]byte(wire), &list) != nil || list.Total != 201 || len(list.Environments) != 201 || list.Environments[0].Environment.Generation != "generation:opaque" || list.Environments[0].Reported.EntityCount != 1 || list.Environments[0].Reported.ObservedAt != nil {
				t.Fatalf("summary output lost identity/count/presence: %s", wire)
			}
		}
	}
}

func TestEnvironmentSummaryCLIKeepsFullDefaultAndRejectsUnsupportedSelections(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/v1/environments" || r.URL.Query().Get("view") != "full" {
			t.Errorf("default transport/projection changed: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"environments":[{"environment":{"id":"dev","config":{"preserved":"full-record"}}}],"total":1}`)
	}))
	defer server.Close()
	output, err := executeEnvironmentRead(t, server.URL, "", "", "json")
	if err != nil || calls != 1 || !strings.Contains(output, "full-record") {
		t.Fatalf("default full output changed: %v %s", err, output)
	}
	for _, args := range []struct {
		view, mode string
		extra      []string
	}{{"full", "connect", []string{"--search", "dev"}}, {"other", "rest", nil}, {"full", "rest", []string{"--search", "dev"}}, {"summary", "other", nil}} {
		before := calls
		output, err := executeEnvironmentRead(t, server.URL, args.view, args.mode, "json", args.extra...)
		if err == nil || calls != before || output != "" {
			t.Fatalf("invalid selection performed request/output: %+v %v %s", args, err, output)
		}
	}
}

func TestEnvironmentSummaryCLIRejectsMalformedPagesWithoutReplay(t *testing.T) {
	for _, name := range []string{"missing-identity", "missing-spoke", "negative-total", "negative-count", "report-identity", "duplicate-identity", "bad-time", "repeated-cursor", "refusal"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != environmentsv1connect.EnvironmentReadServiceListEnvironmentSummariesProcedure || r.Header.Get("X-Platform-Subject") != "reader" {
					t.Errorf("request identity/transport changed")
				}
				request := &environmentsv1.ListEnvironmentSummariesRequest{}
				body, _ := io.ReadAll(r.Body)
				if proto.Unmarshal(body, request) != nil || request.SpaceId != "space_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || request.Search != "dev" || request.GetLimit() != 200 {
					t.Errorf("query/Space changed: %s", body)
				}
				if name == "refusal" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = io.WriteString(w, `{"code":"unavailable","message":"refused"}`)
					return
				}
				page := validEnvironmentReadPage()
				switch name {
				case "missing-identity":
					page.Environments[0].Environment = nil
				case "missing-spoke":
					page.Environments[0].Spoke = nil
				case "negative-total":
					page.Total = -1
				case "negative-count":
					page.Environments[0].Reported = &environmentsv1.EnvironmentReportSummary{Environment: "dev", EntityCount: -1}
				case "report-identity":
					page.Environments[0].Reported = &environmentsv1.EnvironmentReportSummary{Environment: "other"}
				case "duplicate-identity":
					page.Total, page.PageSize = 2, 2
					page.Environments = append(page.Environments, page.Environments[0])
				case "bad-time":
					page.Environments[0].Spoke.ObservedAt = &timestamppb.Timestamp{Seconds: 253402300800}
				case "repeated-cursor":
					page.NextCursor = "same"
					page.Environments[0].Environment.Id = fmt.Sprintf("cursor-row-%d", calls)
				}
				writeEnvironmentReadPage(t, w, page)
			}))
			defer server.Close()
			output, err := executeEnvironmentRead(t, server.URL, "summary", "connect", "json", "--space-id", "space_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--search", "dev")
			want := 1
			if name == "repeated-cursor" {
				want = 2
			}
			if err == nil || output != "" || calls != want {
				t.Fatalf("malformed page replayed or emitted partial output: %v calls=%d output=%s", err, calls, output)
			}
		})
	}
}

func TestEnvironmentSummaryCLIUsesTLSBearerAndRefusesRedirects(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		calls, followed := 0, 0
		sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { followed++; w.WriteHeader(http.StatusOK) }))
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.Header.Get("Authorization") != "Bearer fixture-environment-access" || r.URL.Path != environmentsv1connect.EnvironmentReadServiceListEnvironmentSummariesProcedure {
				t.Error("configured identity/procedure lost")
			}
			if redirect {
				http.Redirect(w, r, sink.URL, http.StatusTemporaryRedirect)
				return
			}
			writeEnvironmentReadPage(t, w, validEnvironmentReadPage())
		}))
		token := filepath.Join(t.TempDir(), "token")
		if err := os.WriteFile(token, []byte("fixture-environment-access\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		root := NewRoot(Runtime{Stdout: &output, Stderr: io.Discard, Getenv: func(string) string { return "" }})
		root.SetArgs([]string{"--server", server.URL, "--ca-file", certificatePEM(t, "environment-hub", server), "--bearer-token-file", token, "environment", "list", "--view", "summary", "--read-transport", "connect"})
		err := root.Execute()
		if calls != 1 || followed != 0 || (redirect && (err == nil || output.Len() != 0)) || (!redirect && err != nil) {
			t.Errorf("identity/redirect/replay contract: redirect=%v error=%v calls=%d followed=%d output=%s", redirect, err, calls, followed, output.String())
		}
		server.Close()
		sink.Close()
	}
}

func TestEnvironmentSummaryCLIPropagatesTimeoutAndCanceledContext(t *testing.T) {
	canceled := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
			canceled <- true
		case <-time.After(2 * time.Second):
			canceled <- false
		}
	}))
	defer server.Close()
	output, err := executeEnvironmentRead(t, server.URL, "summary", "connect", "json", "--timeout", "100ms")
	if err == nil || output != "" {
		t.Fatalf("timeout returned output or success: %v %s", err, output)
	}
	select {
	case observed := <-canceled:
		if !observed {
			t.Fatal("request context did not cancel")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation not observed")
	}
	adapter, err := operator.NewHTTPControlPlane(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.SetEnvironmentReadTransport("connect"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := adapter.ListEnvironmentSummaries(ctx, ""); connect.CodeOf(err) != connect.CodeCanceled {
		t.Fatalf("canceled read did not retain cancellation: %v", err)
	}
}

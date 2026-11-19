package conduitcli

import (
	"bytes"
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	environmentsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1/environmentsv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	"google.golang.org/protobuf/proto"
)

func executeReportedRead(ctx context.Context, server, mode, format string, extra ...string) (string, error) {
	var output bytes.Buffer
	root := NewRoot(Runtime{Stdout: &output, Stderr: io.Discard, Getenv: func(string) string { return "" }})
	args := []string{"--server", server, "--development-subject", "reader", "--output", format, "environment", "reported-state", "env"}
	if mode != "" {
		args = append(args, "--read-transport", mode)
	}
	root.SetArgs(append(args, extra...))
	err := root.ExecuteContext(ctx)
	return output.String(), err
}
func writeReportedRead(t *testing.T, w http.ResponseWriter, report *environmentsv1.ReportedEnvironmentState) {
	t.Helper()
	body, err := proto.Marshal(&environmentsv1.GetEnvironmentReportedStateResponse{Reported: report})
	if err != nil {
		t.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/proto")
	_, _ = w.Write(body)
}
func reportedCLIWire(params []byte) *environmentsv1.ReportedEnvironmentState {
	check := &environmentsv1.HealthCheck{ParamsJson: params}
	container := &environmentsv1.EntityContainerHealth{Checks: []*environmentsv1.HealthCheck{check}}
	node := &environmentsv1.EntityNodeHealth{Containers: []*environmentsv1.EntityContainerHealth{container}}
	entity := &environmentsv1.ReportedEntity{HealthDetails: &environmentsv1.EntityHealthDetails{Nodes: []*environmentsv1.EntityNodeHealth{node}}}
	return &environmentsv1.ReportedEnvironmentState{Environment: "env", Entities: []*environmentsv1.ReportedEntity{entity}}
}

func TestReportedStateCLIUsesRealHubWithJSONAndYAMLParity(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	at := time.Date(2026, 11, 19, 1, 2, 3, 123456789, time.UTC)
	for _, report := range []controlplane.EnvironmentReport{
		{Environment: "env", AgentID: "agent", PlanID: "plan", ObservedAt: at, Entities: []controlplane.ReportedEntity{{InstallationID: "install", Product: "example:app", Version: "1.2.3", Namespace: "ns", ReleaseDigest: "release", ConfigDigest: "config", Health: "FUTURE", RolloutStatus: "FUTURE_ROLLOUT", ConfigStatus: "FUTURE_CONFIG", Lifecycle: "FUTURE_LIFECYCLE", ObservedAt: at, HealthDetails: &controlplane.EntityHealthDetails{Quorum: "all", Nodes: []controlplane.EntityNodeHealth{{NodeID: "node", ProductVersion: "version", Result: "HEALTHY", Containers: []controlplane.EntityContainerHealth{{ContainerID: "container", Liveness: true, Result: "HEALTHY", Checks: []controlplane.HealthCheck{{ID: "check", Message: "literal <message>", Result: "HEALTHY", Params: map[string]any{"attempt": 3, "fraction": 1.25, "nil": nil}}}}}}}}}}, Secrets: []controlplane.ReportedSecret{{Assignment: "assignment", Namespace: "ns", Revision: 7, ProviderRef: "vault://metadata", KubernetesSecretName: "secret-name", Keys: []string{"key"}}}},
		{Environment: "env", Entities: []controlplane.ReportedEntity{{InstallationID: "empty-details", HealthDetails: &controlplane.EntityHealthDetails{}}}},
		{Environment: "env"},
	} {
		if _, err := repository.PutEnvironmentReport(t.Context(), report); err != nil {
			t.Fatal(err)
		}
		handler := composition.NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"reader"}})
		for _, format := range []string{"json", "yaml"} {
			rest, rpc := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" && r.URL.Path == "/v1/environments/env/reported-state" {
					rest++
				}
				if r.URL.Path == environmentsv1connect.EnvironmentObservationServiceGetEnvironmentReportedStateProcedure {
					rpc++
				}
				handler.ServeHTTP(w, r)
			}))
			native, err := executeReportedRead(t.Context(), server.URL, "", format)
			if err != nil {
				t.Fatal(err)
			}
			explicit, err := executeReportedRead(t.Context(), server.URL, "rest", format)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := executeReportedRead(t.Context(), server.URL, "connect", format)
			if err != nil {
				t.Fatal(err)
			}
			if native != explicit || native != wire || rest != 2 || rpc != 1 {
				t.Fatalf("%s parity/default changed: REST %d RPC %d\n%s\n%s", format, rest, rpc, native, wire)
			}
			server.Close()
		}
	}
}

func TestReportedStateCLIRejectsRefusalsAndMalformedResultsWithoutReplay(t *testing.T) {
	for _, status := range []int{401, 403, 404, 500, 503} {
		calls, rest := 0, 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.Method == "GET" {
				rest++
			}
			w.WriteHeader(status)
		}))
		output, err := executeReportedRead(t.Context(), server.URL, "connect", "json")
		server.Close()
		if err == nil || output != "" || calls != 1 || rest != 0 {
			t.Fatalf("status %d replayed/printed: %v calls=%d rest=%d output=%s", status, err, calls, rest, output)
		}
	}
	oversized := reportedCLIWire([]byte(`{"payload":"` + strings.Repeat("x", 1<<20) + `"}`))
	for _, report := range []*environmentsv1.ReportedEnvironmentState{nil, {Environment: "other"}, reportedCLIWire([]byte(`{} {}`)), oversized} {
		calls, rest := 0, 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.Method == "GET" {
				rest++
			}
			writeReportedRead(t, w, report)
		}))
		output, err := executeReportedRead(t.Context(), server.URL, "connect", "json")
		server.Close()
		if err == nil || output != "" || calls != 1 || rest != 0 {
			t.Fatalf("invalid result replayed/printed: %v calls=%d rest=%d output=%s", err, calls, rest, output)
		}
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	if output, err := executeReportedRead(t.Context(), server.URL, "automatic", "json"); err == nil || output != "" || calls != 0 {
		t.Fatalf("invalid mode reached network: %v %d", err, calls)
	}
}

func TestReportedStateCLIPreservesLargeNumbersInJSONAndYAML(t *testing.T) {
	report := reportedCLIWire([]byte(`{"exact":9007199254740993}`))
	report.Revision = math.MaxUint64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeReportedRead(t, w, report) }))
	defer server.Close()
	for _, format := range []string{"json", "yaml"} {
		output, err := executeReportedRead(t.Context(), server.URL, "connect", format)
		if err != nil {
			t.Fatal(err)
		}
		want := "exact: 9007199254740993"
		if format == "json" {
			want = `"exact": 9007199254740993`
		}
		if !strings.Contains(output, want) || !strings.Contains(output, "18446744073709551615") {
			t.Fatalf("%s number rounded/stringified: %s", format, output)
		}
	}
}

func TestReportedStateCLIUsesConfiguredTLSIdentityAndRefusesRedirects(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		calls, followed := 0, 0
		sink := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed++ }))
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.Header.Get("Authorization") != "Bearer fixture-observation-access" || r.URL.Path != environmentsv1connect.EnvironmentObservationServiceGetEnvironmentReportedStateProcedure {
				t.Error("configured carrier/procedure lost")
			}
			if redirect {
				http.Redirect(w, r, sink.URL, http.StatusTemporaryRedirect)
				return
			}
			writeReportedRead(t, w, &environmentsv1.ReportedEnvironmentState{Environment: "env"})
		}))
		token := filepath.Join(t.TempDir(), "token")
		if err := os.WriteFile(token, []byte("fixture-observation-access\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		root := NewRoot(Runtime{Stdout: &output, Stderr: io.Discard, Getenv: func(string) string { return "" }})
		root.SetArgs([]string{"--server", server.URL, "--ca-file", certificatePEM(t, "observation-hub", server), "--bearer-token-file", token, "environment", "reported-state", "env", "--read-transport", "connect"})
		err := root.Execute()
		server.Close()
		sink.Close()
		if calls != 1 || followed != 0 || (redirect && (err == nil || output.Len() != 0)) || (!redirect && err != nil) {
			t.Fatalf("TLS/redirect contract changed: %v calls=%d followed=%d output=%s", err, calls, followed, output.String())
		}
	}
}

func TestReportedStateCLIPropagatesCancellationAndTimeout(t *testing.T) {
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
	output, err := executeReportedRead(t.Context(), server.URL, "connect", "json", "--timeout", "100ms")
	if err == nil || output != "" {
		t.Fatalf("timeout returned success: %v %s", err, output)
	}
	select {
	case got := <-canceled:
		if !got {
			t.Fatal("deadline not propagated")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("deadline not observed")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if output, err := executeReportedRead(ctx, server.URL, "connect", "json"); err == nil || output != "" {
		t.Fatalf("canceled context returned success: %v %s", err, output)
	}
}

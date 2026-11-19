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
	"sync/atomic"
	"testing"
	"time"

	environmentsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1/environmentsv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	"google.golang.org/protobuf/proto"
)

func executeEnvironmentDetailRead(ctx context.Context, server, mode, format string, extra ...string) (string, error) {
	var output bytes.Buffer
	root := NewRoot(Runtime{Stdout: &output, Stderr: io.Discard, Getenv: func(string) string { return "" }})
	args := []string{"--server", server, "--development-subject", "reader", "--output", format, "environment", "get", "env"}
	if mode != "" {
		args = append(args, "--read-transport", mode)
	}
	root.SetArgs(append(args, extra...))
	err := root.ExecuteContext(ctx)
	return output.String(), err
}
func writeEnvironmentDetailRead(t *testing.T, w http.ResponseWriter, record *environmentsv1.EnvironmentRecord) {
	t.Helper()
	body, err := proto.Marshal(&environmentsv1.GetEnvironmentResponse{Record: record})
	if err != nil {
		t.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/proto")
	_, _ = w.Write(body)
}
func environmentDetailCLIWire() *environmentsv1.EnvironmentRecord {
	return &environmentsv1.EnvironmentRecord{Environment: &environmentsv1.Environment{Id: "env", ConfigJson: []byte("null")}, Spoke: &environmentsv1.SpokeLiveness{State: "unreachable"}}
}

func TestEnvironmentDetailCLIHasRealHubJSONYAMLParityAndRESTDefault(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	at := time.Date(2026, 11, 19, 1, 2, 3, 123456789, time.UTC)
	wide := int64(1)
	state := controlplane.DesiredState{Environment: controlplane.Environment{ID: "env", Generation: "opaque", PackageArchitecture: "arm64", ConnectionSetting: "raw-connection", DeclaredStateSource: "raw-source", DefaultReleaseChannel: "raw-channel", Labels: map[string]string{"key": "value"}, Config: map[string]any{"literal": "<value>", "integer": 42, "fraction": 1.25}, Namespaces: []controlplane.NamespaceTarget{{RID: "namespace", Name: "ns", DeletionProtected: new(false), Organizations: []controlplane.Organization{{DisplayName: "org", PrimaryHostname: "example"}}, Installed: map[string]controlplane.InstalledTarget{"app": {Version: "1", ConfigDigest: "config"}}}}, ArtifactSecurityPolicy: &controlplane.ArtifactSecurityPolicy{RequiredOutcome: "PASS"}, ArtifactAvailabilityPolicy: &controlplane.ArtifactAvailabilityPolicy{RequiredOutcome: "ANY"}, ArtifactRegistries: []controlplane.ArtifactRegistryAssignment{{ID: "registry", Revision: int(wide << 40), Mirrors: []string{"mirror"}, CredentialRef: "opaque://credential"}}, Secrets: []controlplane.ExternalSecretAssignment{{Name: "secret", Revision: 3, ProviderRef: "opaque://provider", Keys: []string{"key"}}}, AutomaticSecurityRecall: true, VulnerabilityUpgrades: &controlplane.VulnerabilityUpgradePolicy{RescanSLASeconds: 23}, AutoSuppressionThreshold: 7, Maintenance: controlplane.MaintenancePolicy{Downtime: &controlplane.WeeklyMaintenanceWindow{TimeZone: "UTC", Intervals: []string{"MON 00:00-01:00"}}, Overrides: []controlplane.MaintenanceOverride{{InstallationID: "installation", Type: "raw-type", StartsAt: at, EndsAt: at.Add(time.Hour), Reason: "review", ApprovedBy: "recorded-reviewer"}}}, HelmExecution: &controlplane.HelmExecutionDefaults{DefaultRolloutStrategy: "raw-strategy", ImagePrePull: &controlplane.ImagePrePullPolicy{Enabled: false}}}}
	if _, err := repository.CreatePlan(t.Context(), controlplane.PlanRecord{ID: "plan", Plan: controlplane.Plan{Environment: "env"}, DesiredState: &state, CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
	for _, report := range []*controlplane.EnvironmentReport{nil, {Environment: "env", AgentID: "agent", PlanID: "plan", ObservedAt: at, Entities: []controlplane.ReportedEntity{{InstallationID: "installation", Health: "FUTURE", ObservedAt: at}}}, {Environment: "env"}} {
		if report != nil {
			if _, err := repository.PutEnvironmentReport(t.Context(), *report); err != nil {
				t.Fatal(err)
			}
		}
		hub := composition.NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"reader"}})
		for _, format := range []string{"json", "yaml"} {
			var rest, rpc atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" && r.URL.Path == "/v1/environments/env" {
					rest.Add(1)
				}
				if r.URL.Path == environmentsv1connect.EnvironmentDetailServiceGetEnvironmentProcedure {
					rpc.Add(1)
				}
				hub.ServeHTTP(w, r)
			}))
			native, err := executeEnvironmentDetailRead(t.Context(), server.URL, "", format)
			if err != nil {
				t.Fatal(err)
			}
			explicit, err := executeEnvironmentDetailRead(t.Context(), server.URL, "rest", format)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := executeEnvironmentDetailRead(t.Context(), server.URL, "connect", format)
			if err != nil {
				t.Fatal(err)
			}
			server.Close()
			if native != explicit || native != wire || rest.Load() != 2 || rpc.Load() != 1 {
				t.Fatalf("%s parity/default changed: REST%d RPC%d\n%s\n%s", format, rest.Load(), rpc.Load(), native, wire)
			}
		}
	}
}

func TestEnvironmentDetailCLIRefusesMalformedOrFailedResponsesWithoutReplay(t *testing.T) {
	for _, status := range []int{401, 403, 404, 500, 503} {
		var calls, rest atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			if r.Method == "GET" {
				rest.Add(1)
			}
			w.WriteHeader(status)
		}))
		output, err := executeEnvironmentDetailRead(t.Context(), server.URL, "connect", "json")
		server.Close()
		if err == nil || output != "" || calls.Load() != 1 || rest.Load() != 0 {
			t.Fatalf("status%d replayed/printed: %v %d/%d %s", status, err, calls.Load(), rest.Load(), output)
		}
	}
	for _, change := range []func(*environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord{
		func(*environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord { return nil },
		func(r *environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord {
			r.Environment.Id = "other"
			return r
		},
		func(r *environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord { r.Spoke = nil; return r },
		func(r *environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord {
			r.Environment.ConfigJson = []byte("{} {}")
			return r
		},
		func(r *environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord {
			r.Reported = &environmentsv1.ReportedEnvironmentState{Environment: "other"}
			return r
		},
		func(r *environmentsv1.EnvironmentRecord) *environmentsv1.EnvironmentRecord {
			r.Environment.ConfigJson = []byte(`{"payload":"` + strings.Repeat("x", 1<<20) + `"}`)
			return r
		},
	} {
		var calls, rest atomic.Int32
		record := change(environmentDetailCLIWire())
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			if r.Method == "GET" {
				rest.Add(1)
			}
			writeEnvironmentDetailRead(t, w, record)
		}))
		output, err := executeEnvironmentDetailRead(t.Context(), server.URL, "connect", "json")
		server.Close()
		if err == nil || output != "" || calls.Load() != 1 || rest.Load() != 0 {
			t.Fatalf("invalid result replayed/printed: %v %d/%d %s", err, calls.Load(), rest.Load(), output)
		}
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	if output, err := executeEnvironmentDetailRead(t.Context(), server.URL, "automatic", "json"); err == nil || output != "" || calls.Load() != 0 {
		t.Fatalf("invalid mode reached network: %v %d", err, calls.Load())
	}
}

func TestEnvironmentDetailCLIPreservesLargeConfigurationAndReportNumbers(t *testing.T) {
	record := environmentDetailCLIWire()
	record.Environment.ConfigJson = []byte(`{"exact":9007199254740993}`)
	record.Reported = &environmentsv1.ReportedEnvironmentState{Environment: "env", Revision: math.MaxUint64}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeEnvironmentDetailRead(t, w, record) }))
	defer server.Close()
	for _, format := range []string{"json", "yaml"} {
		output, err := executeEnvironmentDetailRead(t.Context(), server.URL, "connect", format)
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

func TestEnvironmentDetailCLIUsesSelectedSpaceOnBothTransports(t *testing.T) {
	const space = "space_0123456789abcdef0123456789abcdef"
	for _, mode := range []string{"rest", "connect"} {
		for _, included := range []bool{false, true} {
			repository := controlplane.NewMemoryRepository()
			if _, err := repository.PutEnvironmentReport(t.Context(), controlplane.EnvironmentReport{Environment: "env"}); err != nil {
				t.Fatal(err)
			}
			var selected atomic.Int32
			hub := composition.NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"reader"}, RequireSpaceSelection: func(_ context.Context, p controlplane.Principal, id string) error {
				selected.Add(1)
				if p.Username != "reader" || id != space {
					t.Errorf("selection identity changed: %+v %s", p, id)
				}
				return nil
			}, SpaceIDLookup: func(context.Context, controlplane.Principal, string, string) (string, error) { return space, nil }, LoadSpaceIndexMembership: func(context.Context, controlplane.Principal, string) (controlplane.SpaceIndexMembership, error) {
				if included {
					return controlplane.SpaceIndexMembership{EnvironmentIDs: []string{"env"}}, nil
				}
				return controlplane.SpaceIndexMembership{}, nil
			}})
			server := httptest.NewServer(hub)
			output, err := executeEnvironmentDetailRead(t.Context(), server.URL, mode, "json", "--space-id", space)
			server.Close()
			if (err == nil) != included || (output != "") != included || selected.Load() != 1 {
				t.Fatalf("%s included%v: %v selection%d output%s", mode, included, err, selected.Load(), output)
			}
		}
	}
}

func TestEnvironmentDetailCLIUsesConfiguredTLSIdentityAndRefusesRedirects(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		var calls, followed atomic.Int32
		sink := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed.Add(1) }))
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			if r.Header.Get("Authorization") != "Bearer fixture-detail-access" || r.URL.Path != environmentsv1connect.EnvironmentDetailServiceGetEnvironmentProcedure {
				t.Errorf("configured identity/route changed: %s", r.URL.Path)
			}
			if redirect {
				http.Redirect(w, r, sink.URL, http.StatusFound)
				return
			}
			writeEnvironmentDetailRead(t, w, environmentDetailCLIWire())
		}))
		token := filepath.Join(t.TempDir(), "access-token")
		if err := os.WriteFile(token, []byte("fixture-detail-access"), 0o600); err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		root := NewRoot(Runtime{Stdout: &output, Stderr: io.Discard, Getenv: func(string) string { return "" }})
		root.SetArgs([]string{"--server", server.URL, "--ca-file", certificatePEM(t, "detail-hub", server), "--bearer-token-file", token, "environment", "get", "env", "--read-transport", "connect"})
		err := root.ExecuteContext(t.Context())
		server.Close()
		sink.Close()
		if (err != nil) != redirect || calls.Load() != 1 || followed.Load() != 0 || (redirect && output.Len() != 0) {
			t.Fatalf("TLS/redirect behavior changed: %v %d/%d %s", err, calls.Load(), followed.Load(), output.String())
		}
	}
}

func TestEnvironmentDetailCLIPropagatesCancellationAndTimeout(t *testing.T) {
	var calls atomic.Int32
	observed := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
			observed <- true
		case <-time.After(2 * time.Second):
			observed <- false
		}
	}))
	defer server.Close()
	output, err := executeEnvironmentDetailRead(t.Context(), server.URL, "connect", "json", "--timeout", "250ms")
	if err == nil || output != "" || calls.Load() != 1 {
		t.Fatalf("timeout behavior changed: %v %d %s", err, calls.Load(), output)
	}
	select {
	case cancelled := <-observed:
		if !cancelled {
			t.Fatal("deadline not propagated to request")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("deadline not observed")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	before := calls.Load()
	output, err = executeEnvironmentDetailRead(ctx, server.URL, "connect", "json")
	if err == nil || output != "" || calls.Load() != before {
		t.Fatalf("cancelled read reached network: %v %d %s", err, calls.Load(), output)
	}
}

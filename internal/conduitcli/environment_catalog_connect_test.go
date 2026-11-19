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
	"sync/atomic"
	"testing"
	"time"

	environmentsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1/environmentsv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	"google.golang.org/protobuf/proto"
)

func TestEnvironmentCatalogCLIHasRealHubMultiPageParityAndRESTDefault(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	for i := 0; i < 41; i++ {
		id := fmt.Sprintf("env-%03d", i)
		if _, err := repository.PutEnvironmentReport(t.Context(), controlplane.EnvironmentReport{Environment: id, AgentID: "agent", ObservedAt: time.Date(2025, 1, 1, 0, 0, i, 123, time.UTC), Entities: []controlplane.ReportedEntity{{InstallationID: "full-entity-body"}}}); err != nil {
			t.Fatal(err)
		}
	}
	state := controlplane.DesiredState{Environment: controlplane.Environment{ID: "env-000", Generation: "opaque:generation", Config: map[string]any{"literal": "<full-config>", "enabled": false}}}
	if _, err := repository.CreatePlan(t.Context(), controlplane.PlanRecord{ID: "desired", DesiredState: &state, Plan: controlplane.Plan{Environment: "env-000"}}); err != nil {
		t.Fatal(err)
	}
	handler := composition.NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"reader"}})
	counts := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counts[r.Method+" "+r.URL.Path]++
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	for _, format := range []string{"json", "yaml", "table", "csv"} {
		rest, err := executeEnvironmentRead(t, server.URL, "full", "rest", format)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := executeEnvironmentRead(t, server.URL, "full", "connect", format)
		if err != nil || wire != rest {
			t.Fatalf("%s parity: %v\nREST %s\nRPC %s", format, err, rest, wire)
		}
		if format == "json" {
			var list struct {
				Environments []controlplane.EnvironmentCatalogRecord `json:"environments"`
			}
			if err := json.Unmarshal([]byte(wire), &list); err != nil {
				t.Fatal(err)
			}
			if len(list.Environments) != 41 || list.Environments[0].Environment.Config["literal"] != "<full-config>" || list.Environments[0].Reported.Entities[0].InstallationID != "full-entity-body" {
				t.Fatalf("lost full fields: %s", wire)
			}
		}
	}
	before := counts["POST "+environmentsv1connect.EnvironmentCatalogServiceListEnvironmentsProcedure]
	if _, err := executeEnvironmentRead(t, server.URL, "", "", "json"); err != nil {
		t.Fatal(err)
	}
	if before != 12 || counts["POST "+environmentsv1connect.EnvironmentCatalogServiceListEnvironmentsProcedure] != before {
		t.Fatalf("full pages/default changed: %v", counts)
	}
}

func TestEnvironmentCatalogCLIRejectsMalformedPagesWithoutReplay(t *testing.T) {
	for _, name := range []string{"missing-record", "report-mismatch", "negative-total", "wrong-limit", "wrong-size", "duplicate-ID", "empty-next", "repeated-cursor", "second-page-refusal"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != environmentsv1connect.EnvironmentCatalogServiceListEnvironmentsProcedure || r.Header.Get("X-Platform-Subject") != "reader" {
					t.Errorf("transport/identity changed")
				}
				request := &environmentsv1.ListEnvironmentsRequest{}
				body, _ := io.ReadAll(r.Body)
				if err := proto.Unmarshal(body, request); err != nil || request.GetLimit() != 20 || request.GetSpaceId() != "space_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
					t.Errorf("selection changed: %v %v", request, err)
				}
				record := environmentDetailCLIWire()
				page := &environmentsv1.ListEnvironmentsResponse{Environments: []*environmentsv1.EnvironmentRecord{record}, Total: 1, PageSize: 1, Limit: 20}
				switch name {
				case "missing-record":
					page.Environments[0] = nil
				case "report-mismatch":
					record.Reported = &environmentsv1.ReportedEnvironmentState{Environment: "other"}
				case "negative-total":
					page.Total = -1
				case "wrong-limit":
					page.Limit = 200
				case "wrong-size":
					page.PageSize = 2
				case "duplicate-ID":
					page.Environments = append(page.Environments, record)
					page.PageSize = 2
					page.Total = 2
				case "empty-next":
					page.Environments = nil
					page.PageSize = 0
					page.NextCursor = "next"
				case "repeated-cursor":
					record.Environment.Id = fmt.Sprintf("env-%d", calls)
					page.NextCursor = "same"
				case "second-page-refusal":
					if calls == 2 {
						http.Error(w, "refused", http.StatusForbidden)
						return
					}
					page.NextCursor = "next"
				}
				encoded, err := proto.Marshal(page)
				if err != nil {
					t.Error(err)
					return
				}
				w.Header().Set("Content-Type", "application/proto")
				_, _ = w.Write(encoded)
			}))
			defer server.Close()
			output, err := executeEnvironmentRead(t, server.URL, "full", "connect", "json", "--space-id", "space_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
			want := 1
			if name == "repeated-cursor" || name == "second-page-refusal" {
				want = 2
			}
			if err == nil || output != "" || calls != want {
				t.Fatalf("malformed page output/replay: %v output=%s calls=%d want=%d", err, output, calls, want)
			}
		})
	}
}

func writeEnvironmentCatalogRead(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	body, err := proto.Marshal(&environmentsv1.ListEnvironmentsResponse{Environments: []*environmentsv1.EnvironmentRecord{environmentDetailCLIWire()}, Total: 1, PageSize: 1, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/proto")
	_, _ = w.Write(body)
}
func TestEnvironmentCatalogCLIUsesConfiguredTLSIdentityAndRefusesRedirects(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		var calls, followed atomic.Int32
		sink := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed.Add(1) }))
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			if r.Header.Get("Authorization") != "Bearer fixture-detail-access" || r.URL.Path != environmentsv1connect.EnvironmentCatalogServiceListEnvironmentsProcedure {
				t.Errorf("configured identity/route changed: %s", r.URL.Path)
			}
			if redirect {
				http.Redirect(w, r, sink.URL, http.StatusFound)
				return
			}
			writeEnvironmentCatalogRead(t, w)
		}))
		token := filepath.Join(t.TempDir(), "access-token")
		if err := os.WriteFile(token, []byte("fixture-detail-access"), 0o600); err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		root := NewRoot(Runtime{Stdout: &output, Stderr: io.Discard, Getenv: func(string) string { return "" }})
		root.SetArgs([]string{"--server", server.URL, "--ca-file", certificatePEM(t, "detail-hub", server), "--bearer-token-file", token, "environment", "list", "--read-transport", "connect"})
		err := root.ExecuteContext(t.Context())
		server.Close()
		sink.Close()
		if (err != nil) != redirect || calls.Load() != 1 || followed.Load() != 0 || (redirect && output.Len() != 0) {
			t.Fatalf("TLS/redirect behavior changed: %v %d/%d %s", err, calls.Load(), followed.Load(), output.String())
		}
	}
}

func TestEnvironmentCatalogCLIPropagatesTimeoutAndCancellation(t *testing.T) {
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
	execute := func(ctx context.Context) (string, error) {
		var output bytes.Buffer
		root := NewRoot(Runtime{Stdout: &output, Stderr: io.Discard, Getenv: func(string) string { return "" }})
		root.SetArgs([]string{"--server", server.URL, "--development-subject", "reader", "--timeout", "250ms", "environment", "list", "--read-transport", "connect"})
		err := root.ExecuteContext(ctx)
		return output.String(), err
	}
	output, err := execute(t.Context())
	if err == nil || output != "" || calls.Load() != 1 {
		t.Fatalf("timeout: %v %s calls%d", err, output, calls.Load())
	}
	select {
	case cancelled := <-observed:
		if !cancelled {
			t.Fatal("request deadline not propagated")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request cancellation not observed")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	output, err = execute(ctx)
	if err == nil || output != "" || calls.Load() != 1 {
		t.Fatalf("cancelled request replayed: %v %s calls%d", err, output, calls.Load())
	}
}

func TestEnvironmentCatalogCLIUsesSelectedSpaceOnBothTransports(t *testing.T) {
	const space = "space_0123456789abcdef0123456789abcdef"
	for _, mode := range []string{"rest", "connect"} {
		for _, included := range []bool{false, true} {
			repository := controlplane.NewMemoryRepository()
			if _, err := repository.PutEnvironmentReport(t.Context(), controlplane.EnvironmentReport{Environment: "env"}); err != nil {
				t.Fatal(err)
			}
			var selected atomic.Int32
			handler := composition.NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"reader"}, RequireSpaceSelection: func(_ context.Context, p controlplane.Principal, id string) error {
				selected.Add(1)
				if p.Username != "reader" || id != space {
					t.Errorf("selection identity: %+v %s", p, id)
				}
				return nil
			}, SpaceIDLookup: func(context.Context, controlplane.Principal, string, string) (string, error) { return space, nil }, LoadSpaceIndexMembership: func(context.Context, controlplane.Principal, string) (controlplane.SpaceIndexMembership, error) {
				if included {
					return controlplane.SpaceIndexMembership{EnvironmentIDs: []string{"env"}}, nil
				}
				return controlplane.SpaceIndexMembership{}, nil
			}})
			server := httptest.NewServer(handler)
			output, err := executeEnvironmentRead(t, server.URL, "full", mode, "json", "--space-id", space)
			server.Close()
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				Environments []controlplane.EnvironmentCatalogRecord `json:"environments"`
			}
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatal(err)
			}
			want := 0
			if included {
				want = 1
			}
			if selected.Load() != 1 || len(result.Environments) != want {
				t.Fatalf("%s included%v selected%d rows%d", mode, included, selected.Load(), len(result.Environments))
			}
		}
	}
}

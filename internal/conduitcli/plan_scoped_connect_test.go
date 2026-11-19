package conduitcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

	plansv1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1/plansv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	"github.com/jonathanagustin/conduit/internal/operator"
	"google.golang.org/protobuf/proto"
)

const planCLITestSpace = "space_0123456789abcdef0123456789abcdef"

func executeScopedPlanRead(t *testing.T, server, actor, mode, format string, extra ...string) (string, error) {
	t.Helper()
	var output bytes.Buffer
	root := NewRoot(Runtime{Stdout: &output, Stderr: io.Discard, Getenv: func(string) string { return "" }})
	args := []string{"--server", server, "--development-subject", actor, "--space-id", planCLITestSpace, "--output", format, "plan", "list"}
	if mode != "" {
		args = append(args, "--read-transport", mode)
	}
	root.SetArgs(append(args, extra...))
	err := root.ExecuteContext(t.Context())
	return output.String(), err
}

func TestScopedPlanCLIHasRealHubMultiPageParityAndRESTDefault(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	for i := 0; i < 202; i++ {
		environment := "east"
		if i == 201 {
			environment = "west"
		}
		at := time.Date(2026, 11, 19, 0, 0, i, 123, time.UTC)
		if _, err := repository.CreatePlan(t.Context(), controlplane.PlanRecord{ID: fmt.Sprintf("plan-%03d", i), DesiredStateDigest: "must-not-travel", Plan: controlplane.Plan{Environment: environment, Steps: []controlplane.PlanStep{{InstallationID: "app"}}}, Status: controlplane.PlanRunning, CreatedAt: at, UpdatedAt: at.Add(time.Second), RequiredApprovals: 2, Revision: math.MaxUint64 - uint64(i)}); err != nil {
			t.Fatal(err)
		}
	}
	hub := composition.NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"reader"}, RequireSpaceSelection: func(_ context.Context, p controlplane.Principal, id string) error {
		if (p.Username != "reader" && p.Username != "outsider") || id != planCLITestSpace {
			t.Errorf("Space carrier/selection changed: %+v %s", p, id)
		}
		return nil
	}, SpaceIDLookup: func(context.Context, controlplane.Principal, string, string) (string, error) {
		t.Error("unrelated resource lookup")
		return "", nil
	}, LoadSpaceIndexMembership: func(context.Context, controlplane.Principal, string) (controlplane.SpaceIndexMembership, error) {
		return controlplane.SpaceIndexMembership{EnvironmentIDs: []string{"east"}}, nil
	}})
	var rest, scoped, unscoped atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/plans":
			rest.Add(1)
		case plansv1connect.PlanScopedReadServiceListPlanSummariesInSpaceProcedure:
			scoped.Add(1)
		case plansv1connect.PlanReadServiceListPlanSummariesProcedure:
			unscoped.Add(1)
		}
		hub.ServeHTTP(w, r)
	}))
	defer server.Close()
	for _, format := range []string{"json", "yaml", "table", "csv"} {
		native, err := executeScopedPlanRead(t, server.URL, "reader", "rest", format)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := executeScopedPlanRead(t, server.URL, "reader", "connect", format)
		if err != nil || native != wire {
			t.Fatalf("%s parity: %v\nREST%s\nRPC%s", format, err, native, wire)
		}
		if strings.Contains(wire, "must-not-travel") || strings.Contains(wire, "west") {
			t.Fatal("private/unselected fields escaped")
		}
		if format == "json" {
			var result operator.PlanListResult
			if err := json.Unmarshal([]byte(wire), &result); err != nil || result.Total != 201 || len(result.Plans) != 201 || !strings.Contains(wire, "18446744073709551615") {
				t.Fatalf("rows/precision: %+v err%v", result, err)
			}
		}
	}
	if rest.Load() != 8 || scoped.Load() != 8 || unscoped.Load() != 0 {
		t.Fatalf("scoped pages used wrong transport: REST%d scoped%d unscoped%d", rest.Load(), scoped.Load(), unscoped.Load())
	}
	before := scoped.Load()
	if _, err := executeScopedPlanRead(t, server.URL, "reader", "", "json"); err != nil {
		t.Fatal(err)
	}
	if scoped.Load() != before || unscoped.Load() != 0 {
		t.Fatal("default changed transport")
	}
	for _, mode := range []string{"rest", "connect"} {
		output, err := executeScopedPlanRead(t, server.URL, "outsider", mode, "json")
		var page operator.PlanListResult
		if err != nil || json.Unmarshal([]byte(output), &page) != nil || page.Total != 0 || len(page.Plans) != 0 {
			t.Fatalf("membership granted Plan authority: %s %s %v", mode, output, err)
		}
	}
}

func TestScopedPlanCLIUsesConfiguredTLSIdentityAndRefusesRedirect(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		var calls, followed atomic.Int32
		sink := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed.Add(1) }))
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			if r.URL.Path != plansv1connect.PlanScopedReadServiceListPlanSummariesInSpaceProcedure || r.Header.Get("Authorization") != "Bearer scoped-plan-fixture" {
				t.Errorf("configured transport changed: %s", r.URL)
			}
			if redirect {
				http.Redirect(w, r, sink.URL, http.StatusFound)
				return
			}
			encoded, err := proto.Marshal(&plansv1.ListPlanSummariesInSpaceResponse{SpaceId: planCLITestSpace, Page: planReadPage()})
			if err != nil {
				t.Error(err)
				return
			}
			w.Header().Set("Content-Type", "application/proto")
			_, _ = w.Write(encoded)
		}))
		token := filepath.Join(t.TempDir(), "access-token")
		if err := os.WriteFile(token, []byte("scoped-plan-fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		root := NewRoot(Runtime{Stdout: &output, Stderr: io.Discard, Getenv: func(string) string { return "" }})
		root.SetArgs([]string{"--server", server.URL, "--ca-file", certificatePEM(t, "scoped-plan", server), "--bearer-token-file", token, "--space-id", planCLITestSpace, "plan", "list", "--read-transport", "connect"})
		err := root.ExecuteContext(t.Context())
		server.Close()
		sink.Close()
		if (err != nil) != redirect || calls.Load() != 1 || followed.Load() != 0 || (redirect && output.Len() != 0) {
			t.Fatalf("TLS/redirect: %v calls%d followed%d output%s", err, calls.Load(), followed.Load(), output.String())
		}
	}
}

func TestScopedPlanCLIPropagatesTimeoutAndCancellation(t *testing.T) {
	var calls atomic.Int32
	observed := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != plansv1connect.PlanScopedReadServiceListPlanSummariesInSpaceProcedure {
			t.Errorf("wrong cancellation procedure: %s", r.URL.Path)
		}
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
		root.SetArgs([]string{"--server", server.URL, "--development-subject", "reader", "--space-id", planCLITestSpace, "--timeout", "500ms", "--output", "json", "plan", "list", "--read-transport", "connect"})
		err := root.ExecuteContext(ctx)
		return output.String(), err
	}
	output, err := execute(t.Context())
	if err == nil || output != "" || calls.Load() != 1 {
		t.Fatalf("timeout/output: %v %s calls%d", err, output, calls.Load())
	}
	select {
	case cancelled := <-observed:
		if !cancelled {
			t.Fatal("deadline not propagated")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request deadline not observed")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	output, err = execute(ctx)
	if err == nil || output != "" || calls.Load() != 1 {
		t.Fatalf("cancelled request replayed: %v %s calls%d", err, output, calls.Load())
	}
}

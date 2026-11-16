package conduitcli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	packagingv1 "github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1/packagingv1connect"
	"google.golang.org/protobuf/proto"
	"io"
)

func TestRunListUsesGeneratedConnectAndRetainsNativeEvidence(t *testing.T) {
	var mu sync.Mutex
	var requests []*packagingv1.ListPackagingRunsRequest
	var other []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != "POST" || r.URL.Path != packagingv1connect.PackagingRunServiceListPackagingRunsProcedure {
			other = append(other, r.Method+" "+r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Platform-Subject") != "developer" {
			t.Error("configured native identity did not reach RPC")
			http.Error(w, "refused", 401)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		query := &packagingv1.ListPackagingRunsRequest{}
		if err := proto.Unmarshal(body, query); err != nil {
			t.Error(err)
			return
		}
		requests = append(requests, query)
		id, next := "run-a", "opaque/+=%"
		if query.Cursor != "" {
			if query.Cursor != next {
				t.Error("cursor changed")
			}
			id, next = "run-b", ""
		}
		page := &packagingv1.ListPackagingRunsResponse{Tier: "CONDUIT_NATIVE", Configured: true, Authority: "packaging-run", PlanAuthority: "conduit-plan", ApprovalAuthority: "conduit-governance", PageSize: 1, Limit: 200, Total: 2, NextCursor: next, Items: []*packagingv1.GetPackagingRunResponse{{RunId: id, Phase: "review", Outcome: "WAITING", TargetPlanRef: "prod/app", TemplateVersion: "v1", ToolVersions: "zarf=0.82.0", AttachmentSha256: []string{strings.Repeat("a", 64)}, HandoffReconciled: true, DuplicateReviews: 3}}}
		wire, err := proto.Marshal(page)
		if err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/proto")
		w.Write(wire)
	}))
	defer server.Close()
	output, err := executeRunTransport(t, server.URL, "connect", "--phase", "review", "--outcome", "WAITING", "--plan-id", "p1", "--search", "app")
	if err != nil {
		t.Fatalf("run list: %v %s", err, output)
	}
	var result struct {
		Items []struct {
			RunID            string   `json:"runId"`
			TemplateVersion  string   `json:"templateVersion"`
			ToolVersions     string   `json:"toolVersions"`
			Attachment       []string `json:"attachmentSha256"`
			DuplicateReviews int      `json:"duplicateReviews"`
		}
		Total             int
		ApprovalAuthority string
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 || result.Total != 2 || result.ApprovalAuthority != "conduit-governance" || result.Items[0].RunID != "run-a" || result.Items[1].RunID != "run-b" || result.Items[0].TemplateVersion != "v1" || result.Items[0].ToolVersions != "zarf=0.82.0" || result.Items[0].DuplicateReviews != 3 || len(result.Items[0].Attachment) != 1 || result.Items[0].Attachment[0] != strings.Repeat("a", 64) {
		t.Fatalf("lost native evidence: %s", output)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(other) != 0 || len(requests) != 2 {
		t.Fatalf("requests=%v other=%v", requests, other)
	}
	for _, q := range requests {
		if q.View != "summary" || q.Limit == nil || *q.Limit != 200 || q.Phase != "review" || q.Outcome != "WAITING" || q.PlanId != "p1" || q.Search != "app" {
			t.Fatalf("filters changed: %v", q)
		}
	}
}

func TestRunListConnectFailuresNeverReplayThroughREST(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{{401, "unauthenticated"}, {403, "permission_denied"}, {400, "invalid_argument"}, {500, "internal"}, {501, "unimplemented"}} {
		t.Run(tc.code, func(t *testing.T) {
			var mu sync.Mutex
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				paths = append(paths, r.URL.Path)
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				json.NewEncoder(w).Encode(map[string]string{"code": tc.code, "message": "refused"})
			}))
			defer server.Close()
			_, err := executeRunTransport(t, server.URL, "connect")
			if err == nil {
				t.Fatal("refusal became success")
			}
			mu.Lock()
			defer mu.Unlock()
			if len(paths) != 1 || paths[0] != packagingv1connect.PackagingRunServiceListPackagingRunsProcedure {
				t.Fatalf("failure replayed: %v", paths)
			}
		})
	}
}

func executeRunTransport(t *testing.T, server, mode string, args ...string) (string, error) {
	t.Helper()
	var output bytes.Buffer
	root := NewRoot(Runtime{Stdout: &output, Stderr: &output, Getenv: func(string) string { return "" }})
	argv := []string{"--server", server, "--development-subject", "developer", "run", "list", "--read-transport", mode}
	root.SetArgs(append(argv, args...))
	err := root.Execute()
	return output.String(), err
}

func TestRunListExplicitRESTRemainsAvailable(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if r.Method != "GET" || r.URL.Path != "/v1/packaging-runs" || r.URL.Query().Get("view") != "summary" || r.URL.Query().Get("limit") != "200" {
			t.Error("REST mode changed its native request")
		}
		w.Write([]byte(`{"items":[],"total":0}`))
	}))
	defer server.Close()
	if output, err := executeRunTransport(t, server.URL, "rest"); err != nil {
		t.Fatalf("REST: %v %s", err, output)
	}
	if len(paths) != 1 || paths[0] != "GET /v1/packaging-runs" {
		t.Fatalf("REST mode selected another transport: %v", paths)
	}
}

func TestRunListConnectCancelsTheActualRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	canceled := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Error(err)
			return
		}
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
	root.SetArgs([]string{"--server", server.URL, "--development-subject", "developer", "run", "list", "--read-transport", "connect"})
	if err := root.ExecuteContext(ctx); err == nil {
		t.Fatal("cancellation became success")
	}
	if !<-canceled {
		t.Fatal("command cancellation did not reach HTTP request")
	}
}

func TestRunListRejectsAnUnknownTransportBeforeContactingHub(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.Write([]byte(`{}`)) }))
	defer server.Close()
	_, err := executeRunTransport(t, server.URL, "unknown")
	if err == nil || !strings.Contains(err.Error(), "--read-transport") || calls != 0 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestRunListConnectStopsRepeatedCursors(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls > 3 {
			http.Error(w, "client failed to stop", 500)
			return
		}
		wire, _ := proto.Marshal(&packagingv1.ListPackagingRunsResponse{Limit: 200, NextCursor: "again"})
		w.Header().Set("Content-Type", "application/proto")
		w.Write(wire)
	}))
	defer server.Close()
	_, err := executeRunTransport(t, server.URL, "connect")
	if err == nil || !strings.Contains(err.Error(), "repeated a page cursor") || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

package conduitcli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestPlanCLIRefusesUnsupportedScopedConnectWithoutOutputOrFallback(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/conduit.plans.v1.PlanScopedReadService/ListPlanSummariesInSpace" {
			t.Errorf("unscoped/fallback request: %s %s", r.Method, r.URL.Path)
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	var output bytes.Buffer
	root := NewRoot(Runtime{Stdout: &output, Stderr: io.Discard, Getenv: func(string) string { return "" }})
	root.SetArgs([]string{"--server", server.URL, "--development-subject", "reader", "--space-id", "space_0123456789abcdef0123456789abcdef", "--output", "json", "plan", "list", "--read-transport", "connect"})
	err := root.ExecuteContext(t.Context())
	if err == nil || calls.Load() != 1 || output.Len() != 0 {
		t.Fatalf("unscoped output/replay: %v output%s calls%d", err, output.String(), calls.Load())
	}
}

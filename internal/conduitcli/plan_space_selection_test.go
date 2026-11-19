package conduitcli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPlanCLIRefusesScopedConnectWithoutOutputOrFallback(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	var output bytes.Buffer
	root := NewRoot(Runtime{Stdout: &output, Stderr: io.Discard, Getenv: func(string) string { return "" }})
	root.SetArgs([]string{"--server", server.URL, "--development-subject", "reader", "--space-id", "space_0123456789abcdef0123456789abcdef", "--output", "json", "plan", "list", "--read-transport", "connect"})
	err := root.ExecuteContext(t.Context())
	if err == nil || !strings.Contains(err.Error(), "Space selection is unavailable") || calls.Load() != 0 || output.Len() != 0 {
		t.Fatalf("unscoped output/replay: %v output%s calls%d", err, output.String(), calls.Load())
	}
}

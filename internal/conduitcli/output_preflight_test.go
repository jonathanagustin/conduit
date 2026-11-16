package conduitcli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func executeOutputPreflight(t *testing.T, server, format string, runtime Runtime) (string, error) {
	t.Helper()
	var out bytes.Buffer
	runtime.Stdout = &out
	runtime.Stderr = io.Discard
	runtime.Getenv = func(string) string { return "" }
	root := NewRoot(runtime)
	root.SetArgs([]string{"--server", server, "--development-subject", "developer", "--output", format, "plan", "cancel", "plan", "--reason", "reviewed cancellation"})
	err := root.Execute()
	return out.String(), err
}
func TestUnknownOutputIsRejectedBeforeHubMutation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/plans/plan/cancel" {
			t.Error("unexpected native request")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"plan","status":"CANCELLED","revision":2,"plan":{"environment":"dev"}}`)
	}))
	defer server.Close()
	for _, format := range []string{"pretty", "PRETTY", "text", "xml", "", "json yaml"} {
		out, err := executeOutputPreflight(t, server.URL, format, Runtime{})
		if err == nil || !strings.Contains(err.Error(), "use json, yaml, csv, or table") || out != "" {
			t.Errorf("format %q: err=%v output=%q", format, err, out)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("unknown formats sent %d mutating Hub requests", calls.Load())
	}
}
func TestUnknownOutputIsRejectedBeforeConnectionSetup(t *testing.T) {
	var setups atomic.Int32
	runtime := Runtime{ServiceFactory: func(context.Context, ConnectionConfig) (Service, func(), error) {
		setups.Add(1)
		return &fakeService{}, func() {}, nil
	}}
	_, err := executeOutputPreflight(t, "http://127.0.0.1:1", "pretty", runtime)
	if err == nil || !strings.Contains(err.Error(), "unsupported output format") || setups.Load() != 0 {
		t.Fatalf("invalid format reached connection/credential setup: %v setups=%d", err, setups.Load())
	}
}
func TestSupportedOutputNamesKeepNativeMutationAndNormalization(t *testing.T) {
	for _, format := range []string{"json", " YAML ", "csv", "TABLE"} {
		t.Run(format, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"plan","status":"CANCELLED","revision":2,"plan":{"environment":"dev"}}`)
			}))
			defer server.Close()
			out, err := executeOutputPreflight(t, server.URL, format, Runtime{})
			if err != nil || calls.Load() != 1 || !strings.Contains(out, "CANCELLED") {
				t.Fatalf("supported format changed behavior: %v calls=%d output=%q", err, calls.Load(), out)
			}
		})
	}
}
func TestOutputPreflightKeepsLocalCommandsIndependentOfHub(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--help"}, {"completion", "bash"}} {
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			var out bytes.Buffer
			root := NewRoot(Runtime{Stdout: &out, Stderr: io.Discard, Getenv: func(string) string { return "" }, ServiceFactory: func(context.Context, ConnectionConfig) (Service, func(), error) {
				t.Fatal("local command opened a Hub connection")
				return nil, nil, nil
			}})
			root.SetArgs(args)
			if err := root.Execute(); err != nil || out.Len() == 0 {
				t.Fatal("local command now requires Hub setup", err)
			}
		})
	}
}

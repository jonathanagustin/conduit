package conduitcli

import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCommandsReadTheHubAndDoNotDecideLocally(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var notes []string
	note := func(format string, args ...any) {
		mu.Lock()
		notes = append(notes, fmt.Sprintf(format, args...))
		mu.Unlock()
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Platform-Subject") != "developer" {
			http.Error(response, "missing subject", http.StatusUnauthorized)
			return
		}
		query := request.URL.Query()
		switch request.URL.Path {
		case "/v1/environments":
			if query.Get("view") != "full" || query.Get("limit") != "20" {
				note("environments query %s", request.URL.RawQuery)
			}
			response.Write([]byte(`{"environments":[{"environment":{"id":"prod","generation":"gen-1"}}]}`))
		case "/v1/plans":
			if query.Get("view") != "summary" || query.Get("limit") != "200" || query.Get("environment") != "prod" {
				note("plans query %s", request.URL.RawQuery)
			}
			if query.Get("cursor") == "" {
				response.Write([]byte(`{"plans":[{"id":"pln_a","environment":"prod","status":"APPROVED","stepCount":1,"approvalCount":1,"requiredApprovals":1}],"total":2,"nextCursor":"page-2"}`))
				return
			}
			if query.Get("cursor") != "page-2" {
				note("plans cursor %s", query.Get("cursor"))
			}
			response.Write([]byte(`{"plans":[{"id":"pln_b","environment":"prod","status":"AWAITING_APPROVAL","stepCount":2,"approvalCount":0,"requiredApprovals":1}],"total":2}`))
		case "/v1/plans/pln_a":
			if request.Method != http.MethodGet {
				note("plan show method %s", request.Method)
			}
			response.Write([]byte(`{"id":"pln_a","status":"APPROVED","requiredApprovals":1,"revision":4,"plan":{"environment":"prod"}}`))
		case "/v1/plans/pln_a/approvals":
			var body struct {
				Rationale string `json:"rationale"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body.Rationale != "reviewed" {
				note("approve body")
			}
			response.Write([]byte(`{"id":"pln_a","status":"APPROVED","plan":{"environment":"prod"}}`))
		case "/v1/plans/pln_a/cancel":
			var body struct {
				Reason string `json:"reason"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body.Reason != "stop" {
				note("cancel body")
			}
			response.Write([]byte(`{"id":"pln_a","status":"CANCELLED","plan":{"environment":"prod"}}`))
		case "/v1/packaging-runs":
			if query.Get("view") != "summary" || query.Get("phase") != "review" || query.Get("limit") != "200" {
				note("runs query %s", request.URL.RawQuery)
			}
			response.Write([]byte(`{"tier":"CONDUIT_NATIVE","configured":true,"authority":"packaging-run","planAuthority":"conduit-plan","approvalAuthority":"conduit-governance","items":[{"runId":"run-1","phase":"review","outcome":"PASS","planId":"pln_a","targetPlanRef":"prod/checkout"}],"total":1}`))
		default:
			note("unexpected %s %s", request.Method, request.URL.Path)
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	run := func(arguments ...string) (string, error) {
		var output bytes.Buffer
		root := NewRoot(Runtime{Stdout: &output, Stderr: &output, Getenv: func(string) string { return "" }})
		root.SetArgs(append([]string{"--server", server.URL, "--development-subject", "developer", "--timeout", "5s"}, arguments...))
		err := root.Execute()
		return output.String(), err
	}

	environments, err := run("env", "list")
	if err != nil || !strings.Contains(environments, `"id": "prod"`) {
		t.Fatalf("env list: %v\n%s", err, environments)
	}
	table, err := run("-o", "table", "env", "list")
	if err != nil || !strings.Contains(table, "prod") || !strings.Contains(table, "-----") {
		t.Fatalf("env table: %v\n%s", err, table)
	}
	plans, err := run("plan", "list", "--environment", "prod")
	if err != nil || !strings.Contains(plans, "pln_a") || !strings.Contains(plans, "pln_b") {
		t.Fatalf("plan list: %v\n%s", err, plans)
	}
	shown, err := run("plan", "show", "pln_a")
	if err != nil || !strings.Contains(shown, `"status": "APPROVED"`) || !strings.Contains(shown, `"revision": 4`) {
		t.Fatalf("plan show: %v\n%s", err, shown)
	}
	approved, err := run("plan", "approve", "pln_a", "--rationale", "reviewed")
	if err != nil || !strings.Contains(approved, `"status": "APPROVED"`) {
		t.Fatalf("plan approve: %v\n%s", err, approved)
	}
	cancelled, err := run("plan", "cancel", "pln_a", "--reason", "stop")
	if err != nil || !strings.Contains(cancelled, `"status": "CANCELLED"`) {
		t.Fatalf("plan cancel: %v\n%s", err, cancelled)
	}
	runs, err := run("-o", "table", "run", "list", "--phase", "review")
	if err != nil || !strings.Contains(runs, "run-1") || !strings.Contains(runs, "prod/checkout") {
		t.Fatalf("run list: %v\n%s", err, runs)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(notes) != 0 {
		t.Fatalf("hub client diverged from the routes: %s", strings.Join(notes, "; "))
	}
}

func TestPlanListStopsWhenTheHubRepeatsACursor(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Write([]byte(`{"plans":[{"id":"pln_a","environment":"prod","status":"APPROVED"}],"total":1,"nextCursor":"again"}`))
	}))
	defer server.Close()
	var output bytes.Buffer
	root := NewRoot(Runtime{Stdout: &output, Stderr: &output, Getenv: func(string) string { return "" }})
	root.SetArgs([]string{"--server", server.URL, "--development-subject", "developer", "--timeout", "5s", "plan", "list"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "repeated a page cursor") {
		t.Fatalf("error=%v", err)
	}
}

func TestOIDCClientCredentialsSendsTheAccessTokenAndDoesNotEchoTheSecret(t *testing.T) {
	t.Parallel()
	const secret = "client-secret-value"
	const access = "access-token-value"
	var hubAuthorization string
	idp := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/.well-known/openid-configuration":
			response.Header().Set("Content-Type", "application/json")
			issuer := "https://" + request.Host
			response.Write([]byte(`{"issuer":"` + issuer + `","token_endpoint":"` + issuer + `/token"}`))
		case "/token":
			if err := request.ParseForm(); err != nil {
				http.Error(response, "bad form", http.StatusBadRequest)
				return
			}
			if request.PostForm.Get("grant_type") != "client_credentials" || request.PostForm.Get("client_id") != "conduit" {
				response.WriteHeader(http.StatusBadRequest)
				response.Write([]byte(`{"error":"invalid_request"}`))
				return
			}
			if request.PostForm.Get("client_secret") != secret {
				response.WriteHeader(http.StatusUnauthorized)
				response.Write([]byte(`{"error":"invalid_client","error_description":"` + request.PostForm.Get("client_secret") + `"}`))
				return
			}
			response.Write([]byte(`{"access_token":"` + access + `","token_type":"Bearer"}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer idp.Close()
	hub := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		hubAuthorization = request.Header.Get("Authorization")
		response.Write([]byte(`{"environments":[{"environment":{"id":"prod","generation":"gen-1"}}]}`))
	}))
	defer hub.Close()

	secretPath := filepath.Join(t.TempDir(), "client.secret")
	if err := os.WriteFile(secretPath, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	root := NewRoot(Runtime{Stdout: &output, Stderr: &output, Getenv: func(string) string { return "" }})
	root.SetArgs([]string{
		"--server", hub.URL, "--ca-file", certificatePEM(t, "hub", hub), "--timeout", "5s",
		"--oidc-issuer", idp.URL, "--oidc-client-id", "conduit",
		"--oidc-client-secret-file", secretPath, "--oidc-ca-file", certificatePEM(t, "issuer", idp),
		"env", "list",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("env list: %v", err)
	}
	if hubAuthorization != "Bearer "+access || !strings.Contains(output.String(), `"id": "prod"`) {
		t.Fatalf("authorization=%q output=%s", hubAuthorization, output.String())
	}

	if err := os.WriteFile(secretPath, []byte("wrong-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	root = NewRoot(Runtime{Stdout: &output, Stderr: &output, Getenv: func(string) string { return "" }})
	root.SetArgs([]string{
		"--server", hub.URL, "--ca-file", certificatePEM(t, "hub", hub), "--timeout", "5s",
		"--oidc-issuer", idp.URL, "--oidc-client-id", "conduit",
		"--oidc-client-secret-file", secretPath, "--oidc-ca-file", certificatePEM(t, "issuer", idp),
		"env", "list",
	})
	err := root.Execute()
	if err == nil || strings.Contains(err.Error(), "wrong-secret") || !strings.Contains(err.Error(), "invalid_client") {
		t.Fatalf("error=%v", err)
	}
}

func TestOIDCClientSecretMustNotBeGroupReadable(t *testing.T) {
	t.Parallel()
	secretPath := filepath.Join(t.TempDir(), "client.secret")
	if err := os.WriteFile(secretPath, []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configuration := ConnectionConfig{
		Server: "https://hub.example.test", Timeout: 5 * time.Second, OIDCIssuer: "https://issuer.example.test",
		OIDCClientID: "conduit", OIDCClientSecretFile: secretPath,
	}
	_, err := configuration.connect(t.Context())
	if err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("error=%v", err)
	}
}

func certificatePEM(t *testing.T, name string, server *httptest.Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".pem")
	encoded := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

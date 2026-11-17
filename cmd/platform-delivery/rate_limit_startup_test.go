package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Re-execute the race-instrumented test binary through the real entrypoint.
// The child receives only fixture configuration, never the parent credentials.
func TestHubRateLimitProcess(t *testing.T) {
	if os.Getenv("CONDUIT_RATE_LIMIT_TEST_PROCESS") != "1" {
		return
	}
	main()
	os.Exit(0)
}

func rateLimitTestAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func rateLimitTestCommand(t *testing.T, dsn string, overrides map[string]string) (*exec.Cmd, string, string, string) {
	t.Helper()
	directory := t.TempDir()
	address := rateLimitTestAddress(t)
	values := map[string]string{
		"PATH": os.Getenv("PATH"), "HOME": directory,
		"DATA_DIRECTORY": filepath.Join(directory, "data"),
		"XDG_STATE_HOME": filepath.Join(directory, "state"),
		"LISTEN_ADDRESS": address, "CONDUIT_RATE_LIMIT_TEST_PROCESS": "1",
		"ALLOW_INSECURE_AGENT_IDENTITY_HEADERS":       "true",
		"ALLOW_INSECURE_OPERATOR_IDENTITY_HEADERS":    "true",
		"ALLOW_INSECURE_EPHEMERAL_BUNDLE_SIGNING_KEY": "true",
		"PLATFORM_BOOTSTRAP_ADMINISTRATORS":           "quota-operator",
	}
	for _, class := range []string{"OPERATOR", "AGENT", "ANONYMOUS"} {
		values["RATE_LIMIT_"+class+"_RPS"] = "0.001"
		values["RATE_LIMIT_"+class+"_BURST"] = "2"
	}
	metrics := ""
	if dsn != "" {
		metrics = rateLimitTestAddress(t)
		for metrics == address {
			metrics = rateLimitTestAddress(t)
		}
		values["REPOSITORY_KIND"] = "postgres"
		values["POSTGRES_DSN_FILE"] = writeFile(t, "rate-limit-dsn", []byte(dsn+"\n"))
		values["METRICS_ADDRESS"] = metrics
	}
	for key, value := range overrides {
		values[key] = value
	}
	command := exec.Command(os.Args[0], "-test.run=^TestHubRateLimitProcess$")
	for key, value := range values {
		command.Env = append(command.Env, key+"="+value)
	}
	logPath := filepath.Join(directory, "hub.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	command.Stdout, command.Stderr = log, log
	return command, "http://" + address, "http://" + metrics, logPath
}

func startRateLimitTestHub(t *testing.T, dsn string) (string, string) {
	t.Helper()
	command, base, metrics, logPath := rateLimitTestCommand(t, dsn, nil)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var waitErr error
	go func() { waitErr = command.Wait(); close(done) }()
	t.Cleanup(func() {
		select {
		case <-done:
			t.Errorf("Hub exited before cleanup: %v", waitErr)
			return
		default:
		}
		_ = command.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = command.Process.Kill()
			<-done
			t.Error("Hub required forced termination")
		}
		if waitErr != nil {
			log, _ := os.ReadFile(logPath)
			t.Errorf("Hub shutdown failed: %v\n%s", waitErr, log)
		}
	})
	client := &http.Client{Timeout: time.Second}
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		response, err := client.Get(base + "/health/live")
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return base, metrics
			}
		}
		select {
		case <-done:
			log, _ := os.ReadFile(logPath)
			t.Fatalf("Hub exited during startup: %v\n%s", waitErr, log)
		case <-deadline.C:
			log, _ := os.ReadFile(logPath)
			t.Fatalf("Hub did not become ready: %s", log)
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		case <-ticker.C:
		}
	}
}

func exerciseRateLimitTestHub(t *testing.T, base string) {
	t.Helper()
	client := &http.Client{Timeout: 3 * time.Second}
	request := func(path, subject, agent string, want int) {
		t.Helper()
		method, body := http.MethodGet, ""
		if agent != "" {
			method, body = http.MethodPost, "{\"durationSeconds\":60}"
		}
		req, err := http.NewRequestWithContext(t.Context(), method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if subject != "" {
			req.Header.Set("X-Platform-Subject", subject)
		}
		if agent != "" {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Platform-Environment-ID", "quota-env")
			req.Header.Set("X-Platform-Agent-ID", agent)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		_ = response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("%s subject=%q agent=%q: status %d, want %d: %s", path, subject, agent, response.StatusCode, want, payload)
		}
		if want == http.StatusTooManyRequests {
			retry, err := strconv.Atoi(response.Header.Get("Retry-After"))
			if err != nil || retry < 1 {
				t.Fatalf("missing positive Retry-After: %v", response.Header)
			}
		}
	}
	for range 2 {
		request("/v1/products", "quota-operator", "", http.StatusOK)
	}
	request("/v1/products", "quota-operator", "", http.StatusTooManyRequests)
	request("/v1/products", "other-operator", "", http.StatusOK)
	for range 2 {
		request("/unregistered-quota-probe", "", "", http.StatusNotFound)
	}
	request("/unregistered-quota-probe", "", "", http.StatusTooManyRequests)
	// The admitted request reaches the real lease handler and returns its empty
	// work queue. This proves Agent polling, not a granted lease or tool effect.
	for range 2 {
		request("/v1/environments/quota-env/leases", "", "quota-agent", http.StatusNoContent)
	}
	request("/v1/environments/quota-env/leases", "", "quota-agent", http.StatusTooManyRequests)
	request("/v1/environments/quota-env/leases", "", "other-agent", http.StatusNoContent)
	for range 4 {
		request("/health/live", "", "", http.StatusOK)
	}
}

func TestHubRequestLimiterEntrypoint(t *testing.T) {
	base, _ := startRateLimitTestHub(t, "")
	exerciseRateLimitTestHub(t, base)
}

func TestHubRequestLimiterStartupRefusesInvalidConfiguration(t *testing.T) {
	command, _, _, logPath := rateLimitTestCommand(t, "", map[string]string{"RATE_LIMIT_OPERATOR_RPS": "0"})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	// Use CommandContext so a broken startup refusal cannot leave a server running.
	bounded := exec.CommandContext(ctx, command.Path, command.Args[1:]...)
	bounded.Env, bounded.Stdout, bounded.Stderr = command.Env, command.Stdout, command.Stderr
	if err := bounded.Run(); err == nil {
		t.Fatal("invalid request limit started the Hub")
	}
	log, _ := os.ReadFile(logPath)
	if ctx.Err() != nil || !strings.Contains(string(log), "RATE_LIMIT_OPERATOR_RPS") {
		t.Fatalf("startup did not refuse the configured rate limit: %s", log)
	}
}

// Selected by the existing PostgreSQL integration gate's ^TestPostgres filter.
func TestPostgresHubRequestLimiterEntrypointMetrics(t *testing.T) {
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is required for the PostgreSQL integration gate")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	schema := "conduit_quota_" + strings.ToLower(rand.Text())
	if _, err := database.ExecContext(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := database.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		dsn = parsed.String()
	} else {
		dsn += " search_path=" + schema
	}
	base, metrics := startRateLimitTestHub(t, dsn)
	exerciseRateLimitTestHub(t, base)
	response, err := (&http.Client{Timeout: 3 * time.Second}).Get(metrics + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	_ = response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("metrics status %d, read error %v", response.StatusCode, err)
	}
	body := string(payload)
	for _, expected := range []string{
		"conduit_rate_limit_decisions_total{class=\"operator\",outcome=\"allowed\"} 3",
		"conduit_rate_limit_decisions_total{class=\"operator\",outcome=\"limited\"} 1",
		"conduit_rate_limit_decisions_total{class=\"agent\",outcome=\"allowed\"} 3",
		"conduit_rate_limit_decisions_total{class=\"agent\",outcome=\"limited\"} 1",
		"conduit_rate_limit_decisions_total{class=\"anonymous\",outcome=\"allowed\"} 2",
		"conduit_rate_limit_decisions_total{class=\"anonymous\",outcome=\"limited\"} 1",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("missing configured request decision %s; metrics:\n%s", expected, body)
		}
	}
	for _, identity := range []string{"quota-operator", "other-operator", "quota-agent", "other-agent", "127.0.0.1"} {
		if strings.Contains(body, identity) {
			t.Errorf("metrics disclosed caller identity %q", identity)
		}
	}
}

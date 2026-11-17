package composition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	spoke "github.com/jonathanagustin/conduit/internal/agent"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// These are latency upper bounds, not retained request samples. Memory use is
// bounded during long soaks, including after backlog makes requests slower.
var capacityLatencyBounds = [...]time.Duration{100 * time.Microsecond, 250 * time.Microsecond, 500 * time.Microsecond, time.Millisecond, 2 * time.Millisecond, 5 * time.Millisecond, 10 * time.Millisecond, 20 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond, 250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute}

var errCapacityHarnessShutdown = errors.New("capacity harness stopped its workers")

type capacityRPC struct {
	Requests   uint64  `json:"requests"`
	Errors     uint64  `json:"errors"`
	Empty      uint64  `json:"empty_polls"`
	MeanMS     float64 `json:"mean_ms"`
	MaxMS      float64 `json:"max_ms"`
	P50UpperMS float64 `json:"p50_upper_ms"`
	P95UpperMS float64 `json:"p95_upper_ms"`
	P99UpperMS float64 `json:"p99_upper_ms"`
	total      time.Duration
	maximum    time.Duration
	buckets    [len(capacityLatencyBounds) + 1]uint64
}

type capacityMetrics struct {
	sync.Mutex
	rows map[string]*capacityRPC
}

func (metrics *capacityMetrics) observe(operation string, status int, elapsed time.Duration) {
	metrics.Lock()
	defer metrics.Unlock()
	row := metrics.rows[operation]
	if row == nil {
		row = &capacityRPC{}
		metrics.rows[operation] = row
	}
	row.Requests++
	if status < 200 || status >= 300 {
		row.Errors++
	}
	if status == http.StatusNoContent {
		row.Empty++
	}
	row.total += elapsed
	if elapsed > row.maximum {
		row.maximum = elapsed
	}
	bucket := len(capacityLatencyBounds)
	for index, bound := range capacityLatencyBounds {
		if elapsed <= bound {
			bucket = index
			break
		}
	}
	row.buckets[bucket]++
}

func (metrics *capacityMetrics) snapshot() map[string]capacityRPC {
	metrics.Lock()
	defer metrics.Unlock()
	rows := make(map[string]capacityRPC, len(metrics.rows))
	for operation, original := range metrics.rows {
		row := *original
		row.MeanMS = float64(row.total) / float64(time.Millisecond) / float64(row.Requests)
		row.MaxMS = float64(row.maximum) / float64(time.Millisecond)
		quantile := func(percent uint64) float64 {
			wanted := (row.Requests*percent + 99) / 100
			var seen uint64
			for index, count := range row.buckets {
				seen += count
				if seen >= wanted {
					if index == len(capacityLatencyBounds) {
						return row.MaxMS
					}
					return float64(capacityLatencyBounds[index]) / float64(time.Millisecond)
				}
			}
			return 0
		}
		row.P50UpperMS, row.P95UpperMS, row.P99UpperMS = quantile(50), quantile(95), quantile(99)
		rows[operation] = row
	}
	return rows
}

type capacityTransport struct {
	base    http.RoundTripper
	metrics *capacityMetrics
}
type capacityBody struct {
	io.ReadCloser
	once     sync.Once
	failed   atomic.Bool
	complete func(bool)
}

func (body *capacityBody) Read(buffer []byte) (int, error) {
	count, err := body.ReadCloser.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		body.failed.Store(true)
	}
	return count, err
}

func (body *capacityBody) Close() error {
	err := body.ReadCloser.Close()
	body.once.Do(func() { body.complete(body.failed.Load() || err != nil) })
	return err
}

func (transport capacityTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	operation := "other"
	for _, candidate := range []string{"leases", "start", "heartbeat", "finish"} {
		if strings.HasSuffix(request.URL.Path, "/"+candidate) {
			operation = candidate
			break
		}
	}
	started := time.Now()
	observe := func(status int, failed bool) {
		if failed {
			// Only the harness's explicit stop is excluded. Client deadlines and
			// other cancellations still describe interrupted service requests.
			if errors.Is(context.Cause(request.Context()), errCapacityHarnessShutdown) {
				return
			}
			status = 0
		}
		transport.metrics.observe(operation, status, time.Since(started))
	}
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		observe(0, true)
		return response, err
	}
	response.Body = &capacityBody{ReadCloser: response.Body, complete: func(failed bool) { observe(response.StatusCode, failed) }}
	return response, nil
}

// This executor acknowledges no actual Helm/Kubernetes action. Only the real
// Agent/Hub protocol, lifecycle, planner and repository are measured here.
type capacityExecutor struct {
	delay   time.Duration
	entered chan struct{}
	once    sync.Once
	block   bool
}

func (executor *capacityExecutor) Apply(ctx context.Context, _ controlplane.Plan, step controlplane.PlanStep) (spoke.Receipt, error) {
	if executor.entered != nil {
		executor.once.Do(func() { close(executor.entered) })
	}
	if executor.block {
		<-ctx.Done()
		return spoke.Receipt{}, ctx.Err()
	}
	if executor.delay > 0 {
		timer := time.NewTimer(executor.delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return spoke.Receipt{}, ctx.Err()
		}
	}
	return spoke.Receipt{Step: step}, nil
}
func (*capacityExecutor) Observe(ctx context.Context, step controlplane.PlanStep) (controlplane.ReportedEntity, error) {
	if err := ctx.Err(); err != nil {
		return controlplane.ReportedEntity{}, err
	}
	return controlplane.ReportedEntity{InstallationID: step.InstallationID, Product: step.Product, Version: step.Version, ReleaseDigest: step.ReleaseDigest, Namespace: step.Namespace, ConfigDigest: step.ConfigDigest,
		Health: controlplane.HealthHealthy, RolloutStatus: controlplane.RolloutUpToDate, ConfigStatus: controlplane.ConfigApplied, Lifecycle: controlplane.LifecycleInstalled}, nil
}
func (*capacityExecutor) Rollback(context.Context, spoke.Receipt) error { return nil }

type capacityConfig struct {
	Backend      string        `json:"backend"`
	Environments int           `json:"environments"`
	Agents       int           `json:"agents_per_environment"`
	Generations  int           `json:"generations_per_environment"`
	Handlers     int           `json:"hub_handlers"`
	Duration     time.Duration `json:"duration_ns"`
	Poll         time.Duration `json:"poll_interval_ns"`
	ApplyDelay   time.Duration `json:"simulated_apply_delay_ns"`
	Drain        time.Duration `json:"drain_timeout_ns"`
	Timeout      time.Duration `json:"run_timeout_ns"`
	AllowErrors  bool          `json:"allow_transient_errors"`
}
type capacityReport struct {
	SchemaVersion   int                    `json:"schema_version"`
	Config          capacityConfig         `json:"configuration"`
	Commit          string                 `json:"commit"`
	GOOS            string                 `json:"goos"`
	GOARCH          string                 `json:"goarch"`
	GoVersion       string                 `json:"go_version"`
	CPUs            int                    `json:"visible_cpus"`
	Procs           int                    `json:"gomaxprocs"`
	ElapsedSeconds  float64                `json:"elapsed_seconds"`
	Completed       uint64                 `json:"completed_plans"`
	PlansPerSecond  float64                `json:"plans_per_second"`
	ByEnvironment   []uint64               `json:"completions_per_environment"`
	RPC             map[string]capacityRPC `json:"rpc_full_response_latency"`
	AllocationBytes uint64                 `json:"allocation_bytes"`
	HeapEndBytes    uint64                 `json:"heap_end_bytes"`
	Passed          bool                   `json:"passed"`
	Components      map[string]string      `json:"component_classes"`
}

func capacityState(environment, generation string) controlplane.DesiredState {
	artifact := controlplane.Artifact{Repository: "registry.example/capacity", Tag: "1.0.0", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	return controlplane.DesiredState{APIVersion: "platform.delivery/v1alpha1", Kind: "EnvironmentDeployment", Environment: controlplane.Environment{ID: environment, Generation: generation},
		Releases: []controlplane.Release{{Product: "app.capacity:sample", Version: "1.0.0", Chart: artifact, Images: []controlplane.Artifact{artifact}}},
		Entities: []controlplane.Entity{{Name: "sample", Product: "app.capacity:sample", Version: "1.0.0", Namespace: "capacity"}}}
}

func capacityRepository(t *testing.T, backend string) controlplane.Repository {
	t.Helper()
	switch backend {
	case "memory":
		return controlplane.NewMemoryRepository()
	case "file":
		repository, err := controlplane.NewFileRepository(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return repository
	case "postgres":
		dsn := os.Getenv("CONDUIT_CAPACITY_POSTGRES_DSN")
		if dsn == "" {
			t.Fatal("CONDUIT_CAPACITY_POSTGRES_DSN must name a disposable capacity database")
		}
		repository, err := controlplane.NewPostgresRepository(t.Context(), dsn)
		if err != nil {
			t.Fatalf("open capacity PostgreSQL repository failed: %s", controlplane.RedactDiagnosticText(strings.ReplaceAll(err.Error(), dsn, "[redacted DSN]"), 1024))
		}
		t.Cleanup(func() { _ = repository.Close() })
		return repository
	default:
		t.Fatalf("unknown capacity backend %q", backend)
		return nil
	}
}

func capacityClient(t *testing.T, repository controlplane.Repository, metrics *capacityMetrics, partition *atomic.Bool) (*spoke.HTTPControlPlane, *http.Transport) {
	t.Helper()
	handler, err := NewHub(repository, controlplane.HandlerOptions{AllowInsecureAgentIdentityHeaders: true})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	dialer := &net.Dialer{Timeout: time.Second}
	transport := &http.Transport{MaxIdleConns: 1000, MaxIdleConnsPerHost: 1000, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if partition != nil && partition.Load() {
			return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("injected capacity partition")}
		}
		return dialer.DialContext(ctx, network, address)
	}}
	t.Cleanup(transport.CloseIdleConnections)
	client, err := spoke.NewHTTPControlPlane(server.URL, &http.Client{Timeout: 2 * time.Second, Transport: capacityTransport{base: transport, metrics: metrics}})
	if err != nil {
		t.Fatal(err)
	}
	return client, transport
}

type capacityHooks struct {
	ready     func() error
	completed func(int)
}

func runFleetCapacity(t *testing.T, config capacityConfig, hooks ...capacityHooks) capacityReport {
	t.Helper()
	if config.Timeout == 0 {
		config.Timeout = config.Duration + config.Drain + 30*time.Second
	}
	ctx, cancel := context.WithTimeout(t.Context(), config.Timeout)
	defer cancel()
	ctx, abort := context.WithCancelCause(ctx)
	defer abort(nil)
	repository := capacityRepository(t, config.Backend)
	lifecycle := controlplane.NewLifecycle(repository)
	metrics := &capacityMetrics{rows: make(map[string]*capacityRPC)}
	clients := make([]*spoke.HTTPControlPlane, config.Handlers)
	for index := range clients {
		backing := repository
		if index != 0 {
			backing = capacityRepository(t, config.Backend)
		}
		clients[index], _ = capacityClient(t, backing, metrics, nil)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	started := time.Now()
	stopAt := started.Add(config.Duration)
	runID := fmt.Sprintf("capacity-%x", started.UnixNano())
	completions := make([]atomic.Uint64, config.Environments)
	var readyOnce sync.Once
	var readyErr error
	workers, stopWorkers := context.WithCancelCause(ctx)
	var pollers sync.WaitGroup
	for environment := 0; environment < config.Environments; environment++ {
		for number := 0; number < config.Agents; number++ {
			pollers.Add(1)
			go func(environment, number int) {
				defer pollers.Done()
				worker := spoke.Worker{ControlPlane: clients[(environment*config.Agents+number)%len(clients)], Executor: &capacityExecutor{delay: config.ApplyDelay}, Environment: fmt.Sprintf("%s-%04d", runID, environment), AgentID: fmt.Sprintf("agent-%04d-%04d", environment, number), LeaseDuration: 10 * time.Second}
				for {
					if workers.Err() != nil {
						return
					}
					_, err := worker.RunOnce(workers)
					if err != nil && workers.Err() == nil && !config.AllowErrors {
						abort(errors.New("agent operation failed"))
						return
					}
					select {
					case <-workers.Done():
						return
					case <-time.After(config.Poll):
					}
				}
			}(environment, number)
		}
	}
	var producers sync.WaitGroup
	for environment := 0; environment < config.Environments; environment++ {
		producers.Add(1)
		go func(environment int) {
			defer producers.Done()
			for generation := 0; ; generation++ {
				if config.Duration == 0 && generation >= config.Generations || config.Duration > 0 && time.Now().After(stopAt) {
					return
				}
				state := capacityState(fmt.Sprintf("%s-%04d", runID, environment), fmt.Sprintf("generation-%08d", generation))
				var record controlplane.PlanRecord
				for ctx.Err() == nil {
					var err error
					record, err = lifecycle.CreatePlan(ctx, state)
					if err == nil && record.Status == controlplane.PlanAwaitingApproval {
						record, err = lifecycle.ApprovePlan(ctx, record.ID, "capacity-reviewer", "synthetic capacity fixture approval")
					}
					if err == nil {
						break
					}
					if !config.AllowErrors {
						abort(errors.New("capacity proposal failed"))
						return
					}
					select {
					case <-ctx.Done():
						return
					case <-time.After(config.Poll):
					}
				}
				for ctx.Err() == nil {
					current, err := repository.GetPlan(ctx, record.ID)
					if err == nil && current.Status == controlplane.PlanSucceeded {
						observed, reportErr := repository.GetEnvironmentReport(ctx, state.Environment.ID)
						if reportErr != nil && config.AllowErrors {
							select {
							case <-ctx.Done():
								return
							case <-time.After(config.Poll):
							}
							continue
						}
						if reportErr != nil || observed.PlanID != record.ID || len(observed.Entities) != len(current.Plan.Steps) {
							abort(errors.New("successful Plan has no matching committed observation"))
							return
						}
						completions[environment].Add(1)
						if len(hooks) != 0 {
							if hooks[0].completed != nil {
								hooks[0].completed(environment)
							}
							readyOnce.Do(func() {
								if hooks[0].ready != nil {
									readyErr = hooks[0].ready()
								}
							})
							if readyErr != nil {
								abort(readyErr)
								return
							}
						}
						break
					}
					if err == nil && (current.Status == controlplane.PlanFailed || current.Status == controlplane.PlanCancelled) {
						abort(errors.New("synthetic reconciliation did not succeed"))
						return
					}
					if err != nil && !config.AllowErrors {
						abort(errors.New("capacity observation failed"))
						return
					}
					select {
					case <-ctx.Done():
						return
					case <-time.After(config.Poll):
					}
				}
				if ctx.Err() != nil {
					return
				}
			}
		}(environment)
	}
	done := make(chan struct{})
	go func() { producers.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
	stopWorkers(errCapacityHarnessShutdown)
	pollers.Wait()
	producers.Wait()
	elapsed := time.Since(started)
	runtime.ReadMemStats(&after)
	report := capacityReport{SchemaVersion: 1, Config: config, Commit: os.Getenv("CONDUIT_CAPACITY_COMMIT"), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, GoVersion: runtime.Version(), CPUs: runtime.NumCPU(), Procs: runtime.GOMAXPROCS(0), ElapsedSeconds: elapsed.Seconds(), ByEnvironment: make([]uint64, config.Environments), RPC: metrics.snapshot(), AllocationBytes: after.TotalAlloc - before.TotalAlloc, HeapEndBytes: after.HeapAlloc, Passed: context.Cause(ctx) == nil,
		Components: map[string]string{"hub_http": "real", "planner_and_lifecycle": "real", "agent_client_and_worker": "real", "repository": "real", "workload_executor": "simulated", "kubernetes": "unavailable"}}
	for index := range completions {
		report.ByEnvironment[index] = completions[index].Load()
		report.Completed += report.ByEnvironment[index]
		if report.ByEnvironment[index] == 0 {
			report.Passed = false
		}
	}
	report.PlansPerSecond = float64(report.Completed) / elapsed.Seconds()
	if destination := os.Getenv("CONDUIT_CAPACITY_REPORT"); destination != "" {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, append(encoded, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if !report.Passed {
		t.Fatalf("capacity convergence failed; completed=%d; cause=%v", report.Completed, context.Cause(ctx))
	}
	return report
}

func TestFleetCapacitySmoke(t *testing.T) {
	report := runFleetCapacity(t, capacityConfig{Backend: "memory", Environments: 3, Agents: 2, Generations: 3, Handlers: 1, Poll: 5 * time.Millisecond, Drain: 10 * time.Second})
	if report.Completed != 9 || report.RPC["finish"].Requests != 9 || report.RPC["finish"].Errors != 0 {
		t.Fatalf("incomplete protocol measurement: %#v", report)
	}
}

func TestFleetCapacity(t *testing.T) {
	if os.Getenv("CONDUIT_CAPACITY_RUN") != "1" {
		t.Skip("opt in with scripts/test-fleet-capacity.sh")
	}
	integer := func(name string, fallback, maximum int) int {
		value := os.Getenv("CONDUIT_CAPACITY_" + name)
		if value == "" {
			return fallback
		}
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > maximum {
			t.Fatalf("invalid capacity %s", name)
		}
		return parsed
	}
	duration := func(name string, fallback time.Duration) time.Duration {
		value := os.Getenv("CONDUIT_CAPACITY_" + name)
		if value == "" {
			return fallback
		}
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed < 0 || parsed > 24*time.Hour {
			t.Fatalf("invalid capacity %s", name)
		}
		return parsed
	}
	backend := os.Getenv("CONDUIT_CAPACITY_BACKEND")
	if backend == "" {
		backend = "memory"
	}
	config := capacityConfig{Backend: backend, Environments: integer("ENVIRONMENTS", 10, 1000), Agents: integer("AGENTS", 2, 100), Generations: integer("GENERATIONS", 5, 100000), Handlers: integer("HANDLERS", 1, 8), Poll: duration("POLL", 25*time.Millisecond), ApplyDelay: duration("APPLY_DELAY", 0), Duration: duration("DURATION", 0), Drain: duration("DRAIN", 30*time.Second), Timeout: duration("TIMEOUT", 5*time.Minute)}
	if config.Environments*config.Agents > 10000 || config.Poll < time.Millisecond || config.Drain < time.Second {
		t.Fatal("capacity fixture exceeds its safety bounds")
	}
	if config.Handlers > 1 && backend != "postgres" {
		t.Fatal("independent Hub handlers require PostgreSQL; memory/file are single-Hub profiles")
	}
	if config.Timeout <= config.Duration+config.Drain {
		t.Fatal("capacity TIMEOUT must exceed DURATION plus DRAIN")
	}
	report := runFleetCapacity(t, config)
	t.Logf("%d plans completed in %.3fs (%.2f/s); this measures the control-plane fixture, not deployment capacity", report.Completed, report.ElapsedSeconds, report.PlansPerSecond)
}

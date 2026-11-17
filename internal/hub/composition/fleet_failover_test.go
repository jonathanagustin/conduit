package composition

import (
	"database/sql"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// capacityDatabaseEndpoint changes only the TCP destination. Neither SQL
// success nor application state is simulated: the external fixture must
// actually promote the standby before publishing the replacement address.
type capacityDatabaseEndpoint struct {
	sync.Mutex
	target      string
	listener    net.Listener
	connections map[net.Conn]net.Conn
}

func newCapacityDatabaseEndpoint(t *testing.T, target string) *capacityDatabaseEndpoint {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxy := &capacityDatabaseEndpoint{target: target, listener: listener, connections: make(map[net.Conn]net.Conn)}
	t.Cleanup(func() { _ = listener.Close(); proxy.switchTo("") })
	go func() {
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			go proxy.forward(client)
		}
	}()
	return proxy
}

func (proxy *capacityDatabaseEndpoint) forward(client net.Conn) {
	defer client.Close()
	proxy.Lock()
	target := proxy.target
	proxy.Unlock()
	backend, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		return
	}
	defer backend.Close()
	proxy.Lock()
	if proxy.target != target {
		proxy.Unlock()
		return
	}
	proxy.connections[client] = backend
	proxy.Unlock()
	defer func() { proxy.Lock(); delete(proxy.connections, client); proxy.Unlock() }()
	done := make(chan struct{})
	go func() { _, _ = io.Copy(backend, client); _ = backend.Close(); close(done) }()
	_, _ = io.Copy(client, backend)
	_ = client.Close()
	<-done
}

func (proxy *capacityDatabaseEndpoint) switchTo(target string) {
	proxy.Lock()
	defer proxy.Unlock()
	proxy.target = target
	for client, backend := range proxy.connections {
		_ = client.Close()
		_ = backend.Close()
	}
}

func TestFleetPostgresPromotionReconciliation(t *testing.T) {
	if os.Getenv("CONDUIT_CAPACITY_FAILOVER_RUN") != "1" {
		t.Skip("requires scripts/test-fleet-failover.sh and two disposable PostgreSQL instances")
	}
	primary, err := url.Parse(os.Getenv("CONDUIT_CAPACITY_PRIMARY_DSN"))
	if err != nil || primary.Host == "" {
		t.Fatal("capacity primary DSN is required")
	}
	standby, err := url.Parse(os.Getenv("CONDUIT_CAPACITY_STANDBY_DSN"))
	if err != nil || standby.Host == "" {
		t.Fatal("capacity standby DSN is required")
	}
	readyFile, targetFile := os.Getenv("CONDUIT_CAPACITY_READY_FILE"), os.Getenv("CONDUIT_CAPACITY_TARGET_FILE")
	if readyFile == "" || targetFile == "" {
		t.Fatal("capacity failover coordination files are required")
	}
	proxy := newCapacityDatabaseEndpoint(t, primary.Host)
	primary.Host = proxy.listener.Addr().String()
	t.Setenv("CONDUIT_CAPACITY_POSTGRES_DSN", primary.String())
	var promoted atomic.Bool
	var afterPromotion [2]atomic.Uint64
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-t.Context().Done():
				return
			case <-ticker.C:
				contents, err := os.ReadFile(targetFile)
				if err == nil && strings.TrimSpace(string(contents)) == standby.Host {
					proxy.switchTo(standby.Host)
					promoted.Store(true)
					return
				}
			}
		}
	}()
	// Keep producing beyond the real ten-second lease expiry of a Plan whose
	// Agent loses its connection during the fault, then require fresh progress.
	report := runFleetCapacity(t, capacityConfig{Backend: "postgres", Environments: 2, Agents: 2, Handlers: 2, Duration: 20 * time.Second, Poll: 30 * time.Millisecond, ApplyDelay: time.Millisecond, Drain: 30 * time.Second, Timeout: 60 * time.Second, AllowErrors: true}, capacityHooks{ready: func() error {
		file, err := os.OpenFile(readyFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, err = file.WriteString("real reconciliation committed\n")
		closeErr := file.Close()
		if err != nil {
			return err
		}
		return closeErr
	}, completed: func(environment int) {
		if promoted.Load() {
			afterPromotion[environment].Add(1)
		}
	}})
	if !promoted.Load() {
		t.Fatal("the fixture never switched to the promoted PostgreSQL endpoint")
	}
	database, err := sql.Open("pgx", standby.String())
	if err != nil {
		t.Fatal("open promoted database")
	}
	defer database.Close()
	var inRecovery bool
	if err := database.QueryRowContext(t.Context(), "SELECT pg_is_in_recovery()").Scan(&inRecovery); err != nil || inRecovery {
		t.Fatal("replacement PostgreSQL was not observed writable")
	}
	if report.Completed < 4 {
		t.Fatalf("too little sustained reconciliation around promotion: %d Plans", report.Completed)
	}
	for environment := range afterPromotion {
		if afterPromotion[environment].Load() < 2 {
			t.Fatalf("Environment %d did not sustain reconciliation after promotion", environment)
		}
	}
	t.Logf("%d real Plans converged across promotion using two independent repository pools; post-promotion completions by Environment: %d, %d; requests interrupted by the fault are counted", report.Completed, afterPromotion[0].Load(), afterPromotion[1].Load())
}

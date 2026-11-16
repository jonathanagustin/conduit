package devserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.temporal.io/sdk/client"
)

func TestMain(m *testing.M) {
	if dir := os.Getenv("CONDUIT_DEVSERVER_TEST_CHILD"); dir != "" {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)
		data, _ := json.Marshal(os.Args[1:])
		_ = os.WriteFile(filepath.Join(dir, "args"), data, 0600)
		switch os.Getenv("CONDUIT_DEVSERVER_TEST_MODE") {
		case "exit":
			os.Exit(23)
		case "fatal":
			_, _ = io.WriteString(os.Stderr, "Failed to start gRPC listener: bind: address already in use\n")
		}
		_ = os.WriteFile(filepath.Join(dir, "ready"), nil, 0600)
		select {
		case <-signals:
			_ = os.WriteFile(filepath.Join(dir, "stopped"), nil, 0600)
			os.Exit(0)
		case <-time.After(8 * time.Second):
			os.Exit(24)
		}
	}
	os.Exit(m.Run())
}

type observedOutput struct {
	once  sync.Once
	ready chan struct{}
}

func (o *observedOutput) Write(data []byte) (int, error) {
	o.once.Do(func() { close(o.ready) })
	return len(data), nil
}

type trackedClient struct {
	client.Client
	closed atomic.Int32
}

func (c *trackedClient) Close() { c.closed.Add(1) }

func childOptions(t *testing.T, mode string) (Options, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CONDUIT_DEVSERVER_TEST_CHILD", dir)
	t.Setenv("CONDUIT_DEVSERVER_TEST_MODE", mode)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Options{CLIPath: executable, DBPath: filepath.Join(dir, "state.sqlite"), Stdout: io.Discard, Stderr: io.Discard}, dir
}
func awaitChild(ctx context.Context, dir string) error {
	for {
		if _, err := os.Stat(filepath.Join(dir, "ready")); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
}

func TestOwnedProcessAndLoopbackArguments(t *testing.T) {
	options, dir := childOptions(t, "wait")
	c := &trackedClient{}
	running, err := start(context.Background(), options, func(ctx context.Context, o client.Options) (client.Client, error) {
		if o.Namespace != "default" || !strings.HasPrefix(o.HostPort, "127.0.0.1:") {
			t.Errorf("unexpected client options: %+v", o)
		}
		return c, awaitChild(ctx, dir)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = running.Server.Stop() })
	if running.Frontend == running.UI {
		t.Fatal("frontend and UI share a port")
	}
	data, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	var args []string
	if err := json.Unmarshal(data, &args); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"server start-dev --ip 127.0.0.1", "--ui-ip 127.0.0.1", "--db-filename " + options.DBPath, "--ui-disable-news-fetch", "frontend.enableServerVersionCheck=false"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %s", want, joined)
		}
	}
	if err := running.Server.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := running.Server.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "stopped")); err != nil {
		t.Fatal("child was not gracefully stopped:", err)
	}
	select {
	case <-running.Server.done:
	default:
		t.Fatal("child not reaped")
	}
	if got := c.closed.Load(); got != 1 {
		t.Fatalf("client closed %d times", got)
	}
}

func TestFailedStartupReapsOwnedProcess(t *testing.T) {
	for _, mode := range []string{"exit", "fatal", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			options, dir := childOptions(t, mode)
			observed := &observedOutput{ready: make(chan struct{})}
			if mode == "fatal" {
				options.Stderr = observed
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			c := &trackedClient{}
			running, err := start(ctx, options, func(attempt context.Context, _ client.Options) (client.Client, error) {
				if mode == "exit" {
					<-attempt.Done()
					return nil, attempt.Err()
				}
				if err := awaitChild(attempt, dir); err != nil {
					return nil, err
				}
				if mode == "cancel" {
					cancel()
				}
				if mode == "fatal" { // Wait for the exec output copier to observe the child message.
					select {
					case <-attempt.Done():
						return nil, attempt.Err()
					case <-observed.ready:
					}
				}
				return c, nil
			})
			if err == nil || running != nil {
				if running != nil {
					_ = running.Server.Stop()
				}
				t.Fatal("failed child acknowledged as ready")
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
			if mode != "exit" {
				if _, err := os.Stat(filepath.Join(dir, "stopped")); err != nil {
					t.Fatal("failed startup leaked its child:", err)
				}
				if got := c.closed.Load(); got > 1 || (mode == "cancel" && got != 1) {
					t.Fatalf("readiness client closed %d times", got)
				}
			}
		})
	}
}

func TestPortReservationsRemainExclusiveAndReusable(t *testing.T) {
	listeners, err := reservePorts(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	defer closePorts(listeners)
	if listeners[0].Addr().String() == listeners[1].Addr().String() {
		t.Fatal("duplicate port")
	}
	for _, listener := range listeners {
		address := listener.Addr().String()
		other, err := net.Listen("tcp4", address)
		if err == nil {
			other.Close()
			t.Fatal("reservation was released before launch")
		}
		listener.Close()
		other, err = net.Listen("tcp4", address)
		if err != nil {
			t.Fatalf("explicit server bind failed: %v", err)
		}
		other.Close()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if leases, err := reservePorts(ctx, 2); !errors.Is(err, context.Canceled) || len(leases) != 0 {
		t.Fatalf("canceled reservation: %v %v", leases, err)
	}
}

func TestStartupOutputDetectsSplitAndLargeFatalMessages(t *testing.T) {
	for _, chunks := range [][]string{{"Failed to start gRPC ", "listener"}, {"panic:" + strings.Repeat("x", 20000)}, {"bind: address already in use"}} {
		state := &startupOutput{}
		var destination bytes.Buffer
		output := &serverOutput{state: state, destination: &destination}
		for _, chunk := range chunks {
			if _, err := output.Write([]byte(chunk)); err != nil {
				t.Fatal(err)
			}
		}
		if !state.failed {
			t.Fatal("fatal message was ignored")
		}
		if len(output.tail) > 8192 {
			t.Fatal("unbounded retained output")
		}
		if destination.String() != strings.Join(chunks, "") {
			t.Fatal("output forwarding changed")
		}
	}
}

func TestParentCancellationStopsReadyChild(t *testing.T) {
	options, dir := childOptions(t, "wait")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &trackedClient{}
	running, err := start(ctx, options, func(ctx context.Context, _ client.Options) (client.Client, error) { return c, awaitChild(ctx, dir) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = running.Server.Stop() })
	cancel()
	select {
	case <-running.Server.done:
	case <-time.After(3 * time.Second):
		t.Fatal("canceled child was not reaped")
	}
	if _, err := os.Stat(filepath.Join(dir, "stopped")); err != nil {
		t.Fatal(err)
	}
	if c.closed.Load() != 1 {
		t.Fatal("canceled child's client was not closed exactly once")
	}
}

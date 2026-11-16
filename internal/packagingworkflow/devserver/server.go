// Package devserver starts the pinned Temporal CLI development server on
// loopback with a persistent sqlite file. It is the localhost demonstration
// harness, not a production service.
package devserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"go.temporal.io/sdk/client"
)

// Options is the loopback server shape required by the packaging proof.
type Options struct {
	CLIPath string
	DBPath  string
	Stdout  io.Writer
	Stderr  io.Writer
}

// Running is one development server process.
type Running struct {
	Server   *Server
	Frontend string
	UI       string
}

// Server owns the child process and the readiness client, including failed starts.
type Server struct {
	cmd      *exec.Cmd
	done     chan struct{}
	mu       sync.Mutex
	exited   bool
	waitErr  error
	client   client.Client
	frontend string
	stopOnce sync.Once
	stopErr  error
}

// Start binds the frontend and UI to 127.0.0.1, disables the UI news feed,
// and keeps workflow state in DBPath so a restarted process can recover it.
func Start(ctx context.Context, options Options) (*Running, error) {
	return start(ctx, options, client.DialContext)
}

func start(ctx context.Context, options Options, dial func(context.Context, client.Options) (client.Client, error)) (*Running, error) {
	if options.CLIPath == "" || options.DBPath == "" {
		return nil, fmt.Errorf("temporal CLI path and database file are required")
	}
	listeners, err := reservePorts(ctx, 2)
	if err != nil {
		return nil, err
	}
	defer closePorts(listeners)
	frontend, ui := listeners[0].Addr().String(), listeners[1].Addr().String()
	_, frontendPort, _ := net.SplitHostPort(frontend)
	_, uiPort, _ := net.SplitHostPort(ui)
	args := []string{"server", "start-dev", "--ip", "127.0.0.1", "--port", frontendPort, "--namespace", "default", "--dynamic-config-value", "frontend.enableServerVersionCheck=false", "--log-level", "warn", "--db-filename", options.DBPath, "--ui-port", uiPort, "--ui-ip", "127.0.0.1", "--ui-disable-news-fetch"}
	output := &startupOutput{}
	stdout, stderr := options.Stdout, options.Stderr
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	command := exec.CommandContext(ctx, options.CLIPath, args...)
	configureProcess(command)
	command.Stdout = &serverOutput{state: output, destination: stdout}
	command.Stderr = &serverOutput{state: output, destination: stderr}
	command.Cancel = func() error { return interruptProcess(command.Process) }
	command.WaitDelay = 10 * time.Second
	closePorts(listeners)
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start Temporal devserver: %w", err)
	}
	server := &Server{cmd: command, done: make(chan struct{}), frontend: frontend}
	go server.wait()
	fail := func(err error) (*Running, error) { stopErr := server.Stop(); return nil, errors.Join(err, stopErr) }
	startupCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	for {
		if err := startupCtx.Err(); err != nil {
			return fail(err)
		}
		if err := server.startupError(output); err != nil {
			return fail(err)
		}
		attempt, done := context.WithTimeout(startupCtx, 500*time.Millisecond)
		ready, err := dial(attempt, client.Options{HostPort: frontend, Namespace: "default"})
		done()
		if err == nil {
			if err := startupCtx.Err(); err != nil {
				ready.Close()
				return fail(err)
			}
			if err := server.startupError(output); err != nil {
				ready.Close()
				return fail(err)
			}
			server.mu.Lock()
			if server.exited {
				server.mu.Unlock()
				ready.Close()
				return fail(fmt.Errorf("Temporal devserver exited during startup"))
			}
			server.client = ready
			server.mu.Unlock()
			if err := server.startupError(output); err != nil {
				return fail(err)
			}
			if err := startupCtx.Err(); err != nil {
				return fail(err)
			}
			return &Running{Server: server, Frontend: frontend, UI: ui}, nil
		}
		select {
		case <-startupCtx.Done():
			return fail(startupCtx.Err())
		case <-server.done:
			return fail(fmt.Errorf("Temporal devserver exited before RPC readiness"))
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (server *Server) wait() {
	err := server.cmd.Wait()
	server.mu.Lock()
	server.exited = true
	server.waitErr = err
	if server.client != nil {
		server.client.Close()
	}
	server.mu.Unlock()
	close(server.done)
}

func (server *Server) startupError(output *startupOutput) error {
	output.mu.Lock()
	failed := output.failed
	output.mu.Unlock()
	if failed {
		return fmt.Errorf("Temporal devserver reported a fatal startup listener failure")
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.exited {
		if server.waitErr != nil {
			return fmt.Errorf("Temporal devserver exited before RPC readiness: %w", server.waitErr)
		}
		return fmt.Errorf("Temporal devserver exited before RPC readiness")
	}
	return nil
}

// Stop interrupts only this owned process, reaps it and closes its readiness client.
// Repeated calls return the same result. A forced shutdown is reported as an error.
func (server *Server) Stop() error {
	server.stopOnce.Do(func() {
		select {
		case <-server.done:
		default:
			if err := interruptProcess(server.cmd.Process); err != nil && !errors.Is(err, os.ErrProcessDone) {
				server.stopErr = err
				_ = server.cmd.Process.Kill()
			}
		}
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		select {
		case <-server.done:
		case <-timer.C:
			server.stopErr = errors.Join(server.stopErr, fmt.Errorf("Temporal devserver graceful shutdown timed out"))
			_ = server.cmd.Process.Kill()
			select {
			case <-server.done:
			case <-time.After(2 * time.Second):
				server.stopErr = errors.Join(server.stopErr, fmt.Errorf("Temporal devserver was not reaped"))
				return
			}
		}
		server.mu.Lock()
		server.stopErr = errors.Join(server.stopErr, server.waitErr)
		server.mu.Unlock()
	})
	return server.stopErr
}

// Client returns the readiness client. It is closed when the child exits.
func (server *Server) Client() client.Client {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.client
}

// FrontendHostPort returns the loopback endpoint selected for this process.
func (server *Server) FrontendHostPort() string { return server.frontend }

type startupOutput struct {
	mu      sync.Mutex
	writeMu sync.Mutex
	failed  bool
}
type serverOutput struct {
	state       *startupOutput
	destination io.Writer
	tail        string
}

func (output *serverOutput) Write(data []byte) (int, error) {
	state := output.state
	// Each stream has one exec copy goroutine; retain its own partial message.
	combined := output.tail + string(data)
	lower := strings.ToLower(combined)
	if strings.Contains(lower, "failed to start grpc listener") || strings.Contains(lower, "bind: address already in use") || strings.Contains(lower, "panic:") {
		state.mu.Lock()
		state.failed = true
		state.mu.Unlock()
	}
	if len(combined) > 8192 {
		combined = combined[len(combined)-8192:]
	}
	output.tail = combined
	// Serialize destinations without blocking readiness inspection on user I/O.
	state.writeMu.Lock()
	defer state.writeMu.Unlock()
	return output.destination.Write(data)
}

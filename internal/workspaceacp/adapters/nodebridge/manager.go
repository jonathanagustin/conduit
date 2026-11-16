// Package nodebridge connects the Hub to the confined loopback Node ACP bridge
// in web/src/server/main.ts using per-bridge secret tokens, workspace-scope
// headers, and optional offline-bundle loopback-only dialing.
package nodebridge

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/workspaceacp/domain"
	"github.com/jonathanagustin/conduit/internal/workspaceacp/ports"
)

// Config configures the Node ACP subprocess bridge pool.
type Config struct {
	WebDir        string
	NodeBinary    string
	AgentCommand  string
	AgentArgs     []string
	Fixture       bool
	OfflineBundle bool
}

// Pool manages scoped loopback Node ACP bridge subprocesses keyed by (owner, workspace, branch).
type Pool struct {
	cfg     Config
	mu      sync.Mutex
	bridges map[string]*Instance
}

// NewPool creates a Node ACP BridgePool.
func NewPool(cfg Config) *Pool {
	if cfg.NodeBinary == "" {
		cfg.NodeBinary = "node"
	}
	return &Pool{
		cfg:     cfg,
		bridges: map[string]*Instance{},
	}
}

// Acquire returns or starts a confined loopback Node ACP bridge for scope.
func (p *Pool) Acquire(ctx context.Context, scope domain.Scope, files map[string][]byte) (ports.Bridge, error) {
	key := scope.Owner + "\x00" + scope.WorkspaceID + "\x00" + scope.Branch
	p.mu.Lock()
	if existing, ok := p.bridges[key]; ok {
		p.mu.Unlock()
		return existing, nil
	}
	p.mu.Unlock()

	inst, err := startInstance(ctx, p.cfg, scope, files)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	if existing, ok := p.bridges[key]; ok {
		p.mu.Unlock()
		_ = inst.Close()
		return existing, nil
	}
	p.bridges[key] = inst
	p.mu.Unlock()
	return inst, nil
}

// Instances returns a snapshot of running bridge instances (for cleanup and integration tests).
func (p *Pool) Instances() []*Instance {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*Instance, 0, len(p.bridges))
	for _, inst := range p.bridges {
		out = append(out, inst)
	}
	return out
}

// Close terminates all bridge subprocesses managed by the pool.
func (p *Pool) Close() error {
	p.mu.Lock()
	all := make([]*Instance, 0, len(p.bridges))
	for k, inst := range p.bridges {
		all = append(all, inst)
		delete(p.bridges, k)
	}
	p.mu.Unlock()
	var firstErr error
	for _, inst := range all {
		if err := inst.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// LoopbackOnlyHTTPClient returns an *http.Client that refuses to dial any
// non-loopback IP address when offlineBundle is enabled.
func LoopbackOnlyHTTPClient(offlineBundle bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if offlineBundle {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, fmt.Errorf("offline bundle refused invalid address %q: %w", addr, err)
				}
				ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
				if err != nil || len(ips) == 0 {
					return nil, fmt.Errorf("offline bundle refused unresolvable host %q", host)
				}
				var loopbackIP net.IP
				for _, candidate := range ips {
					if !candidate.IP.IsLoopback() {
						return nil, fmt.Errorf("offline bundle refused non-loopback egress to %s (%s)", host, candidate.IP.String())
					}
					if loopbackIP == nil {
						loopbackIP = candidate.IP
					}
				}
				addr = net.JoinHostPort(loopbackIP.String(), port)
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}
	return &http.Client{Transport: transport}
}

var listenPattern = regexp.MustCompile(`http://127\.0\.0\.1:(\d+)`)

// Instance is one confined loopback Node ACP bridge process.
type Instance struct {
	scope      domain.Scope
	port       int
	token      string
	tempDir    string
	cmd        *exec.Cmd
	httpClient *http.Client
	closeOnce  sync.Once
}

func startInstance(ctx context.Context, cfg Config, scope domain.Scope, files map[string][]byte) (*Instance, error) {
	webDir, err := filepath.Abs(cfg.WebDir)
	if err != nil {
		return nil, err
	}
	mainTS := filepath.Join(webDir, "src", "server", "main.ts")
	if _, err := os.Stat(mainTS); err != nil {
		return nil, fmt.Errorf("workspace ACP bridge entrypoint not found at %s: %w", mainTS, err)
	}

	tempDir, err := os.MkdirTemp("", "conduit-wsp-acp-*")
	if err != nil {
		return nil, err
	}
	for relPath, data := range files {
		cleaned := filepath.Clean(filepath.FromSlash(relPath))
		if cleaned == "." || strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
			continue
		}
		full := filepath.Join(tempDir, cleaned)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			_ = os.RemoveAll(tempDir)
			return nil, err
		}
		if err := os.WriteFile(full, data, 0o600); err != nil {
			_ = os.RemoveAll(tempDir)
			return nil, err
		}
	}

	tokenBuf := make([]byte, 32)
	if _, err := rand.Read(tokenBuf); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, err
	}
	token := hex.EncodeToString(tokenBuf)

	cmd := exec.Command(cfg.NodeBinary, "--experimental-strip-types", mainTS)
	cmd.Dir = webDir
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + tempDir,
		"ASSISTANT_WORKSPACE=" + tempDir,
		"ASSISTANT_BRIDGE_PORT=0",
		"CONDUIT_BRIDGE_TOKEN=" + token,
		"CONDUIT_BRIDGE_REQUIRE_TOKEN=1",
		"CONDUIT_WORKSPACE_OWNER=" + scope.Owner,
		"CONDUIT_WORKSPACE_ID=" + scope.WorkspaceID,
		"CONDUIT_WORKSPACE_BRANCH=" + scope.Branch,
		"INITIAL_AGENT_MODE=workspace-write",
	}
	if cfg.OfflineBundle {
		env = append(env, "CONDUIT_OFFLINE_BUNDLE=1", "NO_PROXY=*", "no_proxy=*")
	}
	if cfg.AgentCommand != "" {
		rawArgs, _ := json.Marshal(cfg.AgentArgs)
		env = append(env,
			"ASSISTANT_ACP_COMMAND="+cfg.AgentCommand,
			"ASSISTANT_ACP_ARGS="+string(rawArgs),
		)
		if cfg.Fixture {
			env = append(env, "ASSISTANT_ACP_FIXTURE=1")
		}
	}
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, err
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, err
	}

	portCh := make(chan int, 1)
	errCh := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if m := listenPattern.FindStringSubmatch(line); len(m) == 2 {
				port, convErr := strconv.Atoi(m[1])
				if convErr == nil && port > 0 {
					portCh <- port
					_, _ = io.Copy(io.Discard, stdout)
					return
				}
			}
		}
		errCh <- fmt.Errorf("bridge exited before reporting loopback port: %s", strings.TrimSpace(stderrBuf.String()))
	}()

	startupCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	select {
	case port := <-portCh:
		return &Instance{
			scope:      scope,
			port:       port,
			token:      token,
			tempDir:    tempDir,
			cmd:        cmd,
			httpClient: LoopbackOnlyHTTPClient(cfg.OfflineBundle),
		}, nil
	case err := <-errCh:
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = os.RemoveAll(tempDir)
		return nil, err
	case <-startupCtx.Done():
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("timed out starting Node ACP bridge: %s", strings.TrimSpace(stderrBuf.String()))
	}
}

// Port returns the ephemeral loopback port on 127.0.0.1 for this bridge process.
func (inst *Instance) Port() int { return inst.port }

// HTTPClient returns the bridge's HTTP client (enforcing loopback-only dials in offline mode).
func (inst *Instance) HTTPClient() *http.Client { return inst.httpClient }

func (inst *Instance) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Conduit-Bridge-Token", inst.token)
	req.Header.Set("X-Conduit-Workspace-Owner", inst.scope.Owner)
	req.Header.Set("X-Conduit-Workspace-Id", inst.scope.WorkspaceID)
	req.Header.Set("X-Conduit-Workspace-Branch", inst.scope.Branch)
}

func (inst *Instance) rpcCall(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/acp", inst.port), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	inst.setHeaders(req)
	resp, err := inst.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Result map[string]any `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode bridge response (%d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode >= 400 || envelope.Error != nil {
		switch resp.StatusCode {
		case http.StatusNotFound:
			return nil, domain.ErrNotFound
		case http.StatusConflict:
			return nil, domain.ErrConflict
		case http.StatusBadRequest:
			if envelope.Error != nil && strings.Contains(envelope.Error.Message, "protocolVersion") {
				return nil, domain.ErrProtocolVersion
			}
			return nil, domain.ErrInvalid
		case http.StatusForbidden:
			return nil, domain.ErrForbidden
		case http.StatusUnauthorized:
			return nil, domain.ErrUnauthorized
		}
		if envelope.Error != nil {
			return nil, errors.New(envelope.Error.Message)
		}
		return nil, fmt.Errorf("ACP bridge HTTP %d", resp.StatusCode)
	}
	if envelope.Result == nil {
		envelope.Result = map[string]any{}
	}
	return envelope.Result, nil
}

// Initialize validates the pinned ACP protocol version with the Node bridge.
func (inst *Instance) Initialize(ctx context.Context, protocolVersion int) (map[string]any, error) {
	return inst.rpcCall(ctx, "initialize", map[string]any{"protocolVersion": protocolVersion})
}

// NewSession opens a new ACP session on the Node bridge.
func (inst *Instance) NewSession(ctx context.Context, threadID string) (string, []any, error) {
	res, err := inst.rpcCall(ctx, "session/new", map[string]any{
		"_meta": map[string]any{"threadId": threadID},
	})
	if err != nil {
		return "", nil, err
	}
	sessionID, _ := res["sessionId"].(string)
	if sessionID == "" {
		return "", nil, errors.New("Node ACP bridge returned an empty sessionId")
	}
	opts, _ := res["configOptions"].([]any)
	return sessionID, opts, nil
}

// SetConfigOption updates a session configuration option on the Node bridge.
func (inst *Instance) SetConfigOption(ctx context.Context, sessionID, configID string, value any) ([]any, error) {
	res, err := inst.rpcCall(ctx, "session/set_config_option", map[string]any{
		"sessionId": sessionID,
		"configId":  configID,
		"value":     value,
	})
	if err != nil {
		return nil, err
	}
	opts, _ := res["configOptions"].([]any)
	return opts, nil
}

// Prompt streams an ACP prompt turn over SSE from the Node bridge.
func (inst *Instance) Prompt(
	ctx context.Context,
	rpcID any,
	sessionID, runID string,
	prompt []map[string]any,
	onEvent func(ports.BridgeEvent) error,
) error {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      rpcID,
		"method":  "session/prompt",
		"params": map[string]any{
			"sessionId": sessionID,
			"prompt":    prompt,
			"_meta":     map[string]any{"runId": runID},
		},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/acp", inst.port), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	inst.setHeaders(req)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := inst.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return domain.ErrNotFound
		}
		if resp.StatusCode == http.StatusConflict {
			return domain.ErrConflict
		}
		return fmt.Errorf("ACP prompt HTTP %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		rawJSON := strings.TrimPrefix(line, "data: ")
		ev, err := decodeBridgeEvent([]byte(rawJSON))
		if err != nil {
			return err
		}
		if err := onEvent(ev); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func decodeBridgeEvent(raw []byte) (ports.BridgeEvent, error) {
	var frame map[string]any
	if err := json.Unmarshal(raw, &frame); err != nil {
		return ports.BridgeEvent{}, err
	}
	kind, _ := frame["type"].(string)
	ev := ports.BridgeEvent{
		Type:     kind,
		RawFrame: frame,
	}
	switch kind {
	case "permission_request":
		if reqRaw, ok := frame["request"]; ok {
			buf, _ := json.Marshal(reqRaw)
			var perm ports.BridgePermission
			if err := json.Unmarshal(buf, &perm); err == nil {
				ev.Permission = &perm
			}
		}
	case "activity":
		target := any(frame)
		if nested, ok := frame["activity"]; ok && nested != nil {
			target = nested
		}
		buf, _ := json.Marshal(target)
		var act ports.BridgeActivity
		if err := json.Unmarshal(buf, &act); err == nil {
			ev.Activity = &act
		}
	case "snapshot":
		if snap, ok := frame["snapshot"].(map[string]any); ok {
			ev.Snapshot = snap
		}
	}
	return ev, nil
}

// SettlePermission forwards a Hub-governed permission decision to the Node bridge.
func (inst *Instance) SettlePermission(ctx context.Context, requestID, threadID, runID, optionID string, cancelled bool) error {
	bodyMap := map[string]any{
		"threadId": threadID,
		"runId":    runID,
	}
	if cancelled {
		bodyMap["cancelled"] = true
	} else {
		bodyMap["optionId"] = optionID
	}
	payload, err := json.Marshal(bodyMap)
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/acp/permissions/%s", inst.port, url.PathEscape(requestID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	inst.setHeaders(req)
	resp, err := inst.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return domain.ErrNotFound
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("bridge permission settlement HTTP %d", resp.StatusCode)
	}
	return nil
}

// CancelSession cancels any in-flight turn for sessionID on the Node bridge.
func (inst *Instance) CancelSession(ctx context.Context, sessionID string) error {
	_, err := inst.rpcCall(ctx, "session/cancel", map[string]any{"sessionId": sessionID})
	return err
}

// CloseSession closes sessionID on the Node bridge.
func (inst *Instance) CloseSession(ctx context.Context, sessionID string) error {
	_, err := inst.rpcCall(ctx, "session/close", map[string]any{"sessionId": sessionID})
	return err
}

// Close stops the Node bridge subprocess and removes its temporary workspace directory.
func (inst *Instance) Close() error {
	inst.closeOnce.Do(func() {
		if inst.cmd != nil && inst.cmd.Process != nil {
			_ = inst.cmd.Process.Kill()
			_ = inst.cmd.Wait()
		}
		if inst.tempDir != "" {
			_ = os.RemoveAll(inst.tempDir)
		}
	})
	return nil
}

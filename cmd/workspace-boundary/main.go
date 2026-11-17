// Command workspace-boundary is the process that runs inside a workspace
// container. It listens for allowlisted forwards, or reports what that
// container can actually reach and which credential paths it can see.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: workspace-boundary listen|forward|probe|sleep")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "listen":
		err = listen(os.Args[2:])
	case "forward":
		err = forward()
	case "probe":
		err = probe()
	case "sleep":
		err = sleepUntilSignal()
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func listen(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("listen expects a port")
	}
	ln, err := net.Listen("tcp4", ":"+args[0])
	if err != nil {
		return err
	}
	defer ln.Close()
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		_, _ = io.WriteString(conn, "ok\n")
		conn.Close()
	}
}

type forwardRule struct {
	Listen   int    `json:"listen"`
	Upstream string `json:"upstream"`
}

func forward() error {
	var rules []forwardRule
	if err := json.Unmarshal([]byte(os.Getenv("CONDUIT_FORWARD")), &rules); err != nil {
		return fmt.Errorf("CONDUIT_FORWARD: %w", err)
	}
	if len(rules) == 0 {
		return fmt.Errorf("CONDUIT_FORWARD is empty")
	}
	errCh := make(chan error, len(rules))
	for _, rule := range rules {
		ln, err := net.Listen("tcp4", ":"+strconv.Itoa(rule.Listen))
		if err != nil {
			return err
		}
		go serve(ln, rule.Upstream, errCh)
	}
	return <-errCh
}

func serve(ln net.Listener, upstream string, errCh chan error) {
	defer ln.Close()
	for {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		go handle(conn, upstream)
	}
}

func handle(conn net.Conn, upstream string) {
	defer conn.Close()
	if upstream == "" {
		return
	}
	up, err := net.DialTimeout("tcp4", upstream, 2*time.Second)
	if err != nil {
		return
	}
	defer up.Close()
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(up, conn)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(conn, up)
		done <- struct{}{}
	}()
	<-done
}

type probeSpec struct {
	Dials           []dialSpec `json:"dials"`
	CredentialPaths []string   `json:"credentialPaths"`
	ForbiddenEnv    []string   `json:"forbiddenEnv"`
}

type dialSpec struct {
	Name   string `json:"name"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Expect string `json:"expect"`
}

type dialResult struct {
	Name   string `json:"name"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Expect string `json:"expect"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type credResult struct {
	Path    string `json:"path"`
	Present bool   `json:"present"`
}

type probeResult struct {
	UID              int          `json:"uid"`
	CapEff           string       `json:"capEff"`
	CapBnd           string       `json:"capBnd"`
	CapBounded       bool         `json:"capBounded"`
	Dials            []dialResult `json:"dials"`
	Credentials      []credResult `json:"credentials"`
	ForbiddenEnv     []string     `json:"forbiddenEnv"`
	PolicyWriteError string       `json:"policyWriteError"`
}

func probe() error {
	raw, err := base64.StdEncoding.DecodeString(os.Getenv("CONDUIT_PROBE"))
	if err != nil {
		return fmt.Errorf("CONDUIT_PROBE: %w", err)
	}
	var spec probeSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return fmt.Errorf("CONDUIT_PROBE: %w", err)
	}
	effective := capabilityValue("CapEff:")
	bounding := capabilityValue("CapBnd:")
	result := probeResult{
		UID:         os.Getuid(),
		CapEff:      effective,
		CapBnd:      bounding,
		CapBounded:  capIsZero(effective) && capIsZero(bounding),
		Credentials: make([]credResult, 0, len(spec.CredentialPaths)),
	}
	for _, dial := range spec.Dials {
		ok, detail := dialCheck(dial.Host, dial.Port)
		result.Dials = append(result.Dials, dialResult{
			Name: dial.Name, Host: dial.Host, Port: dial.Port, Expect: dial.Expect, OK: ok, Detail: detail,
		})
	}
	for _, path := range spec.CredentialPaths {
		_, err := os.Stat(path)
		result.Credentials = append(result.Credentials, credResult{Path: path, Present: err == nil})
	}
	for _, key := range spec.ForbiddenEnv {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			result.ForbiddenEnv = append(result.ForbiddenEnv, key)
		}
	}
	if writeErr := os.WriteFile("/workspace-networkpolicy.yaml", []byte("kind: NetworkPolicy\n"), 0o644); writeErr != nil {
		result.PolicyWriteError = writeErr.Error()
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

func dialCheck(host string, port int) (bool, string) {
	conn, err := net.DialTimeout("tcp4", net.JoinHostPort(host, strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		return false, err.Error()
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	n, readErr := conn.Read(buf)
	body := strings.TrimSpace(string(buf[:n]))
	if strings.Contains(body, "ok") {
		return true, body
	}
	if readErr != nil {
		return false, readErr.Error()
	}
	if body == "" {
		return false, "empty response"
	}
	return false, body
}

func capabilityValue(field string) string {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, field) {
			return strings.TrimSpace(strings.TrimPrefix(line, field))
		}
	}
	return ""
}

func capIsZero(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	n, err := strconv.ParseUint(value, 16, 64)
	return err == nil && n == 0
}

func sleepUntilSignal() error {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	<-ch
	return nil
}

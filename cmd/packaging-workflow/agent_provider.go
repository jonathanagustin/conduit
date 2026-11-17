package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/localcmd"
)

// Provider names accepted by `agent-exec --provider`. "deterministic" is the
// built-in offline agent; the rest delegate one turn to a local model CLI.
const (
	providerDeterministic = "deterministic"
	providerClaude        = "claude"
	providerCodex         = "codex"
	providerGemini        = "gemini"
	providerGrok          = "grok"
)

// providerArgv returns the argv for a provider. Each can be replaced with
// CONDUIT_PROVIDER_<NAME>_CMD (whitespace-separated argv). Credentials stay in
// the CLI's own configuration; Conduit never reads or forwards them.
//
// The defaults and whether each has been exercised are one table,
// localcmd.Presets. Every delegating preset is UNVERIFIED there: no run
// against a real codex, claude, gemini or grok CLI is recorded in this
// repository (none is installed in CI), and selecting one logs a warning.
// Override the argv if your version differs.
func providerArgv(name string) ([]string, error) {
	env := "CONDUIT_PROVIDER_" + strings.ToUpper(name) + "_CMD"
	if raw := strings.Fields(os.Getenv(env)); len(raw) > 0 {
		return raw, nil
	}
	if preset, ok := localcmd.PresetByName(name); ok && name != "" && len(preset.Argv) > 0 {
		return append([]string(nil), preset.Argv...), nil
	}
	return nil, fmt.Errorf("unknown agent provider %q (want deterministic, claude, codex, gemini or grok)", name)
}

const providerReplyRule = "\n\nReply with the requested output only: no commentary and no markdown fences. " +
	"When the instructions ask for JSON, reply with exactly one JSON object.\n"

// runProviderPrompt sends one prompt to the provider CLI and returns its
// normalized reply. The prompt text is passed on stdin, never on argv.
func runProviderPrompt(ctx context.Context, provider, prompt string) (string, error) {
	argv, err := providerArgv(provider)
	if err != nil {
		return "", err
	}
	timeout := 90 * time.Second
	if raw := os.Getenv("CONDUIT_MODEL_TIMEOUT"); raw != "" {
		if d, parseErr := time.ParseDuration(raw); parseErr == nil && d > 0 {
			timeout = d
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = strings.NewReader(prompt + providerReplyRule)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s provider failed: %w: %s", provider, err, strings.TrimSpace(truncate(stderr.String(), 400)))
	}
	reply := normalizeProviderReply(stdout.String(), strings.Contains(prompt, "JSON") || strings.Contains(prompt, "toolCalls"))
	if reply == "" {
		return "", fmt.Errorf("%s provider returned an empty reply", provider)
	}
	return reply, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// normalizeProviderReply strips markdown fences and, when JSON is expected and
// the reply is not already one object, extracts the first balanced top-level
// object from surrounding prose.
func normalizeProviderReply(raw string, wantJSON bool) string {
	out := strings.TrimSpace(raw)
	if strings.HasPrefix(out, "```") {
		out = strings.TrimPrefix(out, "```")
		if nl := strings.Index(out, "\n"); nl >= 0 {
			out = out[nl+1:]
		}
		out = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(out), "```"))
	}
	if !wantJSON || json.Valid([]byte(out)) {
		return out
	}
	if obj, ok := firstJSONObject(out); ok {
		return obj
	}
	return out
}

func firstJSONObject(s string) (string, bool) {
	for start := 0; start < len(s); start++ {
		if s[start] != '{' {
			continue
		}
		depth, inStr, esc := 0, false, false
		for i := start; i < len(s); i++ {
			c := s[i]
			switch {
			case esc:
				esc = false
			case inStr && c == '\\':
				esc = true
			case c == '"':
				inStr = !inStr
			case inStr:
			case c == '{':
				depth++
			case c == '}':
				depth--
				if depth == 0 {
					if cand := s[start : i+1]; json.Valid([]byte(cand)) {
						return cand, true
					}
					goto next
				}
			}
		}
	next:
	}
	return "", false
}

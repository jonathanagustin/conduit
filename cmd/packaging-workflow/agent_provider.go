package main

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jonathanagustin/conduit/internal/localcmd"
)

// Only the deterministic offline utility remains selectable. The legacy
// provider functions refuse both vendor and arbitrary operator labels.
const (
	providerDeterministic = "deterministic"
	providerClaude        = "claude"
	providerCodex         = "codex"
	providerGemini        = "gemini"
	providerGrok          = "grok"
)

func providerArgv(name string) ([]string, error) { return nil, localcmd.ErrLocalCommandUnsupported }

const providerReplyRule = "\n\nReply with the requested output only: no commentary and no markdown fences. " +
	"When the instructions ask for JSON, reply with exactly one JSON object.\n"

// runProviderPrompt refuses the retired provider delegation interface.
func runProviderPrompt(ctx context.Context, provider, prompt string) (string, error) {
	return "", localcmd.ErrLocalCommandUnsupported
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

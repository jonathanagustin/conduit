package composition

import (
	"fmt"
	"strings"
)

// workspaceACPAgentLaunch keeps the bundled codex ACP default, covered only by
// an initialize/negotiation smoke (scripts/verify-codex-acp-initialize.mjs).
// Every other agent requires DEVWORKSPACE_ACP_AGENT_CMD with explicit argv;
// removed vendor defaults have no recorded conformance run here.
func workspaceACPAgentLaunch(getenv func(string) string) (command string, args []string, err error) {
	if override := strings.Fields(getenv("DEVWORKSPACE_ACP_AGENT_CMD")); len(override) > 0 {
		return override[0], override[1:], nil
	}
	switch name := strings.ToLower(strings.TrimSpace(getenv("DEVWORKSPACE_ACP_AGENT"))); name {
	case "", "codex":
		return "", nil, nil
	case "claude", "gemini", "grok":
		return "", nil, fmt.Errorf("DEVWORKSPACE_ACP_AGENT=%s has no preset; set DEVWORKSPACE_ACP_AGENT_CMD to an ACP-speaking command", name)
	default:
		return "", nil, fmt.Errorf("unknown DEVWORKSPACE_ACP_AGENT %q (use codex or set DEVWORKSPACE_ACP_AGENT_CMD)", name)
	}
}

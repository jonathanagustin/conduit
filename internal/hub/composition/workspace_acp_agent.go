package composition

import (
	"fmt"
	"strings"
)

// workspaceACPAgentLaunch resolves the coding agent the ACP bridge spawns.
//
// DEVWORKSPACE_ACP_AGENT selects a preset: codex (the bundled codex-acp, the
// default), claude, gemini or grok. DEVWORKSPACE_ACP_AGENT_CMD replaces the
// preset with an explicit whitespace-separated argv, and is required for grok,
// which has no preset. The agent must speak ACP over stdio. Whatever is chosen
// still runs behind the Hub's governed permission mapping, so a preset gives an
// agent no extra authority.
//
// Only the codex default is exercised by this repository. The claude and gemini
// presets name the ACP adapters those vendors are known to ship and have not
// been run here; set DEVWORKSPACE_ACP_AGENT_CMD if your install differs.
func workspaceACPAgentLaunch(getenv func(string) string) (command string, args []string, err error) {
	if override := strings.Fields(getenv("DEVWORKSPACE_ACP_AGENT_CMD")); len(override) > 0 {
		return override[0], override[1:], nil
	}
	switch name := strings.ToLower(strings.TrimSpace(getenv("DEVWORKSPACE_ACP_AGENT"))); name {
	case "", "codex":
		return "", nil, nil
	case "claude":
		return "claude-code-acp", nil, nil
	case "gemini":
		return "gemini", []string{"--experimental-acp"}, nil
	case "grok":
		return "", nil, fmt.Errorf("DEVWORKSPACE_ACP_AGENT=grok has no preset; set DEVWORKSPACE_ACP_AGENT_CMD to an ACP-speaking command")
	default:
		return "", nil, fmt.Errorf("unknown DEVWORKSPACE_ACP_AGENT %q (want codex, claude, gemini or grok)", name)
	}
}

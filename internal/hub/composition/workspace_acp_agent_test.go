package composition

import (
	"reflect"
	"strings"
	"testing"
)

func TestWorkspaceACPAgentLaunch(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	cases := []struct {
		name     string
		env      map[string]string
		wantCmd  string
		wantArgs []string
		wantErr  string
	}{
		{name: "default is bundled codex", env: nil},
		{name: "codex explicit", env: map[string]string{"DEVWORKSPACE_ACP_AGENT": "Codex"}},
		{name: "claude", env: map[string]string{"DEVWORKSPACE_ACP_AGENT": "claude"}, wantCmd: "claude-code-acp"},
		{name: "gemini", env: map[string]string{"DEVWORKSPACE_ACP_AGENT": " gemini "}, wantCmd: "gemini", wantArgs: []string{"--experimental-acp"}},
		{name: "override wins over preset", env: map[string]string{"DEVWORKSPACE_ACP_AGENT": "claude", "DEVWORKSPACE_ACP_AGENT_CMD": "/opt/grok acp --stdio"}, wantCmd: "/opt/grok", wantArgs: []string{"acp", "--stdio"}},
		{name: "grok needs override", env: map[string]string{"DEVWORKSPACE_ACP_AGENT": "grok"}, wantErr: "no preset"},
		{name: "unknown refused", env: map[string]string{"DEVWORKSPACE_ACP_AGENT": "bogus"}, wantErr: "unknown"},
	}
	for _, tc := range cases {
		cmd, args, err := workspaceACPAgentLaunch(env(tc.env))
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("%s: err = %v, want %q", tc.name, err, tc.wantErr)
			}
			continue
		}
		if err != nil || cmd != tc.wantCmd || !reflect.DeepEqual(args, tc.wantArgs) {
			t.Errorf("%s: got (%q, %v, %v), want (%q, %v)", tc.name, cmd, args, err, tc.wantCmd, tc.wantArgs)
		}
	}
}

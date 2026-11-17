package localcmd

import (
	"os"
	"strings"
)

// EnvAllowVar names the operator variable that extends the child environment
// allowlist. Its value is a comma-separated list of exact variable names.
const EnvAllowVar = "CONDUIT_MODEL_ENV_ALLOW"

// allowedEnv is the set of parent variables a model CLI child may see. Names
// are compared in upper case. Everything else, including the Hub's own
// DSNs, signing keys, OIDC secrets and notification settings, is dropped.
var allowedEnv = map[string]bool{
	// Process basics.
	"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true, "TERM": true,
	"LANG": true, "LANGUAGE": true, "TZ": true, "TMPDIR": true, "TMP": true, "TEMP": true,
	"XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true, "XDG_CACHE_HOME": true,
	"XDG_STATE_HOME": true, "XDG_RUNTIME_DIR": true,
	// Windows equivalents.
	"SYSTEMROOT": true, "COMSPEC": true, "PATHEXT": true, "USERPROFILE": true,
	"APPDATA": true, "LOCALAPPDATA": true,
	// Outbound proxy and trust roots.
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "ALL_PROXY": true, "NO_PROXY": true,
	"SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "NODE_EXTRA_CA_CERTS": true,
	"REQUESTS_CA_BUNDLE": true, "CURL_CA_BUNDLE": true,
	// Provider credentials and endpoints the model CLIs read.
	"ANTHROPIC_API_KEY": true, "ANTHROPIC_AUTH_TOKEN": true, "ANTHROPIC_BASE_URL": true,
	"ANTHROPIC_MODEL": true, "CLAUDE_CODE_OAUTH_TOKEN": true, "CLAUDE_CONFIG_DIR": true,
	"OPENAI_API_KEY": true, "OPENAI_BASE_URL": true, "OPENAI_ORG_ID": true,
	"CODEX_HOME": true, "CODEX_API_KEY": true,
	"GEMINI_API_KEY": true, "GOOGLE_API_KEY": true, "GOOGLE_APPLICATION_CREDENTIALS": true,
	"GOOGLE_CLOUD_PROJECT": true, "GOOGLE_CLOUD_LOCATION": true, "GOOGLE_GENAI_USE_VERTEXAI": true,
	"XAI_API_KEY": true, "GROK_API_KEY": true,
	// Read by `packaging-workflow agent-exec`, the ready-made model command.
	"CONDUIT_AGENT_PROVIDER": true, "CONDUIT_MODEL_TIMEOUT": true,
	// A Hub may delegate through agent-exec before reaching the model CLI.
	// Preserve the operator's policy so filtering that second child does
	// not discard variables explicitly admitted for the first child.
	EnvAllowVar: true,
}

// ChildEnv filters a parent environment (os.Environ form) down to the
// allowlist plus the names the operator listed in EnvAllowVar.
func ChildEnv(parent []string) []string {
	extra := map[string]bool{}
	for _, item := range parent {
		key, value, _ := strings.Cut(item, "=")
		if key != EnvAllowVar {
			continue
		}
		for _, name := range strings.Split(value, ",") {
			if name = strings.TrimSpace(name); name != "" {
				extra[name] = true
			}
		}
	}
	env := []string{}
	for _, item := range parent {
		key, _, ok := strings.Cut(item, "=")
		if !ok || key == "" {
			continue
		}
		if envAllowed(key) || extra[key] {
			env = append(env, item)
		}
	}
	return env
}

func envAllowed(key string) bool {
	upper := strings.ToUpper(key)
	if allowedEnv[upper] || strings.HasPrefix(upper, "LC_") {
		return true
	}
	// CONDUIT_PROVIDER_<NAME>_CMD replaces a provider argv for agent-exec.
	return strings.HasPrefix(upper, "CONDUIT_PROVIDER_") && strings.HasSuffix(upper, "_CMD")
}

func childEnv() []string { return ChildEnv(os.Environ()) }

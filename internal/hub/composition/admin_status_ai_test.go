package composition

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	admin "github.com/jonathanagustin/conduit/internal/adminstatus/domain"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func probeWith(env map[string]string, binaries ...string) aiProbe {
	present := map[string]bool{}
	for _, name := range binaries {
		present[name] = true
	}
	return aiProbe{getenv: envOf(env), lookPath: func(name string) (string, error) {
		if present[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}}
}

func componentsByID(components []admin.Component) map[string]admin.Component {
	out := map[string]admin.Component{}
	for _, component := range components {
		out[component.ID] = component
	}
	return out
}

func TestAIProbeReportsProviderKindWithoutLeakingCredentials(t *testing.T) {
	const secret = "sk-test-do-not-print-0123456789"
	key := filepath.Join(t.TempDir(), "model-api-key-file")
	if err := os.WriteFile(key, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	probe := probeWith(map[string]string{
		"CONDUIT_MODEL_BASE_URL": "https://models.internal.example:8443/tenant-route/v1", "CONDUIT_MODEL_NAME": "served-model",
		"CONDUIT_MODEL_API_KEY_FILE": key,
	})
	components := probe.components()
	got := componentsByID(components)
	provider := got["modelProvider"]
	if provider.Status != admin.Configured || !strings.Contains(provider.Detail, "kind http") ||
		!strings.Contains(provider.Detail, "https://models.internal.example:8443") || !strings.Contains(provider.Detail, "served-model") {
		t.Fatalf("provider = %+v", provider)
	}
	// The scratch image case: a model is configured and git is absent.
	if got["aiPlanning"].Status != admin.Unavailable || !strings.Contains(got["aiPlanning"].Detail, "git") {
		t.Fatalf("planning = %+v", got["aiPlanning"])
	}
	if got["aiAssistant"].Status != admin.Configured {
		t.Fatalf("assistant = %+v", got["aiAssistant"])
	}
	raw, err := json.Marshal(components)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{secret, key, "model-api-key-file", "tenant-route", "/v1"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("status leaked %q: %s", leak, raw)
		}
	}
}

func TestAIProbeNeverEchoesAnInvalidConfiguration(t *testing.T) {
	cases := map[string]struct {
		env  map[string]string
		want string
		leak []string
	}{
		"both transports": {
			env:  map[string]string{"CONDUIT_MODEL_CMD": "codex exec -", "CONDUIT_MODEL_BASE_URL": "https://models.example/v1", "CONDUIT_MODEL_NAME": "m"},
			want: "CONDUIT_MODEL_CMD and CONDUIT_MODEL_BASE_URL are both set",
		},
		"inline credentials": {
			env:  map[string]string{"CONDUIT_MODEL_BASE_URL": "https://user:hunter2-password@models.example/v1", "CONDUIT_MODEL_NAME": "m"},
			want: "the model HTTP configuration is not valid", leak: []string{"hunter2-password", "user:"},
		},
		"key in the query": {
			env:  map[string]string{"CONDUIT_MODEL_BASE_URL": "https://models.example/v1?api_key=query-secret-value", "CONDUIT_MODEL_NAME": "m"},
			want: "the model HTTP configuration is not valid", leak: []string{"query-secret-value"},
		},
		"unreadable key file": {
			env:  map[string]string{"CONDUIT_MODEL_BASE_URL": "https://models.example/v1", "CONDUIT_MODEL_NAME": "m", "CONDUIT_MODEL_API_KEY_FILE": "/nonexistent/secret-path/key"},
			want: "the model API key file is not usable", leak: []string{"secret-path"},
		},
		"plaintext without the opt-in": {
			env:  map[string]string{"CONDUIT_MODEL_BASE_URL": "http://vllm.models.svc:8000/v1", "CONDUIT_MODEL_NAME": "m"},
			want: "the model endpoint uses plain http without CONDUIT_MODEL_ALLOW_INSECURE_HTTP=true",
		},
		"shell syntax in the command": {
			env:  map[string]string{"CONDUIT_MODEL_CMD": "codex exec; cat /etc/passwd"},
			want: "CONDUIT_MODEL_CMD is not a valid command line", leak: []string{"passwd"},
		},
		"command not installed": {
			env:  map[string]string{"CONDUIT_MODEL_CMD": "/opt/private-tools/codex exec -"},
			want: "the model command codex was not found", leak: []string{"private-tools"},
		},
	}
	for name, c := range cases {
		components := probeWith(c.env, "git", "go", "docker").components()
		got := componentsByID(components)
		// The whole detail is fixed words: the underlying error is not echoed.
		if got["modelProvider"].Status != admin.Unavailable || got["modelProvider"].Detail != "Provider kind invalid: "+c.want {
			t.Fatalf("%s: provider = %+v", name, got["modelProvider"])
		}
		if got["aiPlanning"].Status != admin.Unavailable {
			t.Fatalf("%s: planning must not be reported usable: %+v", name, got["aiPlanning"])
		}
		raw, _ := json.Marshal(components)
		for _, leak := range c.leak {
			if strings.Contains(string(raw), leak) {
				t.Fatalf("%s: status leaked %q: %s", name, leak, raw)
			}
		}
	}
}

func TestAIProbeReportsTheCommandProviderAndItsPreset(t *testing.T) {
	plain := componentsByID(probeWith(map[string]string{"CONDUIT_MODEL_CMD": "/usr/local/bin/codex exec -"}, "/usr/local/bin/codex", "git").components())
	if plain["modelProvider"].Status != admin.Configured || !strings.Contains(plain["modelProvider"].Detail, "kind cli: command codex;") ||
		!strings.Contains(plain["modelProvider"].Detail, "unverified") || strings.Contains(plain["modelProvider"].Detail, "/usr/local") {
		t.Fatalf("provider = %+v", plain["modelProvider"])
	}
	if plain["aiPlanning"].Status != admin.Configured {
		t.Fatalf("planning with a command and git = %+v", plain["aiPlanning"])
	}
	preset := componentsByID(probeWith(map[string]string{
		"CONDUIT_MODEL_CMD": "packaging-workflow agent-exec --provider claude", "CONDUIT_MODEL_NAME": "operator-chosen",
	}, "packaging-workflow").components())
	if detail := preset["modelProvider"].Detail; strings.Contains(detail, "agent-exec preset claude") || !strings.Contains(detail, "configured model operator-chosen") {
		t.Fatalf("preset provider = %q", detail)
	}
	none := componentsByID(probeWith(nil).components())
	if none["modelProvider"].Status != admin.Unconfigured || !strings.Contains(none["modelProvider"].Detail, "kind none") {
		t.Fatalf("no provider = %+v", none["modelProvider"])
	}
}

func TestAdminSummaryCarriesTheAICapabilities(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	probe := probeWith(map[string]string{"CONDUIT_MODEL_BASE_URL": "https://models.example/v1", "CONDUIT_MODEL_NAME": "m"})
	source := adminStatusSource{repository: repository, orchestrator: "native", now: time.Now, ai: probe.components}
	summary, err := source.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	got := componentsByID(summary.Components)
	for _, id := range []string{"modelProvider", "aiAssistant", "aiPlanning", "aiAppGeneration", "aiPackagingProposals", "aiPackagingCompute"} {
		if got[id].Detail == "" {
			t.Fatalf("summary lacks %s: %+v", id, summary.Components)
		}
	}
	if got["hub"].Status != admin.Available {
		t.Fatalf("existing components changed: %+v", summary.Components)
	}
	// A source built without a probe reports no AI component at all.
	bare, err := adminStatusSource{repository: repository, orchestrator: "native", now: time.Now}.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, present := componentsByID(bare.Components)["modelProvider"]; present {
		t.Fatal("a source with no probe fabricated a model provider status")
	}
}

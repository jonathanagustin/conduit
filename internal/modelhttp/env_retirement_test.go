package modelhttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/modelconfig"
)

func TestCommandsRefuseBeforeHTTPKeysAndCAOrOfflineFallback(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	for _, key := range []string{"CONDUIT_MODEL_CMD", "CONDUIT_ASSISTANT_MODEL_CMD", "CONDUIT_PLANNING_MODEL_CMD", "CONDUIT_PACKAGING_MODEL_CMD", "CONDUIT_APPGEN_MODEL_CMD"} {
		for _, base := range []string{"", server.URL} {
			values := map[string]string{key: "operator-secret-argv", "CONDUIT_MODEL_BASE_URL": base, "CONDUIT_MODEL_NAME": "m", "CONDUIT_MODEL_API_KEY_FILE": "/must-not-read/key", "CONDUIT_MODEL_CA_FILE": "/must-not-read/ca"}
			original := func(name string) string { return values[name] }
			for _, get := range []func(string) string{original, modelconfig.ForCapability(original, modelconfig.Assistant), modelconfig.ForCapability(original, modelconfig.Planning), modelconfig.ForCapability(original, modelconfig.Packaging), modelconfig.ForCapability(original, modelconfig.Appgen)} {
				client, err := FromEnv(get)
				if client != nil || !errors.Is(err, localcmd.ErrLocalCommandUnsupported) {
					t.Fatalf("%s base=%q: client=%v err=%v", key, base, client, err)
				}
				if strings.Contains(err.Error(), "operator-secret") || strings.Contains(err.Error(), "must-not-read") {
					t.Fatalf("refusal exposed configuration: %v", err)
				}
			}
		}
	}
	if calls.Load() != 0 {
		t.Fatal("refused command fell back to HTTP")
	}
}

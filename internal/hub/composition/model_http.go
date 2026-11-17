package composition

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/modelhttp"
)

// errModelBothConfigured is the refusal to choose between two model
// transports. Startup fails rather than guessing which one was meant.
var errModelBothConfigured = errors.New(
	"CONDUIT_MODEL_CMD and CONDUIT_MODEL_BASE_URL are both set; set exactly one model transport")

// modelHTTPClient reads the HTTP model configuration. It returns nil with no
// error when CONDUIT_MODEL_BASE_URL is unset, which leaves every caller on
// its existing path. The key is named by file; no variable holds it inline.
func modelHTTPClient(getenv func(string) string) (*modelhttp.Client, error) {
	if getenv == nil {
		return nil, nil
	}
	base := strings.TrimSpace(getenv("CONDUIT_MODEL_BASE_URL"))
	if base == "" {
		return nil, nil
	}
	if strings.TrimSpace(getenv("CONDUIT_MODEL_CMD")) != "" {
		return nil, errModelBothConfigured
	}
	timeout := modelhttp.DefaultTimeout
	if text := strings.TrimSpace(getenv("CONDUIT_MODEL_TIMEOUT")); text != "" {
		parsed, err := time.ParseDuration(text)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("CONDUIT_MODEL_TIMEOUT must be a positive duration")
		}
		timeout = parsed
	}
	client, err := modelhttp.New(modelhttp.Config{
		BaseURL:           base,
		Model:             getenv("CONDUIT_MODEL_NAME"),
		APIKeyFile:        getenv("CONDUIT_MODEL_API_KEY_FILE"),
		CAFile:            getenv("CONDUIT_MODEL_CA_FILE"),
		AllowInsecureHTTP: getenv("CONDUIT_MODEL_ALLOW_INSECURE_HTTP") == "true",
		Timeout:           timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("CONDUIT_MODEL_BASE_URL: %w", err)
	}
	return client, nil
}

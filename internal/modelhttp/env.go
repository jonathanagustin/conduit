package modelhttp

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/localcmd"
)

// ErrBothConfigured is the refusal to choose between two model transports.
// Startup fails rather than guessing which one was meant.
var ErrBothConfigured = errors.New(
	"CONDUIT_MODEL_CMD and CONDUIT_MODEL_BASE_URL are both set; set exactly one model transport")

// FromEnv refuses retired command settings before inspecting any HTTP
// configuration. An absent endpoint then leaves the existing offline path.
// HTTP keys are file references, never inline credential values.
func FromEnv(getenv func(string) string) (*Client, error) {
	if getenv == nil {
		return nil, nil
	}
	if err := RejectLocalCommands(getenv); err != nil {
		return nil, err
	}
	base := strings.TrimSpace(getenv("CONDUIT_MODEL_BASE_URL"))
	if base == "" {
		return nil, nil
	}
	timeout := DefaultTimeout
	if text := strings.TrimSpace(getenv("CONDUIT_MODEL_TIMEOUT")); text != "" {
		parsed, err := time.ParseDuration(text)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("CONDUIT_MODEL_TIMEOUT must be a positive duration")
		}
		timeout = parsed
	}
	client, err := New(Config{
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

// RejectLocalCommands refuses every retired command setting, even without an
// HTTP endpoint or when the selected capability would otherwise be offline.
// Only fixed setting names enter the error; command values may be sensitive.
func RejectLocalCommands(getenv func(string) string) error {
	if getenv == nil {
		return nil
	}
	for _, name := range []string{"CONDUIT_ASSISTANT_MODEL_CMD", "CONDUIT_PLANNING_MODEL_CMD", "CONDUIT_PACKAGING_MODEL_CMD", "CONDUIT_APPGEN_MODEL_CMD", "CONDUIT_MODEL_CMD"} {
		if strings.TrimSpace(getenv(name)) != "" {
			return fmt.Errorf("%s: %w", name, localcmd.ErrLocalCommandUnsupported)
		}
	}
	return nil
}

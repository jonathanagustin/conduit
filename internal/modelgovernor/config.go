package modelgovernor

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The environment variables FromEnv reads.
const (
	EnvGlobalConcurrency   = "CONDUIT_MODEL_MAX_CONCURRENCY"
	EnvOwnerConcurrency    = "CONDUIT_MODEL_MAX_CONCURRENCY_PER_OWNER"
	EnvQueueWait           = "CONDUIT_MODEL_QUEUE_WAIT"
	EnvMaxAttempts         = "CONDUIT_MODEL_MAX_ATTEMPTS"
	EnvDailyTokenBudget    = "CONDUIT_MODEL_DAILY_TOKEN_BUDGET"
	EnvDailyCallBudget     = "CONDUIT_MODEL_DAILY_CALL_BUDGET"
	EnvTranscripts         = "CONDUIT_AI_TRANSCRIPTS"
	EnvTranscriptRetention = "CONDUIT_AI_TRANSCRIPT_RETENTION"
)

// ErrInvalidConfig is a value FromEnv could not accept. Startup stops on it.
var ErrInvalidConfig = errors.New("model governance configuration is not valid")

// FromEnv reads the configuration. Every variable is optional and an unset
// one takes its default, except that turning transcripts on requires a
// retention: there is no default for how long tenant text is kept.
func FromEnv(getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	var config Config
	var err error
	if config.GlobalConcurrency, err = positiveInt(getenv, EnvGlobalConcurrency); err != nil {
		return Config{}, err
	}
	if config.OwnerConcurrency, err = positiveInt(getenv, EnvOwnerConcurrency); err != nil {
		return Config{}, err
	}
	if config.MaxAttempts, err = positiveInt(getenv, EnvMaxAttempts); err != nil {
		return Config{}, err
	}
	if config.QueueWait, err = positiveDuration(getenv, EnvQueueWait); err != nil {
		return Config{}, err
	}
	tokens, err := positiveInt(getenv, EnvDailyTokenBudget)
	if err != nil {
		return Config{}, err
	}
	calls, err := positiveInt(getenv, EnvDailyCallBudget)
	if err != nil {
		return Config{}, err
	}
	config.DailyTokenBudget, config.DailyCallBudget = int64(tokens), int64(calls)
	switch strings.ToLower(strings.TrimSpace(getenv(EnvTranscripts))) {
	case "", "off":
	case "on":
		config.Transcripts = true
	default:
		return Config{}, fmt.Errorf("%w: %s must be off or on", ErrInvalidConfig, EnvTranscripts)
	}
	if config.TranscriptRetention, err = positiveDuration(getenv, EnvTranscriptRetention); err != nil {
		return Config{}, err
	}
	if config.Transcripts && config.TranscriptRetention <= 0 {
		return Config{}, fmt.Errorf("%w: %s is required when %s is on", ErrInvalidConfig, EnvTranscriptRetention, EnvTranscripts)
	}
	return config, nil
}

// positiveInt reads an integer of at least 1. Unset is zero.
func positiveInt(getenv func(string) string, name string) (int, error) {
	text := strings.TrimSpace(getenv(name))
	if text == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(text)
	if err != nil || value < 1 {
		return 0, fmt.Errorf("%w: %s must be a positive integer", ErrInvalidConfig, name)
	}
	return value, nil
}

func positiveDuration(getenv func(string) string, name string) (time.Duration, error) {
	text := strings.TrimSpace(getenv(name))
	if text == "" {
		return 0, nil
	}
	value, err := time.ParseDuration(text)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%w: %s must be a positive duration", ErrInvalidConfig, name)
	}
	return value, nil
}

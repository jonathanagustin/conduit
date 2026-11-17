package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jonathanagustin/conduit/internal/hub/ratelimit"
	metricsapplication "github.com/jonathanagustin/conduit/internal/observability/application"
)

// loadRequestLimiter reads the Hub's request limits.
//
// Every variable is optional and every default is non-zero: a Hub with no
// rate-limit configuration at all is still limited. There is deliberately no
// variable that turns the limiter off. A deployment that needs more room
// raises a number, which leaves a bound in place and a record of what it is.
//
// A value that is set and unusable stops the Hub. Falling back to the default
// would run a different limit from the one the operator wrote down.
func loadRequestLimiter(getenv func(string) string) (*ratelimit.Limiter, error) {
	config := ratelimit.DefaultConfig()
	for _, class := range []struct {
		name  string
		limit *ratelimit.Limit
	}{
		{"OPERATOR", &config.Operator},
		{"AGENT", &config.Agent},
		{"ANONYMOUS", &config.Anonymous},
	} {
		rateVariable, burstVariable := "RATE_LIMIT_"+class.name+"_RPS", "RATE_LIMIT_"+class.name+"_BURST"
		if text := strings.TrimSpace(getenv(rateVariable)); text != "" {
			parsed, err := strconv.ParseFloat(text, 64)
			// Written as "not within", so that NaN, which compares false
			// to everything, is refused rather than passed.
			if err != nil || !(parsed > 0 && parsed <= 1e6) {
				return nil, fmt.Errorf("%s must be a positive number of requests per second, at most 1000000", rateVariable)
			}
			class.limit.PerSecond = parsed
		}
		if text := strings.TrimSpace(getenv(burstVariable)); text != "" {
			parsed, err := strconv.Atoi(text)
			if err != nil || parsed < 1 || parsed > 1e6 {
				return nil, fmt.Errorf("%s must be a whole number of requests between 1 and 1000000", burstVariable)
			}
			class.limit.Burst = parsed
		}
	}
	return ratelimit.New(config)
}

// requestLimiterMetrics narrows the limiter to what the metrics endpoint
// publishes: three classes and their counts, and no caller.
func requestLimiterMetrics(limiter *ratelimit.Limiter) metricsapplication.ReadRateLimits {
	if limiter == nil {
		return nil
	}
	return func() []metricsapplication.RateLimitClass {
		snapshot := limiter.Snapshot()
		classes := make([]metricsapplication.RateLimitClass, 0, len(snapshot))
		for _, class := range ratelimit.Classes() {
			counts := snapshot[class]
			classes = append(classes, metricsapplication.RateLimitClass{
				Class: string(class), Allowed: counts.Allowed, Limited: counts.Limited, Callers: counts.Keys,
			})
		}
		return classes
	}
}

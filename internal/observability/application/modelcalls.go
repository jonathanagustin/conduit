package application

import (
	"context"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/observability/domain"
)

// ModelCallSeries is one series of model-call counters. Declared here rather
// than imported from the governor, for the same reason as RateLimitClass.
type ModelCallSeries struct {
	Capability, Transport, Outcome string
	Calls, Retries                 uint64
	DurationSeconds                float64
	PromptTokens, CompletionTokens uint64
}

// ReadModelCalls is the governor's snapshot, narrowed.
type ReadModelCalls func() []ModelCallSeries

// The closed sets the three labels may take. They are repeated here, not read
// from the snapshot, so the labels stay bounded by this file whatever a
// caller passes. An owner is never a label: it names a tenant's operator. A
// model name is never a label either: the configured name is operator text
// and the provider's id is remote text, neither is bounded by code, and
// per-model numbers are in the usage store behind an authorization check.
var (
	modelCapabilities = closedSet("assistant", "planning", "appgen", "intelligence", "packaging")
	modelTransports   = closedSet("http", "cli")
	modelOutcomes     = closedSet(
		"ok", "timeout", "rate_limited", "upstream_error", "transport_error", "auth_error", "invalid_response",
		"canceled", "error", "throttled", "budget_exceeded", "audit_failed",
	)
)

func closedSet(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

// WithModelCalls adds model-call counters to a gathered registry.
//
// Everything is a counter. Throttles and budget refusals are the calls series
// at outcome="throttled" and outcome="budget_exceeded"; failures are every
// outcome but "ok". Latency is a sum and the calls series is its count, so
// rate(sum)/rate(calls) is the mean: the registry has counters and gauges and
// no histogram type, and a bucketed series would need an `le` label this
// allowlist does not carry.
func WithModelCalls(
	gather func(context.Context) (*domain.Registry, error), read ReadModelCalls,
) func(context.Context) (*domain.Registry, error) {
	if read == nil {
		return gather
	}
	return func(ctx context.Context) (*domain.Registry, error) {
		registry, err := gather(ctx)
		if err != nil {
			return nil, err
		}
		for _, series := range read() {
			_, capability := modelCapabilities[series.Capability]
			_, transport := modelTransports[series.Transport]
			_, outcome := modelOutcomes[series.Outcome]
			if !capability || !transport || !outcome {
				return nil, fmt.Errorf("model call series %q/%q/%q is outside the closed sets this exporter publishes",
					series.Capability, series.Transport, series.Outcome)
			}
			labels := func() map[string]string {
				return map[string]string{"capability": series.Capability, "transport": series.Transport, "outcome": series.Outcome}
			}
			for _, metric := range []struct {
				name, help string
				value      float64
			}{
				{"conduit_model_calls_total", "model calls by capability, transport and outcome class; outcome throttled and budget_exceeded never reached a model", float64(series.Calls)},
				{"conduit_model_call_retries_total", "attempts beyond each model call's first", float64(series.Retries)},
				{"conduit_model_call_duration_seconds_sum", "seconds spent in model calls, waits and retries included; divide by conduit_model_calls_total for the mean", series.DurationSeconds},
				{"conduit_model_prompt_tokens_total", "prompt tokens the provider reported; a call with no usage object adds nothing", float64(series.PromptTokens)},
				{"conduit_model_completion_tokens_total", "completion tokens the provider reported; a call with no usage object adds nothing", float64(series.CompletionTokens)},
			} {
				registry.Add(domain.Metric{Name: metric.name, Kind: domain.KindCounter, Help: metric.help, Labels: labels(), Value: metric.value})
			}
		}
		return registry, nil
	}
}

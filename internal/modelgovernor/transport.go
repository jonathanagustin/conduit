package modelgovernor

import (
	"context"
	"errors"
	"time"

	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/modelhttp"
)

// Resolver finds the governor for a call that carries none in its context. It
// may return nil, which leaves the call ungoverned.
type Resolver func() *Governor

func governorFor(ctx context.Context, fallback Resolver) *Governor {
	if governor := scopeFrom(ctx).Governor; governor != nil {
		return governor
	}
	if fallback != nil {
		return fallback()
	}
	return nil
}

// HTTPMiddleware governs every completion of one capability's HTTP client.
func HTTPMiddleware(capability Capability, fallback Resolver) modelhttp.Middleware {
	return func(ctx context.Context, model, prompt string, next func(context.Context) (modelhttp.Completion, error)) (modelhttp.Completion, error) {
		governor := governorFor(ctx, fallback)
		if governor == nil {
			return next(ctx)
		}
		var last modelhttp.Completion
		_, err := governor.Do(ctx,
			Call{Capability: capability, Transport: TransportHTTP, ConfiguredModel: model, Prompt: []byte(prompt)},
			ClassifyHTTP,
			func(ctx context.Context) (Result, error) {
				completion, err := next(ctx)
				last = completion
				if err != nil {
					return Result{}, err
				}
				return Result{
					Response: []byte(completion.Text), ProviderModel: completion.Model,
					PromptTokens: completion.PromptTokens, CompletionTokens: completion.CompletionTokens,
				}, nil
			})
		if err != nil {
			return modelhttp.Completion{}, err
		}
		return last, nil
	}
}

// CLIExec governs every run of one capability's model command. The model name
// is the operator's configuration; a command reports none.
func CLIExec(capability Capability, configuredModel string, fallback Resolver) localcmd.ExecFunc {
	return func(ctx context.Context, argv []string, timeout time.Duration, stdin []byte) ([]byte, error) {
		governor := governorFor(ctx, fallback)
		if governor == nil {
			return localcmd.Execute(ctx, argv, timeout, stdin)
		}
		var stdout []byte
		_, err := governor.Do(ctx,
			Call{Capability: capability, Transport: TransportCLI, ConfiguredModel: configuredModel, Prompt: stdin},
			ClassifyCLI,
			func(ctx context.Context) (Result, error) {
				out, err := localcmd.Execute(ctx, argv, timeout, stdin)
				stdout = out
				return Result{Response: out}, err
			})
		if errors.Is(err, ErrBusy) || errors.Is(err, ErrBudgetExceeded) || errors.Is(err, ErrAuditUnavailable) || errors.Is(err, ErrUsageUnavailable) {
			// Nothing a refused call produced is handed on.
			return nil, err
		}
		return stdout, err
	}
}

// ClassifyHTTP sorts an HTTP model failure. Retried: a connection that
// failed, a 429, a 5xx. Not retried: a refused credential, another 4xx, a
// reply that is not a completion, a refused redirect, a cancelled request,
// and a timeout. A timeout is terminal because the request may have been
// served, and because it already spent the patience a retry would spend
// again.
func ClassifyHTTP(err error) Verdict {
	var limited *modelhttp.RateLimitError
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, modelhttp.ErrCanceled):
		return Verdict{Outcome: OutcomeCanceled}
	case errors.As(err, &limited):
		return Verdict{Outcome: OutcomeRateLimited, Retryable: true, RetryAfter: limited.RetryAfter}
	case errors.Is(err, modelhttp.ErrRateLimited):
		return Verdict{Outcome: OutcomeRateLimited, Retryable: true}
	case errors.Is(err, modelhttp.ErrUpstream):
		return Verdict{Outcome: OutcomeUpstream, Retryable: true}
	case errors.Is(err, modelhttp.ErrRedirectRefused):
		return Verdict{Outcome: OutcomeTransport}
	case errors.Is(err, modelhttp.ErrTransport):
		return Verdict{Outcome: OutcomeTransport, Retryable: true}
	case errors.Is(err, modelhttp.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return Verdict{Outcome: OutcomeTimeout}
	case errors.Is(err, modelhttp.ErrUnauthorized), errors.Is(err, modelhttp.ErrKeyFile):
		return Verdict{Outcome: OutcomeAuth}
	case errors.Is(err, modelhttp.ErrMalformed), errors.Is(err, modelhttp.ErrResponseTooLarge):
		return Verdict{Outcome: OutcomeInvalidResponse}
	default:
		return Verdict{Outcome: OutcomeError}
	}
}

// ClassifyCLI sorts a model command failure. Only a process that could not be
// started is retried: nothing read the prompt. A non-zero exit and a timeout
// are terminal, because a command that ran may have spent tokens or acted,
// and a missing command will be missing next time too.
func ClassifyCLI(err error) Verdict {
	switch {
	case errors.Is(err, context.Canceled):
		return Verdict{Outcome: OutcomeCanceled}
	case errors.Is(err, localcmd.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return Verdict{Outcome: OutcomeTimeout}
	case errors.Is(err, localcmd.ErrStartFailed):
		return Verdict{Outcome: OutcomeTransport, Retryable: true}
	default:
		return Verdict{Outcome: OutcomeError}
	}
}

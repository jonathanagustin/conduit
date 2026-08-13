package optimization

import "context"

const HiGHSBackendName = "highs"

// HiGHSProcessSolverBackend adds the shared compiler/preflight in front of the
// generic process adapter. Unsupported models are unavailable, not runtime
// failures, so RunSolverRequest can consider only explicitly named fallbacks.
type HiGHSProcessSolverBackend struct {
	ExternalProcessSolverBackend
}

func (backend HiGHSProcessSolverBackend) Descriptor() SolverBackendDescriptor {
	return backend.ExternalProcessSolverBackend.Descriptor()
}

func (backend HiGHSProcessSolverBackend) Solve(ctx context.Context, request SolverRequest) (SolverBackendOutcome, error) {
	if _, err := CompileHiGHSModel(request); err != nil {
		return SolverBackendOutcome{}, ErrSolverBackendUnavailable
	}
	return backend.ExternalProcessSolverBackend.Solve(ctx, request)
}

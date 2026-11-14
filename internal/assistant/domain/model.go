package domain

import "context"

// Model is the outbound port. A local command is one adapter.
type Model interface {
	Complete(ctx context.Context, prompt string) (string, Provenance, error)
}

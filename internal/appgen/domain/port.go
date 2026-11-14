package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

const (
	LabelNoModel   = "No model call"
	LabelLiveModel = "Live model call"

	MemoryCeilingMiB = 1024
	CPUCeilingMilli  = 1000
	MaxFiles         = 3
	MaxFileBytes     = 4096
	MaxTotalBytes    = 8192
)

var (
	ErrModelUnavailable = errors.New("local model CLI is not configured")
	ErrModelTimeout     = errors.New("local model CLI timed out")
	ErrModelFailed      = errors.New("local model CLI failed")
	ErrSpecInvalid      = errors.New("app spec is not valid")
)

// Request is the operator's description of one HTTP application.
type Request struct {
	Name        string
	Description string
}

// Provenance records who produced a spec and the digests of the exchange.
// Adapter is a command name or "recorded". The digests are lowercase sha256 hex.
type Provenance struct {
	Adapter        string `json:"adapter,omitempty"`
	PromptSHA256   string `json:"promptSha256,omitempty"`
	ResponseSHA256 string `json:"responseSha256,omitempty"`
}

// AppGenerator is the outbound port. Implementations live in adapters.
type AppGenerator interface {
	Generate(ctx context.Context, request Request) (Spec, Provenance, error)
}

// Digest is the sha256 hex of the exact bytes.
func Digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Label reports whether the exchange called a configured command.
func Label(provenance Provenance) string {
	if provenance.Adapter == "" || provenance.Adapter == "recorded" {
		return LabelNoModel
	}
	return LabelLiveModel
}

// Package domain is the HTTP application generation contract.
// It validates one spec and renders packaging documents. It does not start a process.
package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	// ErrAcceptedPlanDigest means generation was asked to implement a plan
	// whose recorded digest does not match the plan text.
	ErrAcceptedPlanDigest = errors.New("accepted plan digest does not match the recorded plan text")
)

// ConversationMessage is one turn of the planning conversation that produced
// the accepted plan. Role is operator or model.
type ConversationMessage struct {
	Role string
	Text string
}

// Request is the operator's description of one HTTP application.
type Request struct {
	Name        string
	Description string
	// Attachments are bound, read-only context the operator attached to the
	// intake request. The generator may read them. It still returns a spec only.
	Attachments []Attachment
	// PlanRevisionID and PlanDigest identify the accepted planning revision.
	// AcceptedPlan is that revision's text. Conversation is the planning
	// transcript, including requirements added after the original problem statement.
	// All three plan fields are empty for a generation that is not bound to a session.
	PlanRevisionID string
	PlanDigest     string
	AcceptedPlan   string
	Conversation   []ConversationMessage
	// Repair is set when an earlier reply failed a gate. The prompt then
	// carries the failure output, framed as untrusted, and the earlier reply.
	Repair *Repair
}

// Attachment is one stored intake file. SHA256 is lowercase hex of Content.
type Attachment struct {
	Name    string `json:"name"`
	SHA256  string `json:"sha256"`
	Content string `json:"-"`
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

// AcceptedPlanDigest is the sha256 of the canonical plan text. It is the same
// canonical form the planning binding records, so a generation input can be
// checked against that binding without trusting a caller-supplied hash.
func AcceptedPlanDigest(text string) string {
	canonical, _ := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: text})
	return Digest(canonical)
}

// Label reports whether the exchange called a configured command.
func Label(provenance Provenance) string {
	if provenance.Adapter == "" || provenance.Adapter == AdapterRecorded {
		return LabelNoModel
	}
	return LabelLiveModel
}

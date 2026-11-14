package domain

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

const MemoryCeilingMiB = 1024

var (
	ErrModelUnavailable = errors.New("local model CLI is not configured")
	ErrModelTimeout     = errors.New("local model CLI timed out")
	ErrModelFailed      = errors.New("local model CLI failed")
	ErrProposalInvalid  = errors.New("proposal is not valid")
)

// PackagingContext is the packaging fact a proposer may see.
type PackagingContext struct {
	RunID        string
	ChartValues  string
	CurrentLimit string
	GateRules    string
}

// Proposal is one field change a person can review.
type Proposal struct {
	Field     string `json:"field"`
	Value     string `json:"value"`
	Rationale string `json:"rationale"`
}

// Provenance records who produced a proposal and the digests of the exchange.
// Adapter is a command name or a model id. The digests are lowercase sha256 hex.
type Provenance struct {
	Adapter        string `json:"adapter,omitempty"`
	PromptSHA256   string `json:"promptSha256,omitempty"`
	ResponseSHA256 string `json:"responseSha256,omitempty"`
}

// ChangeProposer is the outbound port. Implementations live in adapters.
type ChangeProposer interface {
	Propose(ctx context.Context, input PackagingContext) (Proposal, Provenance, error)
}

// Prompt is the instruction sent to a proposer. The response must be one JSON object.
func Prompt(input PackagingContext) string {
	return strings.TrimSpace(fmt.Sprintf(`Reply with one JSON object and no other text.
The object has field, value, and rationale.
field must be "memory". value is a size such as 512Mi. rationale is one sentence.
Packaging run: %s
Chart values: %s
Current limit: %s
Gate rules: %s`,
		oneLine(input.RunID), oneLine(input.ChartValues), oneLine(input.CurrentLimit), oneLine(input.GateRules)))
}

// ParseProposal accepts one JSON object and nothing else.
func ParseProposal(raw []byte) (Proposal, error) {
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(raw)))
	decoder.DisallowUnknownFields()
	var proposal Proposal
	if err := decoder.Decode(&proposal); err != nil {
		return Proposal{}, fmt.Errorf("%w: %v", ErrProposalInvalid, err)
	}
	if decoder.More() {
		return Proposal{}, fmt.Errorf("%w: extra data after the object", ErrProposalInvalid)
	}
	proposal.Field = strings.TrimSpace(proposal.Field)
	proposal.Value = strings.TrimSpace(proposal.Value)
	proposal.Rationale = strings.TrimSpace(proposal.Rationale)
	if proposal.Field != "memory" {
		return Proposal{}, fmt.Errorf("%w: field must be memory", ErrProposalInvalid)
	}
	if _, ok := parseMiB(proposal.Value); !ok {
		return Proposal{}, fmt.Errorf("%w: value must be a size in Mi or Gi", ErrProposalInvalid)
	}
	if proposal.Rationale == "" || len(proposal.Rationale) > MaxReasonBytes {
		return Proposal{}, fmt.Errorf("%w: rationale is required", ErrProposalInvalid)
	}
	for _, r := range proposal.Rationale {
		if unicode.IsControl(r) {
			return Proposal{}, fmt.Errorf("%w: rationale has a control character", ErrProposalInvalid)
		}
	}
	return proposal, nil
}

// AssessMemory applies the packaging memory ceiling. The third result is true
// when the value is blocked. 8Gi uses the same reason as the recorded gate.
func AssessMemory(value string) (Outcome, string, bool) {
	mib, ok := parseMiB(strings.TrimSpace(value))
	if !ok {
		return OutcomeFailRequired, "memory limit is not a size", true
	}
	if mib > MemoryCeilingMiB {
		return OutcomeFailRequired, fmt.Sprintf("memory limit %s exceeds the 1Gi ceiling", strings.TrimSpace(value)), true
	}
	return OutcomePass, "memory limit is within the 1Gi ceiling", false
}

// DigestText is sha256 hex of the exact bytes.
func DigestText(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func parseMiB(value string) (int, bool) {
	if strings.HasSuffix(value, "Mi") {
		n, err := strconv.Atoi(strings.TrimSuffix(value, "Mi"))
		if err != nil || n < 1 {
			return 0, false
		}
		return n, true
	}
	if strings.HasSuffix(value, "Gi") {
		n, err := strconv.Atoi(strings.TrimSuffix(value, "Gi"))
		if err != nil || n < 1 {
			return 0, false
		}
		return n * 1024, true
	}
	return 0, false
}

func oneLine(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "not recorded"
	}
	return strings.ReplaceAll(value, "\n", " ")
}

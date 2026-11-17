// Package domain is the assistant tool contract. It validates tool calls and
// model replies. It does not read the catalog, open a change request, or
// start a process.
package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	ToolQuery   = "query_ontology_objects"
	ToolPropose = "propose_governed_action"
	ToolDiff    = "review_change_diff"
	ToolWrite   = "governed_write"

	LabelNoModel   = "No model call"
	LabelLiveModel = "Live model call"

	// MaxToolCallsPerReply is how many calls one model reply may carry.
	MaxToolCallsPerReply = 3
	// MaxRounds is how many model calls one turn may make. Tool results are
	// fed back between rounds, and the last round has to answer.
	MaxRounds = 4
	// MaxToolCallsPerTurn bounds the calls run across all rounds of one turn.
	MaxToolCallsPerTurn = 8
	// MaxToolResultBytes bounds one tool result as it is fed back to the model.
	MaxToolResultBytes = 8 << 10

	ModeExplore     = "explore"
	ModeReleasePlan = "release_plan"
	ModeConfigDiff  = "config_diff"
	ModeOntology    = "ontology"
)

var (
	ErrToolInvalid      = errors.New("tool call is not valid")
	ErrModelUnavailable = errors.New("local model CLI is not configured")
	ErrModelTimeout     = errors.New("local model CLI timed out")
	ErrModelFailed      = errors.New("local model CLI failed")
	ErrReplyInvalid     = errors.New("model reply is not valid")
	// ErrRoundCap means the model was still asking for tools in its last round.
	ErrRoundCap = errors.New("the model did not answer within the round limit")
	// ErrAuditUnavailable stops a tool call or turn whose audit entry cannot be written.
	ErrAuditUnavailable = errors.New("assistant audit is unavailable")
)

// Chip is one object the operator attached to the turn.
type Chip struct {
	ID         string `json:"id"`
	ObjectType string `json:"objectType"`
	Label      string `json:"label"`
}

// QueryArgs is one object-set read: a type, an optional equality filter, and
// optional link names.
type QueryArgs struct {
	ObjectType string    `json:"objectType"`
	Filter     *EqFilter `json:"filter,omitempty"`
	Links      []string  `json:"links,omitempty"`
}

// EqFilter is one field equality. The catalog evaluates it.
type EqFilter struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

// ProposeArgs names a governed action. Opening a change request still needs a person.
type ProposeArgs struct {
	ActionID         string `json:"actionId"`
	TargetEntityID   string `json:"targetEntityId"`
	TargetObjectType string `json:"targetObjectType"`
	Environment      string `json:"environment"`
	InstallationID   string `json:"installationId"`
	ProductID        string `json:"productId"`
}

// DiffArgs names the change request whose stored diff should be shown.
type DiffArgs struct {
	ChangeRequestID string `json:"changeRequestId"`
}

// WriteArgs is one governed mutation. Missing fields come back as elicitation.
// Confirm is set only after the person submits the card. An action that opens
// a change request names its own environment, installation, and product: the
// server never fills one in.
type WriteArgs struct {
	Action         string `json:"action"`
	TargetID       string `json:"targetId,omitempty"`
	Environment    string `json:"environment,omitempty"`
	InstallationID string `json:"installationId,omitempty"`
	ProductID      string `json:"productId,omitempty"`
	Value          string `json:"value,omitempty"`
	Justification  string `json:"justification,omitempty"`
	Expected       string `json:"expectedOutcome,omitempty"`
	Confirm        bool   `json:"confirm,omitempty"`
}

// Field is one elicitation input. It never asks for a secret.
type Field struct {
	Name     string   `json:"name"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Value    string   `json:"value,omitempty"`
	Options  []string `json:"options,omitempty"`
}

// ToolCall is one validated call.
type ToolCall struct {
	Name      string
	Query     QueryArgs
	Propose   ProposeArgs
	Diff      DiffArgs
	Write     WriteArgs
	Arguments json.RawMessage
}

// ModelReply is the only object a model may return.
type ModelReply struct {
	Text      string        `json:"text"`
	ToolCalls []RawToolCall `json:"toolCalls"`
}

// RawToolCall is one call inside a model reply, before argument checks.
type RawToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Provenance records the command name and the exchange digests.
type Provenance struct {
	ConfiguredModel string `json:"configuredModel,omitempty"`
	ProviderModel   string `json:"providerModel,omitempty"`
	Adapter         string
	PromptSHA256    string
	ResponseSHA256  string
}

// ModeAllows reports whether a mode may run a tool.
func ModeAllows(mode, tool string) bool {
	for _, name := range toolsForMode(mode) {
		if name == tool {
			return true
		}
	}
	return false
}

func toolsForMode(mode string) []string {
	switch strings.TrimSpace(mode) {
	case ModeExplore:
		return []string{ToolQuery}
	case ModeReleasePlan:
		return []string{ToolQuery, ToolPropose, ToolWrite}
	case ModeConfigDiff:
		return []string{ToolQuery, ToolDiff}
	case ModeOntology:
		return []string{ToolQuery, ToolPropose, ToolDiff, ToolWrite}
	default:
		return nil
	}
}

// ParseToolCall accepts one tool name and one JSON object.
func ParseToolCall(name string, arguments json.RawMessage) (ToolCall, error) {
	name = strings.TrimSpace(name)
	call := ToolCall{Name: name, Arguments: bytes.TrimSpace(arguments)}
	if len(call.Arguments) == 0 {
		call.Arguments = []byte("{}")
	}
	switch name {
	case ToolQuery:
		args, err := decode[QueryArgs](call.Arguments)
		if err != nil {
			return ToolCall{}, err
		}
		if strings.TrimSpace(args.ObjectType) == "" {
			return ToolCall{}, fmt.Errorf("%w: objectType is required", ErrToolInvalid)
		}
		if args.Filter != nil && (strings.TrimSpace(args.Filter.Field) == "" || strings.TrimSpace(args.Filter.Value) == "") {
			return ToolCall{}, fmt.Errorf("%w: filter needs a field and a value", ErrToolInvalid)
		}
		if len(args.Links) > 4 {
			return ToolCall{}, fmt.Errorf("%w: at most 4 links", ErrToolInvalid)
		}
		for _, link := range args.Links {
			if strings.TrimSpace(link) == "" {
				return ToolCall{}, fmt.Errorf("%w: link name is empty", ErrToolInvalid)
			}
		}
		call.Query = args
	case ToolPropose:
		args, err := decode[ProposeArgs](call.Arguments)
		if err != nil {
			return ToolCall{}, err
		}
		for _, value := range []string{args.ActionID, args.TargetEntityID, args.TargetObjectType, args.Environment, args.InstallationID, args.ProductID} {
			if strings.TrimSpace(value) == "" {
				return ToolCall{}, fmt.Errorf("%w: action, target, environment, installation, and product are required", ErrToolInvalid)
			}
		}
		call.Propose = args
	case ToolDiff:
		args, err := decode[DiffArgs](call.Arguments)
		if err != nil {
			return ToolCall{}, err
		}
		if strings.TrimSpace(args.ChangeRequestID) == "" {
			return ToolCall{}, fmt.Errorf("%w: changeRequestId is required", ErrToolInvalid)
		}
		call.Diff = args
	case ToolWrite:
		args, err := decode[WriteArgs](call.Arguments)
		if err != nil {
			return ToolCall{}, err
		}
		if !knownWriteAction(args.Action) {
			return ToolCall{}, fmt.Errorf("%w: unknown action", ErrToolInvalid)
		}
		call.Write = args
	default:
		return ToolCall{}, fmt.Errorf("%w: unknown tool %s", ErrToolInvalid, name)
	}
	if !ModeAllows(ModeOntology, name) {
		return ToolCall{}, fmt.Errorf("%w: unknown tool %s", ErrToolInvalid, name)
	}
	return call, nil
}

// ParseReply accepts one reply object with at most three tool calls. The
// object may be the whole reply, sit inside a Markdown fence, or follow prose:
// a raw CLI often prints either. The object itself is still decoded strictly.
func ParseReply(raw []byte) (ModelReply, error) {
	var first error
	for _, candidate := range replyCandidates(raw) {
		reply, shaped, err := parseReplyObject(candidate)
		if err == nil {
			return reply, nil
		}
		if shaped {
			// The right object with a wrong tool call. An earlier object in
			// the same output is not a better answer.
			return ModelReply{}, err
		}
		if first == nil {
			first = err
		}
	}
	if first == nil {
		first = fmt.Errorf("%w: no JSON object in the reply", ErrReplyInvalid)
	}
	return ModelReply{}, first
}

// parseReplyObject decodes one candidate. shaped reports that it was a reply
// object carrying tool calls, so its error is the reply's error.
func parseReplyObject(raw []byte) (ModelReply, bool, error) {
	reply, err := decode[ModelReply](raw)
	if err != nil {
		return ModelReply{}, false, fmt.Errorf("%w: %v", ErrReplyInvalid, err)
	}
	reply.Text = strings.TrimSpace(reply.Text)
	if reply.Text == "" && len(reply.ToolCalls) == 0 {
		return ModelReply{}, false, fmt.Errorf("%w: text or a tool call is required", ErrReplyInvalid)
	}
	if len(reply.ToolCalls) > MaxToolCallsPerReply {
		return ModelReply{}, true, fmt.Errorf("%w: at most %d tool calls", ErrReplyInvalid, MaxToolCallsPerReply)
	}
	for _, call := range reply.ToolCalls {
		if _, err := ParseToolCall(call.Name, call.Arguments); err != nil {
			return ModelReply{}, true, fmt.Errorf("%w: %w", ErrReplyInvalid, err)
		}
	}
	return reply, true, nil
}

// WriteActions lists every action governed_write accepts.
func WriteActions() []string {
	return []string{
		"add_eval_case", "propose_memory", "cancel_packaging_run", "rerun_packaging_run",
		"promote", "recall", "approve_change", "reject_change", "select_course", "start_packaging_run",
	}
}

func knownWriteAction(action string) bool {
	action = strings.TrimSpace(action)
	for _, known := range WriteActions() {
		if action == known {
			return true
		}
	}
	return false
}

// Risk is low when the caller's role may apply the action after Confirm.
// Promote, recall, approval, and resource changes stay high and open a change request.
func Risk(action string) string {
	switch strings.TrimSpace(action) {
	case "add_eval_case", "rerun_packaging_run", "start_packaging_run":
		return "low"
	default:
		return "high"
	}
}

// MissingFields lists what a write still needs. It never asks for a secret.
func MissingFields(args WriteArgs) []Field {
	var fields []Field
	need := func(name, label, value string, options ...string) {
		if strings.TrimSpace(value) != "" {
			return
		}
		fields = append(fields, Field{Name: name, Label: label, Type: "string", Required: true, Options: options})
	}
	// changeTarget is what a change request is filed against. The caller names
	// all three; none of them has a default.
	changeTarget := func() {
		need("environment", "Environment", args.Environment)
		need("installationId", "Installation", args.InstallationID)
		need("productId", "Product", args.ProductID)
	}
	switch strings.TrimSpace(args.Action) {
	case "add_eval_case":
		need("justification", "Scenario", args.Justification)
		need("value", "Memory value", args.Value)
		need("expectedOutcome", "Expected outcome", args.Expected, "PASS", "FAIL_REQUIRED")
	case "propose_memory", "select_course":
		need("value", "Memory value", args.Value, "512Mi", "1Gi", "8Gi")
		need("justification", "Justification", args.Justification)
		changeTarget()
	case "promote", "recall":
		need("targetId", "Target", args.TargetID)
		need("justification", "Justification", args.Justification)
		changeTarget()
	case "cancel_packaging_run":
		need("targetId", "Packaging run", args.TargetID)
		changeTarget()
	case "approve_change", "reject_change":
		need("targetId", "Change request", args.TargetID)
		need("justification", "Justification", args.Justification)
	}
	return fields
}

// Resource is the coordinate the action rules already use.
func Resource(objectType, id string) string {
	if objectType == "PackagingRun" {
		return "packaging-run/" + id
	}
	return objectType + "/" + id
}

// Digest is the sha256 hex of the exact bytes.
func Digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func decode[T any](raw []byte) (T, error) {
	var value T
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, fmt.Errorf("%w: %v", ErrToolInvalid, err)
	}
	if decoder.More() {
		return value, fmt.Errorf("%w: extra data after the object", ErrToolInvalid)
	}
	return value, nil
}

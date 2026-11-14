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

	LabelNoModel   = "No model call"
	LabelLiveModel = "Live model call"

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

// ToolCall is one validated call.
type ToolCall struct {
	Name      string
	Query     QueryArgs
	Propose   ProposeArgs
	Diff      DiffArgs
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
	Adapter        string
	PromptSHA256   string
	ResponseSHA256 string
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
		return []string{ToolQuery, ToolPropose}
	case ModeConfigDiff:
		return []string{ToolQuery, ToolDiff}
	case ModeOntology:
		return []string{ToolQuery, ToolPropose, ToolDiff}
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
	default:
		return ToolCall{}, fmt.Errorf("%w: unknown tool %s", ErrToolInvalid, name)
	}
	if !ModeAllows(ModeOntology, name) {
		return ToolCall{}, fmt.Errorf("%w: unknown tool %s", ErrToolInvalid, name)
	}
	return call, nil
}

// ParseReply accepts one JSON object and at most three tool calls.
func ParseReply(raw []byte) (ModelReply, error) {
	reply, err := decode[ModelReply](raw)
	if err != nil {
		return ModelReply{}, fmt.Errorf("%w: %v", ErrReplyInvalid, err)
	}
	reply.Text = strings.TrimSpace(reply.Text)
	if reply.Text == "" && len(reply.ToolCalls) == 0 {
		return ModelReply{}, fmt.Errorf("%w: text or a tool call is required", ErrReplyInvalid)
	}
	if len(reply.ToolCalls) > 3 {
		return ModelReply{}, fmt.Errorf("%w: at most 3 tool calls", ErrReplyInvalid)
	}
	for _, call := range reply.ToolCalls {
		if _, err := ParseToolCall(call.Name, call.Arguments); err != nil {
			return ModelReply{}, err
		}
	}
	return reply, nil
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

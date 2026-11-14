// Package application runs assistant tools against the ontology and governance
// ports. A model, when one is configured, may only ask for those tools.
package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/assistant/domain"
)

// ObjectHit is one catalog record a read returned.
type ObjectHit struct {
	ID     string `json:"id"`
	Type   string `json:"objectType"`
	Label  string `json:"label"`
	Status string `json:"status,omitempty"`
}

// ObjectPage is one object-set read.
type ObjectPage struct {
	Hits    []ObjectHit
	ReadOps int
	Total   int
}

// Actor is the operator already authenticated by the HTTP adapter.
type Actor struct {
	Username string
	Teams    []string
	Groups   []string
}

// Roles is the union the action rules consult.
func (actor Actor) Roles() []string {
	roles := make([]string, 0, len(actor.Teams)+len(actor.Groups))
	roles = append(roles, actor.Teams...)
	return append(roles, actor.Groups...)
}

// Objects reads the catalog the caller may see.
type Objects interface {
	Query(ctx context.Context, actor Actor, args domain.QueryArgs) (ObjectPage, error)
}

// Decision is the action-rule verdict and the audit event that recorded it.
type Decision struct {
	Known        bool
	Allowed      bool
	Reason       string
	AuditEvent   string
	AuditEventID string
}

// Governor evaluates an action rule and writes that verdict to the audit chain.
// It does not apply the action.
type Governor interface {
	Evaluate(ctx context.Context, actor, action, resource string, roles []string) (Decision, error)
}

// Proposal is a change request that still needs a person.
type Proposal struct {
	ActionID       string
	TargetEntityID string
	ObjectType     string
	Environment    string
	InstallationID string
	ProductID      string
	Author         string
	Reason         string
}

// OpenedChange is the stored request.
type OpenedChange struct {
	ID     string
	Status string
}

// Changes opens a change request and does not approve it.
type Changes interface {
	Open(ctx context.Context, proposal Proposal) (OpenedChange, error)
}

// Hunk is one stored before/after pair.
type Hunk struct {
	Path   string `json:"path"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// ChangeDiff is the diff stored on a change request.
type ChangeDiff struct {
	ID     string
	Title  string
	Status string
	Hunks  []Hunk
}

// Diffs reads a stored change request.
type Diffs interface {
	Read(ctx context.Context, id string) (ChangeDiff, error)
}

// AuditRecord is one assistant tool call.
type AuditRecord struct {
	Actor    string
	Tool     string
	Args     []byte
	Result   []byte
	Decision string
	Reason   string
	Resource string
}

// Auditor writes one audit entry and returns its id.
type Auditor interface {
	Record(ctx context.Context, record AuditRecord) (string, error)
}

// Result is one tool execution.
type Result struct {
	Label                string       `json:"label"`
	Tool                 string       `json:"tool"`
	OK                   bool         `json:"ok"`
	Error                string       `json:"error,omitempty"`
	ReadOps              int          `json:"readOps"`
	WriteOps             int          `json:"writeOps"`
	DurationMs           int64        `json:"durationMs"`
	MatchedCount         int          `json:"matchedCount,omitempty"`
	Entities             []ObjectHit  `json:"entities,omitempty"`
	Allowed              bool         `json:"allowed,omitempty"`
	Reason               string       `json:"reason,omitempty"`
	Constraints          []Constraint `json:"constraints,omitempty"`
	AuditEventID         string       `json:"auditEventId,omitempty"`
	DecisionAuditEventID string       `json:"decisionAuditEventId,omitempty"`
	ChangeRequestID      string       `json:"changeRequestId,omitempty"`
	ChangeRequestStatus  string       `json:"changeRequestStatus,omitempty"`
	Title                string       `json:"title,omitempty"`
	Hunks                []Hunk       `json:"hunks,omitempty"`
}

// Constraint is one action-rule row.
type Constraint struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// Turn is one model round after its tool calls have run.
type Turn struct {
	Label      string            `json:"label"`
	Text       string            `json:"text"`
	Provenance domain.Provenance `json:"provenance"`
	Results    []Result          `json:"results"`
}

// Service executes tools. A nil Model means turns are refused.
type Service struct {
	Objects  Objects
	Governor Governor
	Changes  Changes
	Diffs    Diffs
	Audit    Auditor
	Model    domain.Model
	Now      func() time.Time
}

func (service Service) Invoke(ctx context.Context, actor Actor, mode string, name string, arguments json.RawMessage) (Result, error) {
	started := service.now()
	call, err := domain.ParseToolCall(name, arguments)
	if err != nil {
		result := Result{Label: domain.LabelNoModel, Tool: name, Error: err.Error()}
		return service.finish(ctx, actor, call, result, started, err)
	}
	if !domain.ModeAllows(mode, call.Name) {
		result := Result{Label: domain.LabelNoModel, Tool: call.Name, Error: "this mode does not include " + call.Name}
		return service.finish(ctx, actor, call, result, started, nil)
	}
	result := Result{Label: domain.LabelNoModel, Tool: call.Name, OK: true}
	switch call.Name {
	case domain.ToolQuery:
		if service.Objects == nil {
			result.OK = false
			result.Error = "ontology catalog is not configured"
			break
		}
		page, err := service.Objects.Query(ctx, actor, call.Query)
		if err != nil {
			result.OK = false
			result.Error = err.Error()
			break
		}
		result.Entities = page.Hits
		result.MatchedCount = page.Total
		result.ReadOps = page.ReadOps
		result.WriteOps = 0
	case domain.ToolPropose:
		result = service.propose(ctx, actor, call.Propose, result)
	case domain.ToolDiff:
		if service.Diffs == nil {
			result.OK = false
			result.Error = "change requests are not configured"
			break
		}
		diff, err := service.Diffs.Read(ctx, call.Diff.ChangeRequestID)
		if err != nil {
			result.OK = false
			result.Error = err.Error()
			break
		}
		result.ChangeRequestID = diff.ID
		result.ChangeRequestStatus = diff.Status
		result.Title = diff.Title
		result.Hunks = diff.Hunks
		result.ReadOps = 1
		result.WriteOps = 0
	default:
		result.OK = false
		result.Error = "unknown tool"
	}
	return service.finish(ctx, actor, call, result, started, nil)
}

func (service Service) propose(ctx context.Context, actor Actor, args domain.ProposeArgs, result Result) Result {
	if service.Governor == nil {
		result.OK = false
		result.Error = "action rules are not configured"
		return result
	}
	decision, err := service.Governor.Evaluate(ctx, actor.Username, args.ActionID, domain.Resource(args.TargetObjectType, args.TargetEntityID), actor.Roles())
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	result.DecisionAuditEventID = decision.AuditEventID
	result.Reason = decision.Reason
	result.Allowed = decision.Allowed && decision.Known
	status := "DENY"
	if result.Allowed {
		status = "ALLOW"
	}
	result.Constraints = []Constraint{{ID: args.ActionID, Status: status, Reason: decision.Reason}}
	result.ReadOps = 1
	if !result.Allowed {
		result.WriteOps = 0
		return result
	}
	if service.Changes == nil {
		result.OK = false
		result.Allowed = false
		result.Error = "change requests are not configured"
		result.Reason = result.Error
		return result
	}
	opened, err := service.Changes.Open(ctx, Proposal{
		ActionID: args.ActionID, TargetEntityID: args.TargetEntityID, ObjectType: args.TargetObjectType,
		Environment: args.Environment, InstallationID: args.InstallationID, ProductID: args.ProductID,
		Author: actor.Username, Reason: decision.Reason,
	})
	if err != nil {
		result.OK = false
		result.Allowed = false
		result.Error = err.Error()
		result.Reason = err.Error()
		result.Constraints = []Constraint{{ID: args.ActionID, Status: "DENY", Reason: err.Error()}}
		return result
	}
	result.ChangeRequestID = opened.ID
	result.ChangeRequestStatus = opened.Status
	result.WriteOps = 1
	if opened.Status != "" && !strings.EqualFold(opened.Status, "PENDING_APPROVALS") {
		result.Allowed = false
		result.Reason = "change request is " + opened.Status
		result.Constraints = []Constraint{{ID: args.ActionID, Status: "DENY", Reason: result.Reason}}
	}
	return result
}

func (service Service) Turn(ctx context.Context, actor Actor, mode, message string, chips []domain.Chip) (Turn, error) {
	if service.Model == nil {
		return Turn{Label: domain.LabelNoModel}, domain.ErrModelUnavailable
	}
	prompt := domain.Prompt(mode, message, chips)
	raw, provenance, err := service.Model.Complete(ctx, prompt)
	if err != nil {
		return Turn{Label: domain.LabelLiveModel, Provenance: provenance}, err
	}
	reply, err := domain.ParseReply([]byte(raw))
	if err != nil {
		_, _ = service.audit(ctx, actor.Username, "model_reply", []byte(prompt), []byte(raw), "DENY", err.Error(), "assistant/turn")
		return Turn{Label: domain.LabelLiveModel, Provenance: provenance}, err
	}
	turn := Turn{Label: domain.LabelLiveModel, Text: reply.Text, Provenance: provenance}
	for _, call := range reply.ToolCalls {
		result, err := service.Invoke(ctx, actor, mode, call.Name, call.Arguments)
		if err != nil {
			return Turn{}, err
		}
		result.Label = domain.LabelLiveModel
		turn.Results = append(turn.Results, result)
	}
	return turn, nil
}

func (service Service) finish(ctx context.Context, actor Actor, call domain.ToolCall, result Result, started time.Time, failed error) (Result, error) {
	result.DurationMs = service.now().Sub(started).Milliseconds()
	if result.DurationMs < 0 {
		result.DurationMs = 0
	}
	resource := "assistant/" + result.Tool
	if call.Name == domain.ToolPropose {
		resource = domain.Resource(call.Propose.TargetObjectType, call.Propose.TargetEntityID)
	}
	if call.Name == domain.ToolDiff {
		resource = "change-request/" + call.Diff.ChangeRequestID
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return Result{}, err
	}
	decision := "ALLOW"
	reason := result.Reason
	if failed != nil || result.Error != "" || (result.Tool == domain.ToolPropose && !result.Allowed) {
		decision = "DENY"
	}
	if reason == "" {
		reason = result.Error
	}
	if reason == "" {
		reason = decision
	}
	id, err := service.audit(ctx, actor.Username, result.Tool, call.Arguments, encoded, decision, reason, resource)
	if err != nil {
		return Result{}, err
	}
	result.AuditEventID = id
	if failed != nil {
		return result, failed
	}
	return result, nil
}

func (service Service) audit(ctx context.Context, actor, tool string, args, result []byte, decision, reason, resource string) (string, error) {
	if service.Audit == nil {
		return "", errors.New("assistant audit is not configured")
	}
	return service.Audit.Record(ctx, AuditRecord{
		Actor: actor, Tool: tool, Args: args, Result: result, Decision: decision, Reason: reason, Resource: resource,
	})
}

func (service Service) now() time.Time {
	if service.Now != nil {
		return service.Now()
	}
	return time.Now()
}

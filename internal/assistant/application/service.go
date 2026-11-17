// Package application runs assistant tools against the ontology and governance
// ports. A model, when one is configured, may only ask for those tools.
package application

import (
	"context"
	"encoding/json"
	"fmt"
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

// Reviews records an approval or rejection. It refuses the author of the request.
type Reviews interface {
	Approve(ctx context.Context, id, actor, rationale string, roles []string) (string, error)
	Reject(ctx context.Context, id, actor, rationale string, roles []string) (string, error)
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
	Label                string            `json:"label"`
	Tool                 string            `json:"tool"`
	OK                   bool              `json:"ok"`
	Error                string            `json:"error,omitempty"`
	ReadOps              int               `json:"readOps"`
	WriteOps             int               `json:"writeOps"`
	DurationMs           int64             `json:"durationMs"`
	MatchedCount         int               `json:"matchedCount,omitempty"`
	Entities             []ObjectHit       `json:"entities,omitempty"`
	Allowed              bool              `json:"allowed,omitempty"`
	Reason               string            `json:"reason,omitempty"`
	Constraints          []Constraint      `json:"constraints,omitempty"`
	AuditEventID         string            `json:"auditEventId,omitempty"`
	DecisionAuditEventID string            `json:"decisionAuditEventId,omitempty"`
	ChangeRequestID      string            `json:"changeRequestId,omitempty"`
	ChangeRequestStatus  string            `json:"changeRequestStatus,omitempty"`
	Title                string            `json:"title,omitempty"`
	Hunks                []Hunk            `json:"hunks,omitempty"`
	Risk                 string            `json:"risk,omitempty"`
	NeedsConfirm         bool              `json:"needsConfirm,omitempty"`
	Applied              bool              `json:"applied,omitempty"`
	ObjectID             string            `json:"objectId,omitempty"`
	State                string            `json:"state,omitempty"`
	Elicitation          *domain.WriteArgs `json:"elicitation,omitempty"`
	Fields               []domain.Field    `json:"fields,omitempty"`
}

// Constraint is one action-rule row.
type Constraint struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// Turn is one operator message answered: the model's last text and every tool
// call that ran across the rounds. ToolCalls and Results have the same order.
type Turn struct {
	Label      string            `json:"label"`
	Text       string            `json:"text"`
	Provenance domain.Provenance `json:"provenance"`
	Results    []Result          `json:"results"`
	ToolCalls  []ToolCall        `json:"toolCalls,omitempty"`
	// Rounds is how many model calls the turn made.
	Rounds   int     `json:"rounds"`
	ThreadID string  `json:"threadId,omitempty"`
	Model    *Served `json:"model,omitempty"`
}

// ToolCall is one call as the server ran it.
type ToolCall struct {
	ID        string          `json:"id"`
	Round     int             `json:"round"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Served names what answered the turn. Kind is the adapter; Model is the id
// the provider reported, when the adapter reads one.
type Served struct {
	Kind  string `json:"kind"`
	Model string `json:"model,omitempty"`
}

// Service executes tools. A nil Model means turns are refused.
type Service struct {
	Objects  Objects
	Governor Governor
	Changes  Changes
	Diffs    Diffs
	Audit    Auditor
	Model    domain.Model
	Cases    EvalCases
	Authors  ChangeAuthor
	Reviews  Reviews
	// Threads stores conversations. Nil refuses a turn that names a thread.
	Threads Threads
	Now     func() time.Time
	// NewID returns 32 hex characters for a thread id. Nil uses crypto/rand.
	NewID func() (string, error)
}

// EvalCases stores one operator feedback case.
type EvalCases interface {
	Add(scenario, bound, value, expected string) (string, error)
}

// ChangeAuthor is the person who opened a change request.
type ChangeAuthor interface {
	Author(ctx context.Context, id string) (string, error)
}

// Invoke runs one tool call a person submitted.
func (service Service) Invoke(ctx context.Context, actor Actor, mode string, name string, arguments json.RawMessage) (Result, error) {
	return service.invoke(ctx, actor, mode, name, arguments, false)
}

// invoke runs one tool call. held marks a call a model made after it read
// earlier turns or tool results: such a call can open a change request, and it
// can never apply anything. See write.
func (service Service) invoke(ctx context.Context, actor Actor, mode string, name string, arguments json.RawMessage, held bool) (Result, error) {
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
	case domain.ToolWrite:
		result = service.write(ctx, actor, call.Write, result, held)
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

// Refusal reasons a write can carry before any rule or service is consulted.
const (
	ReasonNotExecutable = "action is not executable in this deployment: no service is wired to run it"
	ReasonNoActionRule  = "no action rule governs this action, so it is refused"
	ReasonNoGovernor    = "action rules are not configured"
	// ReasonIntentOnly is what an opened change request means today. Nothing
	// reads the action id back out of an approved request.
	ReasonIntentOnly = "opened for a separate approver; the change request records the proposal and its approval, and approving it does not run the action"
)

func (service Service) write(ctx context.Context, actor Actor, args domain.WriteArgs, result Result, held bool) Result {
	args.Action = strings.TrimSpace(args.Action)
	result.Risk = domain.Risk(args.Action)
	refuse := func(reason string) Result {
		result.Allowed = false
		result.Reason = reason
		result.Constraints = []Constraint{{ID: args.Action, Status: "DENY", Reason: reason}}
		return result
	}
	// Refuse before asking the person for fields or a confirmation that could
	// not lead anywhere.
	effect := effectOf(args.Action)
	if effect == effectNone {
		return refuse(ReasonNotExecutable)
	}
	rule := ruleID(args.Action)
	if rule == "" && !ruleExempt(args.Action) {
		return refuse(ReasonNoActionRule)
	}
	if rule != "" && service.Governor == nil {
		return refuse(ReasonNoGovernor)
	}
	if fields := domain.MissingFields(args); len(fields) > 0 {
		result.Fields = fields
		result.Elicitation = &args
		result.Reason = "more detail is required"
		result.WriteOps = 0
		return result
	}
	if args.Action == "approve_change" && service.Authors != nil {
		author, err := service.Authors.Author(ctx, args.TargetID)
		if err != nil {
			result.OK = false
			result.Allowed = false
			result.Reason = err.Error()
			return result
		}
		if strings.EqualFold(strings.TrimSpace(author), strings.TrimSpace(actor.Username)) && author != "" {
			return refuse("a person cannot approve their own change request")
		}
	}
	if rule != "" {
		decision, err := service.Governor.Evaluate(ctx, actor.Username, rule, domain.Resource("PackagingRun", firstNonEmpty(args.TargetID, args.Value)), actor.Roles())
		if err != nil {
			result.OK = false
			result.Reason = err.Error()
			return result
		}
		result.DecisionAuditEventID = decision.AuditEventID
		if !decision.Allowed || !decision.Known {
			return refuse(decision.Reason)
		}
	}
	// Confirm is the person's. A held call is the model's, made after it read
	// text the operator did not type, so its confirm flag counts for nothing
	// and a review waits for the person like any other direct effect. Text a
	// tool returned cannot confirm or approve on the operator's behalf.
	confirmed := args.Confirm && !held
	if (result.Risk == "low" || (held && effect == effectReview)) && !confirmed {
		result.NeedsConfirm = true
		result.Elicitation = &args
		result.Reason = "confirm to apply this action"
		return result
	}
	switch effect {
	case effectReview:
		if service.Reviews == nil {
			result.OK = false
			result.Reason = "reviews are not configured"
			return result
		}
		var status string
		var err error
		if args.Action == "approve_change" {
			status, err = service.Reviews.Approve(ctx, args.TargetID, actor.Username, args.Justification, actor.Roles())
		} else {
			status, err = service.Reviews.Reject(ctx, args.TargetID, actor.Username, args.Justification, actor.Roles())
		}
		if err != nil {
			result.OK = false
			return refuse(err.Error())
		}
		result.Applied = true
		result.Allowed = true
		result.ObjectID = args.TargetID
		result.State = status
		result.Reason = "review recorded"
		result.WriteOps = 1
		return result
	case effectChangeRequest:
		if service.Changes == nil {
			result.OK = false
			result.Reason = "change requests are not configured"
			return result
		}
		opened, err := service.Changes.Open(ctx, Proposal{
			ActionID: args.Action, TargetEntityID: firstNonEmpty(args.TargetID, args.Value),
			ObjectType: "ChangeRequest", Environment: strings.TrimSpace(args.Environment),
			InstallationID: strings.TrimSpace(args.InstallationID), ProductID: strings.TrimSpace(args.ProductID),
			Author: actor.Username, Reason: args.Justification,
		})
		if err != nil {
			result.OK = false
			result.Reason = err.Error()
			return result
		}
		result.Allowed = true
		result.ChangeRequestID = opened.ID
		result.ChangeRequestStatus = opened.Status
		result.ObjectID = opened.ID
		result.State = opened.Status
		result.Reason = ReasonIntentOnly
		result.WriteOps = 1
		return result
	case effectEvalCase:
		if service.Cases == nil {
			result.OK = false
			result.Reason = "eval cases are not configured"
			return result
		}
		id, err := service.Cases.Add(args.Justification, args.TargetID, args.Value, args.Expected)
		if err != nil {
			result.OK = false
			result.Reason = err.Error()
			return result
		}
		result.Applied = true
		result.Allowed = true
		result.ObjectID = id
		result.State = "recorded"
		result.Reason = "eval case recorded"
		result.WriteOps = 1
		return result
	}
	return refuse(ReasonNotExecutable)
}

// effect is the one service call an action makes. effectNone has no service
// behind it and is refused: a result never reports an effect that did not happen.
type effect int

const (
	effectNone effect = iota
	effectReview
	effectChangeRequest
	effectEvalCase
)

// effectOf names what runs for an action. start_packaging_run and
// rerun_packaging_run are low risk, so they would apply directly, and this
// service has no port that starts a packaging run.
func effectOf(action string) effect {
	switch action {
	case "approve_change", "reject_change":
		return effectReview
	case "add_eval_case":
		return effectEvalCase
	case "start_packaging_run", "rerun_packaging_run":
		return effectNone
	}
	if domain.Risk(action) == "high" {
		return effectChangeRequest
	}
	return effectNone
}

// ruleID maps a write to its ontology action rule. recall, propose_memory, and
// select_course have no rule in the catalog and are refused.
func ruleID(action string) string {
	switch action {
	case "approve_change":
		return "approve_plan"
	case "reject_change":
		return "reject_plan"
	case "promote":
		return "promote_release_manual"
	case "start_packaging_run", "rerun_packaging_run":
		return "rerun_packaging_run"
	case "cancel_packaging_run":
		return "cancel_packaging_run"
	default:
		return ""
	}
}

// ruleExempt is the one write with no action rule that still runs: an eval case
// is the operator's own feedback record, kept after an explicit Confirm.
func ruleExempt(action string) bool {
	return action == "add_eval_case"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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
	if failed != nil || result.Error != "" || ((result.Tool == domain.ToolPropose || result.Tool == domain.ToolWrite) && !result.Allowed && len(result.Fields) == 0 && !result.NeedsConfirm) {
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
		return "", fmt.Errorf("%w: assistant audit is not configured", domain.ErrAuditUnavailable)
	}
	id, err := service.Audit.Record(ctx, AuditRecord{
		Actor: actor, Tool: tool, Args: args, Result: result, Decision: decision, Reason: reason, Resource: resource,
	})
	if err != nil {
		return "", fmt.Errorf("%w: %w", domain.ErrAuditUnavailable, err)
	}
	return id, nil
}

func (service Service) now() time.Time {
	if service.Now != nil {
		return service.Now()
	}
	return time.Now()
}

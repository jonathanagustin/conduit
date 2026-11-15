// Package domain holds the planning-session contract: intake, Needs Input,
// the model-written plan, and the Accept binding. Accept is the control
// point. No package or image compute happens before it.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode"
)

// ProfileOnboard is the work profile this session runs. It is the packaging
// skill, the packaging tools, and the gates, versioned and hashed together.
const ProfileOnboard = "onboard"

// DefaultModelLabel is the neutral label shown when the model does not report its name.
const DefaultModelLabel = "Configured model"

// Session statuses. A session is created at started. Turns move it through
// inspecting, needs_input, and planning. Ready means a plan is waiting for
// Accept. Accepted means the binding is recorded.
const (
	StatusStarted    = "started"
	StatusInspecting = "inspecting"
	StatusNeedsInput = "needs_input"
	StatusPlanning   = "planning"
	StatusReady      = "ready"
	StatusAccepted   = "accepted"
)

// Question types match the existing elicitation field types so the console
// can reuse those forms. A question never asks for a secret.
const (
	QuestionString  = "string"
	QuestionBoolean = "boolean"
	QuestionSelect  = "select"
)

// Message roles on the planning transcript.
const (
	RoleOperator = "operator"
	RoleModel    = "model"
)

// Event types on the planning stream and the recorded event log.
const (
	EventMessage    = "message"
	EventInspection = "inspection"
	EventNeedsInput = "needs_input"
	EventPlan       = "plan"
	EventStatus     = "status"
	EventError      = "error"
	EventDone       = "done"
)

// Stable error codes for the JSON envelope. The console branches on code.
const (
	CodeInvalidIntake        = "INVALID_INTAKE"
	CodeSessionNotFound      = "SESSION_NOT_FOUND"
	CodeModelNotConfigured   = "MODEL_NOT_CONFIGURED"
	CodePlanRequired         = "PLAN_REQUIRED"
	CodeCommitNotPinned      = "COMMIT_NOT_PINNED"
	CodeNeedsInputPending    = "NEEDS_INPUT_PENDING"
	CodeAlreadyAccepted      = "ALREADY_ACCEPTED"
	CodeTurnInProgress       = "TURN_IN_PROGRESS"
	CodeNotAccepted          = "NOT_ACCEPTED"
	CodeComputeBeforeAccept  = "COMPUTE_BEFORE_ACCEPT"
	CodeComputeNotConfigured = "COMPUTE_NOT_CONFIGURED"
	CodeNoPendingQuestions   = "NO_PENDING_QUESTIONS"
	CodeUnknownQuestion      = "UNKNOWN_QUESTION"
	CodeInvalidAnswer        = "INVALID_ANSWER"
)

// Sentinel errors. The HTTP adapter maps these to status and code.
var (
	ErrNotFound             = errors.New("planning session was not found")
	ErrModelNotConfigured   = errors.New("planning model is not configured")
	ErrPlanRequired         = errors.New("a plan is required before Accept")
	ErrCommitNotPinned      = errors.New("the source commit SHA is not pinned")
	ErrNeedsInputPending    = errors.New("Needs Input questions are still unanswered")
	ErrAlreadyAccepted      = errors.New("the planning session is already accepted")
	ErrTurnInProgress       = errors.New("a planning turn is already in progress")
	ErrComputeBeforeAccept  = errors.New("compute is refused before Accept")
	ErrComputeNotConfigured = errors.New("package compute is not configured")
	ErrNoPendingQuestions   = errors.New("there are no pending Needs Input questions")
	ErrUnknownQuestion      = errors.New("the answer does not match a pending question")
	ErrInvalidAnswer        = errors.New("the answer is not valid for the question")
)

// Intake is the operator-supplied repository and ref. It is recorded on
// create and copied into the Accept binding.
type Intake struct {
	RepoURL string `json:"repoUrl"`
	Ref     string `json:"ref"`
	// Message is the operator's opening message. It is empty on create and
	// filled in the Accept binding from the first operator message.
	Message string `json:"message,omitempty"`
}

// ProfileRef names the onboard profile and, once snapshotted, its version
// and hash. Empty version and hash mean the snapshot has not been taken yet.
type ProfileRef struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Hash    string `json:"hash"`
}

// Inspection is a live view of the source repository. Later turns fill this
// from the packaging inspect_repo and read_file tools.
type Inspection struct {
	Language       string   `json:"language"`
	PackageManager string   `json:"packageManager"`
	Kind           string   `json:"kind"`
	StartCommand   string   `json:"startCommand"`
	Ports          []int    `json:"ports"`
	HasDockerfile  bool     `json:"hasDockerfile"`
	Tree           []string `json:"tree"`
	ToolCalls      []string `json:"toolCalls"`
}

// Question is one Needs Input prompt. The console renders it with the
// existing elicitation forms. It never asks for a secret.
type Question struct {
	ValidationError string   `json:"validationError,omitempty"`
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Label           string   `json:"label"`
	Type            string   `json:"type"`
	Required        bool     `json:"required"`
	Options         []string `json:"options,omitempty"`
	Value           string   `json:"value,omitempty"`
}

// Answer is the operator's response to one Needs Input question.
type Answer struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

// Plan is the model-written plan the operator accepts.
type Plan struct {
	Text   string `json:"text"`
	SHA256 string `json:"sha256"`
}

// Message is one line on the planning transcript.
type Message struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// Event is one item on the turn stream and the session event log.
type Event struct {
	ID         string      `json:"id"`
	Type       string      `json:"type"`
	Timestamp  time.Time   `json:"timestamp"`
	Message    *Message    `json:"message,omitempty"`
	Inspection *Inspection `json:"inspection,omitempty"`
	NeedsInput []Question  `json:"needsInput,omitempty"`
	Plan       *Plan       `json:"plan,omitempty"`
	Status     string      `json:"status,omitempty"`
	Error      string      `json:"error,omitempty"`
	Code       string      `json:"code,omitempty"`
}

// Binding is recorded at Accept and never rewritten. It is the profile
// version and hash, the intake, the source commit, the model, and the plan.
type Binding struct {
	Profile        ProfileRef `json:"profile"`
	Intake         Intake     `json:"intake"`
	ResolvedCommit string     `json:"resolvedCommit"`
	Model          string     `json:"model"`
	Plan           Plan       `json:"plan"`
	AcceptedAt     time.Time  `json:"acceptedAt"`
}

// Session is the planning-session document the HTTP contract returns.
type Session struct {
	ID             string      `json:"id"`
	Status         string      `json:"status"`
	Profile        ProfileRef  `json:"profile"`
	Intake         Intake      `json:"intake"`
	ResolvedCommit string      `json:"resolvedCommit"`
	Model          string      `json:"model"`
	Inspection     *Inspection `json:"inspection"`
	NeedsInput     []Question  `json:"needsInput"`
	Answers        []Answer    `json:"answers"`
	Plan           *Plan       `json:"plan"`
	Binding        *Binding    `json:"binding"`
	Messages       []Message   `json:"messages"`
	Events         []Event     `json:"events"`
	CreatedAt      time.Time   `json:"createdAt"`
	UpdatedAt      time.Time   `json:"updatedAt"`
}

// IntakeError is a rejected repository URL or ref.
type IntakeError struct {
	Reason string
}

func (err IntakeError) Error() string {
	return err.Reason
}

func (err IntakeError) Code() string {
	return CodeInvalidIntake
}

// ParseIntake checks a repository URL and ref. It accepts only http and
// https URLs with a host and a path, and rejects credentials in the URL.
func ParseIntake(repoURL, ref string) (Intake, error) {
	repoURL = strings.TrimSpace(repoURL)
	ref = strings.TrimSpace(ref)
	if repoURL == "" {
		return Intake{}, IntakeError{Reason: "repoUrl is required"}
	}
	if len(repoURL) > 2048 {
		return Intake{}, IntakeError{Reason: "repoUrl is too long"}
	}
	parsed, err := url.Parse(repoURL)
	if err != nil {
		return Intake{}, IntakeError{Reason: "repoUrl is not a valid URL"}
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return Intake{}, IntakeError{Reason: "repoUrl must be an http or https URL"}
	}
	if parsed.Host == "" {
		return Intake{}, IntakeError{Reason: "repoUrl must include a host"}
	}
	if parsed.User != nil {
		return Intake{}, IntakeError{Reason: "repoUrl must not include credentials"}
	}
	if strings.Trim(parsed.Path, "/") == "" {
		return Intake{}, IntakeError{Reason: "repoUrl must include a repository path"}
	}
	if ref == "" {
		return Intake{}, IntakeError{Reason: "ref is required"}
	}
	if len(ref) > 255 {
		return Intake{}, IntakeError{Reason: "ref is too long"}
	}
	if strings.ContainsAny(ref, " \t\r\n") || strings.Contains(ref, "..") {
		return Intake{}, IntakeError{Reason: "ref is not a valid git revision"}
	}
	for _, r := range ref {
		if r == 0 || unicode.IsControl(r) {
			return Intake{}, IntakeError{Reason: "ref is not a valid git revision"}
		}
	}
	return Intake{RepoURL: repoURL, Ref: ref}, nil
}

// CloneSession copies a session so callers can read it without sharing
// the store's mutable slices and pointers.
func CloneSession(session *Session) *Session {
	if session == nil {
		return nil
	}
	out := *session
	out.NeedsInput = append([]Question(nil), session.NeedsInput...)
	out.Answers = append([]Answer(nil), session.Answers...)
	out.Messages = append([]Message(nil), session.Messages...)
	out.Events = CloneEvents(session.Events)
	if session.Inspection != nil {
		inspected := *session.Inspection
		inspected.Ports = append([]int(nil), session.Inspection.Ports...)
		inspected.Tree = append([]string(nil), session.Inspection.Tree...)
		inspected.ToolCalls = append([]string(nil), session.Inspection.ToolCalls...)
		out.Inspection = &inspected
	}
	if session.Plan != nil {
		plan := *session.Plan
		out.Plan = &plan
	}
	if session.Binding != nil {
		binding := *session.Binding
		out.Binding = &binding
	}
	if out.NeedsInput == nil {
		out.NeedsInput = []Question{}
	}
	if out.Answers == nil {
		out.Answers = []Answer{}
	}
	if out.Messages == nil {
		out.Messages = []Message{}
	}
	if out.Events == nil {
		out.Events = []Event{}
	}
	return &out
}

// PlanHash returns the sha256 of the canonical plan JSON. The canonical form
// is the plan text alone under a fixed field name, so a caller-supplied hash
// never decides the bound value.
func PlanHash(plan Plan) string {
	canonical, _ := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: plan.Text})
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// IsPinnedCommit reports whether sha is a full lowercase hex git object id
// (40 characters for sha1, 64 for sha256).
func IsPinnedCommit(sha string) bool {
	if len(sha) != 40 && len(sha) != 64 {
		return false
	}
	for _, r := range sha {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// RequireAccepted is the no-compute guard. Every build, packaging, or
// workspace start path for a session calls it first.
func RequireAccepted(session *Session) error {
	if session == nil || session.Status != StatusAccepted || session.Binding == nil {
		return ErrComputeBeforeAccept
	}
	return nil
}

// PendingRequired reports whether any required Needs Input question is
// still unanswered, or the planner has rejected a recorded answer.
func PendingRequired(questions []Question, answers []Answer) bool {
	answered := map[string]string{}
	for _, answer := range answers {
		answered[answer.ID] = answer.Value
	}
	for _, question := range questions {
		if strings.TrimSpace(question.ValidationError) != "" {
			return true
		}
		if !question.Required {
			continue
		}
		if strings.TrimSpace(answered[question.ID]) == "" {
			return true
		}
	}
	return false
}

// CloneEvents copies events, including pointer fields, so callers do not
// share the store's mutable values.
func CloneEvents(events []Event) []Event {
	if events == nil {
		return []Event{}
	}
	out := make([]Event, len(events))
	for i, event := range events {
		item := event
		if event.Message != nil {
			message := *event.Message
			item.Message = &message
		}
		if event.Inspection != nil {
			inspected := *event.Inspection
			inspected.Ports = append([]int(nil), event.Inspection.Ports...)
			inspected.Tree = append([]string(nil), event.Inspection.Tree...)
			inspected.ToolCalls = append([]string(nil), event.Inspection.ToolCalls...)
			item.Inspection = &inspected
		}
		item.NeedsInput = append([]Question(nil), event.NeedsInput...)
		if event.Plan != nil {
			plan := *event.Plan
			item.Plan = &plan
		}
		out[i] = item
	}
	return out
}

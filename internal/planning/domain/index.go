package domain

import (
	"errors"
	"time"
)

// MaxIndexedExecutions bounds one authoritative read, before HTTP pagination.
// An over-limit inventory is an error, never a successful shortened catalog.
const MaxIndexedExecutions = 1000

var ErrIndexTooLarge = errors.New("planning session inventory exceeds 1000 executions")

// CONDUIT-INVENTED: planning.SessionInventory
// Displaced-by: An authoritative planning-session discovery contract defining owner isolation, summary fields, pagination and retained execution visibility.
//
// SessionSummary omits prompts, messages, plans, answers and event payloads.
// WorkflowRunID identifies the exact execution observed by the durable reader.
type SessionSummary struct {
	ID            string     `json:"id"`
	Status        string     `json:"status"`
	Profile       ProfileRef `json:"profile"`
	RepoURL       string     `json:"repoUrl,omitempty"`
	Ref           string     `json:"ref,omitempty"`
	AppName       string     `json:"appName,omitempty"`
	WorkflowRunID string     `json:"workflowRunId,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

func SummarizeSession(session *Session) SessionSummary {
	return SessionSummary{ID: session.ID, Status: session.Status, Profile: session.Profile,
		RepoURL: session.Intake.RepoURL, Ref: session.Intake.Ref, AppName: session.Intake.AppName,
		CreatedAt: session.CreatedAt, UpdatedAt: session.UpdatedAt}
}

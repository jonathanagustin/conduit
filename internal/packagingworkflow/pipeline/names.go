package pipeline

import "time"

const (
	// WorkflowName is the stable type name recorded in history. Renaming the
	// Go function must not rename this.
	WorkflowName = "packaging.Run"
	TaskQueue    = "packaging-localhost"
	SignalReview = "packaging.Review"
	UpdateReview = "packaging.SubmitReview"
	QueryStatus  = "packaging.Status"
	QueryEvents  = "packaging.Events"

	ActAssess       = "packaging.Assess"
	ActPropose      = "packaging.Propose"
	ActValidate     = "packaging.Validate"
	ActReadApproval = "packaging.ReadApproval"
	ActHandoff      = "packaging.Handoff"

	changeID             = "packaging-pipeline-v1"
	defaultApprovalWait  = 15 * time.Minute
	defaultHistoryEvents = 256
	maxContinueAsNew     = 4

	// MaxConcurrentActivities is the worker cap for model, lint and handoff
	// work. It is a localhost bound, not a cluster autoscaler.
	MaxConcurrentActivities    = 2
	MaxConcurrentWorkflowTasks = 2
)

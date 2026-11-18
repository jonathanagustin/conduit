package workflow

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
	appgenskill "github.com/jonathanagustin/conduit/skills/appgen"
	packagingskill "github.com/jonathanagustin/conduit/skills/packaging"
)

// Stable Temporal names for the planning session workflow.
const (
	TaskQueue      = "planning-sessions"
	WorkflowName   = "planning.Session"
	QuerySession   = "planning.GetSession"
	QueryOwner     = "planning.GetOwner"
	QueryBinding   = "planning.GetBinding"
	QueryEvents    = "planning.GetEvents"
	UpdateTurn     = "planning.Turn"
	UpdateAnswer   = "planning.Answer"
	UpdateAccept   = "planning.Accept"
	ActTurn        = "planning.TurnActivity"
	ActAuditAccept = "planning.AuditAcceptActivity"

	changeID                 = "planning-session"
	defaultHistoryEvents     = 256
	maxContinueAsNew         = 4
	defaultTurnStartToClose  = 2 * time.Minute
	defaultAuditStartToClose = 30 * time.Second
	defaultHeartbeatTimeout  = 15 * time.Second
)

// Application error type constants used across workflow/activity boundaries so
// callers can map Temporal errors back to domain sentinels deterministically.
const (
	ErrTypeBadInput            = "BadInput"
	ErrTypeModelNotConfigured  = "ModelNotConfigured"
	ErrTypeNeedsInputPending   = "NeedsInputPending"
	ErrTypeNoPendingQuestions  = "NoPendingQuestions"
	ErrTypeUnknownQuestion     = "UnknownQuestion"
	ErrTypeInvalidAnswer       = "InvalidAnswer"
	ErrTypePlanRequired        = "PlanRequired"
	ErrTypeComputeBeforeAccept = "ComputeBeforeAccept"
	ErrTypeCommitNotPinned     = "CommitNotPinned"
	ErrTypeAlreadyAccepted     = "AlreadyAccepted"
	ErrTypeAuditFailed         = "AuditFailed"
	ErrTypeForbidden           = "Forbidden"
)

// Input starts or continues a durable planning session workflow.
type Input struct {
	SessionID                string            `json:"sessionId"`
	Actor                    string            `json:"actor"`
	Profile                  string            `json:"profile,omitempty"`
	RepoURL                  string            `json:"repoUrl"`
	Ref                      string            `json:"ref"`
	ProblemStatement         string            `json:"problemStatement,omitempty"`
	AppName                  string            `json:"appName,omitempty"`
	ContinueAsNewAfterEvents int               `json:"continueAsNewAfterEvents,omitempty"`
	CANCount                 int               `json:"canCount,omitempty"`
	Carry                    *domain.Session   `json:"carry,omitempty"`
	TurnRuns                 map[string]string `json:"turnRuns,omitempty"`
}

// TurnRequest is the payload for UpdateTurn.
type TurnRequest struct {
	Actor   string `json:"actor,omitempty"`
	Message string `json:"message"`
	RunID   string `json:"runId,omitempty"`
}

// AnswerRequest is the payload for UpdateAnswer.
type AnswerRequest struct {
	Actor   string          `json:"actor,omitempty"`
	Answers []domain.Answer `json:"answers"`
	RunID   string          `json:"runId,omitempty"`
}

// AcceptRequest is the payload for UpdateAccept.
type AcceptRequest struct {
	Actor string `json:"actor,omitempty"`
}

// TurnActivityInput is passed to ActTurn.
type TurnActivityInput struct {
	Actor      string          `json:"actor"`
	WorkflowID string          `json:"workflowId"`
	RunID      string          `json:"runId"`
	Session    *domain.Session `json:"session"`
	Message    string          `json:"message"`
}

// AuditAcceptActivityInput is passed to ActAuditAccept.
type AuditAcceptActivityInput struct {
	Actor   string            `json:"actor"`
	Record  application.Audit `json:"record"`
	RunID   string            `json:"runId"`
	Session string            `json:"session"`
}

// WorkflowID returns the canonical Temporal WorkflowID for a planning session.
func WorkflowID(sessionID string) string {
	return "planning-session-" + strings.TrimSpace(sessionID)
}

// SessionWorkflow runs a multi-turn planning session using synchronous Temporal
// Updates with validators, a workflow.Mutex to serialize concurrent turns, and
// Activities for model execution and governance audit persistence.
func SessionWorkflow(ctx workflow.Context, in Input) (*domain.Session, error) {
	version := workflow.GetVersion(ctx, changeID, workflow.DefaultVersion, 1)
	if version < 1 {
		return nil, temporal.NewNonRetryableApplicationError(
			"unsupported planning session workflow version",
			"UnsupportedWorkflowVersion", nil,
		)
	}
	logger := workflow.GetLogger(ctx)
	metrics := workflow.GetMetricsHandler(ctx)

	session, err := initSession(ctx, in)
	if err != nil {
		return nil, err
	}
	mutex := workflow.NewMutex(ctx)
	var canRequested bool
	runs, err := newTurnRuns(ctx, in.TurnRuns, in.Actor, func() bool { return session.Status == domain.StatusAccepted })
	if err != nil {
		return nil, err
	}

	if err := workflow.SetQueryHandler(ctx, QuerySession, func() (*domain.Session, error) {
		return domain.CloneSession(session), nil
	}); err != nil {
		return nil, err
	}
	if err := workflow.SetQueryHandler(ctx, QueryOwner, func() (string, error) {
		return in.Actor, nil
	}); err != nil {
		return nil, err
	}
	if err := workflow.SetQueryHandler(ctx, QueryBinding, func() (*domain.Binding, error) {
		if session.Status != domain.StatusAccepted || session.Binding == nil {
			return nil, temporal.NewNonRetryableApplicationError(domain.ErrComputeBeforeAccept.Error(), ErrTypePlanRequired, nil)
		}
		cloned := domain.CloneSession(session)
		return cloned.Binding, nil
	}); err != nil {
		return nil, err
	}
	if err := workflow.SetQueryHandler(ctx, QueryEvents, func() ([]domain.Event, error) {
		return domain.CloneEvents(session.Events), nil
	}); err != nil {
		return nil, err
	}

	if err := workflow.SetUpdateHandlerWithOptions(
		ctx,
		UpdateTurn,
		func(ctx workflow.Context, req TurnRequest) (*domain.Session, error) {
			return runs.turn(ctx, req.RunID, func(activityCtx workflow.Context, permitted func() bool) (*domain.Session, error) {
				if err := mutex.Lock(ctx); err != nil {
					return nil, err
				}
				defer mutex.Unlock()
				if session.Status == domain.StatusAccepted {
					return nil, temporal.NewNonRetryableApplicationError(domain.ErrAlreadyAccepted.Error(), ErrTypeAlreadyAccepted, nil)
				}
				if !permitted() {
					return nil, turnRunError(ErrTypeRunCancelled, "planning turn was cancelled")
				}
				updated, turnErr := handleTurn(ctx, activityCtx, in, session, req, permitted)
				if turnErr != nil {
					metrics.WithTags(map[string]string{"outcome": "fail"}).Counter(MetricPlanningTurnsTotal).Inc(1)
					logger.Warn("planning workflow turn failed", "sessionId", session.ID, "error", turnErr)
					return nil, turnErr
				}
				metrics.WithTags(map[string]string{"outcome": "pass"}).Counter(MetricPlanningTurnsTotal).Inc(1)
				if shouldContinueAsNew(ctx, in) {
					canRequested = true
				}
				return domain.CloneSession(updated), nil
			})
		},
		workflow.UpdateHandlerOptions{
			Validator: func(ctx workflow.Context, req TurnRequest) error {
				if err := validateTurnRunID(req.RunID, true); err != nil {
					return err
				}
				if err := checkActor(in.Actor, req.Actor); err != nil {
					return err
				}
				if session.Status == domain.StatusAccepted {
					return temporal.NewNonRetryableApplicationError(domain.ErrAlreadyAccepted.Error(), ErrTypeAlreadyAccepted, nil)
				}
				return nil
			},
		},
	); err != nil {
		return nil, err
	}

	if err := workflow.SetUpdateHandlerWithOptions(
		ctx,
		UpdateAnswer,
		func(ctx workflow.Context, req AnswerRequest) (*domain.Session, error) {
			if err := mutex.Lock(ctx); err != nil {
				return nil, err
			}
			defer mutex.Unlock()
			if req.RunID != "" {
				if state := runs.states[req.RunID]; state != "" {
					if state == "cancelled" || state == "cancellation_requested" {
						return nil, turnRunError(ErrTypeRunCancelled, domain.ErrRunCancelled.Error())
					}
					return nil, turnRunError(ErrTypeRunConflict, domain.ErrRunConflict.Error())
				}
			}
			updated, ansErr := handleAnswer(ctx, session, req)
			if ansErr != nil {
				return nil, ansErr
			}
			if shouldContinueAsNew(ctx, in) {
				canRequested = true
			}
			return domain.CloneSession(updated), nil
		},
		workflow.UpdateHandlerOptions{
			Validator: func(ctx workflow.Context, req AnswerRequest) error {
				if err := validateTurnRunID(req.RunID, true); err != nil {
					return err
				}
				if err := checkActor(in.Actor, req.Actor); err != nil {
					return err
				}
				if session.Status == domain.StatusAccepted {
					return temporal.NewNonRetryableApplicationError(domain.ErrAlreadyAccepted.Error(), ErrTypeAlreadyAccepted, nil)
				}
				return validateAnswers(session.NeedsInput, req.Answers)
			},
		},
	); err != nil {
		return nil, err
	}

	if err := workflow.SetUpdateHandlerWithOptions(
		ctx,
		UpdateAccept,
		func(ctx workflow.Context, req AcceptRequest) (*domain.Session, error) {
			if err := mutex.Lock(ctx); err != nil {
				return nil, err
			}
			defer mutex.Unlock()
			if session.Status == domain.StatusAccepted || session.Binding != nil {
				return nil, temporal.NewNonRetryableApplicationError(domain.ErrAlreadyAccepted.Error(), ErrTypeAlreadyAccepted, nil)
			}
			updated, accErr := handleAccept(ctx, in, session)
			if accErr != nil {
				metrics.WithTags(map[string]string{"outcome": "fail"}).Counter(MetricPlanningAcceptsTotal).Inc(1)
				logger.Warn("planning workflow accept failed", "sessionId", session.ID, "error", accErr)
				return nil, accErr
			}
			metrics.WithTags(map[string]string{"outcome": "pass"}).Counter(MetricPlanningAcceptsTotal).Inc(1)
			logger.Info("planning workflow session accepted",
				"sessionId", session.ID,
				"resolvedCommit", session.ResolvedCommit,
				"model", session.Binding.Model,
				"planHash", session.Binding.Plan.SHA256,
			)
			return domain.CloneSession(updated), nil
		},
		workflow.UpdateHandlerOptions{
			Validator: func(ctx workflow.Context, req AcceptRequest) error {
				if err := checkActor(in.Actor, req.Actor); err != nil {
					return err
				}
				if session.Status == domain.StatusAccepted || session.Binding != nil {
					return temporal.NewNonRetryableApplicationError(domain.ErrAlreadyAccepted.Error(), ErrTypeAlreadyAccepted, nil)
				}
				if session.Plan == nil || strings.TrimSpace(session.Plan.Text) == "" {
					return temporal.NewNonRetryableApplicationError(domain.ErrPlanRequired.Error(), ErrTypePlanRequired, nil)
				}
				if domain.PendingRequired(session.NeedsInput, session.Answers) {
					return temporal.NewNonRetryableApplicationError(domain.ErrNeedsInputPending.Error(), ErrTypeNeedsInputPending, nil)
				}
				if !domain.IsBoundCommit(session.Profile.Name, session.ResolvedCommit) {
					return temporal.NewNonRetryableApplicationError(domain.ErrCommitNotPinned.Error(), ErrTypeCommitNotPinned, nil)
				}
				return nil
			},
		},
	); err != nil {
		return nil, err
	}

	logger.Info("planning workflow session active", "sessionId", session.ID, "repoUrl", session.Intake.RepoURL, "ref", session.Intake.Ref)

	if err := workflow.Await(ctx, func() bool {
		return session.Status == domain.StatusAccepted || canRequested
	}); err != nil {
		return nil, err
	}

	// Idiomatic Temporal drain: always wait for all in-flight update handlers
	// to finish before completing the workflow or continuing as new.
	if err := workflow.Await(ctx, func() bool {
		return workflow.AllHandlersFinished(ctx)
	}); err != nil {
		return nil, err
	}

	if canRequested && session.Status != domain.StatusAccepted {
		next := in
		next.CANCount++
		next.Carry = domain.CloneSession(session)
		next.TurnRuns = runs.snapshot()
		return nil, workflow.NewContinueAsNewError(ctx, SessionWorkflow, next)
	}

	return domain.CloneSession(session), nil
}

func initSession(ctx workflow.Context, in Input) (*domain.Session, error) {
	if in.Carry != nil {
		return domain.CloneSession(in.Carry), nil
	}
	profile, intake, questions, err := domain.ParseCreateInput(domain.CreateInput{
		Profile:          in.Profile,
		RepoURL:          in.RepoURL,
		Ref:              in.Ref,
		ProblemStatement: in.ProblemStatement,
		AppName:          in.AppName,
	})
	if err != nil {
		return nil, temporal.NewNonRetryableApplicationError(err.Error(), ErrTypeBadInput, nil)
	}
	id := strings.TrimSpace(in.SessionID)
	if id == "" {
		id = "ps_1"
	}
	now := workflow.Now(ctx).UTC()
	if profile.Name == domain.ProfileAppgen {
		profile.Version = appgenskill.Version
		profile.Hash = appgenskill.SHA256()
	} else {
		profile.Version = packagingskill.Version
		profile.Hash = packagingskill.SHA256()
	}
	return &domain.Session{
		ID:         id,
		Status:     domain.StatusStarted,
		Profile:    profile,
		Intake:     intake,
		Messages:   []domain.Message{},
		NeedsInput: questions,
		Answers:    []domain.Answer{},
		Events:     []domain.Event{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

func handleTurn(ctx, activityCtx workflow.Context, in Input, session *domain.Session, req TurnRequest, permitted func() bool) (*domain.Session, error) {
	info := workflow.GetInfo(ctx)
	actCtx := workflow.WithActivityOptions(activityCtx, workflow.ActivityOptions{
		StartToCloseTimeout: defaultTurnStartToClose,
		HeartbeatTimeout:    defaultHeartbeatTimeout,
		WaitForCancellation: true,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    10 * time.Second,
			MaximumAttempts:    3,
			NonRetryableErrorTypes: []string{
				ErrTypeBadInput,
				ErrTypeModelNotConfigured,
				ErrTypeAlreadyAccepted,
			},
		},
	})
	var result application.TurnResult
	err := workflow.ExecuteActivity(actCtx, ActTurn, TurnActivityInput{
		Actor:      in.Actor,
		WorkflowID: info.WorkflowExecution.ID,
		RunID:      info.WorkflowExecution.RunID,
		Session:    domain.CloneSession(session),
		Message:    strings.TrimSpace(req.Message),
	}).Get(ctx, &result)
	if err != nil {
		return nil, err
	}
	return applyTurnResult(ctx, session, req, result, permitted)
}

func applyTurnResult(ctx workflow.Context, session *domain.Session, req TurnRequest, result application.TurnResult, permitted func() bool) (*domain.Session, error) {
	if !permitted() {
		return nil, turnRunError(ErrTypeRunCancelled, "planning turn was cancelled")
	}
	now := workflow.Now(ctx).UTC()
	text := strings.TrimSpace(req.Message)
	if text != "" {
		msg := domain.Message{Role: domain.RoleOperator, Text: text}
		session.Messages = append(session.Messages, msg)
		appendEvent(session, now, domain.Event{Type: domain.EventMessage, Message: &msg})
	}
	for i := range result.Messages {
		m := result.Messages[i]
		session.Messages = append(session.Messages, m)
		appendEvent(session, now, domain.Event{Type: domain.EventMessage, Message: &m})
	}
	if len(result.InspectionEvents) > 0 {
		for _, event := range result.InspectionEvents {
			if event == nil {
				continue
			}
			cp := *event
			session.Inspection = &cp
			appendEvent(session, now, domain.Event{Type: domain.EventInspection, Inspection: &cp})
		}
		if result.Inspection != nil {
			cp := *result.Inspection
			session.Inspection = &cp
		}
	} else if result.Inspection != nil {
		cp := *result.Inspection
		session.Inspection = &cp
		appendEvent(session, now, domain.Event{Type: domain.EventInspection, Inspection: &cp})
	}
	if result.NeedsInput != nil {
		session.NeedsInput = append([]domain.Question(nil), result.NeedsInput...)
		if len(session.NeedsInput) > 0 {
			appendEvent(session, now, domain.Event{
				Type:       domain.EventNeedsInput,
				NeedsInput: append([]domain.Question(nil), session.NeedsInput...),
			})
		}
	}
	if result.Plan != nil && strings.TrimSpace(result.Plan.Text) != "" {
		plan := domain.Plan{Text: result.Plan.Text}
		plan.SHA256 = domain.PlanHash(plan)
		session.Plan = &plan
		appendEvent(session, now, domain.Event{Type: domain.EventPlan, Plan: &plan})
	}
	if result.ResolvedCommit != "" {
		session.ResolvedCommit = result.ResolvedCommit
	}
	if result.Model != "" {
		session.Model = result.Model
		session.ConfiguredModel = result.ConfiguredModel
		session.ProviderModel = result.ProviderModel
	}
	if session.Model == "" {
		session.Model = domain.DefaultModelLabel
	}
	if result.Profile.Name != "" || result.Profile.Version != "" || result.Profile.Hash != "" {
		if result.Profile.Name == "" {
			result.Profile.Name = domain.ProfileOnboard
		}
		session.Profile = result.Profile
	}
	rejected := map[string]bool{}
	for _, question := range session.NeedsInput {
		if strings.TrimSpace(question.ValidationError) != "" {
			rejected[question.ID] = true
		}
	}
	if len(rejected) > 0 {
		session.Plan = nil
		kept := []domain.Answer{}
		for _, answer := range session.Answers {
			if !rejected[answer.ID] {
				kept = append(kept, answer)
			}
		}
		session.Answers = kept
	}
	session.Status = statusAfterTurn(session)
	session.UpdatedAt = now
	appendEvent(session, now, domain.Event{Type: domain.EventStatus, Status: session.Status})
	appendEvent(session, now, domain.Event{Type: domain.EventDone, Status: session.Status})
	return session, nil
}

func handleAnswer(ctx workflow.Context, session *domain.Session, req AnswerRequest) (*domain.Session, error) {
	if err := validateAnswers(session.NeedsInput, req.Answers); err != nil {
		return nil, err
	}
	now := workflow.Now(ctx).UTC()
	byID := map[string]domain.Answer{}
	for _, existing := range session.Answers {
		byID[existing.ID] = existing
	}
	changed := false
	for _, answer := range req.Answers {
		value := strings.TrimSpace(answer.Value)
		existing, exists := byID[answer.ID]
		if !exists || existing.Value != value {
			changed = true
		}
		byID[answer.ID] = domain.Answer{ID: answer.ID, Value: value}
	}
	for index := range session.NeedsInput {
		for _, answer := range req.Answers {
			if answer.ID == session.NeedsInput[index].ID {
				session.NeedsInput[index].ValidationError = ""
			}
		}
	}
	if changed {
		session.Plan = nil
	}
	session.Answers = session.Answers[:0]
	for _, question := range session.NeedsInput {
		if item, ok := byID[question.ID]; ok {
			session.Answers = append(session.Answers, item)
			if session.Profile.Name == domain.ProfileAppgen {
				switch question.ID {
				case "problem_statement":
					session.Intake.ProblemStatement = item.Value
				case "app_name":
					session.Intake.AppName = item.Value
				}
			}
		}
	}
	session.Status = statusAfterTurn(session)
	session.UpdatedAt = now
	appendEvent(session, now, domain.Event{Type: domain.EventStatus, Status: session.Status})
	appendEvent(session, now, domain.Event{Type: domain.EventDone, Status: session.Status})
	return session, nil
}

func handleAccept(ctx workflow.Context, in Input, session *domain.Session) (*domain.Session, error) {
	now := workflow.Now(ctx).UTC()
	intake := session.Intake
	for _, msg := range session.Messages {
		if msg.Role == domain.RoleOperator {
			intake.Message = msg.Text
			break
		}
	}
	plan := domain.Plan{Text: session.Plan.Text, SHA256: domain.PlanHash(*session.Plan)}
	model := session.Model
	if model == "" {
		model = domain.DefaultModelLabel
	}
	binding := &domain.Binding{
		Profile:         session.Profile,
		Intake:          intake,
		ResolvedCommit:  session.ResolvedCommit,
		Model:           model,
		ConfiguredModel: session.ConfiguredModel, ProviderModel: session.ProviderModel,
		Plan:       plan,
		AcceptedAt: now,
	}
	info := workflow.GetInfo(ctx)
	actCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: defaultAuditStartToClose,
		WaitForCancellation: true,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:        time.Second,
			BackoffCoefficient:     2.0,
			MaximumInterval:        5 * time.Second,
			MaximumAttempts:        3,
			NonRetryableErrorTypes: []string{ErrTypeBadInput, ErrTypeAuditFailed},
		},
	})
	auditRecord := application.Audit{
		Action:           application.ActionAccept,
		SessionID:        session.ID,
		ProfileName:      binding.Profile.Name,
		ProfileVersion:   binding.Profile.Version,
		ProfileHash:      binding.Profile.Hash,
		RepoURL:          intake.RepoURL,
		Ref:              intake.Ref,
		ProblemStatement: intake.ProblemStatement,
		AppName:          intake.AppName,
		Message:          intake.Message,
		ResolvedCommit:   binding.ResolvedCommit,
		Model:            binding.Model,
		ConfiguredModel:  binding.ConfiguredModel, ProviderModel: binding.ProviderModel,
		PlanHash:   binding.Plan.SHA256,
		AcceptedAt: now,
	}
	if err := workflow.ExecuteActivity(actCtx, ActAuditAccept, AuditAcceptActivityInput{
		Actor:   in.Actor,
		Record:  auditRecord,
		RunID:   info.WorkflowExecution.RunID,
		Session: session.ID,
	}).Get(ctx, nil); err != nil {
		return nil, err
	}
	session.Plan = &plan
	session.Binding = binding
	session.Status = domain.StatusAccepted
	session.UpdatedAt = now
	appendEvent(session, now, domain.Event{Type: domain.EventStatus, Status: session.Status})
	appendEvent(session, now, domain.Event{Type: domain.EventDone, Status: session.Status})
	return session, nil
}

func appendEvent(session *domain.Session, now time.Time, event domain.Event) {
	event.ID = fmt.Sprintf("ev_%d", len(session.Events)+1)
	event.Timestamp = now
	session.Events = append(session.Events, event)
}

func statusAfterTurn(session *domain.Session) string {
	if domain.PendingRequired(session.NeedsInput, session.Answers) {
		return domain.StatusNeedsInput
	}
	if session.Plan != nil && strings.TrimSpace(session.Plan.Text) != "" {
		return domain.StatusReady
	}
	if session.Inspection != nil || session.Profile.Name == domain.ProfileAppgen {
		return domain.StatusPlanning
	}
	return domain.StatusInspecting
}

func validateAnswers(questions []domain.Question, answers []domain.Answer) error {
	if len(questions) == 0 {
		return temporal.NewNonRetryableApplicationError(domain.ErrNoPendingQuestions.Error(), ErrTypeNoPendingQuestions, nil)
	}
	known := map[string]domain.Question{}
	for _, q := range questions {
		known[q.ID] = q
	}
	if len(answers) == 0 {
		return temporal.NewNonRetryableApplicationError(domain.ErrInvalidAnswer.Error(), ErrTypeInvalidAnswer, nil)
	}
	for _, ans := range answers {
		q, ok := known[ans.ID]
		if !ok {
			return temporal.NewNonRetryableApplicationError(domain.ErrUnknownQuestion.Error(), ErrTypeUnknownQuestion, nil)
		}
		val := strings.TrimSpace(ans.Value)
		if (q.Required || q.ValidationError != "") && val == "" {
			return temporal.NewNonRetryableApplicationError(domain.ErrInvalidAnswer.Error(), ErrTypeInvalidAnswer, nil)
		}
		if q.ID == "app_name" && !domain.IsValidAppName(val) {
			return temporal.NewNonRetryableApplicationError(domain.ErrInvalidAnswer.Error(), ErrTypeInvalidAnswer, nil)
		}
		switch q.Type {
		case domain.QuestionBoolean:
			if val != "true" && val != "false" && val != "yes" && val != "no" {
				return temporal.NewNonRetryableApplicationError(domain.ErrInvalidAnswer.Error(), ErrTypeInvalidAnswer, nil)
			}
		case domain.QuestionSelect:
			matched := false
			for _, opt := range q.Options {
				if val == opt {
					matched = true
					break
				}
			}
			if !matched {
				return temporal.NewNonRetryableApplicationError(domain.ErrInvalidAnswer.Error(), ErrTypeInvalidAnswer, nil)
			}
		}
	}
	return nil
}

func checkActor(owner, caller string) error {
	owner = strings.TrimSpace(owner)
	caller = strings.TrimSpace(caller)
	if owner == "" || caller == "" || owner != caller {
		return temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("session belongs to %q, not %q", owner, caller),
			ErrTypeForbidden, nil,
		)
	}
	return nil
}

func shouldContinueAsNew(ctx workflow.Context, in Input) bool {
	if in.CANCount >= maxContinueAsNew {
		return false
	}
	limit := in.ContinueAsNewAfterEvents
	if limit <= 0 {
		limit = defaultHistoryEvents
	}
	info := workflow.GetInfo(ctx)
	return info.GetCurrentHistoryLength() >= limit || info.GetContinueAsNewSuggested()
}

// MapWorkflowError converts a Temporal workflow/update error back into the
// canonical planning domain sentinel error expected by HTTP and CLI callers.
func MapWorkflowError(err error) error {
	if err == nil {
		return nil
	}
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		switch appErr.Type() {
		case ErrTypeModelNotConfigured:
			return fmt.Errorf("%w: %s", domain.ErrModelNotConfigured, appErr.Message())
		case ErrTypeNeedsInputPending:
			return domain.ErrNeedsInputPending
		case ErrTypeNoPendingQuestions:
			return domain.ErrNoPendingQuestions
		case ErrTypeUnknownQuestion:
			return domain.ErrUnknownQuestion
		case ErrTypeInvalidAnswer:
			return domain.ErrInvalidAnswer
		case ErrTypeComputeBeforeAccept:
			return domain.ErrComputeBeforeAccept
		case ErrTypePlanRequired:
			return domain.ErrPlanRequired
		case ErrTypeCommitNotPinned:
			return domain.ErrCommitNotPinned
		case ErrTypeAlreadyAccepted:
			return domain.ErrAlreadyAccepted
		case ErrTypeAuditFailed:
			return fmt.Errorf("record the planning.accept audit event: %s", appErr.Message())
		case ErrTypeRunCancelled:
			return domain.ErrRunCancelled
		case ErrTypeRunConflict:
			return domain.ErrRunConflict
		case ErrTypeRunCapacity:
			return domain.ErrRunCapacity
		case ErrTypeForbidden:
			return domain.ErrNotFound
		case ErrTypeBadInput:
			return domain.IntakeError{Reason: appErr.Message()}
		}
	}
	return err
}

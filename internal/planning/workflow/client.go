package workflow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"

	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

// WorkflowClient is the slice of Temporal's client.Client used by ClientService.
type WorkflowClient interface {
	ExecuteWorkflow(ctx context.Context, options client.StartWorkflowOptions, workflow any, args ...any) (client.WorkflowRun, error)
	QueryWorkflow(ctx context.Context, workflowID string, runID string, queryType string, args ...any) (converter.EncodedValue, error)
	UpdateWorkflow(ctx context.Context, options client.UpdateWorkflowOptions) (client.WorkflowUpdateHandle, error)
}

// ClientService implements the planning session lifecycle (Create, Get, Turn,
// Answer, Accept, Events, Compute) backed by Temporal workflows.
type ClientService struct {
	client    WorkflowClient
	taskQueue string
	actor     string
	seq       *atomic.Uint64
	compute   application.AppgenComputer
}

// NewClientService creates a Temporal-backed planning session service scoped
// to an authenticated operator actor.
func NewClientService(c WorkflowClient, taskQueue, actor string, seq *atomic.Uint64) *ClientService {
	if strings.TrimSpace(taskQueue) == "" {
		taskQueue = TaskQueue
	}
	if seq == nil {
		seq = &atomic.Uint64{}
	}
	return &ClientService{
		client:    c,
		taskQueue: taskQueue,
		actor:     strings.TrimSpace(actor),
		seq:       seq,
	}
}

// WithCompute sets the post-Accept computer. Unset compute stays refused.
func (s *ClientService) WithCompute(compute application.AppgenComputer) *ClientService {
	s.compute = compute
	return s
}

// Create starts a new durable planning session workflow and queries its initial state.
func (s *ClientService) Create(ctx context.Context, repoURL, ref string) (*domain.Session, error) {
	sessionID, err := newSessionID()
	if err != nil {
		return nil, err
	}
	return s.CreateWithID(ctx, sessionID, repoURL, ref)
}

// CreateWithInput starts a new durable planning session workflow for either onboard or appgen intake.
func (s *ClientService) CreateWithInput(ctx context.Context, in domain.CreateInput) (*domain.Session, error) {
	if s.client == nil {
		return nil, errors.New("planning Temporal client is not configured")
	}
	if _, _, _, err := domain.ParseCreateInput(in); err != nil {
		return nil, err
	}
	sessionID, err := newSessionID()
	if err != nil {
		return nil, err
	}
	wfID := WorkflowID(sessionID)
	_, err = s.client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    wfID,
		TaskQueue:             s.taskQueue,
		WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
	}, WorkflowName, Input{
		SessionID:        sessionID,
		Actor:            s.actor,
		Profile:          in.Profile,
		RepoURL:          in.RepoURL,
		Ref:              in.Ref,
		ProblemStatement: in.ProblemStatement,
		AppName:          in.AppName,
	})
	if err != nil {
		return nil, MapWorkflowError(err)
	}
	return s.Get(ctx, sessionID)
}

// CreateWithID starts a new durable planning session workflow with an explicit session ID.
func (s *ClientService) CreateWithID(ctx context.Context, sessionID, repoURL, ref string) (*domain.Session, error) {
	if s.client == nil {
		return nil, errors.New("planning Temporal client is not configured")
	}
	if _, err := domain.ParseIntake(repoURL, ref); err != nil {
		return nil, err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = fmt.Sprintf("ps_%d", s.seq.Add(1))
	}
	wfID := WorkflowID(sessionID)
	_, err := s.client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    wfID,
		TaskQueue:             s.taskQueue,
		WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
	}, WorkflowName, Input{
		SessionID: sessionID,
		Actor:     s.actor,
		RepoURL:   repoURL,
		Ref:       ref,
	})
	if err != nil {
		return nil, MapWorkflowError(err)
	}
	return s.Get(ctx, sessionID)
}

// Get queries the current state of a planning session workflow.
func (s *ClientService) Get(ctx context.Context, id string) (*domain.Session, error) {
	if s.client == nil {
		return nil, errors.New("planning Temporal client is not configured")
	}
	if s.actor == "" {
		return nil, domain.ErrNotFound
	}
	wfID := WorkflowID(id)
	ownerVal, err := s.client.QueryWorkflow(ctx, wfID, "", QueryOwner)
	if err != nil {
		return nil, mapNotFound(err)
	}
	if ownerVal == nil {
		return nil, domain.ErrNotFound
	}
	var owner string
	if err := ownerVal.Get(&owner); err != nil {
		return nil, err
	}
	if owner != s.actor {
		return nil, domain.ErrNotFound
	}
	val, err := s.client.QueryWorkflow(ctx, wfID, "", QuerySession)
	if err != nil {
		return nil, mapNotFound(err)
	}
	var session domain.Session
	if err := val.Get(&session); err != nil {
		return nil, err
	}
	return &session, nil
}

// Events queries the recorded event log of a planning session workflow.
func (s *ClientService) Events(ctx context.Context, id string) ([]domain.Event, error) {
	session, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return domain.CloneEvents(session.Events), nil
}

// Turn sends a synchronous UpdateTurn to the planning session workflow and
// waits for the turn activity to complete.
func (s *ClientService) Turn(ctx context.Context, id, message string) (*domain.Session, error) {
	if s.client == nil {
		return nil, errors.New("planning Temporal client is not configured")
	}
	var prevCalls int
	if before, err := s.Get(ctx, id); err == nil && before != nil && before.Inspection != nil {
		prevCalls = len(before.Inspection.ToolCalls)
	}
	handle, err := s.client.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   WorkflowID(id),
		UpdateName:   UpdateTurn,
		Args:         []any{TurnRequest{Actor: s.actor, Message: message}},
		WaitForStage: client.WorkflowUpdateStageCompleted,
	})
	if err != nil {
		return nil, mapNotFound(MapWorkflowError(err))
	}
	var session domain.Session
	if err := handle.Get(ctx, &session); err != nil {
		return nil, mapNotFound(MapWorkflowError(err))
	}
	if session.Inspection != nil && len(session.Inspection.ToolCalls) > prevCalls {
		for _, toolName := range session.Inspection.ToolCalls[prevCalls:] {
			if callID, obsErr := application.ToolStarted(ctx, toolName, json.RawMessage(`{}`)); obsErr == nil && callID != "" {
				_ = application.ToolFinished(ctx, callID, true)
			}
		}
	}
	if len(session.NeedsInput) > 0 {
		_ = application.QuestionsObserved(ctx, session.NeedsInput)
	}
	return &session, nil
}

// Answer sends a synchronous UpdateAnswer to record operator answers on the workflow.
func (s *ClientService) Answer(ctx context.Context, id string, answers []domain.Answer) (*domain.Session, error) {
	if s.client == nil {
		return nil, errors.New("planning Temporal client is not configured")
	}
	handle, err := s.client.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   WorkflowID(id),
		UpdateName:   UpdateAnswer,
		Args:         []any{AnswerRequest{Actor: s.actor, Answers: answers}},
		WaitForStage: client.WorkflowUpdateStageCompleted,
	})
	if err != nil {
		return nil, mapNotFound(MapWorkflowError(err))
	}
	var session domain.Session
	if err := handle.Get(ctx, &session); err != nil {
		return nil, mapNotFound(MapWorkflowError(err))
	}
	return &session, nil
}

// Accept sends a synchronous UpdateAccept to record the governance audit and
// bind the accepted plan on the workflow.
func (s *ClientService) Accept(ctx context.Context, id string) (*domain.Session, error) {
	if s.client == nil {
		return nil, errors.New("planning Temporal client is not configured")
	}
	handle, err := s.client.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   WorkflowID(id),
		UpdateName:   UpdateAccept,
		Args:         []any{AcceptRequest{Actor: s.actor}},
		WaitForStage: client.WorkflowUpdateStageCompleted,
	})
	if err != nil {
		return nil, mapNotFound(MapWorkflowError(err))
	}
	var session domain.Session
	if err := handle.Get(ctx, &session); err != nil {
		return nil, mapNotFound(MapWorkflowError(err))
	}
	return &session, nil
}

// Compute verifies the session has been accepted on the workflow and returns
// the bound session (or refuses compute before accept).
func (s *ClientService) Compute(ctx context.Context, id string) (*domain.Session, error) {
	session, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := domain.RequireAccepted(session); err != nil {
		return nil, err
	}
	if s.compute == nil {
		return nil, domain.ErrComputeNotConfigured
	}
	if err := s.compute(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

func newSessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("allocate a planning session id: %w", err)
	}
	return "ps_" + hex.EncodeToString(raw[:]), nil
}

func mapNotFound(err error) error {
	if err == nil {
		return nil
	}
	var notFound *serviceerror.NotFound
	if errors.As(err, &notFound) {
		return domain.ErrNotFound
	}
	return err
}

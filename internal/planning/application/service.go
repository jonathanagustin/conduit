// Package application stores planning sessions and applies the contract
// transitions: start, turn, answer, Accept, and compute refusal.
package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

// Planner runs one planning turn. A nil planner leaves turns unconfigured
// until a later change inspects the repository, asks Needs Input, and writes
// the plan.
type Planner interface {
	Turn(ctx context.Context, session *domain.Session, message string) (*TurnResult, error)
}

// TurnResult is what a Planner may add to a session after one turn.
type TurnResult struct {
	Messages         []domain.Message
	Inspection       *domain.Inspection
	InspectionEvents []*domain.Inspection
	NeedsInput       []domain.Question
	Plan             *domain.Plan
	ResolvedCommit   string
	Model            string
	Profile          domain.ProfileRef
}

// ActionAccept is the audit action written when a session is accepted.
const ActionAccept = "planning.accept"

// Audit is one record for the audit chain. Accept fills the binding fields
// and the profile and plan hashes.
type Audit struct {
	Action         string
	SessionID      string
	ProfileName    string
	ProfileVersion string
	ProfileHash    string
	RepoURL        string
	Ref            string
	Message        string
	ResolvedCommit string
	Model          string
	PlanHash       string
	AcceptedAt     time.Time
}

// Auditor writes one audit record.
type Auditor interface {
	Record(ctx context.Context, event Audit) error
}

// WithAuditor sets the audit port. Accept fails closed when an auditor is
// set and the write fails.
func (service *Service) WithAuditor(auditor Auditor) *Service {
	service.auditor = auditor
	return service
}

// Service is a process-local planning-session store.
type Service struct {
	auditor     Auditor
	planner     Planner
	now         func() time.Time
	mu          sync.Mutex
	sessions    map[string]*domain.Session
	activeTurns map[string]bool
}

// NewService returns an empty in-memory store. A nil planner leaves turns
// unconfigured so this contract can ship without calling a model.
func NewService(planner Planner) *Service {
	return &Service{
		planner:     planner,
		now:         func() time.Time { return time.Now().UTC() },
		sessions:    map[string]*domain.Session{},
		activeTurns: map[string]bool{},
	}
}

// Create records intake and returns a session in started. It does not
// inspect the repository or call a model.
func (service *Service) Create(_ context.Context, repoURL, ref string) (*domain.Session, error) {
	intake, err := domain.ParseIntake(repoURL, ref)
	if err != nil {
		return nil, err
	}
	id, err := newSessionID()
	if err != nil {
		return nil, err
	}
	now := service.now()
	session := &domain.Session{
		ID:         id,
		Status:     domain.StatusStarted,
		Profile:    domain.ProfileRef{Name: domain.ProfileOnboard},
		Intake:     intake,
		NeedsInput: []domain.Question{},
		Answers:    []domain.Answer{},
		Messages:   []domain.Message{},
		Events:     []domain.Event{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	service.mu.Lock()
	service.sessions[id] = session
	service.mu.Unlock()
	return domain.CloneSession(session), nil
}

// Get returns a copy of the session.
func (service *Service) Get(_ context.Context, id string) (*domain.Session, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	session, ok := service.sessions[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return domain.CloneSession(session), nil
}

// Turn records an optional operator message and asks the planner to
// inspect, elicit, or write a plan. It does not start package compute.
func (service *Service) Turn(ctx context.Context, id, message string) (*domain.Session, error) {
	service.mu.Lock()
	session, err := service.mutable(id)
	if err != nil {
		service.mu.Unlock()
		return nil, err
	}
	if session.Status == domain.StatusAccepted {
		service.mu.Unlock()
		return nil, domain.ErrAlreadyAccepted
	}
	if service.planner == nil {
		service.mu.Unlock()
		return nil, domain.ErrModelNotConfigured
	}
	if service.activeTurns[id] {
		service.mu.Unlock()
		return nil, domain.ErrTurnInProgress
	}
	service.activeTurns[id] = true
	snapshot := domain.CloneSession(session)
	service.mu.Unlock()
	defer func() {
		service.mu.Lock()
		delete(service.activeTurns, id)
		service.mu.Unlock()
	}()

	result, err := service.planner.Turn(ctx, snapshot, strings.TrimSpace(message))
	if err != nil {
		return nil, err
	}

	service.mu.Lock()
	defer service.mu.Unlock()
	session, err = service.mutable(id)
	if err != nil {
		return nil, err
	}
	if session.Status == domain.StatusAccepted {
		return nil, domain.ErrAlreadyAccepted
	}
	if strings.TrimSpace(message) != "" {
		session.Messages = append(session.Messages, domain.Message{
			Role: domain.RoleOperator, Text: strings.TrimSpace(message),
		})
	}
	if result != nil {
		session.Messages = append(session.Messages, result.Messages...)
		if result.Inspection != nil {
			session.Inspection = result.Inspection
		}
		if result.NeedsInput != nil {
			session.NeedsInput = result.NeedsInput
		}
		if result.Plan != nil {
			session.Plan = result.Plan
		}
		if result.ResolvedCommit != "" {
			session.ResolvedCommit = result.ResolvedCommit
		}
		if result.Model != "" {
			session.Model = result.Model
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
	}
	// Rejected values are no longer answers, and cannot keep an old draft eligible.
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
	session.UpdatedAt = service.now()
	service.recordTurnEvents(session, message, result)
	return domain.CloneSession(session), nil
}

// Answer records responses to outstanding Needs Input questions.
func (service *Service) Answer(_ context.Context, id string, answers []domain.Answer) (*domain.Session, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	session, err := service.mutable(id)
	if err != nil {
		return nil, err
	}
	if service.activeTurns[id] {
		return nil, domain.ErrTurnInProgress
	}
	if session.Status == domain.StatusAccepted {
		return nil, domain.ErrAlreadyAccepted
	}
	if len(session.NeedsInput) == 0 {
		return nil, domain.ErrNoPendingQuestions
	}
	known := map[string]domain.Question{}
	for _, question := range session.NeedsInput {
		known[question.ID] = question
	}
	if len(answers) == 0 {
		return nil, domain.ErrInvalidAnswer
	}
	for _, answer := range answers {
		question, ok := known[answer.ID]
		if !ok {
			return nil, domain.ErrUnknownQuestion
		}
		if err := validateAnswer(question, answer.Value); err != nil {
			return nil, err
		}
	}
	byID := map[string]domain.Answer{}
	for _, existing := range session.Answers {
		byID[existing.ID] = existing
	}
	changed := false
	for _, answer := range answers {
		value := strings.TrimSpace(answer.Value)
		existing, exists := byID[answer.ID]
		if !exists || existing.Value != value {
			changed = true
		}
		byID[answer.ID] = domain.Answer{ID: answer.ID, Value: value}
	}
	for index := range session.NeedsInput {
		for _, answer := range answers {
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
		}
	}
	session.Status = statusAfterTurn(session)
	session.UpdatedAt = service.now()
	service.appendEvent(session, domain.Event{Type: domain.EventStatus, Status: session.Status})
	service.appendEvent(session, domain.Event{Type: domain.EventDone, Status: session.Status})
	return domain.CloneSession(session), nil
}

// Accept records the binding. It refuses unless a plan exists, every
// required Needs Input question is answered, and the commit SHA is pinned.
// The binding is written once; a second Accept is refused.
func (service *Service) Accept(ctx context.Context, id string) (*domain.Session, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	session, err := service.mutable(id)
	if err != nil {
		return nil, err
	}
	if service.activeTurns[id] {
		return nil, domain.ErrTurnInProgress
	}
	if session.Status == domain.StatusAccepted || session.Binding != nil {
		return nil, domain.ErrAlreadyAccepted
	}
	if session.Plan == nil || strings.TrimSpace(session.Plan.Text) == "" {
		return nil, domain.ErrPlanRequired
	}
	if domain.PendingRequired(session.NeedsInput, session.Answers) {
		return nil, domain.ErrNeedsInputPending
	}
	if !domain.IsPinnedCommit(session.ResolvedCommit) {
		return nil, domain.ErrCommitNotPinned
	}
	now := service.now()
	intake := session.Intake
	for _, message := range session.Messages {
		if message.Role == domain.RoleOperator {
			intake.Message = message.Text
			break
		}
	}
	plan := domain.Plan{Text: session.Plan.Text, SHA256: domain.PlanHash(*session.Plan)}
	model := session.Model
	if model == "" {
		model = domain.DefaultModelLabel
	}
	binding := &domain.Binding{
		Profile:        session.Profile,
		Intake:         intake,
		ResolvedCommit: session.ResolvedCommit,
		Model:          model,
		Plan:           plan,
		AcceptedAt:     now,
	}
	if service.auditor != nil {
		err := service.auditor.Record(ctx, Audit{
			Action:         ActionAccept,
			SessionID:      session.ID,
			ProfileName:    binding.Profile.Name,
			ProfileVersion: binding.Profile.Version,
			ProfileHash:    binding.Profile.Hash,
			RepoURL:        intake.RepoURL,
			Ref:            intake.Ref,
			Message:        intake.Message,
			ResolvedCommit: binding.ResolvedCommit,
			Model:          binding.Model,
			PlanHash:       plan.SHA256,
			AcceptedAt:     now,
		})
		if err != nil {
			return nil, fmt.Errorf("record the planning.accept audit event: %w", err)
		}
	}
	session.Binding = binding
	session.Status = domain.StatusAccepted
	session.UpdatedAt = now
	service.appendEvent(session, domain.Event{Type: domain.EventStatus, Status: session.Status})
	service.appendEvent(session, domain.Event{Type: domain.EventDone, Status: session.Status})
	return domain.CloneSession(session), nil
}

// Events returns the recorded planning events for one session.
func (service *Service) Events(_ context.Context, id string) ([]domain.Event, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	session, ok := service.sessions[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return domain.CloneEvents(session.Events), nil
}

func (service *Service) recordTurnEvents(session *domain.Session, message string, result *TurnResult) {
	if strings.TrimSpace(message) != "" {
		msg := domain.Message{Role: domain.RoleOperator, Text: strings.TrimSpace(message)}
		service.appendEvent(session, domain.Event{Type: domain.EventMessage, Message: &msg})
	}
	if result != nil {
		for i := range result.Messages {
			msg := result.Messages[i]
			service.appendEvent(session, domain.Event{Type: domain.EventMessage, Message: &msg})
		}
		if len(result.InspectionEvents) > 0 {
			for _, snapshot := range result.InspectionEvents {
				service.appendEvent(session, domain.Event{Type: domain.EventInspection, Inspection: snapshot})
			}
		} else if result.Inspection != nil {
			service.appendEvent(session, domain.Event{Type: domain.EventInspection, Inspection: result.Inspection})
		}
		if len(result.NeedsInput) > 0 {
			service.appendEvent(session, domain.Event{Type: domain.EventNeedsInput, NeedsInput: result.NeedsInput})
		}
		if result.Plan != nil {
			service.appendEvent(session, domain.Event{Type: domain.EventPlan, Plan: result.Plan})
		}
	}
	service.appendEvent(session, domain.Event{Type: domain.EventStatus, Status: session.Status})
	service.appendEvent(session, domain.Event{Type: domain.EventDone, Status: session.Status})
}

func (service *Service) appendEvent(session *domain.Session, event domain.Event) {
	event.ID = fmt.Sprintf("ev_%d", len(session.Events)+1)
	event.Timestamp = service.now()
	session.Events = append(session.Events, event)
}

// Compute is refused until Accept. After Accept this contract still has
// no package runner, so it reports that compute is not configured.
func (service *Service) Compute(_ context.Context, id string) (*domain.Session, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	session, err := service.mutable(id)
	if err != nil {
		return nil, err
	}
	if err := domain.RequireAccepted(session); err != nil {
		return nil, err
	}
	return nil, domain.ErrComputeNotConfigured
}

func (service *Service) mutable(id string) (*domain.Session, error) {
	session, ok := service.sessions[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return session, nil
}

func statusAfterTurn(session *domain.Session) string {
	if domain.PendingRequired(session.NeedsInput, session.Answers) {
		return domain.StatusNeedsInput
	}
	if session.Plan != nil && strings.TrimSpace(session.Plan.Text) != "" {
		return domain.StatusReady
	}
	if session.Inspection != nil {
		return domain.StatusPlanning
	}
	return domain.StatusInspecting
}

func validateAnswer(question domain.Question, value string) error {
	value = strings.TrimSpace(value)
	if (question.Required || question.ValidationError != "") && value == "" {
		return domain.ErrInvalidAnswer
	}
	switch question.Type {
	case domain.QuestionBoolean:
		if value != "true" && value != "false" && value != "yes" && value != "no" {
			return domain.ErrInvalidAnswer
		}
	case domain.QuestionSelect:
		for _, option := range question.Options {
			if value == option {
				return nil
			}
		}
		return domain.ErrInvalidAnswer
	}
	return nil
}

func newSessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("allocate a planning session id: %w", err)
	}
	return "ps_" + hex.EncodeToString(raw[:]), nil
}

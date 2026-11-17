// Package application runs proposal evaluations and course options through the
// packaging memory gate. It does not apply a change.
package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

// ErrCourseForbidden means the caller cannot propose a change for the target.
var ErrCourseForbidden = errors.New("course target is forbidden")

// ErrCourseTargetMissing means the target is absent from current desired fleet state.
var ErrCourseTargetMissing = errors.New("course target is not in current desired fleet state")

// ErrCourseBlocked means the selected value failed the required memory gate.
var ErrCourseBlocked = errors.New("course fails the required memory gate")

const labelNoModel = "No model call"

// liveEvalCaseID is the one recorded case that also goes through a live adapter.
const liveEvalCaseID = "mem-512"

// Case is one eval scenario bound to a memory value and an expected gate outcome.
type Case struct {
	ID              string `json:"id"`
	Scenario        string `json:"scenario"`
	BoundObject     string `json:"boundObject"`
	Value           string `json:"value"`
	ExpectedOutcome string `json:"expectedOutcome"`
	Source          string `json:"source"`
}

// Column is one adapter's result for a case.
type Column struct {
	Adapter string `json:"adapter"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason"`
	Passed  bool   `json:"passed"`
	Label   string `json:"label"`
}

// CaseResult is one case after the configured adapters scored it.
type CaseResult struct {
	Case    Case     `json:"case"`
	Columns []Column `json:"columns"`
}

// Option is one scored proposal. Selecting it still needs a person.
type Option struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Value    string `json:"value"`
	Outcome  string `json:"outcome"`
	Reason   string `json:"reason"`
	Blocked  bool   `json:"blocked"`
	Label    string `json:"label"`
	Tradeoff string `json:"tradeoff"`
}

// Config is the proposal pipeline the Hub is actually running.
type Config struct {
	Adapter  string `json:"adapter"`
	Timeout  string `json:"timeout"`
	GateRule string `json:"gateRule"`
	Label    string `json:"label"`
}

// Opener stores a change request and does not approve it.
type Opener interface {
	Open(ctx context.Context, value, rationale, author, environment, installation, product string) (string, string, error)
}

// Auditor records one intelligence decision.
type Auditor interface {
	Record(ctx context.Context, actor, action, resource, decision, reason string) (string, error)
}

// Proposer asks a configured local command for one memory value.
type Proposer interface {
	Propose(ctx context.Context, scenario, value string) (string, error)
}

// Service scores cases and options with the packaging gate.
type Service struct {
	AdapterName string
	Timeout     string
	Opener      Opener
	Audit       Auditor
	Proposer    Proposer
	// Stored keeps operator-added cases. New sets an in-memory store; the Hub
	// replaces it with a durable one when it has a database.
	Stored CaseStore
	mu     sync.Mutex
	last   []CaseResult
}

func New(adapterName, timeout string) *Service {
	if strings.TrimSpace(adapterName) == "" {
		adapterName = "recorded"
	}
	if strings.TrimSpace(timeout) == "" {
		timeout = "45s"
	}
	return &Service{AdapterName: adapterName, Timeout: timeout, Stored: NewMemoryCases()}
}

func (service *Service) Logic() Config {
	label := labelNoModel
	if service.AdapterName != "recorded" {
		label = "Live model call"
	}
	return Config{Adapter: service.AdapterName, Timeout: service.Timeout, GateRule: domain.MemoryGateRule, Label: label}
}

// Cases is the built-in cases followed by the stored ones, in the order they
// were added.
func (service *Service) Cases(ctx context.Context) ([]Case, error) {
	out := builtinCases()
	if service.Stored == nil {
		return out, nil
	}
	stored, err := service.Stored.List(ctx)
	if err != nil {
		return nil, err
	}
	return append(out, stored...), nil
}

// AddCase stores an operator-added case. The id is derived from the content,
// so adding the same case twice returns the first one.
func (service *Service) AddCase(ctx context.Context, scenario, bound, value, expected string) (Case, error) {
	scenario = strings.TrimSpace(scenario)
	expected = strings.TrimSpace(expected)
	if scenario == "" || expected == "" {
		return Case{}, errCase
	}
	bound, value = strings.TrimSpace(bound), strings.TrimSpace(value)
	identity, err := json.Marshal([]string{scenario, bound, value, expected})
	if err != nil {
		return Case{}, err
	}
	added := Case{ID: "case-" + domain.DigestText(identity)[:16], Scenario: scenario, BoundObject: bound, Value: value, ExpectedOutcome: expected, Source: "operator feedback"}
	if service.Stored == nil {
		return Case{}, errorString("evaluation cases are not configured")
	}
	return service.Stored.Add(ctx, added, StoredCaseLimit())
}

// ErrCaseInvalid is a case with no scenario or no expected outcome.
var ErrCaseInvalid error = errorString("scenario and expected outcome are required")

var errCase = ErrCaseInvalid

type errorString string

func (e errorString) Error() string { return string(e) }

func (service *Service) Run(ctx context.Context, actor string) ([]CaseResult, string, error) {
	cases, err := service.Cases(ctx)
	if err != nil {
		return nil, "", err
	}
	results := make([]CaseResult, 0, len(cases))
	for _, item := range cases {
		columns := []Column{scoreColumn("recorded", labelNoModel, item)}
		if service.Proposer != nil && item.ID == liveEvalCaseID {
			proposed, err := service.Proposer.Propose(ctx, item.Scenario, item.Value)
			if err != nil {
				columns = append(columns, Column{Adapter: service.AdapterName, Outcome: "error", Reason: err.Error(), Label: "Live model call"})
			} else {
				columns = append(columns, scoreColumn(service.AdapterName, "Live model call", Case{Value: proposed, ExpectedOutcome: item.ExpectedOutcome}))
			}
		}
		results = append(results, CaseResult{Case: item, Columns: columns})
	}
	service.mu.Lock()
	service.last = results
	service.mu.Unlock()
	auditID := ""
	if service.Audit != nil {
		id, err := service.Audit.Record(ctx, actor, "intelligence.eval", "intelligence/evals", "ALLOW", "recorded gate")
		if err != nil {
			return nil, "", err
		}
		auditID = id
	}
	return results, auditID, nil
}

func scoreColumn(adapter, label string, item Case) Column {
	outcome, reason, blocked := domain.AssessMemory(item.Value)
	if strings.TrimSpace(item.Value) == "" {
		outcome, reason, blocked = domain.OutcomeFailRequired, "memory limit is not a size", true
	}
	got := string(outcome)
	passed := got == item.ExpectedOutcome || (blocked && item.ExpectedOutcome == string(domain.OutcomeFailRequired))
	return Column{Adapter: adapter, Outcome: got, Reason: reason, Passed: passed, Label: label}
}

func (service *Service) Courses() []Option {
	values := []struct{ id, value, tradeoff string }{
		{"coa-512", "512Mi", "within the 1Gi ceiling"},
		{"coa-1gi", "1Gi", "at the ceiling"},
		{"coa-8gi", "8Gi", "exceeds the ceiling"},
	}
	options := make([]Option, 0, len(values))
	for _, item := range values {
		outcome, reason, blocked := domain.AssessMemory(item.value)
		options = append(options, Option{
			ID: item.id, Title: "memory limit " + item.value, Value: item.value,
			Outcome: string(outcome), Reason: reason, Blocked: blocked,
			Label: labelNoModel, Tradeoff: item.tradeoff,
		})
	}
	return options
}

func (service *Service) Select(ctx context.Context, actor, value, environment, installation, product string) (string, string, error) {
	if service.Opener == nil {
		return "", "", errorString("change requests are not configured")
	}
	outcome, reason, blocked := domain.AssessMemory(value)
	if blocked || strings.TrimSpace(value) == "" {
		return "", "", ErrCourseBlocked
	}
	id, status, err := service.Opener.Open(ctx, value, reason, actor, environment, installation, product)
	if err != nil {
		return "", "", err
	}
	if service.Audit != nil {
		if _, err := service.Audit.Record(ctx, actor, "intelligence.course", "change-request/"+id, string(outcome), reason); err != nil {
			return "", "", err
		}
	}
	return id, status, nil
}

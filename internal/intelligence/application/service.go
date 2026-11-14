// Package application runs proposal evaluations and course options through the
// packaging memory gate. It does not apply a change.
package application

import (
	"context"
	"strings"
	"sync"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

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
	mu          sync.Mutex
	cases       []Case
	last        []CaseResult
}

func New(adapterName, timeout string) *Service {
	if strings.TrimSpace(adapterName) == "" {
		adapterName = "recorded"
	}
	if strings.TrimSpace(timeout) == "" {
		timeout = "45s"
	}
	return &Service{
		AdapterName: adapterName,
		Timeout:     timeout,
		cases: []Case{
			{ID: "mem-512", Scenario: "A 512Mi limit is within the ceiling", BoundObject: "sample-app-1", Value: "512Mi", ExpectedOutcome: "PASS", Source: "gate rule"},
			{ID: "mem-8gi", Scenario: "An 8Gi limit exceeds the ceiling", BoundObject: "sample-app-blocked", Value: "8Gi", ExpectedOutcome: "FAIL_REQUIRED", Source: "gate rule"},
			{ID: "mem-blank", Scenario: "A missing size is not a memory limit", BoundObject: "sample-app-1", Value: "", ExpectedOutcome: "FAIL_REQUIRED", Source: "gate rule"},
		},
	}
}

func (service *Service) Logic() Config {
	label := labelNoModel
	if service.AdapterName != "recorded" {
		label = "Live model call"
	}
	return Config{Adapter: service.AdapterName, Timeout: service.Timeout, GateRule: domain.MemoryGateRule, Label: label}
}

func (service *Service) Cases() []Case {
	service.mu.Lock()
	defer service.mu.Unlock()
	out := make([]Case, len(service.cases))
	copy(out, service.cases)
	return out
}

func (service *Service) AddCase(scenario, bound, value, expected string) (Case, error) {
	scenario = strings.TrimSpace(scenario)
	expected = strings.TrimSpace(expected)
	if scenario == "" || expected == "" {
		return Case{}, errCase
	}
	added := Case{
		ID:       "case-" + domain.DigestText([]byte(scenario + value))[:8],
		Scenario: scenario, BoundObject: strings.TrimSpace(bound), Value: strings.TrimSpace(value),
		ExpectedOutcome: expected, Source: "operator feedback",
	}
	service.mu.Lock()
	service.cases = append(service.cases, added)
	service.mu.Unlock()
	return added, nil
}

var errCase = errorString("scenario and expected outcome are required")

type errorString string

func (e errorString) Error() string { return string(e) }

func (service *Service) Run(ctx context.Context, actor string) ([]CaseResult, string, error) {
	cases := service.Cases()
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
	outcome, reason, _ := domain.AssessMemory(value)
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

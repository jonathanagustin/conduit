package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

func allowEnvironment(environment string) Visible {
	return func(_ *http.Request, got string) bool { return got == environment }
}

type catalog struct {
	runs      []domain.RunStatus
	events    map[string][]domain.StepEvent
	missing   map[string]bool
	mutations int
}

func (c *catalog) Read(_ context.Context, runID string) (domain.RunStatus, error) {
	if c.missing[runID] {
		return domain.RunStatus{}, ErrNotFound
	}
	for _, run := range c.runs {
		if run.RunID == runID {
			return run, nil
		}
	}
	return domain.RunStatus{}, ErrNotFound
}

func (c *catalog) List(context.Context) ([]domain.RunStatus, error) {
	return append([]domain.RunStatus(nil), c.runs...), nil
}

func (c *catalog) Events(_ context.Context, runID string) ([]domain.StepEvent, error) {
	if c.missing[runID] {
		return nil, ErrNotFound
	}
	events, ok := c.events[runID]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]domain.StepEvent(nil), events...), nil
}

func mount(t *testing.T, cfg Config) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, cfg)
	return mux
}

func authed() Authorize { return func(*http.Request) error { return nil } }

func get(mux *http.ServeMux, target string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
	return response
}

func decodePage(t *testing.T, response *httptest.ResponseRecorder) runPage {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var page runPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	return page
}

func TestListRefusesAnonymousAndStaysMountedWhenUnconfigured(t *testing.T) {
	mux := mount(t, Config{Authorize: func(*http.Request) error {
		return StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
	}})
	if response := get(mux, "/v1/packaging-runs"); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d", response.Code)
	}
	if response := get(mux, "/v1/packaging-runs/sample/events"); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous events = %d", response.Code)
	}
	open := mount(t, Config{Authorize: authed()})
	page := decodePage(t, get(open, "/v1/packaging-runs"))
	if page.Configured || page.Total != 0 || len(page.Items) != 0 || page.Authority != authorityPackaging ||
		page.ApprovalAuthority != authorityApproval || page.PlanAuthority != authorityPlan || page.Limit != fullPageLimit {
		t.Fatalf("unconfigured list = %#v", page)
	}
	events := get(open, "/v1/packaging-runs/missing/events")
	if events.Code != http.StatusOK || strings.Contains(events.Body.String(), `"configured": true`) {
		t.Fatalf("unconfigured events = %d %s", events.Code, events.Body.String())
	}
	var eventBody eventPage
	if err := json.Unmarshal(events.Body.Bytes(), &eventBody); err != nil {
		t.Fatal(err)
	}
	if eventBody.Configured || eventBody.Total != 0 || len(eventBody.Items) != 0 || eventBody.Authority != authorityPackaging {
		t.Fatalf("unconfigured events = %#v", eventBody)
	}
	posted := httptest.NewRecorder()
	open.ServeHTTP(posted, httptest.NewRequest(http.MethodPost, "/v1/packaging-runs", strings.NewReader(`{}`)))
	if posted.Code == http.StatusOK {
		t.Fatal("POST list was accepted")
	}
	posted = httptest.NewRecorder()
	open.ServeHTTP(posted, httptest.NewRequest(http.MethodPost, "/v1/packaging-runs/sample/events", strings.NewReader(`{}`)))
	if posted.Code == http.StatusOK {
		t.Fatal("POST events was accepted")
	}
}

func TestListPaginatesFiltersAndHidesUnauthorizedRows(t *testing.T) {
	store := &catalog{runs: []domain.RunStatus{
		{RunID: "run-c", Phase: domain.PhaseComplete, Outcome: string(domain.OutcomeHandoffRecorded), TargetPlanRef: "env-a/app", PlanID: "plan_c", Reason: "handed off"},
		{RunID: "run-a", Phase: domain.PhaseAssess, Outcome: string(domain.OutcomePass), TargetPlanRef: "env-a/app", SourceDigest: "sha256:aaa"},
		{RunID: "run-b", Phase: domain.PhaseBlocked, Outcome: string(domain.OutcomeFailRequired), TargetPlanRef: "env-b/secret", PlanID: "plan_b", Reason: "hidden-token"},
		{RunID: "run-d", Phase: domain.PhaseReview, Outcome: "", TargetPlanRef: "env-a/app", Authority: "temporal-approval"},
	}}
	mux := mount(t, Config{Authorize: authed(), Visible: allowEnvironment("env-a"), Reader: store})
	empty := decodePage(t, get(mux, "/v1/packaging-runs?phase=does-not-exist"))
	if empty.Total != 0 || len(empty.Items) != 0 || empty.PageSize != 0 || empty.Configured != true {
		t.Fatalf("empty = %#v", empty)
	}
	first := decodePage(t, get(mux, "/v1/packaging-runs?limit=1"))
	if first.Total != 3 || first.PageSize != 1 || first.Limit != 1 || first.NextCursor == "" || first.HasPrevious {
		t.Fatalf("first = %#v", first)
	}
	if first.Items[0].RunID != "run-a" || first.Items[0].Authority != authorityPackaging || first.Items[0].PlanID != "" {
		t.Fatalf("first row = %#v", first.Items[0])
	}
	second := decodePage(t, get(mux, "/v1/packaging-runs?limit=1&cursor="+first.NextCursor))
	if second.PageSize != 1 || second.Items[0].RunID != "run-c" || second.Total != 3 || !second.HasPrevious {
		t.Fatalf("second = %#v", second)
	}
	if strings.Contains(second.Items[0].PlanID, "plan_c") && strings.Contains(mustJSON(t, second.Items[0]), "AWAITING_APPROVAL") {
		t.Fatal("plan row exposed plan status")
	}
	raw, _ := json.Marshal(second.Items[0])
	var keys map[string]any
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	if _, ok := keys["planStatus"]; ok || second.Items[0].PlanID != "plan_c" {
		t.Fatalf("row = %s", raw)
	}
	third := decodePage(t, get(mux, "/v1/packaging-runs?limit=1&cursor="+second.NextCursor))
	if third.Items[0].RunID != "run-d" || third.Items[0].Authority != authorityPackaging || third.NextCursor != "" {
		t.Fatalf("third = %#v", third)
	}
	reset := get(mux, "/v1/packaging-runs?limit=1&phase=assess&cursor="+first.NextCursor)
	if reset.Code != http.StatusUnprocessableEntity {
		t.Fatalf("filter change = %d %s", reset.Code, reset.Body.String())
	}
	hidden := decodePage(t, get(mux, "/v1/packaging-runs?search=hidden-token"))
	if hidden.Total != 0 || strings.Contains(mustJSON(t, hidden), "run-b") || strings.Contains(mustJSON(t, hidden), "plan_b") {
		t.Fatalf("hidden row leaked: %#v", hidden)
	}
	for _, target := range []string{
		"/v1/packaging-runs?limit=0",
		"/v1/packaging-runs?limit=21",
		"/v1/packaging-runs?view=summary&limit=201",
		"/v1/packaging-runs?view=other",
		"/v1/packaging-runs?cursor=not-a-cursor",
	} {
		response := get(mux, target)
		if response.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s = %d %s", target, response.Code, response.Body.String())
		}
	}
	summary := decodePage(t, get(mux, "/v1/packaging-runs?view=summary"))
	if summary.Limit != summaryDefaultLimit || summary.Total != 3 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestEventsAreOrderedRedactedAndDoNotRepeatWork(t *testing.T) {
	secret := "leak BEGIN PRIVATE KEY material"
	store := &catalog{
		runs: []domain.RunStatus{
			{RunID: "partial", Phase: domain.PhaseAssess, TargetPlanRef: "env-a/app"},
			{RunID: "blocked", Phase: domain.PhaseBlocked, Outcome: string(domain.OutcomeFailRequired), TargetPlanRef: "env-a/app"},
			{RunID: "waiting", Phase: domain.PhaseReview, TargetPlanRef: "env-a/app"},
			{RunID: "handed", Phase: domain.PhaseComplete, TargetPlanRef: "env-a/app", PlanID: "plan_handed"},
			{RunID: "other", Phase: domain.PhaseComplete, TargetPlanRef: "env-b/secret", PlanID: "plan_other"},
		},
		events: map[string][]domain.StepEvent{
			"partial": {{Sequence: 1, Step: domain.StepAssess, Outcome: string(domain.OutcomePass), Reason: "assessed"}},
			"blocked": {
				{Sequence: 1, Step: domain.StepAssess, Outcome: string(domain.OutcomePass)},
				{Sequence: 2, Step: domain.StepPropose, Outcome: string(domain.OutcomePass)},
				{Sequence: 3, Step: domain.StepValidate, Outcome: string(domain.OutcomeFailRequired), Reason: "lint failed"},
			},
			"waiting": {
				{Sequence: 1, Step: domain.StepReview, Outcome: domain.EventWaiting, Reason: "waiting for review"},
			},
			"handed": {
				{Sequence: 1, Step: domain.StepAssess, Outcome: string(domain.OutcomePass)},
				{Sequence: 2, Step: domain.StepHandoff, Outcome: string(domain.OutcomeHandoffRecorded), PlanID: "plan_handed", Reason: secret},
			},
			"other": {{Sequence: 1, Step: domain.StepHandoff, PlanID: "plan_other"}},
		},
		missing: map[string]bool{"unknown": true},
	}
	before := append([]domain.StepEvent(nil), store.events["partial"]...)
	mux := mount(t, Config{Authorize: authed(), Visible: allowEnvironment("env-a"), Reader: store})

	partial := get(mux, "/v1/packaging-runs/partial/events")
	partialPage := decodeEvents(t, partial)
	if partialPage.Total != 1 || partialPage.Items[0].Step != domain.StepAssess || strings.Contains(partial.Body.String(), domain.StepHandoff) {
		t.Fatalf("partial = %s", partial.Body.String())
	}
	blocked := decodeEvents(t, get(mux, "/v1/packaging-runs/blocked/events"))
	if blocked.Items[2].Outcome != string(domain.OutcomeFailRequired) || stepsOf(blocked.Items) != "assess,propose,validate" {
		t.Fatalf("blocked = %#v", blocked.Items)
	}
	waiting := decodeEvents(t, get(mux, "/v1/packaging-runs/waiting/events"))
	if waiting.Items[0].Outcome != domain.EventWaiting || strings.Contains(mustJSON(t, waiting), domain.StepHandoff) {
		t.Fatalf("waiting = %#v", waiting.Items)
	}
	handed := get(mux, "/v1/packaging-runs/handed/events?limit=1")
	handedPage := decodeEvents(t, handed)
	if handedPage.Items[0].Step != domain.StepAssess || handedPage.NextCursor == "" {
		t.Fatalf("handed first = %#v", handedPage)
	}
	rest := decodeEvents(t, get(mux, "/v1/packaging-runs/handed/events?limit=1&cursor="+handedPage.NextCursor))
	if rest.Total != 2 || len(rest.Items) != 1 || rest.Items[0].PlanID != "plan_handed" || rest.Items[0].Reason != "redacted" {
		t.Fatalf("resume = %#v", rest)
	}
	if strings.Contains(mustJSON(t, rest), "BEGIN PRIVATE KEY") || strings.Contains(mustJSON(t, rest), "leak") || strings.Contains(mustJSON(t, rest), "AWAITING_APPROVAL") || strings.Contains(mustJSON(t, rest), "planStatus") {
		t.Fatalf("resume leaked: %s", mustJSON(t, rest))
	}
	again := decodeEvents(t, get(mux, "/v1/packaging-runs/handed/events?limit=1&cursor="+handedPage.NextCursor))
	if again.Items[0].Sequence != rest.Items[0].Sequence || store.mutations != 0 {
		t.Fatalf("resume repeated work: %#v mutations=%d", again.Items, store.mutations)
	}
	if len(store.events["partial"]) != len(before) || store.events["partial"][0].Step != before[0].Step {
		t.Fatal("reading events changed the journal")
	}
	query, err := parsePage(nil)
	if err != nil {
		t.Fatal(err)
	}
	resume := encodeCursor(eventKey(store.events["partial"][0]), query.fingerprint())
	lagged := decodeEvents(t, get(mux, "/v1/packaging-runs/partial/events?cursor="+resume))
	if lagged.Total != 1 || len(lagged.Items) != 0 || strings.Contains(mustJSON(t, lagged), string(domain.OutcomeHandoffRecorded)) {
		t.Fatalf("lagged client invented a completion: %#v", lagged)
	}
	if got := get(mux, "/v1/packaging-runs/unknown/events"); got.Code != http.StatusNotFound {
		t.Fatalf("unknown = %d %s", got.Code, got.Body.String())
	}
	hidden := get(mux, "/v1/packaging-runs/other/events")
	if hidden.Code != http.StatusNotFound || strings.Contains(hidden.Body.String(), "plan_other") {
		t.Fatalf("hidden events = %d %s", hidden.Code, hidden.Body.String())
	}
	if got := get(mux, "/v1/packaging-runs/handed/events?cursor=!!!!"); got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad cursor = %d", got.Code)
	}
}

func decodeEvents(t *testing.T, response *httptest.ResponseRecorder) eventPage {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var page eventPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	return page
}

func stepsOf(events []domain.StepEvent) string {
	names := make([]string, 0, len(events))
	for _, event := range events {
		names = append(names, event.Step)
	}
	return strings.Join(names, ",")
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

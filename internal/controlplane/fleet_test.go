package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFleetProjectionSeparatesStatusAxesAndDetectsDrift(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	record, err := NewLifecycle(repository).CreatePlan(ctx, assessedFoundation())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	entities := make([]ReportedEntity, 0, len(record.Plan.Steps))
	for _, step := range record.Plan.Steps {
		entities = append(entities, ReportedEntity{
			InstallationID: step.InstallationID, Product: step.Product, Version: step.Version,
			ReleaseDigest: step.ReleaseDigest, Namespace: step.Namespace, ConfigDigest: step.ConfigDigest,
			Health: HealthHealthy, RolloutStatus: RolloutUpToDate,
			ConfigStatus: ConfigApplied, Lifecycle: LifecycleInstalled, ObservedAt: now.Add(-2 * time.Minute),
		})
	}
	entities[0].Version = "0.0.1"
	entities[0].ConfigDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := repository.PutEnvironmentReport(ctx, EnvironmentReport{
		Environment: "dev-use2", AgentID: "agent-dev-use2", ObservedAt: now.Add(-2 * time.Minute), Entities: entities,
	}); err != nil {
		t.Fatal(err)
	}

	projection, err := BuildFleetProjection(ctx, repository, FleetQuery{MaxAge: time.Minute}, now)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Summary.Total != 2 || projection.Summary.Stale != 2 || projection.MaxResultAgeSeconds != 120 {
		t.Fatalf("summary = %#v", projection)
	}
	if projection.Installations[0].RolloutStatus != RolloutOutOfDate ||
		projection.Installations[0].ConfigStatus != ConfigDrifted ||
		projection.Installations[0].Health != HealthHealthy {
		t.Fatalf("independent axes = %#v", projection.Installations[0])
	}

	filtered, err := BuildFleetProjection(ctx, repository, FleetQuery{Rollout: RolloutUpToDate}, now)
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Summary.Total != 1 || len(filtered.Installations) != 1 {
		t.Fatalf("filtered projection = %#v", filtered)
	}
}

func TestFleetProjectionIncludesUnobservedDesiredInstallations(t *testing.T) {
	repository := NewMemoryRepository()
	if _, err := NewLifecycle(repository).CreatePlan(context.Background(), assessedFoundation()); err != nil {
		t.Fatal(err)
	}
	projection, err := BuildFleetProjection(context.Background(), repository, FleetQuery{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if projection.Summary.Total != 2 || projection.Summary.Stale != 2 ||
		projection.Summary.Health[HealthUnknown] != 2 || projection.Summary.Lifecycle[LifecycleUnknown] != 2 {
		t.Fatalf("unobserved projection = %#v", projection)
	}
}

func TestFleetProjectionHTTPFiltersAndValidatesFreshness(t *testing.T) {
	repository := NewMemoryRepository()
	if _, err := NewLifecycle(repository).CreatePlan(context.Background(), assessedFoundation()); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureAgentIdentityHeaders: true, AllowInsecureOperatorIdentityHeaders: true})
	request := httptest.NewRequest(http.MethodGet,
		"/v1/fleet/installations?environment=dev-use2&health=UNKNOWN&maxAgeSeconds=60", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("fleet = %d %s", response.Code, response.Body.String())
	}
	var projection FleetProjection
	if err := json.Unmarshal(response.Body.Bytes(), &projection); err != nil || projection.Summary.Total != 2 {
		t.Fatalf("fleet response = %#v, %v", projection, err)
	}

	invalid := httptest.NewRecorder()
	handler.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet,
		"/v1/fleet/installations?maxAgeSeconds=0", nil))
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid freshness = %d %s", invalid.Code, invalid.Body.String())
	}
}

func TestFleetBulkActionExpandsIntoGovernedAuditedChanges(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	record, err := NewLifecycle(repository).CreatePlan(ctx, assessedFoundation())
	if err != nil {
		t.Fatal(err)
	}
	governance := NewGovernanceService(repository)
	for _, team := range []Team{
		{Name: "operators", DisplayName: "Operators", AdminUsers: []string{"operator"}, MemberUsers: []string{"operator"}},
		{Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"}},
	} {
		if _, err := governance.PutTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := governance.PutGrant(ctx, ResourceGrant{
		ResourceRID: environmentRID("dev-use2"), Team: "operators",
		Operations: []string{OperationBulkChange, OperationOperate, OperationReadResource},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutPolicy(ctx, ApprovalPolicy{
		ID: "bulk-review", DisplayName: "Bulk review", Enabled: true,
		Expression: `type == "SUPPRESS" ? require_team("reviewers", "review bulk suppression") : no_op()`,
	}); err != nil {
		t.Fatal(err)
	}
	targets := make([]FleetActionTarget, 0, len(record.Plan.Steps))
	for _, step := range record.Plan.Steps {
		targets = append(targets, FleetActionTarget{Environment: "dev-use2", InstallationID: step.InstallationID})
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	response := governanceRequest(t, handler, http.MethodPost, "/v1/fleet/actions", "operator", "", FleetActionRequest{
		Action: BulkSuppress, Targets: targets, Reason: "maintenance", DurationSeconds: 600,
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("bulk action = %d %s", response.Code, response.Body.String())
	}
	var proposed struct {
		Results []FleetActionResult `json:"results"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &proposed); err != nil || len(proposed.Results) != 1 {
		t.Fatalf("proposed=%#v err=%v", proposed, err)
	}
	changeRequest := proposed.Results[0].ChangeRequest
	if changeRequest.Status != ChangePendingApprovals || len(changeRequest.Changes) != len(targets) || changeRequest.Execution != nil {
		t.Fatalf("change request=%#v", changeRequest)
	}
	if suppressions, _ := repository.ListSuppressions(ctx, "dev-use2"); len(suppressions) != 0 {
		t.Fatalf("executed before approval: %#v", suppressions)
	}
	response = governanceRequest(t, handler, http.MethodPost, "/v1/change-requests/"+changeRequest.ID+"/approve", "reviewer", "", map[string]string{"rationale": "targets verified"})
	if response.Code != http.StatusOK {
		t.Fatalf("approve = %d %s", response.Code, response.Body.String())
	}
	stored, err := repository.GetChangeRequest(ctx, changeRequest.ID)
	if err != nil || stored.Execution == nil || stored.Execution.Status != ExecutionSucceeded || len(stored.Execution.Items) != len(targets) {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
	suppressions, err := repository.ListSuppressions(ctx, "dev-use2")
	if err != nil || len(suppressions) != len(targets) {
		t.Fatalf("suppressions=%#v err=%v", suppressions, err)
	}
	events, err := repository.ListAuthorizationAudit(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	executed := 0
	for _, event := range events {
		if event.Action == "FLEET_BULK_SUPPRESS" && event.Decision == "ALLOW" {
			executed++
		}
	}
	if executed != len(targets) {
		t.Fatalf("execution audit count=%d events=%#v", executed, events)
	}
}

// A fleet action suppresses or resumes up to five hundred installations in one
// request, so its bounds are the blast radius. The duration rule is compound --
// zero means indefinite, anything else must be at least a minute and at most
// thirty days -- and all three of its edges, plus the target-count bounds, were
// unpinned.
func TestFleetActionBoundsAreExact(t *testing.T) {
	t.Parallel()
	targets := func(count int) []FleetActionTarget {
		list := make([]FleetActionTarget, 0, count)
		for index := range count {
			list = append(list, FleetActionTarget{
				Environment: "dev-use2", InstallationID: fmt.Sprintf("app-%d", index),
			})
		}
		return list
	}
	request := func(mutate func(*FleetActionRequest)) FleetActionRequest {
		base := FleetActionRequest{Action: BulkSuppress, Targets: targets(1), Reason: "incident"}
		mutate(&base)
		return base
	}

	for _, test := range []struct {
		name      string
		request   FleetActionRequest
		wantError bool
	}{
		{name: "one target", request: request(func(*FleetActionRequest) {})},
		{name: "five hundred targets", request: request(func(r *FleetActionRequest) { r.Targets = targets(500) })},
		{name: "no targets", request: request(func(r *FleetActionRequest) { r.Targets = nil }), wantError: true},
		{
			name:    "five hundred and one targets",
			request: request(func(r *FleetActionRequest) { r.Targets = targets(501) }), wantError: true,
		},
		// Zero is indefinite, not "no suppression".
		{name: "indefinite", request: request(func(r *FleetActionRequest) { r.DurationSeconds = 0 })},
		{name: "one minute", request: request(func(r *FleetActionRequest) { r.DurationSeconds = 60 })},
		{
			name:    "just under a minute",
			request: request(func(r *FleetActionRequest) { r.DurationSeconds = 59 }), wantError: true,
		},
		{name: "thirty days", request: request(func(r *FleetActionRequest) { r.DurationSeconds = 30 * 24 * 60 * 60 })},
		{
			name:    "a second over thirty days",
			request: request(func(r *FleetActionRequest) { r.DurationSeconds = 30*24*60*60 + 1 }), wantError: true,
		},
		{
			name:    "negative duration",
			request: request(func(r *FleetActionRequest) { r.DurationSeconds = -1 }), wantError: true,
		},
		// The action is a closed set, and a reason is mandatory because the
		// suppression it creates is what an operator later has to justify.
		{name: "unsuppress", request: request(func(r *FleetActionRequest) { r.Action = BulkUnsuppress })},
		{
			name:    "unknown action",
			request: request(func(r *FleetActionRequest) { r.Action = "PAUSE" }), wantError: true,
		},
		{
			name:    "blank reason",
			request: request(func(r *FleetActionRequest) { r.Reason = "   " }), wantError: true,
		},
		{
			name: "duplicate target",
			request: request(func(r *FleetActionRequest) {
				r.Targets = []FleetActionTarget{
					{Environment: "dev-use2", InstallationID: "app"},
					{Environment: "dev-use2", InstallationID: "app"},
				}
			}), wantError: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := validateFleetAction(test.request); (err != nil) != test.wantError {
				t.Fatalf("rejected=%v, want %v (%v)", err != nil, test.wantError, err)
			}
		})
	}
}

// A fleet action is applied item by item and can be retried, so the record of
// what already succeeded is what stops a retry re-applying it. Both helpers
// that maintain it were unpinned.
func TestFleetExecutionRecordIsIdempotentAndOrdered(t *testing.T) {
	t.Parallel()

	t.Run("succeeded lookup", func(t *testing.T) {
		t.Parallel()
		execution := &ChangeExecution{Items: []ChangeExecutionItem{
			{ChangeID: "a", Status: ExecutionSucceeded},
			{ChangeID: "b", Status: ExecutionFailed},
		}}
		if !executionItemSucceeded(execution, "a") {
			t.Error("a succeeded item was not recognised, so a retry would re-apply it")
		}
		// A failed item must not read as succeeded, or a retry would skip the
		// one item that still needs applying.
		if executionItemSucceeded(execution, "b") {
			t.Error("a failed item read as succeeded")
		}
		if executionItemSucceeded(execution, "absent") {
			t.Error("an unknown change read as succeeded")
		}
		// No execution record at all means nothing has been applied yet.
		if executionItemSucceeded(nil, "a") {
			t.Error("a nil execution read as succeeded")
		}
	})

	t.Run("upsert replaces rather than appends", func(t *testing.T) {
		t.Parallel()
		execution := &ChangeExecution{}
		upsertExecutionItem(execution, ChangeExecutionItem{ChangeID: "b", Status: ExecutionFailed})
		upsertExecutionItem(execution, ChangeExecutionItem{ChangeID: "a", Status: ExecutionSucceeded})
		// Re-recording the same change must overwrite it, or a retry leaves two
		// entries for one item and the record stops meaning anything.
		upsertExecutionItem(execution, ChangeExecutionItem{ChangeID: "b", Status: ExecutionSucceeded})
		if len(execution.Items) != 2 {
			t.Fatalf("items = %#v, want two", execution.Items)
		}
		// And the record stays ordered by change, so two runs of the same
		// actions produce the same document.
		if execution.Items[0].ChangeID != "a" || execution.Items[1].ChangeID != "b" {
			t.Fatalf("items are not ordered by change: %#v", execution.Items)
		}
		if execution.Items[1].Status != ExecutionSucceeded {
			t.Fatalf("the replacement did not take: %#v", execution.Items[1])
		}
	})
}

// fleetActionTargetID becomes the change request's TargetID, and that is hashed
// into the request ID -- so it decides whether resubmitting the same fleet
// action finds the existing request or creates a second one. Sorting the
// targets first is what makes it independent of the order they were listed in,
// which a caller has no reason to keep stable across retries.
func TestFleetActionTargetIDIsIndependentOfTargetOrder(t *testing.T) {
	t.Parallel()
	request := FleetActionRequest{Action: BulkSuppress, Reason: "incident", DurationSeconds: 3600}
	ascending := []FleetActionTarget{
		{Environment: "dev-use2", InstallationID: "api"},
		{Environment: "dev-use2", InstallationID: "workspace"},
	}
	descending := []FleetActionTarget{
		{Environment: "dev-use2", InstallationID: "workspace"},
		{Environment: "dev-use2", InstallationID: "api"},
	}

	first := fleetActionTargetID(request, "dev-use2", ascending)
	if second := fleetActionTargetID(request, "dev-use2", descending); first != second {
		t.Fatalf("the same action listed in a different order produced %s and %s", first, second)
	}
	// Pinned, not just self-consistent. This identifier is content-addressed and
	// it is how a replayed action is recognised as the same action, so it has to
	// mean the same thing across releases as well as within one run. Reversing the
	// sort keeps every property asserted above and still renames every action in
	// the fleet, which would turn every in-flight change request into a new one.
	// If this value changes, that is the decision being made -- not a test to fix.
	const pinned = "blk_a395b422b97450de573c62be1b73fe92"
	if first != pinned {
		t.Errorf("the fleet action identifier changed: %s, was %s", first, pinned)
	}
	// The sort must not disturb the caller's slice: the request is used again
	// after the ID is computed.
	if descending[0].InstallationID != "workspace" {
		t.Fatalf("the caller's targets were reordered in place: %#v", descending)
	}

	// Everything the ID is derived from must actually change it, or two
	// different actions would collapse onto one change request.
	for _, test := range []struct {
		name        string
		environment string
		request     FleetActionRequest
		targets     []FleetActionTarget
	}{
		{
			name: "a different action", environment: "dev-use2", targets: ascending,
			request: FleetActionRequest{Action: BulkUnsuppress, Reason: "incident", DurationSeconds: 3600},
		},
		{
			name: "a different reason", environment: "dev-use2", targets: ascending,
			request: FleetActionRequest{Action: BulkSuppress, Reason: "other", DurationSeconds: 3600},
		},
		{
			name: "a different duration", environment: "dev-use2", targets: ascending,
			request: FleetActionRequest{Action: BulkSuppress, Reason: "incident", DurationSeconds: 60},
		},
		{name: "a different Environment", environment: "prod-use1", request: request, targets: ascending},
		{
			name: "a different target set", environment: "dev-use2", request: request,
			targets: []FleetActionTarget{{Environment: "dev-use2", InstallationID: "api"}},
		},
	} {
		if got := fleetActionTargetID(test.request, test.environment, test.targets); got == first {
			t.Errorf("%s produced the same id %s", test.name, got)
		}
	}
}

// A bulk suppression with no duration is INDEFINITE — it runs until someone lifts it. The
// distinction lives in one guard, `if duration > 0`, and the existing cases only assert that a zero
// duration is ACCEPTED, never that it produces a suppression with no end. A mutation sweep found
// the gap: making the guard always-true gives every indefinite suppression an end time equal to
// now, so it expires immediately and the Entity starts alerting again on the next sweep.
func TestBulkSuppressionWithoutADurationNeverExpires(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)

	suppress := func(t *testing.T, installation string, seconds float64) SuppressionRecord {
		t.Helper()
		id, err := executeFleetChange(ctx, repository, lifecycle,
			ChangeRequest{Environment: "dev-use2", Author: Principal{Username: "operator"}},
			EntityChange{
				Type: BulkSuppress, InstallationID: installation,
				Request: map[string]any{"reason": "incident", "durationSeconds": seconds},
			})
		if err != nil {
			t.Fatal(err)
		}
		record, err := repository.GetSuppression(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return record
	}

	if record := suppress(t, "app-indefinite", 0); record.EndsAt != nil {
		t.Errorf("a suppression with no duration ends at %s; it must run until lifted", record.EndsAt)
	}
	bounded := suppress(t, "app-bounded", 600)
	if bounded.EndsAt == nil {
		t.Fatal("a 600s suppression has no end")
	}
	if !bounded.EndsAt.After(time.Now().UTC()) {
		t.Errorf("a 600s suppression already ended at %s", bounded.EndsAt)
	}
}

// The same tie between Plans that catalog retirement has, on the other index.
// Which Plan is latest decides what the fleet believes is desired, so it cannot
// depend on the order the Plans came back in.
func TestLatestPlanRecordsResolveTiesDeterministically(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	tied := []PlanRecord{
		{ID: "pln_aaa", Plan: Plan{Environment: "prod"}, CreatedAt: at},
		{ID: "pln_ccc", Plan: Plan{Environment: "prod"}, CreatedAt: at},
		{ID: "pln_bbb", Plan: Plan{Environment: "prod"}, CreatedAt: at},
	}
	if got := latestPlanRecords(tied)["prod"].ID; got != "pln_ccc" {
		t.Errorf("tied Plans resolved to %q, wanted the highest ID", got)
	}
	reversed := []PlanRecord{tied[2], tied[1], tied[0]}
	if got := latestPlanRecords(reversed)["prod"].ID; got != "pln_ccc" {
		t.Errorf("reversed input resolved to %q", got)
	}
	later := append(append([]PlanRecord{}, tied...),
		PlanRecord{ID: "pln_aab", Plan: Plan{Environment: "prod"}, CreatedAt: at.Add(time.Second)})
	if got := latestPlanRecords(later)["prod"].ID; got != "pln_aab" {
		t.Errorf("a later Plan lost to a higher ID: %q", got)
	}
}

// Three properties of the projection that no test had reached: where the
// staleness boundary actually falls, that the per-axis counters count up, and
// that Environment orders ahead of installation identifier rather than beside it.
func TestFleetProjectionStalenessCountersAndOrdering(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	// Two Environments whose alphabetical order disagrees with their
	// installations': ordering by identifier alone would reverse them.
	for _, report := range []EnvironmentReport{
		{Environment: "b-prod", AgentID: "agent-b", ObservedAt: now.Add(-time.Minute), Entities: []ReportedEntity{{
			InstallationID: "aaa", Product: "app.example:api", Version: "1.0.0",
			Health: HealthHealthy, RolloutStatus: RolloutUpToDate, ConfigStatus: ConfigApplied,
			Lifecycle: LifecycleInstalled, ObservedAt: now.Add(-time.Minute),
		}}},
		{Environment: "a-dev", AgentID: "agent-a", ObservedAt: now.Add(-30 * time.Second), Entities: []ReportedEntity{{
			InstallationID: "zzz", Product: "app.example:api", Version: "1.0.0",
			Health: HealthHealthy, RolloutStatus: RolloutUpToDate, ConfigStatus: ConfigApplied,
			Lifecycle: LifecycleInstalled, ObservedAt: now.Add(-30 * time.Second),
		}}},
	} {
		if _, err := repository.PutEnvironmentReport(ctx, report); err != nil {
			t.Fatal(err)
		}
	}

	// A MaxAge of exactly one minute: the observation at one minute old is not
	// yet stale, and only the one past it would be.
	projection, err := BuildFleetProjection(ctx, repository, FleetQuery{MaxAge: time.Minute}, now)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Summary.Total != 2 {
		t.Fatalf("projection = %#v", projection.Summary)
	}
	if projection.Summary.Stale != 0 {
		t.Errorf("an observation exactly at MaxAge was called stale: %#v", projection.Summary)
	}
	older, err := BuildFleetProjection(ctx, repository, FleetQuery{MaxAge: time.Minute - time.Second}, now)
	if err != nil {
		t.Fatal(err)
	}
	if older.Summary.Stale != 1 {
		t.Errorf("an observation past MaxAge was not called stale: %#v", older.Summary)
	}

	// No MaxAge means no staleness question was asked, so nothing is stale --
	// not "everything older than zero", which is everything.
	unbounded, err := BuildFleetProjection(ctx, repository, FleetQuery{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if unbounded.Summary.Stale != 0 {
		t.Errorf("a query with no MaxAge called %d installations stale", unbounded.Summary.Stale)
	}

	// Each axis is summarised separately, and each counts up.
	if projection.Summary.Rollout[RolloutUpToDate] != 2 {
		t.Errorf("rollout summary = %#v", projection.Summary.Rollout)
	}
	if projection.Summary.Config[ConfigApplied] != 2 {
		t.Errorf("config summary = %#v", projection.Summary.Config)
	}
	if projection.Summary.Health[HealthHealthy] != 2 || projection.Summary.Lifecycle[LifecycleInstalled] != 2 {
		t.Errorf("health/lifecycle summary = %#v %#v", projection.Summary.Health, projection.Summary.Lifecycle)
	}

	// Environment first, installation second.
	if len(projection.Installations) != 2 ||
		projection.Installations[0].Environment != "a-dev" || projection.Installations[1].Environment != "b-prod" {
		t.Errorf("ordering = %#v", projection.Installations)
	}
}

func TestFleetPaginationTraversesStableOrderWithoutChangingGlobalSummary(t *testing.T) {
	t.Parallel()
	items := []FleetInstallation{
		{Environment: "a", InstallationID: "one"},
		{Environment: "a", InstallationID: "two"},
		{Environment: "b", InstallationID: "one"},
		{Environment: "b", InstallationID: "two"},
		{Environment: "c", InstallationID: "one"},
	}
	projection := FleetProjection{
		Installations: items,
		Summary:       FleetSummary{Total: len(items), Health: map[string]int{}, Rollout: map[string]int{}, Config: map[string]int{}, Lifecycle: map[string]int{}},
	}
	query := FleetQuery{Search: " ONE "}

	first, err := paginateFleetProjection(projection, query, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if first.PageSize != 2 || first.Limit != 2 || first.Summary.Total != 5 || first.HasPrevious || first.NextCursor == "" {
		t.Fatalf("first page = %#v", first)
	}
	second, err := paginateFleetProjection(projection, query, first.NextCursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	if second.PageSize != 2 || !second.HasPrevious || second.PreviousCursor != "" || second.NextCursor == "" {
		t.Fatalf("second page = %#v", second)
	}
	third, err := paginateFleetProjection(projection, query, second.NextCursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	if third.PageSize != 1 || third.NextCursor != "" || !third.HasPrevious || third.PreviousCursor == "" {
		t.Fatalf("third page = %#v", third)
	}
	back, err := paginateFleetProjection(projection, query, third.PreviousCursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{back.Installations[0].InstallationID, back.Installations[1].InstallationID}; got[0] != "one" || got[1] != "two" || back.Installations[0].Environment != "b" {
		t.Fatalf("previous page = %#v", back.Installations)
	}

	seen := append(append([]FleetInstallation{}, first.Installations...), second.Installations...)
	seen = append(seen, third.Installations...)
	for index, item := range seen {
		if item != items[index] {
			t.Fatalf("item %d = %#v, want %#v", index, item, items[index])
		}
	}
	if _, err := paginateFleetProjection(projection, FleetQuery{Search: "different"}, first.NextCursor, 2); !errors.Is(err, errInvalidFleetCursor) {
		t.Fatalf("cursor reused across filters: %v", err)
	}
	if _, err := paginateFleetProjection(projection, FleetQuery{InstallationID: "one"}, first.NextCursor, 2); !errors.Is(err, errInvalidFleetCursor) {
		t.Fatalf("cursor reused across exact installation targets: %v", err)
	}
}

func TestFleetHTTPExactInstallationLookupKeepsAuthorizationAndCounts(t *testing.T) {
	t.Parallel()
	repository := NewMemoryRepository()
	for _, environment := range []string{"visible", "hidden"} {
		entities := []ReportedEntity{
			{InstallationID: "api", Product: "app.example:exact", Health: HealthHealthy},
			{InstallationID: "api-extra", Product: "app.example:substring", Health: HealthUnhealthy},
		}
		for index := 0; index < 25; index++ {
			entities = append(entities, ReportedEntity{InstallationID: fmt.Sprintf("aaa-%02d", index), Product: "app.example:unrelated", Health: HealthHealthy})
		}
		if _, err := repository.PutEnvironmentReport(t.Context(), EnvironmentReport{
			Environment: environment, AgentID: "fixture-reporter", ObservedAt: time.Now(),
			Entities: entities,
		}); err != nil {
			t.Fatal(err)
		}
	}
	governance := NewGovernanceService(repository)
	if _, err := governance.PutTeam(t.Context(), Team{Name: "readers", DisplayName: "Readers", AdminUsers: []string{"admin"}, MemberUsers: []string{"reader"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(t.Context(), ResourceGrant{ResourceRID: environmentRID("visible"), Team: "readers", Operations: []string{OperationReadResource}}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	for _, tc := range []struct {
		environment, installation string
		count                     int
	}{
		{"visible", "api", 1}, {"visible", "API", 0}, {"visible", "missing", 0}, {"hidden", "api", 0},
	} {
		response := governanceRequest(t, handler, http.MethodGet, "/v1/fleet/installations?environment="+tc.environment+"&installationId="+tc.installation+"&limit=1", "reader", "", nil)
		if response.Code != http.StatusOK {
			t.Fatalf("lookup: %d %s", response.Code, response.Body.String())
		}
		var page FleetProjection
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Installations) != tc.count || page.Summary.Total != tc.count || page.PageSize != tc.count || page.NextCursor != "" {
			t.Fatalf("%s/%s: %#v", tc.environment, tc.installation, page)
		}
		if tc.count == 1 && (page.Installations[0].Product != "app.example:exact" || len(page.Facets.Product) != 1) {
			t.Fatalf("wrong exact target or facet disclosure: %#v", page)
		}
		if tc.count == 0 && (len(page.Facets.Product) != 0 || page.Facets.Environment["hidden"] != 0) {
			t.Fatalf("unavailable target leaked facets: %#v", page.Facets)
		}
	}
}

func TestFleetCursorWithoutInstallationFilterRemainsCompatible(t *testing.T) {
	// Pinned pre-installationId encoding: adding an optional exact lookup must
	// not invalidate an already issued continuation of an ordinary index.
	const previous = "7318e2e4efe357767a9d73b7d566e5ee2b8c188fc5c2753c5ac6ebd5bde73e15"
	if got := fleetQueryFingerprint(FleetQuery{}); got != previous {
		t.Fatalf("unfiltered cursor binding changed: %s", got)
	}
	if fleetQueryFingerprint(FleetQuery{InstallationID: "api"}) == previous || fleetQueryFingerprint(FleetQuery{InstallationID: "api"}) == fleetQueryFingerprint(FleetQuery{InstallationID: "api-extra"}) {
		t.Fatal("exact targets must have distinct cursor bindings")
	}
}

func TestFleetExactInstallationCursorBindsSpaceAndIdentity(t *testing.T) {
	projection := FleetProjection{Installations: []FleetInstallation{
		{Environment: "a", InstallationID: "api"},
		{Environment: "b", InstallationID: "api"},
	}}
	query := FleetQuery{InstallationID: "api"}
	space := "space_" + strings.Repeat("a", 32)
	first, err := paginateFleetProjectionInSpace(projection, query, "", 1, space)
	if err != nil || first.NextCursor == "" {
		t.Fatalf("first exact page: %#v, %v", first, err)
	}
	if _, err := paginateFleetProjectionInSpace(projection, query, first.NextCursor, 1, space); err != nil {
		t.Fatalf("same Space and exact identity rejected: %v", err)
	}
	for _, other := range []struct {
		query FleetQuery
		space string
	}{
		{query, ""},
		{query, "space_" + strings.Repeat("b", 32)},
		{FleetQuery{}, space},
		{FleetQuery{InstallationID: "api-extra"}, space},
	} {
		if _, err := paginateFleetProjectionInSpace(projection, other.query, first.NextCursor, 1, other.space); !errors.Is(err, errInvalidFleetCursor) {
			t.Fatalf("cursor crossed Space or exact identity: %#v, %v", other, err)
		}
	}
}

func TestFleetHTTPPaginatesOnlyAfterAuthorization(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	for _, environment := range []string{"a-visible", "b-hidden", "c-visible"} {
		if _, err := repository.PutEnvironmentReport(ctx, EnvironmentReport{
			Environment: environment, AgentID: "agent-" + environment, ObservedAt: time.Now().UTC(),
			Entities: []ReportedEntity{{InstallationID: "api", Product: "app.example:api", Health: HealthHealthy}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	governance := NewGovernanceService(repository)
	if _, err := governance.PutTeam(ctx, Team{Name: "readers", DisplayName: "Readers", AdminUsers: []string{"team-admin"}, MemberUsers: []string{"reader"}}); err != nil {
		t.Fatal(err)
	}
	for _, environment := range []string{"a-visible", "c-visible"} {
		if _, err := governance.PutGrant(ctx, ResourceGrant{
			ResourceRID: environmentRID(environment), Team: "readers", Operations: []string{OperationReadResource},
		}); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	firstResponse := governanceRequest(t, handler, http.MethodGet, "/v1/fleet/installations?limit=1", "reader", "", nil)
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("first page = %d %s", firstResponse.Code, firstResponse.Body.String())
	}
	var first FleetProjection
	if err := json.Unmarshal(firstResponse.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.Summary.Total != 2 || first.PageSize != 1 || len(first.Installations) != 1 ||
		first.Installations[0].Environment != "a-visible" || first.NextCursor == "" {
		t.Fatalf("first page = %#v", first)
	}
	secondResponse := governanceRequest(t, handler, http.MethodGet,
		"/v1/fleet/installations?limit=1&cursor="+first.NextCursor, "reader", "", nil)
	var second FleetProjection
	if secondResponse.Code != http.StatusOK {
		t.Fatalf("second page = %d %s", secondResponse.Code, secondResponse.Body.String())
	}
	if err := json.Unmarshal(secondResponse.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if second.Summary.Total != 2 || second.PageSize != 1 || second.Installations[0].Environment != "c-visible" ||
		!second.HasPrevious || second.NextCursor != "" {
		t.Fatalf("second page = %#v", second)
	}
}

func TestFleetHTTPRejectsInvalidPaginationInputs(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewMemoryRepository(), HandlerOptions{
		AllowInsecureAgentIdentityHeaders: true, AllowInsecureOperatorIdentityHeaders: true,
	})
	for _, path := range []string{
		"/v1/fleet/installations?limit=0",
		"/v1/fleet/installations?limit=201",
		"/v1/fleet/installations?limit=not-a-number",
		"/v1/fleet/installations?cursor=not-base64!",
		"/v1/fleet/installations?cursor=" + strings.Repeat("a", 2049),
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s = %d %s", path, response.Code, response.Body.String())
		}
	}
}

// TestTheFleetAPIAndTheOverviewAgreeAboutAnIntermittentEnvironment is the
// defect: they did not.
//
// A gap in reported state from an INTERMITTENT_AGENTS Environment is expected
// and is not an outage. The domain predicate said so and the projection
// honoured it, but the map driving it was populated in the console overview and
// nowhere else -- so a maxAgeSeconds query against the operator-facing fleet
// API marked every installation there stale and counted it in Summary.Stale.
// Two surfaces, two answers about the same Environment.
func TestTheFleetAPIAndTheOverviewAgreeAboutAnIntermittentEnvironment(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	state := assessedFoundation()
	state.Environment.ConnectionSetting = ConnectionIntermittentAgents
	record, err := NewLifecycle(repository).CreatePlan(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	// A report far older than the query's tolerance. On an Environment whose
	// gaps are expected this is silence, not staleness.
	entities := make([]ReportedEntity, 0, len(record.Plan.Steps))
	for _, step := range record.Plan.Steps {
		entities = append(entities, ReportedEntity{
			InstallationID: step.InstallationID, Product: step.Product, Version: step.Version,
			ReleaseDigest: step.ReleaseDigest, Namespace: step.Namespace, ConfigDigest: step.ConfigDigest,
			Health: HealthHealthy, RolloutStatus: RolloutUpToDate,
			ConfigStatus: ConfigApplied, Lifecycle: LifecycleInstalled, ObservedAt: now.Add(-6 * time.Hour),
		})
	}
	if _, err := repository.PutEnvironmentReport(ctx, EnvironmentReport{
		Environment: state.Environment.ID, AgentID: "agent-dev-use2",
		ObservedAt: now.Add(-6 * time.Hour), Entities: entities,
	}); err != nil {
		t.Fatal(err)
	}

	projection, err := BuildFleetProjection(ctx, repository, FleetQuery{MaxAge: time.Minute}, now)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Summary.Total == 0 {
		t.Fatal("the fixture produced no installations, so this asserts nothing")
	}
	if projection.Summary.Stale != 0 {
		t.Errorf("an expected gap was counted as staleness: %d of %d stale",
			projection.Summary.Stale, projection.Summary.Total)
	}
	for _, installation := range projection.Installations {
		if installation.Stale {
			t.Errorf("%s in an intermittent Environment reads stale", installation.InstallationID)
		}
	}
}

// The exemption is the connection setting's, not every Environment's. An
// Environment that expects continuous reporting still goes stale, or the fix
// would have turned a real outage into silence everywhere.
func TestAnOrdinaryEnvironmentStillGoesStale(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	record, err := NewLifecycle(repository).CreatePlan(ctx, assessedFoundation())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	entities := make([]ReportedEntity, 0, len(record.Plan.Steps))
	for _, step := range record.Plan.Steps {
		entities = append(entities, ReportedEntity{
			InstallationID: step.InstallationID, Product: step.Product, Version: step.Version,
			ReleaseDigest: step.ReleaseDigest, Namespace: step.Namespace, ConfigDigest: step.ConfigDigest,
			Health: HealthHealthy, RolloutStatus: RolloutUpToDate,
			ConfigStatus: ConfigApplied, Lifecycle: LifecycleInstalled, ObservedAt: now.Add(-6 * time.Hour),
		})
	}
	if _, err := repository.PutEnvironmentReport(ctx, EnvironmentReport{
		Environment: "dev-use2", AgentID: "agent-dev-use2",
		ObservedAt: now.Add(-6 * time.Hour), Entities: entities,
	}); err != nil {
		t.Fatal(err)
	}

	projection, err := BuildFleetProjection(ctx, repository, FleetQuery{MaxAge: time.Minute}, now)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Summary.Stale != projection.Summary.Total || projection.Summary.Total == 0 {
		t.Fatalf("an aged report on an ordinary Environment did not read stale: %#v", projection.Summary)
	}
}

// A facet count is a disclosure. Without this, the route's authorization filter
// covers the rows and the summary and leaves the facets counting the whole
// fleet -- so an operator with no grant on an Environment would still learn how
// many installations it holds, and in which health, from the numbers beside the
// filter controls.
func TestFleetHTTPFacetsCountOnlyAuthorizedEnvironments(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	for environment, health := range map[string]string{"visible": HealthHealthy, "hidden": HealthUnhealthy} {
		if _, err := repository.PutEnvironmentReport(ctx, EnvironmentReport{
			Environment: environment, AgentID: "agent-" + environment, ObservedAt: time.Now().UTC(),
			Entities: []ReportedEntity{{InstallationID: "api", Product: "app.example:api", Health: health}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	governance := NewGovernanceService(repository)
	if _, err := governance.PutTeam(ctx, Team{
		Name: "readers", DisplayName: "Readers", AdminUsers: []string{"team-admin"}, MemberUsers: []string{"reader"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(ctx, ResourceGrant{
		ResourceRID: environmentRID("visible"), Team: "readers", Operations: []string{OperationReadResource},
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})

	// Selecting the visible Environment's health makes the hidden one a health
	// near miss, which is exactly the item a facet would count and a row would
	// not -- so this is the query where an unfiltered facet leaks.
	response := governanceRequest(t, handler, http.MethodGet,
		"/v1/fleet/installations?health="+HealthHealthy, "reader", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("read = %d %s", response.Code, response.Body.String())
	}
	var projection FleetProjection
	if err := json.Unmarshal(response.Body.Bytes(), &projection); err != nil {
		t.Fatal(err)
	}
	if got, want := projection.Facets.Health[HealthHealthy], 1; got != want {
		t.Errorf("Facets.Health[HEALTHY] = %d, want %d", got, want)
	}
	if got := projection.Facets.Health[HealthUnhealthy]; got != 0 {
		t.Errorf("Facets.Health[UNHEALTHY] = %d, want 0 -- it counts an Environment this reader cannot read", got)
	}
	if got := projection.Facets.Environment["hidden"]; got != 0 {
		t.Errorf("Facets.Environment[hidden] = %d, want 0 -- the Environment name itself is a disclosure", got)
	}

	// A reader granted both Environments sees the near miss, which is what makes
	// the assertions above statements about authorization rather than about the
	// facets never counting a near miss at all.
	if _, err := governance.PutTeam(ctx, Team{
		Name: "wide-readers", DisplayName: "Wide readers",
		AdminUsers: []string{"team-admin"}, MemberUsers: []string{"wide-reader"},
	}); err != nil {
		t.Fatal(err)
	}
	for _, environment := range []string{"visible", "hidden"} {
		if _, err := governance.PutGrant(ctx, ResourceGrant{
			ResourceRID: environmentRID(environment), Team: "wide-readers",
			Operations: []string{OperationReadResource},
		}); err != nil {
			t.Fatal(err)
		}
	}
	wide := governanceRequest(t, handler, http.MethodGet,
		"/v1/fleet/installations?health="+HealthHealthy, "wide-reader", "", nil)
	if wide.Code != http.StatusOK {
		t.Fatalf("wide read = %d %s", wide.Code, wide.Body.String())
	}
	var full FleetProjection
	if err := json.Unmarshal(wide.Body.Bytes(), &full); err != nil {
		t.Fatal(err)
	}
	if got, want := full.Facets.Health[HealthUnhealthy], 1; got != want {
		t.Errorf("wide-reader Facets.Health[UNHEALTHY] = %d, want %d", got, want)
	}
	if got, want := len(full.Installations), 1; got != want {
		t.Errorf("wide-reader installations = %d, want %d -- a near miss is not a result", got, want)
	}
}

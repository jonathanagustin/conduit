package optimization

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestResolvePlatformStateAcquisitionBindsExactSafeCoordinates(t *testing.T) {
	requirements, calls, err := ResolvePlatformStateAcquisition("dev-use2", "pln_abc123")
	if err != nil {
		t.Fatal(err)
	}
	if len(requirements) != 4 || len(calls) != 3 {
		t.Fatalf("requirements=%d calls=%d", len(requirements), len(calls))
	}
	for _, call := range calls {
		if strings.ContainsAny(call.Endpoint, "{}") || call.Method != "GET" {
			t.Fatalf("unresolved or mutating call: %#v", call)
		}
	}
	for _, invalid := range []string{"", "..", "env/other", "env%2fother", "env?admin=true", strings.Repeat("x", 65)} {
		if _, _, err := ResolvePlatformStateAcquisition(invalid, "pln_abc123"); err == nil {
			t.Fatalf("accepted unsafe environment id %q", invalid)
		}
		if _, _, err := ResolvePlatformStateAcquisition("dev-use2", invalid); err == nil {
			t.Fatalf("accepted unsafe plan id %q", invalid)
		}
	}
}

func TestPlatformHubFixtureAdapterExecutesCompleteShadowAcquisition(t *testing.T) {
	capturedAt := time.Date(2026, 8, 11, 18, 30, 0, 0, time.UTC)
	requirements, calls, err := ResolvePlatformStateAcquisition("dev-use2", "pln_abc123")
	if err != nil {
		t.Fatal(err)
	}
	fixtures := []PlatformHubReadFixture{
		platformHubFixture(calls[0], capturedAt, `{
			"environment":{"id":"dev-use2","generation":"gen-7","connectionSetting":"AGENTS_CONNECTED","labels":{"region":"us-east-2"},"config":{"password":"must-not-persist"},"secrets":[{"name":"db","providerRef":"must-not-persist","revision":3}]},
			"reported":{"environment":"dev-use2","revision":5}
		}`),
		platformHubFixture(calls[1], capturedAt, `{
			"id":"pln_abc123","plan":{"environment":"dev-use2","generation":"gen-7"},"status":"RUNNING",
			"requiredApprovals":1,"approvals":[{"subject":"reviewer","rationale":"must-not-persist","createdAt":"2026-08-11T18:29:58Z"}],
			"lease":{"agentId":"spoke-1","token":"must-not-persist","attempt":2,"issuedAt":"2026-08-11T18:29:58Z","expiresAt":"2026-08-11T18:34:58Z"},
			"failure":"must-not-persist","updatedAt":"2026-08-11T18:29:59Z","revision":4
		}`),
		platformHubFixture(calls[2], capturedAt, `{
			"environment":"dev-use2","agentId":"spoke-1","planId":"pln_abc123","observedAt":"2026-08-11T18:29:59Z","revision":9,
			"entities":[{"installationId":"api","product":"com.example:api","version":"2.0.0","releaseDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","namespace":"ironcase","configDigest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","health":"HEALTHY","rolloutStatus":"UP_TO_DATE","configStatus":"APPLIED","lifecycle":"INSTALLED","observedAt":"2026-08-11T18:29:59Z","healthDetails":{"nodes":[{"checks":[{"message":"must-not-persist"}]}]}}],
			"secrets":[{"assignment":"db","providerRef":"must-not-persist","namespace":"ironcase","revision":3}]
		}`),
	}
	adapter, err := NewPlatformHubFixtureReadAdapter(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	store := FileTwinStore{Directory: t.TempDir()}
	executor := AcquisitionExecutor{
		Reader: adapter, Extractor: PlatformHubFactExtractor{}, Facts: store, CollectorIdentity: "shadow:platform-hub-fixtures",
		Now: acquisitionClock(capturedAt.Add(time.Second)),
	}
	receipt, err := executor.Execute(context.Background(), requirements, calls)
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Complete || !receipt.PlannedComplete || len(receipt.Calls) != 3 || len(receipt.FactIDs) != 4 {
		t.Fatalf("receipt = %#v", receipt)
	}
	for _, call := range receipt.Calls {
		if call.Status != AcquisitionCallComplete || !strings.HasPrefix(call.SourceRevision, platformHubRevisionPrefix(call.CallID)) {
			t.Fatalf("call receipt = %#v", call)
		}
	}
	for _, factID := range receipt.FactIDs {
		fact, err := store.GetFact(factID)
		if err != nil {
			t.Fatal(err)
		}
		value := string(fact.Value)
		for _, forbidden := range []string{"must-not-persist", "providerRef", "agentId", "token", "rationale", "failure", "healthDetails"} {
			if strings.Contains(value, forbidden) {
				t.Fatalf("%s leaked into %s fact: %s", forbidden, fact.Plane, value)
			}
		}
	}
}

func TestPlatformHubPlanExtractionIsRuntimeApplicable(t *testing.T) {
	testCases := []struct {
		name       string
		status     string
		required   int
		approvals  string
		extra      string
		wantPlanes []StatePlane
	}{
		{name: "pending admission", status: "AWAITING_APPROVAL", required: 1, wantPlanes: []StatePlane{StateAdmitted}},
		{name: "approved is not executing", status: "APPROVED", required: 1, approvals: `,"approvals":[{"subject":"reviewer","createdAt":"2026-08-11T18:29:58Z"}]`, wantPlanes: []StatePlane{StateAdmitted}},
		{name: "leased preserves both planes", status: "LEASED", required: 1, approvals: `,"approvals":[{"subject":"reviewer","createdAt":"2026-08-11T18:29:58Z"}]`, extra: `,"lease":{"attempt":1,"issuedAt":"2026-08-11T18:29:58Z","expiresAt":"2026-08-11T18:34:58Z"}`, wantPlanes: []StatePlane{StateAdmitted, StateExecuting}},
		{name: "cancelled before lease is not execution", status: "CANCELLED", required: 1, extra: `,"cancellation":{"createdAt":"2026-08-11T18:29:59Z"}`, wantPlanes: []StatePlane{StateAdmitted}},
		{name: "recorded is neither admitted nor executing", status: "RECORDED"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := AcquisitionReadRequest{ID: "hub.plan", Source: platformHubAcquisitionSource, Method: "GET", Endpoint: "/v1/plans/pln_test", Consistency: "repository-read"}
			body := []byte(`{"id":"pln_test","plan":{"environment":"dev","generation":"gen-1"},"status":"` + testCase.status + `","requiredApprovals":` + strconv.Itoa(testCase.required) + `,"updatedAt":"2026-08-11T18:29:59Z","revision":2` + testCase.approvals + testCase.extra + `}`)
			response := platformHubFixtureResponse(t, request, body)
			facts, err := (PlatformHubFactExtractor{}).Extract(request, response)
			if err != nil {
				t.Fatal(err)
			}
			if len(facts) != len(testCase.wantPlanes) {
				t.Fatalf("facts = %#v", facts)
			}
			for index, plane := range testCase.wantPlanes {
				if facts[index].Plane != plane {
					t.Fatalf("facts[%d].plane=%s want %s", index, facts[index].Plane, plane)
				}
			}
		})
	}
}

func TestPlatformHubReportFreshnessUsesOldestEntityObservation(t *testing.T) {
	now := time.Date(2026, 8, 11, 18, 30, 0, 0, time.UTC)
	requirements := []StateRequirement{{ID: "platform.observed.reported-state", Plane: StateObserved, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 5}}
	call := AcquisitionCall{ID: "hub.reported-state", Source: platformHubAcquisitionSource, Method: "GET", Endpoint: "/v1/environments/dev/reported-state", Consistency: "exact-spoke-report", CostUnits: 1, Observations: []FieldObservation{{Requirement: "platform.observed.reported-state", Plane: StateObserved, Fidelity: FidelityExact}}}
	fixture := platformHubFixture(call, now, `{
		"environment":"dev","observedAt":"2026-08-11T18:29:59Z","revision":3,
		"entities":[{"installationId":"old","product":"p","version":"1","namespace":"n","health":"HEALTHY","rolloutStatus":"UP_TO_DATE","configStatus":"APPLIED","lifecycle":"INSTALLED","observedAt":"2026-08-11T18:29:50Z"}]
	}`)
	adapter, err := NewPlatformHubFixtureReadAdapter([]PlatformHubReadFixture{fixture})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := (AcquisitionExecutor{Reader: adapter, Extractor: PlatformHubFactExtractor{}, Facts: FileTwinStore{Directory: t.TempDir()}, CollectorIdentity: "shadow:test", Now: acquisitionClock(now)}).Execute(context.Background(), requirements, []AcquisitionCall{call})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Complete || receipt.Calls[0].Status != AcquisitionCallStale {
		t.Fatalf("receipt = %#v", receipt)
	}
}

func TestPlatformHubFixtureAdapterFailsClosedOnForeignOrMalformedEvidence(t *testing.T) {
	request := AcquisitionReadRequest{ID: "hub.environment", Source: platformHubAcquisitionSource, Method: "GET", Endpoint: "/v1/environments/dev", Consistency: "repository-read"}
	fixtureRequest := request
	fixture := PlatformHubReadFixture{Request: fixtureRequest, StatusCode: 200, ObservedAt: "2026-08-11T18:30:00Z", Body: json.RawMessage(`{"environment":{"id":"other","generation":"g1"}}`)}
	adapter, err := NewPlatformHubFixtureReadAdapter([]PlatformHubReadFixture{fixture})
	if err != nil {
		t.Fatal(err)
	}
	response, err := adapter.Read(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (PlatformHubFactExtractor{}).Extract(request, response); err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("foreign identity error = %v", err)
	}
	response.SourceRevision = "plan:foreign:r1"
	request.Endpoint = "/v1/environments/other"
	if _, err := (PlatformHubFactExtractor{}).Extract(request, response); err == nil || !strings.Contains(err.Error(), "revision") {
		t.Fatalf("revision error = %v", err)
	}

	mutating := fixtureRequest
	mutating.Method = "POST"
	if _, err := NewPlatformHubFixtureReadAdapter([]PlatformHubReadFixture{{Request: mutating, StatusCode: 200}}); err == nil {
		t.Fatal("accepted mutating fixture")
	}
	missing, err := NewPlatformHubFixtureReadAdapter(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := missing.Read(context.Background(), fixtureRequest); err == nil {
		t.Fatal("missing fixture did not fail closed")
	}

	planRequest := AcquisitionReadRequest{ID: "hub.plan", Source: platformHubAcquisitionSource, Method: "GET", Endpoint: "/v1/plans/pln_test", Consistency: "repository-read"}
	invalidLifecycle := []byte(`{"id":"pln_test","plan":{"environment":"dev","generation":"g1"},"status":"RUNNING","requiredApprovals":1,"updatedAt":"2026-08-11T18:29:59Z","revision":1}`)
	if _, err := (PlatformHubFactExtractor{}).Extract(planRequest, platformHubFixtureResponse(t, planRequest, invalidLifecycle)); err == nil || !strings.Contains(err.Error(), "lacks lease") {
		t.Fatalf("invalid lifecycle error = %v", err)
	}
}

func TestPlatformHubFixtureAdapterOwnsImmutableFixtureCopies(t *testing.T) {
	request := AcquisitionReadRequest{ID: "hub.environment", Source: platformHubAcquisitionSource, Method: "GET", Endpoint: "/v1/environments/dev", Consistency: "repository-read"}
	body := json.RawMessage(`{"environment":{"id":"dev","generation":"g1"}}`)
	adapter, err := NewPlatformHubFixtureReadAdapter([]PlatformHubReadFixture{{Request: request, StatusCode: 200, ObservedAt: "2026-08-11T18:30:00Z", Body: body}})
	if err != nil {
		t.Fatal(err)
	}
	body[0] = 'x'
	first, err := adapter.Read(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	first.Body[0] = 'x'
	second, err := adapter.Read(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if second.Body[0] != '{' || !json.Valid(second.Body) {
		t.Fatalf("fixture mutated through caller alias: %q", second.Body)
	}
}

func platformHubFixture(call AcquisitionCall, observedAt time.Time, body string) PlatformHubReadFixture {
	return PlatformHubReadFixture{
		Request:    AcquisitionReadRequest{ID: call.ID, Source: call.Source, Method: call.Method, Endpoint: call.Endpoint, Consistency: call.Consistency},
		StatusCode: 200, ObservedAt: observedAt.UTC().Format(time.RFC3339Nano), Body: json.RawMessage(body),
	}
}

func platformHubFixtureResponse(t *testing.T, request AcquisitionReadRequest, body []byte) AcquisitionReadResponse {
	t.Helper()
	revision, err := platformHubSourceRevision(request, body)
	if err != nil {
		t.Fatal(err)
	}
	return AcquisitionReadResponse{StatusCode: 200, ObservedAt: "2026-08-11T18:30:00Z", SourceRevision: revision, Body: body}
}

func platformHubRevisionPrefix(callID string) string {
	switch callID {
	case "hub.environment":
		return "environment:"
	case "hub.plan":
		return "plan:"
	case "hub.reported-state":
		return "report:"
	default:
		return "invalid:"
	}
}

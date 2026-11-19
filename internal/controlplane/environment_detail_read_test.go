package controlplane

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

type environmentDetailReadSpy struct {
	plans                  []PlanRecord
	reports                []EnvironmentReport
	calls                  *[]string
	planError, reportError error
}

func (s *environmentDetailReadSpy) ListPlans(context.Context, string) ([]PlanRecord, error) {
	*s.calls = append(*s.calls, "plans")
	return s.plans, s.planError
}
func (s *environmentDetailReadSpy) ListEnvironmentReports(context.Context) ([]EnvironmentReport, error) {
	*s.calls = append(*s.calls, "reports")
	return s.reports, s.reportError
}

func TestEnvironmentDetailReadOrdersAuthoritySelectionPrivateReadsAndClock(t *testing.T) {
	const spaceID = "space_0123456789abcdef0123456789abcdef"
	repository := NewMemoryRepository()
	governance := NewGovernanceService(repository)
	if _, err := governance.PutTeam(t.Context(), Team{Name: "readers", DisplayName: "Readers", AdminUsers: []string{"admin"}, MemberUsers: []string{"reader"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(t.Context(), ResourceGrant{ResourceRID: "environment/env", Team: "readers", Operations: []string{OperationReadResource}}); err != nil {
		t.Fatal(err)
	}
	reader, err := governance.ResolvePrincipal(t.Context(), IdentityClaims{Username: "reader"})
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := governance.ResolvePrincipal(t.Context(), IdentityClaims{Username: "outsider"})
	if err != nil {
		t.Fatal(err)
	}
	instant := time.Date(2026, 11, 19, 1, 2, 3, 4, time.UTC)
	for _, tc := range []struct {
		name                                                                 string
		principal                                                            Principal
		selection                                                            *string
		decodeError, authorizeError, membershipError, planError, reportError error
		excluded, unavailable, unconfigured, missing                         bool
		want                                                                 error
		calls                                                                []string
	}{
		{name: "resource refusal precedes malformed selection", principal: outsider, decodeError: ErrInvalidSpaceQuery, want: ErrForbidden},
		{name: "malformed selection", principal: reader, decodeError: ErrInvalidSpaceQuery, want: ErrInvalidSpaceQuery, calls: []string{"decode"}},
		{name: "present empty", principal: reader, selection: new(""), want: ErrInvalidSpaceSelection, calls: []string{"decode"}},
		{name: "invalid identity", principal: reader, selection: new("wrong"), want: ErrInvalidSpaceSelection, calls: []string{"decode"}},
		{name: "selection unconfigured", principal: reader, selection: new(spaceID), unconfigured: true, want: ErrSpaceSelectionUnavailable, calls: []string{"decode"}},
		{name: "Space unreadable", principal: reader, selection: new(spaceID), authorizeError: ErrForbidden, want: ErrForbidden, calls: []string{"decode", "authorize"}},
		{name: "membership unconfigured", principal: reader, selection: new(spaceID), unavailable: true, want: ErrSpaceMembershipUnavailable, calls: []string{"decode", "authorize"}},
		{name: "private membership failure", principal: reader, selection: new(spaceID), membershipError: errors.New("private diagnostic"), want: ErrSpaceMembershipRead, calls: []string{"decode", "authorize", "membership"}},
		{name: "membership mismatch", principal: reader, selection: new(spaceID), excluded: true, want: ErrNotFound, calls: []string{"decode", "authorize", "membership"}},
		{name: "plan failure", principal: reader, planError: ErrConflict, want: ErrConflict, calls: []string{"decode", "plans"}},
		{name: "report failure", principal: reader, reportError: ErrConflict, want: ErrConflict, calls: []string{"decode", "plans", "reports"}},
		{name: "missing record does not sample clock", principal: reader, missing: true, want: ErrNotFound, calls: []string{"decode", "plans", "reports"}},
		{name: "unscoped", principal: reader, calls: []string{"decode", "plans", "reports", "clock"}},
		{name: "scoped", principal: reader, selection: new(spaceID), calls: []string{"decode", "authorize", "membership", "plans", "reports", "clock"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			options := HandlerOptions{
				RequireSpaceSelection: func(context.Context, Principal, string) error {
					calls = append(calls, "authorize")
					return tc.authorizeError
				},
				SpaceIDLookup: func(context.Context, Principal, string, string) (string, error) {
					t.Fatal("detail used unrelated resource lookup")
					return "", nil
				},
				LoadSpaceIndexMembership: func(context.Context, Principal, string) (SpaceIndexMembership, error) {
					calls = append(calls, "membership")
					if tc.excluded {
						return SpaceIndexMembership{}, nil
					}
					return SpaceIndexMembership{EnvironmentIDs: []string{"env"}}, tc.membershipError
				},
			}
			if tc.unconfigured {
				options.RequireSpaceSelection = nil
			}
			if tc.unavailable {
				options.LoadSpaceIndexMembership = nil
			}
			spy := &environmentDetailReadSpy{calls: &calls, planError: tc.planError, reportError: tc.reportError}
			if !tc.missing {
				spy.reports = []EnvironmentReport{{Environment: "env", AgentID: "agent", ObservedAt: instant}}
			}
			got, err := ReadAuthorizedEnvironmentDetail(t.Context(), spy, governance, tc.principal, options, "env", func() (*string, error) { calls = append(calls, "decode"); return tc.selection, tc.decodeError }, func() time.Time {
				calls = append(calls, "clock")
				return instant.Add(SpokeLivenessFreshFor + time.Nanosecond)
			})
			if !errors.Is(err, tc.want) || !reflect.DeepEqual(calls, tc.calls) {
				t.Fatalf("err=%v want=%v calls=%v want=%v", err, tc.want, calls, tc.calls)
			}
			if tc.want == nil && (got.Environment.ID != "env" || got.Reported == nil || got.Spoke.State != SpokeStale || !got.Spoke.ObservedAt.Equal(instant)) {
				t.Fatalf("record=%+v", got)
			}
			if tc.membershipError != nil && strings.Contains(err.Error(), "private diagnostic") {
				t.Fatal("private membership error leaked")
			}
		})
	}
}

func TestEnvironmentDetailReadRetainsCompleteDesiredRecordAndReportPresence(t *testing.T) {
	repository := NewMemoryRepository()
	governance := NewGovernanceService(repository)
	options := HandlerOptions{BootstrapAdministrators: []string{"admin"}}
	principal, err := governance.ResolvePrincipal(t.Context(), IdentityClaims{Username: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	instant := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	desired := Environment{ID: "env", Generation: "opaque", PackageArchitecture: "arm64", ConnectionSetting: "CONNECTED", DeclaredStateSource: "source", DefaultReleaseChannel: "channel", Labels: map[string]string{"key": "value"}, Config: map[string]any{"literal": "<value>", "number": float64(42)}, Namespaces: []NamespaceTarget{{RID: "namespace", Name: "name", DeletionProtected: new(false), Installed: map[string]InstalledTarget{"app": {Version: "1", ConfigDigest: "sha256:config"}}}}, ArtifactSecurityPolicy: &ArtifactSecurityPolicy{RequiredOutcome: "PASS"}, ArtifactAvailabilityPolicy: &ArtifactAvailabilityPolicy{RequiredOutcome: "ANY"}, ArtifactRegistries: []ArtifactRegistryAssignment{{ID: "registry", Revision: 3}}, Secrets: []ExternalSecretAssignment{{Name: "secret", ProviderRef: "opaque-reference"}}, AutomaticSecurityRecall: true, VulnerabilityUpgrades: &VulnerabilityUpgradePolicy{AllowRecalledForVulnerabilities: true, RescanSLASeconds: 17}, AutoSuppressionThreshold: 9, Maintenance: MaintenancePolicy{Downtime: &WeeklyMaintenanceWindow{TimeZone: "UTC", Intervals: []string{"MON 00:00-01:00"}}}, HelmExecution: &HelmExecutionDefaults{ImagePrePull: &ImagePrePullPolicy{Enabled: false}}}
	var calls []string
	spy := &environmentDetailReadSpy{calls: &calls, plans: []PlanRecord{{ID: "plan", Plan: Plan{Environment: "env"}, DesiredState: &DesiredState{Environment: desired}, CreatedAt: instant}}}
	got, err := ReadAuthorizedEnvironmentDetail(t.Context(), spy, governance, principal, options, "env", nil, func() time.Time { return instant })
	if err != nil || !reflect.DeepEqual(got.Environment, desired) || got.Reported != nil || got.Spoke.State != SpokeUnreachable || !got.Spoke.ObservedAt.IsZero() {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	got.Environment.Labels["key"] = "changed"
	*got.Environment.Namespaces[0].DeletionProtected = true
	if desired.Labels["key"] != "value" || *desired.Namespaces[0].DeletionProtected {
		t.Fatal("projection aliases reader's source")
	}
	spy.reports = []EnvironmentReport{{Environment: "env"}}
	got, err = ReadAuthorizedEnvironmentDetail(t.Context(), spy, governance, principal, options, "env", nil, nil)
	if err != nil || got.Reported == nil || got.Spoke.State != SpokeUnreachable || !got.Spoke.ObservedAt.IsZero() {
		t.Fatalf("empty report invented liveness: %+v %v", got, err)
	}
}

func TestEnvironmentDetailRESTPreservesSpaceErrorBodiesAndAuthorityOrder(t *testing.T) {
	const spaceID = "space_0123456789abcdef0123456789abcdef"
	for _, tc := range []struct {
		name, actor, query              string
		selectionError, membershipError error
		noSelection, noMembership       bool
		status                          int
		body                            string
	}{
		{name: "malformed encoding", actor: "admin", query: "?spaceId=%zz", status: 400, body: `{"error":"invalid query encoding"}`},
		{name: "present empty", actor: "admin", query: "?spaceId=", status: 400, body: `{"error":"spaceId must name one native Space identity"}`},
		{name: "duplicate", actor: "admin", query: "?spaceId=" + spaceID + "&spaceId=" + spaceID, status: 400, body: `{"error":"spaceId must name one native Space identity"}`},
		{name: "forbidden before malformed", actor: "outsider", query: "?spaceId=%zz", status: 403, body: `{"error":"forbidden","status":"403"}`},
		{name: "selection unavailable", actor: "admin", query: "?spaceId=" + spaceID, noSelection: true, status: 503, body: `{"error":"native Space selection is not configured"}`},
		{name: "selection callback lifecycle error", actor: "admin", query: "?spaceId=" + spaceID, selectionError: ErrInvalidSpaceSelection, status: 500, body: `{"error":"spaceId must name one native Space identity","status":"500"}`},
		{name: "membership unavailable", actor: "admin", query: "?spaceId=" + spaceID, noMembership: true, status: 503, body: `{"error":"native Space index selection is not configured"}`},
		{name: "membership failure redacted", actor: "admin", query: "?spaceId=" + spaceID, membershipError: errors.New("private diagnostic"), status: 500, body: `{"error":"native Space membership could not be read"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}, RequireSpaceSelection: func(context.Context, Principal, string) error { return tc.selectionError }, SpaceIDLookup: func(context.Context, Principal, string, string) (string, error) { return spaceID, nil }, LoadSpaceIndexMembership: func(context.Context, Principal, string) (SpaceIndexMembership, error) {
				return SpaceIndexMembership{EnvironmentIDs: []string{"env"}}, tc.membershipError
			}}
			if tc.noSelection {
				options.RequireSpaceSelection = nil
			}
			if tc.noMembership {
				options.LoadSpaceIndexMembership = nil
			}
			handler := NewHandler(NewMemoryRepository(), options)
			request := httptest.NewRequest(http.MethodGet, "/v1/environments/env"+tc.query, nil)
			request.Header.Set("X-Platform-Subject", tc.actor)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.status || strings.TrimSpace(response.Body.String()) != tc.body {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
		})
	}
}

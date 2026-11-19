package controlplane

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type scopedPlanReadSpy struct {
	records []PlanRecord
	calls   *[]string
	failure error
}

func (s *scopedPlanReadSpy) ListPlans(context.Context, string) ([]PlanRecord, error) {
	*s.calls = append(*s.calls, "plans")
	return s.records, s.failure
}
func (s *scopedPlanReadSpy) GetChangeRequest(context.Context, string) (ChangeRequest, error) {
	*s.calls = append(*s.calls, "change")
	return ChangeRequest{}, ErrNotFound
}

func TestScopedPlanSummaryReadOrdersSelectionBoundsAndPrivateReads(t *testing.T) {
	const id = "space_0123456789abcdef0123456789abcdef"
	governance := NewGovernanceService(NewMemoryRepository())
	admin, err := governance.ResolvePrincipal(t.Context(), IdentityClaims{Username: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, space, cursor                 string
		limit                               *int32
		missingSelection, missingMembership bool
		authorize, membership, plans, want  error
		calls                               []string
	}{
		{name: "empty Space", want: ErrInvalidScopedPlanSummarySelection},
		{name: "invalid Space", space: "invalid", want: ErrInvalidScopedPlanSummarySelection},
		{name: "missing selection capability", space: id, missingSelection: true, want: ErrScopedPlanSummaryUnavailable},
		{name: "selection refused before invalid limit", space: id, authorize: ErrForbidden, limit: new(int32(0)), want: ErrForbidden, calls: []string{"authorize"}},
		{name: "selection unavailable callback", space: id, authorize: ErrSpaceSelectionUnavailable, want: ErrScopedPlanSummaryUnavailable, calls: []string{"authorize"}},
		{name: "selection query-named failure stays lifecycle", space: id, authorize: ErrInvalidPlanSummaryQuery, want: ErrScopedPlanSummaryLifecycleFailure, calls: []string{"authorize"}},
		{name: "missing membership", space: id, missingMembership: true, want: ErrScopedPlanSummaryUnavailable, calls: []string{"authorize"}},
		{name: "membership failure", space: id, membership: errors.New("private membership diagnostic"), want: ErrSpaceMembershipRead, calls: []string{"authorize", "membership"}},
		{name: "zero limit before private Plans", space: id, limit: new(int32(0)), want: ErrInvalidPlanSummaryQuery, calls: []string{"authorize", "membership"}},
		{name: "over bound before private Plans", space: id, limit: new(int32(201)), want: ErrInvalidPlanSummaryQuery, calls: []string{"authorize", "membership"}},
		{name: "catalog unavailable-named failure stays lifecycle", space: id, plans: ErrSpaceSelectionUnavailable, want: ErrScopedPlanSummaryLifecycleFailure, calls: []string{"authorize", "membership", "plans"}},
		{name: "invalid cursor", space: id, cursor: "invalid", want: ErrInvalidPlanSummaryQuery, calls: []string{"authorize", "membership", "plans"}},
		{name: "default", space: id, calls: []string{"authorize", "membership", "plans"}},
		{name: "maximum", space: id, limit: new(int32(200)), calls: []string{"authorize", "membership", "plans"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			options := HandlerOptions{BootstrapAdministrators: []string{"admin"}, RequireSpaceSelection: func(context.Context, Principal, string) error {
				calls = append(calls, "authorize")
				return tc.authorize
			}, SpaceIDLookup: func(context.Context, Principal, string, string) (string, error) {
				t.Fatal("unexpected lookup")
				return "", nil
			}, LoadSpaceIndexMembership: func(context.Context, Principal, string) (SpaceIndexMembership, error) {
				calls = append(calls, "membership")
				return SpaceIndexMembership{EnvironmentIDs: []string{"env"}}, tc.membership
			}}
			if tc.missingSelection {
				options.RequireSpaceSelection = nil
			}
			if tc.missingMembership {
				options.LoadSpaceIndexMembership = nil
			}
			reader := &scopedPlanReadSpy{calls: &calls, failure: tc.plans, records: []PlanRecord{{ID: "plan", Plan: Plan{Environment: "env"}}, {ID: "excluded", Plan: Plan{Environment: "other"}}}}
			page, err := ReadAuthorizedPlanSummaryPageInSpace(t.Context(), reader, governance, admin, options, tc.space, PlanQuery{}, tc.cursor, tc.limit)
			if !errors.Is(err, tc.want) || !reflect.DeepEqual(calls, tc.calls) {
				t.Fatalf("err=%v want=%v calls=%v want=%v", err, tc.want, calls, tc.calls)
			}
			if tc.want == nil {
				want := 100
				if tc.limit != nil {
					want = int(*tc.limit)
				}
				if page.Total != 1 || len(page.Plans) != 1 || page.Plans[0].ID != "plan" || page.Limit != want {
					t.Fatalf("page=%+v", page)
				}
			}
		})
	}
}

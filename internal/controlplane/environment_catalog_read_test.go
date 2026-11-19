package controlplane

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestEnvironmentCatalogReadFullBoundVisibilityAndClock(t *testing.T) {
	repository := NewMemoryRepository()
	governance := NewGovernanceService(repository)
	if _, err := governance.PutTeam(t.Context(), Team{Name: "readers", DisplayName: "Readers", AdminUsers: []string{"admin"}, MemberUsers: []string{"reader"}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 21; i++ {
		if _, err := governance.PutGrant(t.Context(), ResourceGrant{ResourceRID: fmt.Sprintf("environment/env-%02d", i), Team: "readers", Operations: []string{OperationReadResource}}); err != nil {
			t.Fatal(err)
		}
	}
	principal, err := governance.ResolvePrincipal(t.Context(), IdentityClaims{Username: "reader"})
	if err != nil {
		t.Fatal(err)
	}
	instant := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	var calls []string
	spy := &environmentDetailReadSpy{calls: &calls}
	for i := 0; i < 201; i++ {
		spy.reports = append(spy.reports, EnvironmentReport{Environment: fmt.Sprintf("env-%02d", i), AgentID: "agent", ObservedAt: instant})
	}
	clock := func() time.Time {
		calls = append(calls, "clock")
		return instant.Add(SpokeLivenessFreshFor + time.Nanosecond)
	}
	first, err := ReadAuthorizedEnvironmentCatalogPage(t.Context(), spy, governance, principal, HandlerOptions{}, "", "", nil, nil, clock)
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 21 || first.PageSize != 20 || first.Limit != 20 || len(first.Environments) != 20 || first.NextCursor == "" || !reflect.DeepEqual(calls, []string{"plans", "reports", "clock"}) {
		t.Fatalf("page=%+v calls=%v", first, calls)
	}
	for _, record := range first.Environments {
		if record.Reported == nil || record.Spoke.State != SpokeStale || !record.Reported.ObservedAt.Equal(instant) {
			t.Fatalf("record=%+v", record)
		}
	}
	next, err := ReadAuthorizedEnvironmentCatalogPage(t.Context(), spy, governance, principal, HandlerOptions{}, "", first.NextCursor, nil, nil, clock)
	if err != nil || next.Total != 21 || len(next.Environments) != 1 || next.Environments[0].Environment.ID != "env-20" || !next.HasPrevious {
		t.Fatalf("next=%+v err=%v", next, err)
	}
	for _, limit := range []int32{0, -1, 21, 200} {
		calls = nil
		_, err := ReadAuthorizedEnvironmentCatalogPage(t.Context(), spy, governance, principal, HandlerOptions{}, "", "", nil, &limit, clock)
		if !errors.Is(err, ErrInvalidEnvironmentCatalogQuery) || !reflect.DeepEqual(calls, []string{"plans", "reports"}) {
			t.Fatalf("limit=%d err=%v calls=%v", limit, err, calls)
		}
	}
	_, err = ReadAuthorizedEnvironmentCatalogPage(t.Context(), spy, governance, principal, HandlerOptions{}, "different", first.NextCursor, nil, nil, clock)
	if !errors.Is(err, ErrInvalidEnvironmentCatalogQuery) {
		t.Fatalf("changed filter accepted: %v", err)
	}
}

func TestEnvironmentCatalogReadSpaceRefusalAndErrorStages(t *testing.T) {
	const id = "space_0123456789abcdef0123456789abcdef"
	repository := NewMemoryRepository()
	governance := NewGovernanceService(repository)
	principal, err := governance.ResolvePrincipal(t.Context(), IdentityClaims{Username: "reader"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                     string
		selection                *string
		missing                  bool
		authorize, member, plans error
		want                     error
		calls                    []string
	}{
		{name: "present empty", selection: new(""), want: ErrInvalidEnvironmentCatalogQuery},
		{name: "invalid ID", selection: new("other"), want: ErrInvalidEnvironmentCatalogQuery},
		{name: "missing capability", selection: new(id), missing: true, want: ErrEnvironmentCatalogUnavailable},
		{name: "Space forbidden", selection: new(id), authorize: ErrForbidden, want: ErrForbidden, calls: []string{"authorize"}},
		{name: "callback sentinel stays lifecycle error", selection: new(id), authorize: ErrInvalidSpaceSelection, want: ErrInvalidSpaceSelection, calls: []string{"authorize"}},
		{name: "membership failure redacted", selection: new(id), member: errors.New("private diagnostic"), want: ErrSpaceMembershipRead, calls: []string{"authorize", "membership"}},
		{name: "catalog sentinel stays lifecycle error", plans: ErrSpaceSelectionUnavailable, want: ErrSpaceSelectionUnavailable, calls: []string{"plans"}},
		{name: "ungranted index empty", selection: new(id), calls: []string{"authorize", "membership", "plans", "reports", "clock"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			options := HandlerOptions{
				RequireSpaceSelection: func(context.Context, Principal, string) error {
					calls = append(calls, "authorize")
					return tc.authorize
				},
				SpaceIDLookup: func(context.Context, Principal, string, string) (string, error) {
					t.Fatal("unexpected resource lookup")
					return "", nil
				},
				LoadSpaceIndexMembership: func(context.Context, Principal, string) (SpaceIndexMembership, error) {
					calls = append(calls, "membership")
					return SpaceIndexMembership{EnvironmentIDs: []string{"env"}}, tc.member
				},
			}
			if tc.missing {
				options.RequireSpaceSelection = nil
			}
			spy := &environmentDetailReadSpy{calls: &calls, planError: tc.plans, reports: []EnvironmentReport{{Environment: "env"}}}
			page, err := ReadAuthorizedEnvironmentCatalogPage(t.Context(), spy, governance, principal, options, "", "", tc.selection, nil, func() time.Time { calls = append(calls, "clock"); return time.Time{} })
			if !errors.Is(err, tc.want) || !reflect.DeepEqual(calls, tc.calls) {
				t.Fatalf("err=%v want=%v calls=%v want=%v", err, tc.want, calls, tc.calls)
			}
			if tc.want == nil && (page.Total != 0 || len(page.Environments) != 0) {
				t.Fatalf("Space granted resource authority: %+v", page)
			}
			if tc.authorize != nil && errors.Is(err, ErrInvalidEnvironmentCatalogQuery) || tc.plans != nil && errors.Is(err, ErrEnvironmentCatalogUnavailable) {
				t.Fatal("lifecycle error reclassified")
			}
		})
	}
}

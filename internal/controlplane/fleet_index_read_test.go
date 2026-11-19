package controlplane

import (
	"context"
	"errors"
	"testing"
	"time"
)

func fleetReadNumber(n int32) *int32 { return &n }
func TestAuthorizedFleetReadDefaultsPresenceAndCursorBinding(t *testing.T) {
	source := &fleetReadSnapshots{}
	for _, id := range []string{"a", "b", "c"} {
		source.plans = append(source.plans, PlanRecord{ID: id, Plan: Plan{Environment: id, Steps: []PlanStep{{InstallationID: id, Product: "app", Version: "1"}}}})
	}
	governance := NewGovernanceService(NewMemoryRepository())
	options := HandlerOptions{BootstrapAdministrators: []string{"root"}}
	principal := Principal{Username: "root"}
	now := time.Date(2026, 11, 19, 0, 0, 0, 17, time.UTC)
	read := func(r FleetReadRequest) (FleetProjection, error) {
		return readAuthorizedFleetPageWithClock(t.Context(), source, governance, principal, options, r, func() time.Time { return now })
	}
	all, err := read(FleetReadRequest{})
	if err != nil || all.Limit != 100 || all.PageSize != 3 || all.Summary.Total != 3 || !all.GeneratedAt.Equal(now) {
		t.Fatalf("default page=%#v %v", all, err)
	}
	first, err := read(FleetReadRequest{Limit: fleetReadNumber(1)})
	if err != nil || first.NextCursor == "" || first.PageSize != 1 || first.Summary.Total != 3 {
		t.Fatalf("first page=%#v %v", first, err)
	}
	second, err := read(FleetReadRequest{Cursor: first.NextCursor, Limit: fleetReadNumber(2)})
	if err != nil || second.PageSize != 2 || !second.HasPrevious || second.PreviousCursor != "" || second.Summary.Total != 3 {
		t.Fatalf("changed page size=%#v %v", second, err)
	}
	for _, request := range []FleetReadRequest{{Limit: fleetReadNumber(0)}, {Limit: fleetReadNumber(201)}, {MaxAgeSeconds: fleetReadNumber(0)}, {MaxAgeSeconds: fleetReadNumber(86401)}, {Cursor: "invalid"}, {Cursor: first.NextCursor, Search: "different"}, {Cursor: first.NextCursor, MaxAgeSeconds: fleetReadNumber(60)}, {Cursor: first.NextCursor, InstallationID: "b"}, {Cursor: first.NextCursor, Health: HealthHealthy}} {
		if _, err := read(request); !errors.Is(err, ErrInvalidFleetReadQuery) {
			t.Fatalf("invalid or rebound cursor admitted: %#v %v", request, err)
		}
	}
	for _, n := range []int32{1, 86400} {
		if _, err := read(FleetReadRequest{MaxAgeSeconds: &n}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAuthorizedFleetReadSpacePrecedesQueryValidationAndSnapshots(t *testing.T) {
	source := &fleetReadSnapshots{}
	order := []string{}
	spaceID := "space_0123456789abcdef0123456789abcdef"
	options := HandlerOptions{BootstrapAdministrators: []string{"root"}, SpaceIDLookup: func(context.Context, Principal, string, string) (string, error) { return "", nil }, RequireSpaceSelection: func(context.Context, Principal, string) error {
		order = append(order, "authorize")
		return ErrForbidden
	}, LoadSpaceIndexMembership: func(context.Context, Principal, string) (SpaceIndexMembership, error) {
		order = append(order, "members")
		return SpaceIndexMembership{}, nil
	}}
	_, err := readAuthorizedFleetPageWithClock(t.Context(), source, NewGovernanceService(NewMemoryRepository()), Principal{Username: "root"}, options, FleetReadRequest{SpaceID: spaceID, Limit: fleetReadNumber(0)}, func() time.Time { t.Fatal("clock sampled after refusal"); return time.Time{} })
	if !errors.Is(err, ErrForbidden) || len(order) != 1 || len(source.calls) != 0 {
		t.Fatalf("selection order=%v reads=%v err=%v", order, source.calls, err)
	}
	options.RequireSpaceSelection = func(context.Context, Principal, string) error { order = append(order, "authorize"); return nil }
	_, err = ReadAuthorizedFleetPage(t.Context(), source, NewGovernanceService(NewMemoryRepository()), Principal{Username: "root"}, options, FleetReadRequest{SpaceID: spaceID, Limit: fleetReadNumber(0)})
	if !errors.Is(err, ErrInvalidFleetReadQuery) || len(order) != 3 || order[2] != "members" || len(source.calls) != 0 {
		t.Fatalf("validation order=%v reads=%v err=%v", order, source.calls, err)
	}
}

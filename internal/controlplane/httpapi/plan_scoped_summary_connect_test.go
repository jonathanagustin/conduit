package httpapi

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	plansv1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestScopedPlanSummaryConnectRedactsStageFailures(t *testing.T) {
	lifecycle := func(err error) error {
		return fmt.Errorf("%w: %w", controlplane.ErrScopedPlanSummaryLifecycleFailure, err)
	}
	for _, tc := range []struct {
		err  error
		code connect.Code
	}{
		{controlplane.ErrInvalidScopedPlanSummarySelection, connect.CodeInvalidArgument},
		{controlplane.ErrInvalidPlanSummaryQuery, connect.CodeInvalidArgument},
		{controlplane.ErrScopedPlanSummaryUnavailable, connect.CodeUnavailable},
		{lifecycle(controlplane.ErrInvalidPlanSummaryQuery), connect.CodeInternal},
		{lifecycle(controlplane.ErrScopedPlanSummaryUnavailable), connect.CodeInternal},
		{lifecycle(controlplane.ErrSpaceSelectionUnavailable), connect.CodeInternal},
		{lifecycle(controlplane.ErrForbidden), connect.CodePermissionDenied},
		{lifecycle(controlplane.ErrNotFound), connect.CodeNotFound},
		{lifecycle(controlplane.ErrUnauthorized), connect.CodeUnauthenticated},
		{lifecycle(errors.New("private diagnostic")), connect.CodeInternal},
	} {
		t.Run(tc.err.Error(), func(t *testing.T) {
			service := planScopedSummaryService{cfg: PlanScopedSummaryConfig{Read: func(context.Context, string, controlplane.PlanQuery, string, *int32) (controlplane.PlanSummaryPage, error) {
				return controlplane.PlanSummaryPage{}, tc.err
			}}}
			response, err := service.ListPlanSummariesInSpace(t.Context(), &plansv1.ListPlanSummariesInSpaceRequest{SpaceId: "space_0123456789abcdef0123456789abcdef"})
			var wire *connect.Error
			if response != nil || connect.CodeOf(err) != tc.code || !errors.As(err, &wire) || wire.Message() != "scoped Plan summary unavailable" {
				t.Fatalf("redaction/category: %v %v", response, err)
			}
		})
	}
}

func TestScopedPlanSummaryConnectRequiresSelectionAndReturnsNoPartialPage(t *testing.T) {
	calls := 0
	service := planScopedSummaryService{cfg: PlanScopedSummaryConfig{Read: func(context.Context, string, controlplane.PlanQuery, string, *int32) (controlplane.PlanSummaryPage, error) {
		calls++
		return controlplane.PlanSummaryPage{Plans: []controlplane.PlanSummary{{ID: "valid", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, {ID: "invalid", CreatedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}}}, nil
	}}}
	if response, err := service.ListPlanSummariesInSpace(t.Context(), &plansv1.ListPlanSummariesInSpaceRequest{}); response != nil || connect.CodeOf(err) != connect.CodeInvalidArgument || calls != 0 {
		t.Fatalf("missing selection reached reader: %v %v calls%d", response, err, calls)
	}
	response, err := service.ListPlanSummariesInSpace(t.Context(), &plansv1.ListPlanSummariesInSpaceRequest{SpaceId: "space_0123456789abcdef0123456789abcdef"})
	if response != nil || connect.CodeOf(err) != connect.CodeInternal || calls != 1 {
		t.Fatalf("partial page: %v %v calls%d", response, err, calls)
	}
	unconfigured := planScopedSummaryService{}
	if _, err := unconfigured.ListPlanSummariesInSpace(t.Context(), &plansv1.ListPlanSummariesInSpaceRequest{SpaceId: "space_0123456789abcdef0123456789abcdef"}); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatal(err)
	}
}

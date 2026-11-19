package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	environmentsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestEnvironmentCatalogConnectRedactsFailuresWithoutChangingLifecycleCategory(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code connect.Code
	}{
		{controlplane.ErrInvalidEnvironmentCatalogQuery, connect.CodeInvalidArgument},
		{controlplane.ErrEnvironmentCatalogUnavailable, connect.CodeUnavailable},
		{controlplane.ErrUnauthorized, connect.CodeUnauthenticated},
		{controlplane.ErrForbidden, connect.CodePermissionDenied},
		{controlplane.ErrNotFound, connect.CodeNotFound},
		{controlplane.ErrInvalidSpaceSelection, connect.CodeInternal},
		{controlplane.ErrSpaceSelectionUnavailable, connect.CodeInternal},
		{controlplane.ErrSpaceMembershipRead, connect.CodeInternal},
		{errors.New("private diagnostic: provider credentials"), connect.CodeInternal},
	} {
		t.Run(tc.err.Error(), func(t *testing.T) {
			service := environmentCatalogService{cfg: EnvironmentCatalogConfig{Read: func(context.Context, string, string, *string, *int32) (controlplane.EnvironmentCatalogPage, error) {
				return controlplane.EnvironmentCatalogPage{}, tc.err
			}}}
			response, err := service.ListEnvironments(t.Context(), &environmentsv1.ListEnvironmentsRequest{})
			if response != nil || connect.CodeOf(err) != tc.code {
				t.Fatalf("response=%v error=%v", response, err)
			}
			var wire *connect.Error
			if !errors.As(err, &wire) || (wire.Message() != "Environment catalog unavailable" && wire.Message() != "invalid Environment catalog query") {
				t.Fatalf("diagnostic escaped: %v", err)
			}
		})
	}
	service := environmentCatalogService{}
	if _, err := service.ListEnvironments(t.Context(), &environmentsv1.ListEnvironmentsRequest{}); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatal(err)
	}
}

func TestEnvironmentCatalogConnectDoesNotReturnPartialPageAfterMappingFailure(t *testing.T) {
	service := environmentCatalogService{cfg: EnvironmentCatalogConfig{Read: func(context.Context, string, string, *string, *int32) (controlplane.EnvironmentCatalogPage, error) {
		return controlplane.EnvironmentCatalogPage{Environments: []controlplane.EnvironmentCatalogRecord{
			{Environment: controlplane.Environment{ID: "first"}},
			{Environment: controlplane.Environment{ID: "invalid"}, Reported: &controlplane.EnvironmentReport{Environment: "invalid", ObservedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}},
		}}, nil
	}}}
	response, err := service.ListEnvironments(t.Context(), &environmentsv1.ListEnvironmentsRequest{})
	if response != nil || connect.CodeOf(err) != connect.CodeInternal {
		t.Fatalf("partial page=%v err=%v", response, err)
	}
}

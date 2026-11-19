package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	fleetv1 "github.com/jonathanagustin/conduit/gen/go/conduit/fleet/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/fleet/v1/fleetv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

const FleetReadConnectRoute = "POST /conduit.fleet.v1.FleetReadService/ListFleetInstallations"

// FleetReadConfig binds the original authenticated carrier to a native read.
type FleetReadConfig struct {
	Authenticate func(*http.Request) (context.Context, error)
	Read         func(context.Context, controlplane.FleetReadRequest) (controlplane.FleetProjection, error)
}

// RegisterFleetReadConnect mounts only the exact read; authentication precedes decoding.
func RegisterFleetReadConnect(mux *http.ServeMux, cfg FleetReadConfig) {
	server := connect.NewServer()
	fleetv1connect.RegisterFleetReadServiceHandler(server, &fleetReadService{cfg: cfg})
	rpc := http.NewServeMux()
	connecthttp.Mount(rpc, server, connecthttp.WithReadMaxBytes(1<<20))
	mux.Handle(FleetReadConnectRoute, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ctx context.Context
		err := controlplane.ErrUnauthorized
		if cfg.Authenticate != nil {
			ctx, err = cfg.Authenticate(r)
		}
		if err != nil || ctx == nil {
			code := connect.CodeInternal
			if err == nil || errors.Is(err, controlplane.ErrUnauthorized) {
				code = connect.CodeUnauthenticated
			} else if errors.Is(err, controlplane.ErrForbidden) {
				code = connect.CodePermissionDenied
			}
			w.Header().Set("X-Content-Type-Options", "nosniff")
			_ = connecthttp.NewErrorWriter().Write(w, r, connect.NewError(code, code.String()))
			return
		}
		rpc.ServeHTTP(w, r.WithContext(ctx))
	}))
}

type fleetReadService struct{ cfg FleetReadConfig }

func (service *fleetReadService) ListFleetInstallations(ctx context.Context, r *fleetv1.ListFleetInstallationsRequest) (*fleetv1.ListFleetInstallationsResponse, error) {
	if service.cfg.Read == nil {
		return nil, connect.NewError(connect.CodeUnavailable, "Fleet read unavailable")
	}
	p, err := service.cfg.Read(ctx, controlplane.FleetReadRequest{Environment: r.Environment, InstallationID: r.InstallationId, Product: r.Product, Health: r.Health, RolloutStatus: r.RolloutStatus, ConfigStatus: r.ConfigStatus, Lifecycle: r.Lifecycle, Search: r.Search, Cursor: r.Cursor, SpaceID: r.SpaceId, Limit: r.Limit, MaxAgeSeconds: r.MaxAgeSeconds})
	if err != nil {
		code := connect.CodeInternal
		switch {
		case errors.Is(err, controlplane.ErrInvalidFleetReadQuery), errors.Is(err, controlplane.ErrInvalidSpaceSelection):
			code = connect.CodeInvalidArgument
		case errors.Is(err, controlplane.ErrUnauthorized):
			code = connect.CodeUnauthenticated
		case errors.Is(err, controlplane.ErrForbidden):
			code = connect.CodePermissionDenied
		case errors.Is(err, controlplane.ErrNotFound):
			code = connect.CodeNotFound
		case errors.Is(err, controlplane.ErrSpaceSelectionUnavailable), errors.Is(err, controlplane.ErrSpaceMembershipUnavailable):
			code = connect.CodeUnavailable
		case errors.Is(err, context.Canceled):
			code = connect.CodeCanceled
		case errors.Is(err, context.DeadlineExceeded):
			code = connect.CodeDeadlineExceeded
		}
		return nil, connect.NewError(code, "Fleet read unavailable")
	}
	result, err := fleetProjectionMessage(p)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, "Invalid native Fleet result")
	}
	return result, nil
}

func fleetCountsMessage(values map[string]int) (map[string]int64, error) {
	result := make(map[string]int64, len(values))
	for key, count := range values {
		if count < 0 {
			return nil, fmt.Errorf("negative native count")
		}
		name := nativeIndexString(key)
		if _, exists := result[name]; exists {
			return nil, fmt.Errorf("ambiguous native count key")
		}
		result[name] = int64(count)
	}
	return result, nil
}

func fleetProjectionMessage(p controlplane.FleetProjection) (*fleetv1.ListFleetInstallationsResponse, error) {
	if p.PageSize < 0 || p.PageSize > 200 || p.Limit < 1 || p.Limit > 200 || p.Summary.Total < 0 || p.Summary.Stale < 0 || p.MaxResultAgeSeconds < 0 {
		return nil, fmt.Errorf("invalid native Fleet counts")
	}
	result := &fleetv1.ListFleetInstallationsResponse{MaxResultAgeSeconds: p.MaxResultAgeSeconds, PageSize: int32(p.PageSize), Limit: int32(p.Limit), NextCursor: nativeIndexString(p.NextCursor), PreviousCursor: nativeIndexString(p.PreviousCursor), HasPrevious: p.HasPrevious, Summary: &fleetv1.FleetSummary{Total: int64(p.Summary.Total), Stale: int64(p.Summary.Stale)}, Facets: &fleetv1.FleetFacets{}}
	var err error
	result.GeneratedAt, err = environmentSummaryTimestamp(p.GeneratedAt)
	if err != nil {
		return nil, err
	}
	for _, pair := range []struct {
		source map[string]int
		target *map[string]int64
	}{
		{p.Summary.Health, &result.Summary.Health}, {p.Summary.Rollout, &result.Summary.Rollout}, {p.Summary.Config, &result.Summary.Config}, {p.Summary.Lifecycle, &result.Summary.Lifecycle},
		{p.Facets.Environment, &result.Facets.Environment}, {p.Facets.Product, &result.Facets.Product}, {p.Facets.Health, &result.Facets.Health}, {p.Facets.Rollout, &result.Facets.RolloutStatus}, {p.Facets.Config, &result.Facets.ConfigStatus}, {p.Facets.Lifecycle, &result.Facets.Lifecycle},
	} {
		*pair.target, err = fleetCountsMessage(pair.source)
		if err != nil {
			return nil, err
		}
	}
	for _, i := range p.Installations {
		if i.ObservationAgeSeconds < 0 {
			return nil, fmt.Errorf("invalid native observation age")
		}
		item := &fleetv1.FleetInstallation{Environment: nativeIndexString(i.Environment), InstallationId: nativeIndexString(i.InstallationID), Product: nativeIndexString(i.Product), Namespace: nativeIndexString(i.Namespace), DesiredVersion: nativeIndexString(i.DesiredVersion), ObservedVersion: nativeIndexString(i.ObservedVersion), DesiredReleaseDigest: nativeIndexString(i.DesiredReleaseDigest), ObservedReleaseDigest: nativeIndexString(i.ObservedReleaseDigest), DesiredConfigDigest: nativeIndexString(i.DesiredConfigDigest), ObservedConfigDigest: nativeIndexString(i.ObservedConfigDigest), Health: nativeIndexString(i.Health), RolloutStatus: nativeIndexString(i.RolloutStatus), ConfigStatus: nativeIndexString(i.ConfigStatus), Lifecycle: nativeIndexString(i.Lifecycle), ObservationAgeSeconds: i.ObservationAgeSeconds, Stale: i.Stale}
		item.ObservedAt, err = environmentSummaryTimestamp(i.ObservedAt)
		if err != nil {
			return nil, err
		}
		result.Installations = append(result.Installations, item)
	}
	return result, nil
}

package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	fleetv1 "github.com/jonathanagustin/conduit/gen/go/conduit/fleet/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/fleet/v1/fleetv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func fleetClient(t *testing.T, cfg FleetReadConfig) fleetv1connect.FleetReadServiceClient {
	t.Helper()
	mux := http.NewServeMux()
	RegisterFleetReadConnect(mux, cfg)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return fleetv1connect.NewFleetReadServiceClient(connect.NewClient(connecthttp.NewTransport(server.Client(), server.URL)))
}
func fleetWireFixture() controlplane.FleetProjection {
	now := time.Date(2026, 11, 19, 1, 2, 3, 123456789, time.FixedZone("source", 3600))
	p := controlplane.FleetProjection{GeneratedAt: now, MaxResultAgeSeconds: 9007199254740993, Installations: []controlplane.FleetInstallation{{Environment: "dev", InstallationID: "app", Product: "native:app", Namespace: "ns", DesiredVersion: "2", ObservedVersion: "1", DesiredReleaseDigest: "sha256:desired", ObservedReleaseDigest: "sha256:observed", DesiredConfigDigest: "sha256:config", ObservedConfigDigest: "sha256:old", Health: "FUTURE_HEALTH", RolloutStatus: "FUTURE_ROLLOUT", ConfigStatus: "FUTURE_CONFIG", Lifecycle: "FUTURE_LIFECYCLE", ObservedAt: now, ObservationAgeSeconds: 9007199254740993, Stale: true}}}
	p.Summary.Total = 3
	p.Summary.Stale = 2
	p.Summary.Health = map[string]int{"FUTURE_HEALTH": 3}
	p.Summary.Rollout = map[string]int{"FUTURE_ROLLOUT": 3}
	p.Summary.Config = map[string]int{"FUTURE_CONFIG": 3}
	p.Summary.Lifecycle = map[string]int{"FUTURE_LIFECYCLE": 3}
	p.Facets.Environment = map[string]int{"dev": 3}
	p.Facets.Product = map[string]int{"native:app": 3}
	p.Facets.Health = map[string]int{"FUTURE_HEALTH": 3}
	p.Facets.Rollout = map[string]int{"FUTURE_ROLLOUT": 3}
	p.Facets.Config = map[string]int{"FUTURE_CONFIG": 3}
	p.Facets.Lifecycle = map[string]int{"FUTURE_LIFECYCLE": 3}
	p.PageSize = 1
	p.Limit = 1
	p.NextCursor = "next"
	p.PreviousCursor = "previous"
	p.HasPrevious = true
	return p
}
func TestFleetGeneratedClientPreservesCarrierInputsAndAllVisibleFields(t *testing.T) {
	native := fleetWireFixture()
	large := int64(9007199254740993)
	if strconv.IntSize < 64 {
		large = 1<<30 + 1
	}
	native.Summary.Health["FUTURE_HEALTH"] = int(large)
	var input controlplane.FleetReadRequest
	client := fleetClient(t, FleetReadConfig{Authenticate: authenticated, Read: func(ctx context.Context, r controlplane.FleetReadRequest) (controlplane.FleetProjection, error) {
		if ctx.Value(carrierKey{}) != "robot-on-behalf-of-alice" {
			t.Error("carrier lost")
		}
		input = r
		return native, nil
	}})
	limit, maxAge := int32(7), int32(60)
	got, err := client.ListFleetInstallations(t.Context(), &fleetv1.ListFleetInstallationsRequest{Environment: "e", InstallationId: "i", Product: "p", Health: "h", RolloutStatus: "r", ConfigStatus: "c", Lifecycle: "l", Search: " s ", Cursor: "cursor", Limit: &limit, MaxAgeSeconds: &maxAge, SpaceId: "space"})
	if err != nil {
		t.Fatal(err)
	}
	want := controlplane.FleetReadRequest{Environment: "e", InstallationID: "i", Product: "p", Health: "h", RolloutStatus: "r", ConfigStatus: "c", Lifecycle: "l", Search: " s ", Cursor: "cursor", Limit: &limit, MaxAgeSeconds: &maxAge, SpaceID: "space"}
	if !reflect.DeepEqual(input, want) {
		t.Fatalf("input=%#v", input)
	}
	if !reflect.DeepEqual(got.Installations[0].ObservedAt.AsTime(), native.Installations[0].ObservedAt.UTC()) || got.MaxResultAgeSeconds != 9007199254740993 || got.Installations[0].ObservationAgeSeconds != 9007199254740993 || got.Summary.Health["FUTURE_HEALTH"] != large || !got.GeneratedAt.AsTime().Equal(native.GeneratedAt) || got.Summary.Total != 3 || got.PageSize != 1 || got.Limit != 1 || !got.HasPrevious || got.NextCursor != "next" || got.PreviousCursor != "previous" {
		t.Fatalf("projection=%v", got)
	}
	for _, pair := range []struct {
		got map[string]int64
		key string
	}{
		{got.Facets.Environment, "dev"}, {got.Facets.Product, "native:app"},
		{got.Facets.Health, "FUTURE_HEALTH"}, {got.Facets.RolloutStatus, "FUTURE_ROLLOUT"},
		{got.Facets.ConfigStatus, "FUTURE_CONFIG"}, {got.Facets.Lifecycle, "FUTURE_LIFECYCLE"},
		{got.Summary.Rollout, "FUTURE_ROLLOUT"}, {got.Summary.Config, "FUTURE_CONFIG"}, {got.Summary.Lifecycle, "FUTURE_LIFECYCLE"},
	} {
		if len(pair.got) != 1 || pair.got[pair.key] != 3 {
			t.Fatalf("counts changed: %v", pair.got)
		}
	}
	if got.Summary.Stale != 2 {
		t.Fatalf("stale count=%d", got.Summary.Stale)
	}
	a, b := got.Installations[0], native.Installations[0]
	if a.Environment != b.Environment || a.InstallationId != b.InstallationID || a.Product != b.Product || a.Namespace != b.Namespace || a.DesiredVersion != b.DesiredVersion || a.ObservedVersion != b.ObservedVersion || a.DesiredReleaseDigest != b.DesiredReleaseDigest || a.ObservedReleaseDigest != b.ObservedReleaseDigest || a.DesiredConfigDigest != b.DesiredConfigDigest || a.ObservedConfigDigest != b.ObservedConfigDigest || a.Health != b.Health || a.RolloutStatus != b.RolloutStatus || a.ConfigStatus != b.ConfigStatus || a.Lifecycle != b.Lifecycle || !a.Stale {
		t.Fatalf("installation=%v facets=%v", a, got.Facets)
	}
}

func TestFleetMapperRefusesBadNativeDataAndPreservesMissingObservation(t *testing.T) {
	p := fleetWireFixture()
	p.Installations[0].ObservedAt = time.Time{}
	got, err := fleetProjectionMessage(p)
	if err != nil || got.Installations[0].ObservedAt != nil {
		t.Fatal("missing observation invented", got, err)
	}
	for _, change := range []func(*controlplane.FleetProjection){func(p *controlplane.FleetProjection) { p.GeneratedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }, func(p *controlplane.FleetProjection) {
		p.Installations[0].ObservedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	}, func(p *controlplane.FleetProjection) { p.Summary.Total = -1 }, func(p *controlplane.FleetProjection) { p.Facets.Health["bad"] = -1 }, func(p *controlplane.FleetProjection) { p.PageSize = 201 }, func(p *controlplane.FleetProjection) { p.Installations[0].ObservationAgeSeconds = -1 }, func(p *controlplane.FleetProjection) { p.Facets.Health = map[string]int{"\xff": 1, "\ufffd": 2} }} {
		p := fleetWireFixture()
		change(&p)
		if got, err := fleetProjectionMessage(p); err == nil || got != nil {
			t.Fatalf("bad native result admitted: %v %v", got, err)
		}
	}
}
func TestFleetErrorsAreTypedSanitizedAndAuthenticationPrecedesDecode(t *testing.T) {
	for _, tc := range []struct {
		cause error
		code  connect.Code
	}{{controlplane.ErrForbidden, connect.CodePermissionDenied}, {controlplane.ErrNotFound, connect.CodeNotFound}, {controlplane.ErrInvalidFleetReadQuery, connect.CodeInvalidArgument}, {controlplane.ErrInvalidSpaceSelection, connect.CodeInvalidArgument}, {controlplane.ErrSpaceMembershipUnavailable, connect.CodeUnavailable}, {errors.New("PRIVATE"), connect.CodeInternal}} {
		client := fleetClient(t, FleetReadConfig{Authenticate: authenticated, Read: func(context.Context, controlplane.FleetReadRequest) (controlplane.FleetProjection, error) {
			return controlplane.FleetProjection{}, tc.cause
		}})
		_, err := client.ListFleetInstallations(t.Context(), &fleetv1.ListFleetInstallationsRequest{})
		if connect.CodeOf(err) != tc.code || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatalf("error=%v", err)
		}
	}
	calls := 0
	mux := http.NewServeMux()
	RegisterFleetReadConnect(mux, FleetReadConfig{Read: func(context.Context, controlplane.FleetReadRequest) (controlplane.FleetProjection, error) {
		calls++
		return fleetWireFixture(), nil
	}})
	request := httptest.NewRequest("POST", strings.TrimPrefix(FleetReadConnectRoute, "POST "), strings.NewReader("{"))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != 401 || calls != 0 {
		t.Fatalf("auth-before-decode=%d calls=%d", response.Code, calls)
	}
	client := fleetClient(t, FleetReadConfig{Authenticate: authenticated, Read: func(context.Context, controlplane.FleetReadRequest) (controlplane.FleetProjection, error) {
		calls++
		return fleetWireFixture(), nil
	}})
	_, err := client.ListFleetInstallations(t.Context(), &fleetv1.ListFleetInstallationsRequest{Search: strings.Repeat("x", 1<<20)})
	if err == nil || calls != 0 {
		t.Fatal("oversized input reached read")
	}
}

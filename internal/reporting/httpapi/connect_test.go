package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/reporting/domain"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestOverviewConversionPreservesEveryNativePanelField(t *testing.T) {
	view := domain.OverviewProjection{
		GeneratedAt: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC), StaleAfterSeconds: 300,
		AwaitingApprovals: domain.Panel[domain.OverviewChangeRequest]{Status: "CURRENT", Count: 7, Items: []domain.OverviewChangeRequest{{ID: "review", Title: "Independent review", Environment: "east"}}},
		FailedPlans:       domain.Panel[domain.OverviewPlan]{Status: "CURRENT", Count: 8, Items: []domain.OverviewPlan{{ID: "failed", Environment: "west"}}},
		RunningPlans:      domain.Panel[domain.OverviewPlan]{Status: "CURRENT", Count: 9, Items: []domain.OverviewPlan{{ID: "running", Environment: "north"}}},
		UnhealthyEntities: domain.Panel[domain.OverviewInstallation]{Status: "CURRENT", Count: 10, Items: []domain.OverviewInstallation{{Environment: "south", InstallationID: "service", Health: "UNHEALTHY"}}},
		StaleReports:      domain.Panel[struct{}]{Status: "UNAVAILABLE", Count: 0},
		BlockedPromotions: domain.Panel[domain.OverviewPromotion]{Status: "CURRENT", Count: 11, Items: []domain.OverviewPromotion{{ID: "promotion", Product: "app", Version: "2.0.0", FromChannel: "dev", ToChannel: "stable"}}},
	}
	wire, err := response(view)
	if err != nil {
		t.Fatal(err)
	}
	nativeJSON, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	wireJSON, err := (protojson.MarshalOptions{EmitUnpopulated: true}).Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	var native, converted map[string]any
	if err := json.Unmarshal(nativeJSON, &native); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(wireJSON, &converted); err != nil {
		t.Fatal(err)
	}
	normalizeOverviewJSON(native)
	normalizeOverviewJSON(converted)
	if !reflect.DeepEqual(native, converted) {
		t.Fatalf("native=%s\nproto=%s", nativeJSON, wireJSON)
	}
}

func normalizeOverviewJSON(row map[string]any) {
	for key, value := range row {
		if key == "items" && value == nil {
			row[key] = []any{}
		}
		if key == "count" || key == "staleAfterSeconds" {
			if text, ok := value.(string); ok {
				number, err := strconv.ParseFloat(text, 64)
				if err == nil {
					row[key] = number
				}
			}
		}
		if child, ok := value.(map[string]any); ok {
			normalizeOverviewJSON(child)
		}
	}
}

type observedBody struct {
	io.Reader
	reads int
}

func (body *observedBody) Read(buffer []byte) (int, error) {
	body.reads++
	return body.Reader.Read(buffer)
}
func TestOverviewAuthenticationPrecedesBodyAndProjection(t *testing.T) {
	for _, test := range []struct {
		name         string
		authenticate func(*http.Request) (context.Context, error)
		status       int
	}{
		{name: "unconfigured", status: 401},
		{name: "anonymous", authenticate: func(*http.Request) (context.Context, error) { return nil, ErrUnauthenticated }, status: 401},
		{name: "forbidden delegation", authenticate: func(*http.Request) (context.Context, error) { return nil, ErrPermissionDenied }, status: 403},
		{name: "resolver unavailable", authenticate: func(*http.Request) (context.Context, error) { return nil, errors.New("sensitive backend detail") }, status: 500},
	} {
		t.Run(test.name, func(t *testing.T) {
			reads := 0
			mux := http.NewServeMux()
			RegisterConnect(mux, Config{Authenticate: test.authenticate, Read: func(context.Context) (domain.OverviewProjection, error) {
				reads++
				return domain.OverviewProjection{}, nil
			}})
			body := &observedBody{Reader: strings.NewReader(`{}`)}
			request := httptest.NewRequest(http.MethodPost, strings.TrimPrefix(ConnectRoute, "POST "), body)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Connect-Protocol-Version", "1")
			result := httptest.NewRecorder()
			mux.ServeHTTP(result, request)
			if result.Code != test.status || reads != 0 || body.reads != 0 {
				t.Fatalf("status=%d projectionReads=%d bodyReads=%d", result.Code, reads, body.reads)
			}
			if strings.Contains(result.Body.String(), "sensitive backend detail") {
				t.Fatal("identity backend error leaked")
			}
		})
	}
}

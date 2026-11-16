package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	packagingv1 "github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"google.golang.org/protobuf/encoding/protojson"
)

const listProcedure = "/conduit.packaging.v1.PackagingRunService/ListPackagingRuns"

func callList(mux http.Handler, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, listProcedure, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connect-Protocol-Version", "1")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

func TestConnectListSharesNativeFiltersAndCursors(t *testing.T) {
	reader := &catalog{runs: []domain.RunStatus{
		{RunID: "a", TargetPlanRef: "visible/app", Phase: domain.PhaseReview, Reason: "first"},
		{RunID: "b", TargetPlanRef: "hidden/app", Phase: domain.PhaseReview, Reason: "secret hidden row"},
		{RunID: "c", TargetPlanRef: "visible/app", Phase: domain.PhaseReview},
		{RunID: "d", TargetPlanRef: "visible/app", Phase: domain.PhaseComplete},
	}}
	mux := mountBoth(Config{Reader: reader, Authorize: authed(), Visible: allowEnvironment("visible")})
	values := url.Values{"view": {"summary"}, "limit": {"1"}, "phase": {domain.PhaseReview}}
	for page := 0; page < 2; page++ {
		native := httptest.NewRecorder()
		mux.ServeHTTP(native, httptest.NewRequest(http.MethodGet, "/v1/packaging-runs?"+values.Encode(), nil))
		if native.Code != 200 {
			t.Fatal(native.Body.String())
		}
		var expected runPage
		if err := json.Unmarshal(native.Body.Bytes(), &expected); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]any{"view": "summary", "limit": 1, "phase": domain.PhaseReview, "cursor": values.Get("cursor")})
		wire := callList(mux, string(body))
		if wire.Code != 200 {
			t.Fatal(wire.Body.String())
		}
		actual := &packagingv1.ListPackagingRunsResponse{}
		if err := protojson.Unmarshal(wire.Body.Bytes(), actual); err != nil {
			t.Fatal(err)
		}
		if actual.Total != int64(expected.Total) || actual.Total != 2 || actual.PageSize != int32(expected.PageSize) || actual.Limit != 1 || actual.HasPrevious != expected.HasPrevious || actual.NextCursor != expected.NextCursor || actual.PreviousCursor != expected.PreviousCursor || len(actual.Items) != 1 || actual.Items[0].RunId != expected.Items[0].RunID || actual.Items[0].Reason != expected.Items[0].Reason || actual.Tier != expected.Tier || actual.ApprovalAuthority != expected.ApprovalAuthority {
			t.Fatalf("native=%+v rpc=%+v", expected, actual)
		}
		if strings.Contains(wire.Body.String(), "hidden") {
			t.Fatal("foreign row leaked")
		}
		values.Set("cursor", expected.NextCursor)
	}
	if reader.mutations != 0 {
		t.Fatal("read mutated catalog")
	}
}

func TestConnectListInvalidInputAndUnconfiguredWorker(t *testing.T) {
	mux := mountBoth(Config{Authorize: authed()})
	for _, body := range []string{`{"limit":0}`, `{"limit":-1}`, `{"limit":21}`, `{"view":"unknown"}`, `{"cursor":"malformed"}`} {
		response := callList(mux, body)
		if response.Code != 400 || !strings.Contains(response.Body.String(), "invalid_argument") {
			t.Fatalf("input %s => %d %s", body, response.Code, response.Body.String())
		}
	}
	response := callList(mux, `{}`)
	actual := &packagingv1.ListPackagingRunsResponse{}
	if err := protojson.Unmarshal(response.Body.Bytes(), actual); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || actual.Configured || actual.Limit != 20 || actual.Total != 0 || actual.Authority != "packaging-run" {
		t.Fatalf("unconfigured=%+v status=%d", actual, response.Code)
	}
}

func TestConnectListAuthenticatesBeforeBodyAndStore(t *testing.T) {
	body := &connectObservedBody{Reader: strings.NewReader(`{broken`)}
	request := httptest.NewRequest(http.MethodPost, listProcedure, body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connect-Protocol-Version", "1")
	response := httptest.NewRecorder()
	mountBoth(Config{}).ServeHTTP(response, request)
	if response.Code != 401 || body.reads != 0 {
		t.Fatalf("status=%d reads=%d", response.Code, body.reads)
	}
	for _, path := range []string{listProcedure, "/conduit.packaging.v1.PackagingRunService/DeletePackagingRun"} {
		response := httptest.NewRecorder()
		method := http.MethodGet
		expected := 405
		if strings.Contains(path, "Delete") {
			method = http.MethodPost
			expected = 404
		}
		mountBoth(Config{Authorize: authed()}).ServeHTTP(response, httptest.NewRequest(method, path, nil))
		if response.Code != expected {
			t.Fatalf("%s %s => %d", method, path, response.Code)
		}
	}
}

type unavailableCatalog struct{ catalog }

func (unavailableCatalog) List(context.Context) ([]domain.RunStatus, error) {
	return nil, errors.New("private database credentials")
}

func TestConnectListSanitizesReaderFailures(t *testing.T) {
	response := callList(mountBoth(Config{Authorize: authed(), Reader: &unavailableCatalog{}}), `{}`)
	if response.Code != 503 || !strings.Contains(response.Body.String(), "unavailable") || strings.Contains(response.Body.String(), "credentials") {
		t.Fatalf("failure: %d %s", response.Code, response.Body.String())
	}
}

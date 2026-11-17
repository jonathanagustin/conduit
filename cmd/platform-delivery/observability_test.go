package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	metricsapplication "github.com/jonathanagustin/conduit/internal/observability/application"
)

// The listener serves what the request wrapper recorded: the series reach a
// scraper through the same validated path as every other one.
func TestTheMetricsEndpointExportsRequestMetricsFromTheObservedHub(t *testing.T) {
	recorder := composition.NewRequestRecorder()
	hub := composition.ObserveRequests(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}), controlplane.HandlerOptions{}, composition.RequestObservation{
		Recorder: recorder, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	for _, target := range []string{"/v1/environments/tenant-a-prod", "/v1/environments/tenant-b-prod", "/nowhere/tenant-c"} {
		hub.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, nil))
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	stop := startMetricsListener(t.Context(), address, func(context.Context) (metricsapplication.ReconciliationStats, error) {
		return metricsapplication.ReconciliationStats{}, nil
	}, nil, requestMetrics(recorder), nil)
	if stop == nil {
		t.Fatal("no listener started")
	}
	defer stop()
	var body string
	for attempt := 0; attempt < 50 && body == ""; attempt++ {
		response, err := http.Get("http://" + address + "/metrics")
		if err != nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		payload, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("metrics = %d: %s", response.StatusCode, payload)
		}
		body = string(payload)
	}
	for _, required := range []string{
		`conduit_http_requests_total{code_class="2xx",method="GET",route="/v1/environments/{environmentID}"} 2`,
		`conduit_http_requests_total{code_class="2xx",method="GET",route="unmatched"} 1`,
		`# TYPE conduit_http_request_duration_seconds histogram`,
		`conduit_http_requests_in_flight 0`,
		`conduit_build_info{`,
	} {
		if !strings.Contains(body, required) {
			t.Errorf("missing %q in:\n%s", required, body)
		}
	}
	if strings.Contains(body, "tenant-") {
		t.Fatalf("a tenant identifier reached the exposition:\n%s", body)
	}
}

// Delivery that is switched off exports nothing, rather than a backlog that
// grows forever and pages somebody about a feature nobody enabled.
func TestNotificationMetricsAreAbsentWhenDeliveryIsOff(t *testing.T) {
	if extension := notificationDeliveryMetrics(nil, nil, metricsapplication.NewDeliveryRecorder()); extension != nil {
		t.Fatal("a Hub with no notification store or transport exports delivery metrics")
	}
}

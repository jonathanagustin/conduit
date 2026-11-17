package composition

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func fleetMetricsClient(t *testing.T, handler http.HandlerFunc) (*http.Client, string, *capacityMetrics) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	t.Cleanup(transport.CloseIdleConnections)
	metrics := &capacityMetrics{rows: make(map[string]*capacityRPC)}
	return &http.Client{Timeout: 250 * time.Millisecond, Transport: capacityTransport{base: transport, metrics: metrics}}, server.URL + "/leases", metrics
}

func TestFleetCapacityTransportOnlySkipsHarnessShutdown(t *testing.T) {
	for _, stop := range []struct {
		name   string
		cause  error
		errors uint64
	}{
		{name: "ordinary-cancellation", cause: context.Canceled, errors: 1},
		{name: "harness-shutdown", cause: errCapacityHarnessShutdown},
	} {
		t.Run(stop.name, func(t *testing.T) {
			entered := make(chan struct{})
			client, address, metrics := fleetMetricsClient(t, func(_ http.ResponseWriter, request *http.Request) {
				close(entered)
				<-request.Context().Done()
			})
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(context.Canceled)
			go func() {
				select {
				case <-entered:
					cancel(stop.cause)
				case <-ctx.Done():
				}
			}()
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.Do(request)
			if response != nil {
				_ = response.Body.Close()
			}
			if !errors.Is(err, stop.cause) || context.Cause(ctx) != stop.cause {
				t.Fatalf("request did not stop for the intended cause: %v, %v", err, context.Cause(ctx))
			}
			if stop.errors != 0 {
				requireFleetRPCMeasurement(t, metrics, stop.errors)
			} else if rows := metrics.snapshot(); len(rows) != 0 {
				t.Fatalf("coordinated shutdown became a service measurement: %#v", rows)
			}
		})
	}
}

func TestFleetCapacityTransportSkipsShutdownBodyFailure(t *testing.T) {
	client, address, metrics := fleetMetricsClient(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		writer.(http.Flusher).Flush()
		<-request.Context().Done()
	})
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(context.Canceled)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	cancel(errCapacityHarnessShutdown)
	if _, err := io.ReadAll(response.Body); err == nil {
		t.Fatal("shutdown did not interrupt the real response body")
	}
	_ = response.Body.Close()
	_ = response.Body.Close()
	if rows := metrics.snapshot(); len(rows) != 0 {
		t.Fatalf("shutdown body failure became a service measurement: %#v", rows)
	}
}

func TestFleetCapacityTransportRecordsCompletedResponseOnce(t *testing.T) {
	client, address, metrics := fleetMetricsClient(t, func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("complete response"))
	})
	response, err := client.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(response.Body)
	if err != nil || string(contents) != "complete response" {
		t.Fatalf("incomplete response: %q, %v", contents, err)
	}
	_ = response.Body.Close()
	_ = response.Body.Close()
	requireFleetRPCMeasurement(t, metrics, 0)
}

func requireFleetRPCMeasurement(t *testing.T, metrics *capacityMetrics, errors uint64) {
	t.Helper()
	row, ok := metrics.snapshot()["leases"]
	if !ok || row.Requests != 1 || row.Errors != errors || row.MeanMS <= 0 || row.MaxMS < row.MeanMS {
		t.Fatalf("want one measured RPC with %d errors and latency, got %#v", errors, metrics.snapshot())
	}
}

func TestFleetCapacityTransportRecordsClientDeadline(t *testing.T) {
	entered := make(chan struct{})
	client, address, metrics := fleetMetricsClient(t, func(_ http.ResponseWriter, request *http.Request) {
		close(entered)
		<-request.Context().Done()
	})
	response, err := client.Get(address)
	if response != nil {
		_ = response.Body.Close()
	}
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("want a real client timeout before response headers, got %v", err)
	}
	select {
	case <-entered:
	default:
		t.Fatal("the timed-out request never reached the HTTP server")
	}
	requireFleetRPCMeasurement(t, metrics, 1)
}

func TestFleetCapacityTransportRecordsResponseBodyFailures(t *testing.T) {
	for _, fault := range []string{"truncated", "deadline"} {
		t.Run(fault, func(t *testing.T) {
			client, address, metrics := fleetMetricsClient(t, func(writer http.ResponseWriter, request *http.Request) {
				if fault == "truncated" {
					writer.Header().Set("Content-Length", "20")
					_, _ = writer.Write([]byte("{"))
					return
				}
				writer.WriteHeader(http.StatusOK)
				writer.(http.Flusher).Flush()
				<-request.Context().Done()
			})
			response, err := client.Get(address)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("want headers to succeed before the body failure, got %d", response.StatusCode)
			}
			_, err = io.ReadAll(response.Body)
			if fault == "truncated" && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("want a truncated HTTP body, got %v", err)
			}
			if fault == "deadline" {
				var timeout net.Error
				if !errors.As(err, &timeout) || !timeout.Timeout() {
					t.Fatalf("want a real timeout while reading the body, got %v", err)
				}
			}
			_ = response.Body.Close()
			_ = response.Body.Close()
			requireFleetRPCMeasurement(t, metrics, 1)
		})
	}
}

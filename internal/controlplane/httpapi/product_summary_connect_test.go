package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	catalogv1 "github.com/jonathanagustin/conduit/gen/go/conduit/catalog/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/catalog/v1/catalogv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func productSummaryClient(t *testing.T, cfg ProductSummaryConfig) catalogv1connect.ProductReadServiceClient {
	t.Helper()
	mux := http.NewServeMux()
	RegisterProductSummaryConnect(mux, cfg)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return catalogv1connect.NewProductReadServiceClient(connect.NewClient(connecthttp.NewTransport(server.Client(), server.URL)))
}

func TestProductSummaryGeneratedClientPreservesNativeValues(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("native int counts exceed this architecture's range")
	}
	large := int64(9007199254740993)
	client := productSummaryClient(t, ProductSummaryConfig{Authenticate: authenticated, Read: func(ctx context.Context, search, cursor string, limit int) (controlplane.ProductCatalogSummaryPage, error) {
		if ctx.Value(carrierKey{}) != "robot-on-behalf-of-alice" || search != " source " || cursor != "opaque/+=%" || limit != 17 {
			t.Fatalf("native inputs changed: %v %q %q %d", ctx.Value(carrierKey{}), search, cursor, limit)
		}
		return controlplane.ProductCatalogSummaryPage{Products: []controlplane.ProductCatalogSummary{{Product: "team:app", ReleaseCount: int(large), LatestVersion: "1.10.0"}}, Total: int(large + 2), PageMetadata: controlplane.PageMetadata{Limit: 17, PageSize: 1, NextCursor: "next/+=", PreviousCursor: "previous/+=", HasPrevious: true}}, nil
	}})
	limit := int32(17)
	page, err := client.ListProductSummaries(t.Context(), &catalogv1.ListProductSummariesRequest{Limit: &limit, Search: " source ", Cursor: "opaque/+=%"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 9007199254740995 || page.Products[0].ReleaseCount != 9007199254740993 || page.Products[0].Product != "team:app" || page.Products[0].LatestVersion != "1.10.0" || page.Limit != 17 || page.PageSize != 1 || page.NextCursor != "next/+=" || page.PreviousCursor != "previous/+=" || !page.HasPrevious {
		t.Fatalf("projection changed: %v", page)
	}
}

func TestProductSummaryGeneratedClientCancelsNativeRead(t *testing.T) {
	started := make(chan struct{})
	observed := make(chan bool, 1)
	client := productSummaryClient(t, ProductSummaryConfig{Authenticate: authenticated, Read: func(ctx context.Context, _, _ string, _ int) (controlplane.ProductCatalogSummaryPage, error) {
		close(started)
		select {
		case <-ctx.Done():
			observed <- true
			return controlplane.ProductCatalogSummaryPage{}, ctx.Err()
		case <-time.After(2 * time.Second):
			observed <- false
			return controlplane.ProductCatalogSummaryPage{}, errors.New("cancellation not observed")
		}
	}})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := client.ListProductSummaries(ctx, &catalogv1.ListProductSummariesRequest{})
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("read did not start")
	}
	cancel()
	select {
	case err := <-finished:
		if connect.CodeOf(err) != connect.CodeCanceled {
			t.Fatalf("cancellation category: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client did not cancel")
	}
	select {
	case canceled := <-observed:
		if !canceled {
			t.Fatal("native reader lost cancellation")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("native reader did not stop")
	}
}

func TestProductSummaryGeneratedClientErrorsAndDefaults(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failure error
		code    connect.Code
	}{{"store", errors.New("PRIVATE-STORE-DIAGNOSTIC"), connect.CodeUnavailable}, {"cursor", controlplane.ErrInvalidProductSummaryQuery, connect.CodeInvalidArgument}} {
		t.Run(tc.name, func(t *testing.T) {
			client := productSummaryClient(t, ProductSummaryConfig{Authenticate: authenticated, Read: func(_ context.Context, search, cursor string, limit int) (controlplane.ProductCatalogSummaryPage, error) {
				if limit != 100 {
					t.Fatalf("default limit=%d", limit)
				}
				return controlplane.ProductCatalogSummaryPage{}, tc.failure
			}})
			_, err := client.ListProductSummaries(t.Context(), &catalogv1.ListProductSummariesRequest{})
			if connect.CodeOf(err) != tc.code || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("error mapping: %v", err)
			}
		})
	}
	for _, limit := range []int32{0, -1, 201} {
		client := productSummaryClient(t, ProductSummaryConfig{Authenticate: authenticated, Read: func(context.Context, string, string, int) (controlplane.ProductCatalogSummaryPage, error) {
			t.Fatal("invalid limit reached reader")
			return controlplane.ProductCatalogSummaryPage{}, nil
		}})
		if _, err := client.ListProductSummaries(t.Context(), &catalogv1.ListProductSummariesRequest{Limit: &limit}); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("limit=%d: %v", limit, err)
		}
	}
}

func TestProductSummaryAuthenticationPrecedesDecodeAndRead(t *testing.T) {
	for _, tc := range []struct {
		name   string
		auth   func(*http.Request) (context.Context, error)
		status int
		code   string
	}{{"missing", nil, 401, "unauthenticated"}, {"nil-context", func(*http.Request) (context.Context, error) { return nil, nil }, 401, "unauthenticated"}, {"forbidden", func(*http.Request) (context.Context, error) { return nil, controlplane.ErrForbidden }, 403, "permission_denied"}, {"failure", func(*http.Request) (context.Context, error) { return nil, errors.New("PRIVATE-AUTH-DIAGNOSTIC") }, 500, "internal"}} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			RegisterProductSummaryConnect(mux, ProductSummaryConfig{Authenticate: tc.auth, Read: func(context.Context, string, string, int) (controlplane.ProductCatalogSummaryPage, error) {
				t.Fatal("unauthorized request reached reader")
				return controlplane.ProductCatalogSummaryPage{}, nil
			}})
			request := httptest.NewRequest(http.MethodPost, "/conduit.catalog.v1.ProductReadService/ListProductSummaries", strings.NewReader("not protobuf JSON"))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != tc.status || !strings.Contains(response.Body.String(), tc.code) || strings.Contains(response.Body.String(), "PRIVATE") {
				t.Fatalf("auth response: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

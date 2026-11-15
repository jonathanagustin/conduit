package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/adminstatus/domain"
	api "github.com/jonathanagustin/conduit/internal/adminstatus/transport"
)

type source struct {
	calls atomic.Int64
	fail  bool
}

func (s *source) Read(context.Context) (domain.Summary, error) {
	s.calls.Add(1)
	if s.fail {
		return domain.Summary{}, errors.New("backend secret must not be rendered")
	}
	return domain.Summary{GeneratedAt: time.Now().UTC(), Components: []domain.Component{}, Storage: []domain.Storage{domain.MissingStorage("registry", "Registry", "No usage telemetry")}, Counters: []domain.Counter{domain.Count("pending", "Pending", 0, "No pending reviews", "changes")}, Audit: domain.Audit{Status: domain.Unavailable, Detail: "No verifier configured"}}, nil
}
func TestGeneratedClientAndReadOnlyAuthorization(t *testing.T) {
	backend := &source{}
	for _, code := range []int{401, 403, 0} {
		mux := http.NewServeMux()
		Register(mux, Config{Reader: backend, Authorize: func(*http.Request) int { return code }})
		instance := httptest.NewServer(mux)
		client, err := api.NewClientWithResponses(instance.URL)
		if err != nil {
			t.Fatal(err)
		}
		before := backend.calls.Load()
		response, err := client.GetAdminStatusWithResponse(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if code != 0 {
			if response.StatusCode() != code || backend.calls.Load() != before {
				t.Fatalf("denied status read touched its source: %+v", response)
			}
		} else {
			if response.JSON200 == nil || response.JSON200.Storage[0].UsedBytes != nil || response.JSON200.Counters[0].Value == nil || *response.JSON200.Counters[0].Value != 0 {
				t.Fatalf("unknown capacity/real zero changed: %+v", response)
			}
			if response.HTTPResponse.Header.Get("Cache-Control") != "no-store" {
				t.Fatal("administrator measurements may not be cached")
			}
		}
		request, _ := http.NewRequest(http.MethodPost, instance.URL+"/v1/admin/status", nil)
		result, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		result.Body.Close()
		if result.StatusCode != 405 {
			t.Fatalf("status accepted a mutation method: %d", result.StatusCode)
		}
		instance.Close()
	}
	backend.fail = true
	mux := http.NewServeMux()
	Register(mux, Config{Reader: backend, Authorize: func(*http.Request) int { return 0 }})
	instance := httptest.NewServer(mux)
	defer instance.Close()
	client, _ := api.NewClientWithResponses(instance.URL)
	response, err := client.GetAdminStatusWithResponse(t.Context())
	if err != nil || response.JSON503 == nil || response.JSON503.Error != "Status source unavailable" {
		t.Fatalf("backend failure exposed: %+v %v", response, err)
	}
}

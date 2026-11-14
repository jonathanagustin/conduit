package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/delivery/maintenance"
)

type controller struct{ calls int }

func (c *controller) CreateMaintenanceOverrides(context.Context, maintenance.OverrideDraft) (maintenance.OverrideBatch, error) {
	c.calls++
	return maintenance.OverrideBatch{}, nil
}
func (c *controller) Lease(context.Context, time.Duration) (any, error)  { c.calls++; return nil, nil }
func (c *controller) Start(context.Context, string, string) (any, error) { c.calls++; return nil, nil }
func TestTemporaryMaintenanceHTTPStrictBeforeEffect(t *testing.T) {
	for _, test := range []struct {
		name, path, body string
		status           int
	}{
		{"unknown", "/v1/temporary-maintenance-overrides", `{"unreviewed":true}`, 400},
		{"extra-document", "/v1/temporary-maintenance-overrides", `{} {}`, 400},
		{"query", "/v1/temporary-maintenance-overrides?approve=true", `{}`, 400},
		{"large", "/v1/temporary-maintenance-overrides", `{"reason":"` + strings.Repeat("x", 128<<10) + `"}`, 413},
		{"missing-reason", "/v1/temporary-maintenance-overrides", `{"entities":[{"environment":"prod","entity":"api"}],"duration":"1h"}`, 400},
		{"lease-unknown", "/v1/environments/prod/maintenance-admission/leases", `{"durationSeconds":60,"approve":true}`, 400},
		{"start-body", "/v1/plans/pln_test/maintenance-admission/start", `{}`, 400},
		{"start-query", "/v1/plans/pln_test/maintenance-admission/start?force=true", "", 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			port := &controller{}
			mux := http.NewServeMux()
			if err := Register(mux, Config{BindOperator: func(*http.Request) (maintenance.OverrideController, error) { return port, nil }, BindAgent: func(*http.Request) (AgentController, error) { return port, nil }}); err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body)))
			if response.Code != test.status || port.calls != 0 {
				t.Fatal(response.Code, response.Body.String(), port.calls)
			}
		})
	}
}

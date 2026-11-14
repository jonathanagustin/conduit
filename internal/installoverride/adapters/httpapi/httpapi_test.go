package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/installoverride/ports"
)

type recordingController struct{ calls int }

func (c *recordingController) ProposeModuleOverride(context.Context, ports.ModuleDraft, bool) (ports.ModuleReview, error) {
	c.calls++
	return ports.ModuleReview{}, nil
}
func (c *recordingController) ApplyModuleOverride(context.Context, string) (ports.ModuleAdmission, error) {
	c.calls++
	return ports.ModuleAdmission{}, nil
}

func TestStrictModuleOverrideHTTPRejectsBeforeController(t *testing.T) {
	for _, test := range []struct {
		name, path, body string
		status           int
	}{
		{"unknown", "/v1/module-override-proposals", `{"draft":{"unreviewed":true}}`, 400},
		{"trailing", "/v1/module-override-proposals", `{"draft":{}} {}`, 400},
		{"query", "/v1/module-override-proposals?ignoreRecalls=true", `{"draft":{}}`, 400},
		{"large", "/v1/module-override-proposals", `{"draft":{"rationale":"` + strings.Repeat("x", 256<<10) + `"}}`, 413},
		{"apply-body", "/v1/module-override-proposals/crq_test/apply", `{}`, 400},
		{"apply-query", "/v1/module-override-proposals/crq_test/apply?force=true", "", 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			controller := &recordingController{}
			mux := http.NewServeMux()
			if err := Register(mux, Config{Bind: func(*http.Request) (ports.ModuleController, error) { return controller, nil }}); err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body)))
			if response.Code != test.status || controller.calls != 0 {
				t.Fatal(response.Code, response.Body.String(), controller.calls)
			}
		})
	}
}

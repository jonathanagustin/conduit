package httpapi

import (
	"errors"
	"github.com/jonathanagustin/conduit/internal/delivery/releasestatus/domain"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadAuthorizationAndSanitizedFailures(t *testing.T) {
	for _, test := range []struct {
		name string
		auth int
		err  error
		want int
	}{{"anonymous", 401, nil, 401}, {"denied", 403, nil, 403}, {"missing", 0, domain.ErrNotFound, 404}, {"storage", 0, errors.New("PRIVATE_DETAIL"), 503}, {"valid", 0, nil, 200}} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			mux := http.NewServeMux()
			Register(mux, Config{Authorize: func(*http.Request) int { return test.auth }, Read: func(*http.Request, string, string) (domain.View, error) {
				calls++
				return domain.View{Product: "app", Version: "1.0.0"}, test.err
			}})
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest("GET", "/v1/products/app/releases/1.0.0/rollout", nil))
			if response.Code != test.want || strings.Contains(response.Body.String(), "PRIVATE_DETAIL") || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("response: %d %s", response.Code, response.Body.String())
			}
			if test.auth != 0 && calls != 0 {
				t.Fatal("read before authorization")
			}
		})
	}
}
func TestReadRejectsUnspecifiedInputs(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, Config{Authorize: func(*http.Request) int { return 0 }, Read: func(*http.Request, string, string) (domain.View, error) {
		t.Fatal("unexpected read")
		return domain.View{}, nil
	}})
	for _, test := range []struct{ path, body string }{{"/v1/products/app/releases/1/rollout?environment=private", ""}, {"/v1/products/app/releases/1/rollout", "{}"}, {"/v1/products/app/releases/%20/rollout", ""}} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest("GET", test.path, strings.NewReader(test.body)))
		if response.Code != 400 {
			t.Fatalf("%s: %d", test.path, response.Code)
		}
	}
}

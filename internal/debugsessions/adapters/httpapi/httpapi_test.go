package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/debugsessions/adapters/memory"
	"github.com/jonathanagustin/conduit/internal/debugsessions/application"
	"github.com/jonathanagustin/conduit/internal/debugsessions/domain"
	"github.com/jonathanagustin/conduit/internal/debugsessions/ports"
)

type allow struct{}

func (allow) Require(context.Context, string, domain.Scope) (ports.Decision, error) {
	return ports.Decision{Identity: domain.Identity{Actor: "actor", Owner: "owner"}}, nil
}
func TestCreateBoundaryRejectsAmbiguousAndUnknownJSONBeforeWriting(t *testing.T) {
	valid := `{"sessionId":"dsr_abcdefghijklmnopqrstuvwxyz","target":{"environment":"env","namespace":"app","profile":"requested"}}`
	for _, body := range []string{
		"null", "[]", valid + valid, strings.Repeat(" ", 4097),
		strings.Replace(valid, `"sessionId":`, `"owner":"forged","sessionId":`, 1),
		strings.Replace(valid, `"sessionId":`, `"sessionId":"dsr_bcdefghijklmnopqrstuvwxyz2","sessionId":`, 1),
		strings.Replace(valid, `"sessionId":`, `"SessionId":`, 1),
		strings.Replace(valid, `"namespace":`, `"Namespace":`, 1),
		strings.Replace(valid, `"namespace":`, `"namespace":"other","namespace":`, 1),
		strings.Replace(valid, `"sessionId":`, `"lifetimeSeconds":1.1,"sessionId":`, 1),
	} {
		t.Run(body[:min(len(body), 40)], func(t *testing.T) {
			journal := memory.New()
			s, err := application.New(journal, allow{}, time.Now)
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			if err := Register(mux, func(*http.Request) (*application.Service, error) { return s, nil }); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/v1/debug-session-requests", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != 400 {
				t.Fatal(w.Code, w.Body)
			}
			ids, err := journal.IDs(t.Context(), "", 1)
			if err != nil || len(ids) != 0 {
				t.Fatal(ids, err)
			}
		})
	}
}
func TestUntrustedInputsCannotBecomePermissionFlagsOrScopeSelectors(t *testing.T) {
	journal := memory.New()
	s, _ := application.New(journal, allow{}, time.Now)
	mux := http.NewServeMux()
	_ = Register(mux, func(*http.Request) (*application.Service, error) { return s, nil })
	for _, path := range []string{
		"/v1/debug-session-requests?environment=env&namespace=app&owner=other",
		"/v1/debug-session-requests?environment=env&environment=other&namespace=app",
		"/v1/debug-session-requests?environment=env&namespace=app&spaceId=space",
		"/v1/debug-session-requests?environment=%zz&namespace=app",
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 400 {
			t.Fatal(path, w.Code, w.Body)
		}
	}
}
func TestIdentityFailurePrecedesBodyAndInternalErrorsStaySanitized(t *testing.T) {
	for _, failure := range []struct {
		err    error
		status int
	}{{StatusError{401, "operator identity required"}, 401}, {errors.New("postgres://operator:PVT_DEBUG_PASSWORD@host/database"), 500}} {
		mux := http.NewServeMux()
		_ = Register(mux, func(*http.Request) (*application.Service, error) { return nil, failure.err })
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/v1/debug-session-requests", strings.NewReader("malformed")))
		if w.Code != failure.status || strings.Contains(w.Body.String(), "PVT_DEBUG_PASSWORD") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Body, w.Header())
		}
	}
}
func TestCreateRequiresJSONContentTypeAndRespondsWithNativeMetadata(t *testing.T) {
	journal := memory.New()
	s, _ := application.New(journal, allow{}, time.Now)
	mux := http.NewServeMux()
	_ = Register(mux, func(*http.Request) (*application.Service, error) { return s, nil })
	body := `{"sessionId":"dsr_abcdefghijklmnopqrstuvwxyz","target":{"environment":"env","namespace":"app","profile":"requested"}}`
	for _, media := range []string{"", "text/plain", "application/x-www-form-urlencoded"} {
		r := httptest.NewRequest("POST", "/v1/debug-session-requests", strings.NewReader(body))
		r.Header.Set("Content-Type", media)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 415 {
			t.Fatal(media, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/v1/debug-session-requests", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"REQUESTED"`) || !strings.Contains(w.Body.String(), `"valueTier":"CONDUIT_NATIVE"`) {
		t.Fatal(w.Code, w.Body)
	}
}

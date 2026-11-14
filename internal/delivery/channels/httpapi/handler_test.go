package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jonathanagustin/conduit/internal/delivery/channels/domain"
)

type stubBoard struct {
	view domain.View
	err  error
	saw  []string
}

func (stub *stubBoard) Board(_ context.Context, allow func(string) bool) (domain.View, error) {
	if !allow("east") {
		stub.saw = append(stub.saw, "denied")
	}
	return stub.view, stub.err
}

func TestAnonymousCallerIsRefused(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, Config{
		Board:     &stubBoard{view: domain.View{Channels: domain.Channels(), Rows: []domain.Row{}}},
		Authorize: func(*http.Request) error { return context.Canceled },
	})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/channel-subscriptions", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestBoardIsReturnedForAnAuthorizedCaller(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, Config{
		Board: &stubBoard{view: domain.View{
			Channels: domain.Channels(),
			Rows: []domain.Row{{
				Environment: "east", Product: "ledger", Subscribed: "stable",
				Versions: map[string]string{"dev": "1.0.0", "candidate": "1.1.0", "stable": "1.2.0"},
				Running:  "1.2.0",
			}},
		}},
		Authorize: func(*http.Request) error { return nil },
		Allow:     func(*http.Request, string) bool { return true },
	})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/channel-subscriptions", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
	var view domain.View
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Rows) != 1 || view.Rows[0].Versions["stable"] != "1.2.0" || view.Rows[0].Subscribed != "stable" {
		t.Fatalf("view = %#v", view)
	}
}

package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
	api "github.com/jonathanagustin/conduit/internal/planning/transport"
)

type cancellationStore struct {
	*application.Service
	calls   int
	receipt *domain.TurnCancellationReceipt
	err     error
}

func (s *cancellationStore) CancelTurn(_ context.Context, session, run string) (*domain.TurnCancellationReceipt, error) {
	s.calls++
	if session != "owned" {
		return nil, domain.ErrNotFound
	}
	if s.err != nil {
		return nil, s.err
	}
	if s.receipt != nil {
		return s.receipt, nil
	}
	return &domain.TurnCancellationReceipt{RunID: run, State: "cancellation_requested", ValueTier: "CONDUIT_NATIVE"}, nil
}

func TestCancellationGeneratedContractAndStrictOwnerSelection(t *testing.T) {
	store := &cancellationStore{Service: application.NewService(nil)}
	mux := http.NewServeMux()
	Register(mux, Config{Authorize: func(r *http.Request) error {
		if r.Header.Get("X-Test-Owner") != "alice" {
			return errors.New("unauthenticated")
		}
		return nil
	}, ResolveService: func(*http.Request) (Sessions, error) { return store, nil }})
	server := httptest.NewServer(mux)
	defer server.Close()
	client, err := api.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.CancelPlanningTurnWithResponse(context.Background(), "owned", "run-1", api.CancelPlanningTurnJSONRequestBody{}, func(_ context.Context, r *http.Request) error { r.Header.Set("X-Test-Owner", "alice"); return nil })
	if err != nil || response.JSON200 == nil || response.JSON200.RunId != "run-1" || response.JSON200.State != api.CancellationRequested || response.JSON200.ValueTier != api.CancelPlanningTurn200JSONResponseBodyValueTierCONDUITNATIVE {
		t.Fatalf("generated cancellation: %#v %v", response, err)
	}
	for _, tc := range []struct {
		owner, session, run, body string
		want                      int
	}{
		{"", "owned", "run", "{", 401},
		{"alice", "owned", "run", `{"actor":"bob"}`, 400},
		{"alice", "owned", "run", `{"runId":"other"}`, 400},
		{"alice", "owned", "run", "null", 400},
		{"alice", "owned", "run", "{} {}", 400},
		{"alice", "owned", "run", strings.Repeat("x", maxBody+1), 413},
		{"alice", "owned", strings.Repeat("x", 257), "", 400},
		{"alice", "other-owner-session", "run", "", 404},
		{"alice", "owned", "run", "", 200},
	} {
		before := store.calls
		request := httptest.NewRequest(http.MethodPost, "/v1/planning-sessions/"+tc.session+"/turns/"+tc.run+"/cancel", strings.NewReader(tc.body))
		request.Header.Set("X-Test-Owner", tc.owner)
		got := httptest.NewRecorder()
		mux.ServeHTTP(got, request)
		if got.Code != tc.want {
			t.Errorf("%s %s: %d %s", tc.owner, tc.run, got.Code, got.Body.String())
		}
		if (tc.want == 400 || tc.want == 401 || tc.want == 413) && store.calls != before {
			t.Error("refused input reached cancellation store")
		}
	}
}

func TestCancellationRefusesUnsupportedUnavailableAndInvalidAcknowledgement(t *testing.T) {
	for _, tc := range []struct {
		name  string
		store Sessions
		want  int
	}{
		{"unsupported", application.NewService(nil), 501},
		{"unavailable", &cancellationStore{Service: application.NewService(nil), err: errors.New("backend unavailable")}, 502},
		{"capacity", &cancellationStore{Service: application.NewService(nil), err: domain.ErrRunCapacity}, 429},
		{"other-run", &cancellationStore{Service: application.NewService(nil), receipt: &domain.TurnCancellationReceipt{RunID: "other", State: "cancelled", ValueTier: "CONDUIT_NATIVE"}}, 502},
		{"unknown-state", &cancellationStore{Service: application.NewService(nil), receipt: &domain.TurnCancellationReceipt{RunID: "run", State: "running", ValueTier: "CONDUIT_NATIVE"}}, 502},
		{"unknown-tier", &cancellationStore{Service: application.NewService(nil), receipt: &domain.TurnCancellationReceipt{RunID: "run", State: "cancelled", ValueTier: "UNKNOWN"}}, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			Register(mux, Config{Service: tc.store, Authorize: func(*http.Request) error { return nil }})
			got := doJSON(t, mux, http.MethodPost, "/v1/planning-sessions/owned/turns/run/cancel", nil)
			if got.status != tc.want {
				t.Fatalf("%d %s", got.status, got.raw)
			}
		})
	}
}

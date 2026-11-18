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
		{"unsupported", struct{ Sessions }{application.NewService(nil)}, 501},
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

type delayedCancellationPlanner struct {
	entered chan struct{}
	release chan struct{}
}

func (p delayedCancellationPlanner) Turn(context.Context, *domain.Session, string) (*application.TurnResult, error) {
	close(p.entered)
	<-p.release
	return &application.TurnResult{Plan: &domain.Plan{Text: "late result"}}, nil
}

func TestGeneratedCancellationFencesActualDevelopmentStore(t *testing.T) {
	planner := delayedCancellationPlanner{entered: make(chan struct{}), release: make(chan struct{})}
	store := application.NewService(planner)
	session, err := store.Create(context.Background(), "https://github.com/org/app", "main")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	Register(mux, Config{Service: store, Authorize: func(*http.Request) error { return nil }})
	server := httptest.NewServer(mux)
	defer server.Close()
	client, err := api.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	ctx := application.WithRunScope(context.Background(), application.RunScope{ThreadID: session.ID, RunID: "active"})
	go func() { _, err := store.Turn(ctx, session.ID, "inspect"); done <- err }()
	<-planner.entered
	response, err := client.CancelPlanningTurnWithResponse(context.Background(), session.ID, "active", api.CancelPlanningTurnJSONRequestBody{})
	if err != nil || response.JSON200 == nil || response.JSON200.State != api.CancellationRequested {
		t.Errorf("active acknowledgement: %#v %v", response, err)
	}
	close(planner.release)
	if err := <-done; !errors.Is(err, domain.ErrRunCancelled) {
		t.Fatalf("late result: %v", err)
	}
	response, err = client.CancelPlanningTurnWithResponse(context.Background(), session.ID, "active", api.CancelPlanningTurnJSONRequestBody{})
	if err != nil || response.JSON200 == nil || response.JSON200.State != api.Cancelled {
		t.Fatalf("terminal acknowledgement: %#v %v", response, err)
	}
	read, err := client.GetPlanningSessionWithResponse(context.Background(), session.ID)
	if err != nil || read.JSON200 == nil || read.JSON200.Plan != nil || len(read.JSON200.Messages) != 0 {
		t.Fatalf("late result escaped fence: %#v %v", read, err)
	}
}

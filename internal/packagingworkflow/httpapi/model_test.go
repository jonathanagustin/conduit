package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/application"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

type proposalCall struct {
	input               domain.PackagingContext
	environment, author string
}
type observingModel struct{ calls []proposalCall }

func (model *observingModel) Propose(_ context.Context, input domain.PackagingContext, environment, author string) (application.Result, error) {
	model.calls = append(model.calls, proposalCall{input, environment, author})
	return application.Result{RunID: input.RunID}, nil
}

type modelRunReader struct {
	Reader
	run   domain.RunStatus
	err   error
	reads int
}

func (reader *modelRunReader) Read(context.Context, string) (domain.RunStatus, error) {
	reader.reads++
	return reader.run, reader.err
}

func TestModelProposalUsesTheRecordedEnvironmentAndAuthenticatedAuthor(t *testing.T) {
	reader := &modelRunReader{run: domain.RunStatus{RunID: "run-east", TargetPlanRef: "environment-east/sample-app"}}
	model := &observingModel{}
	mux := http.NewServeMux()
	Register(mux, Config{Authorize: func(*http.Request) error { return nil }, Reader: reader, Model: model, OperatorName: func(*http.Request) string { return "signed-in-reviewer" }})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/packaging-runs/run-east/model-proposal", strings.NewReader(`{"attachmentSha256":["reviewed-attachment"]}`)))
	if response.Code != 200 || len(model.calls) != 1 {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, len(model.calls), response.Body.String())
	}
	call := model.calls[0]
	if call.environment != "environment-east" || call.author != "signed-in-reviewer" || call.input.RunID != "run-east" || len(call.input.AttachmentSHA256) != 1 || call.input.AttachmentSHA256[0] != "reviewed-attachment" {
		t.Fatalf("proposal binding=%+v", call)
	}
	if reader.reads != 1 {
		t.Fatalf("read %d snapshots, want one authoritative snapshot", reader.reads)
	}
}

func TestModelProposalRefusesUnboundRunsBeforeCallingTheModel(t *testing.T) {
	for name, tc := range map[string]struct {
		reader Reader
		status int
	}{
		"unconfigured":        {nil, 404},
		"missing":             {&modelRunReader{err: ErrNotFound}, 404},
		"unavailable":         {&modelRunReader{err: errors.New("private backend error")}, 502},
		"wrong run":           {&modelRunReader{run: domain.RunStatus{RunID: "another", TargetPlanRef: "east/app"}}, 502},
		"blank run":           {&modelRunReader{run: domain.RunStatus{TargetPlanRef: "east/app"}}, 502},
		"missing target":      {&modelRunReader{run: domain.RunStatus{RunID: "selected"}}, 409},
		"unparseable target":  {&modelRunReader{run: domain.RunStatus{RunID: "selected", TargetPlanRef: "east"}}, 409},
		"empty target":        {&modelRunReader{run: domain.RunStatus{RunID: "selected", TargetPlanRef: "east/"}}, 409},
		"invalid environment": {&modelRunReader{run: domain.RunStatus{RunID: "selected", TargetPlanRef: "east\n/app"}}, 409},
	} {
		t.Run(name, func(t *testing.T) {
			model := &observingModel{}
			mux := http.NewServeMux()
			Register(mux, Config{Authorize: func(*http.Request) error { return nil }, Reader: tc.reader, Model: model})
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/packaging-runs/selected/model-proposal", strings.NewReader(`{}`)))
			if response.Code != tc.status || len(model.calls) != 0 {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, len(model.calls), response.Body.String())
			}
			if strings.Contains(response.Body.String(), "private backend error") {
				t.Fatal("backend detail leaked")
			}
		})
	}
}

func TestModelProposalRefusesCallerTargetOverridesAndAdditionalBodies(t *testing.T) {
	for _, body := range []string{`{"environment":"other"}`, `{"author":"another-person"}`, `{} {}`, `null`, `[]`} {
		t.Run(body, func(t *testing.T) {
			reader := &modelRunReader{run: domain.RunStatus{RunID: "selected", TargetPlanRef: "east/app"}}
			model := &observingModel{}
			mux := http.NewServeMux()
			Register(mux, Config{Authorize: func(*http.Request) error { return nil }, Reader: reader, Model: model})
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/packaging-runs/selected/model-proposal", strings.NewReader(body)))
			if response.Code != 400 || len(model.calls) != 0 {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, len(model.calls), response.Body.String())
			}
		})
	}
}

func TestModelProposalKeepsAnonymousAndUnavailableModelRefusals(t *testing.T) {
	for _, anonymous := range []bool{false, true} {
		reader := &modelRunReader{run: domain.RunStatus{RunID: "selected", TargetPlanRef: "east/app"}}
		authorize := func(*http.Request) error { return nil }
		want := 422
		if anonymous {
			want = 401
			authorize = func(*http.Request) error { return StatusError{Status: 401, Message: "unauthorized"} }
		}
		mux := http.NewServeMux()
		Register(mux, Config{Authorize: authorize, Reader: reader})
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/packaging-runs/selected/model-proposal", strings.NewReader(`{}`)))
		if response.Code != want {
			t.Fatal("refusal", response.Code, response.Body.String())
		}
		if anonymous && reader.reads != 0 {
			t.Fatal("anonymous request read run state")
		}
	}
}

package httpapi

import (
	"encoding/json"
	"github.com/jonathanagustin/conduit/internal/devworkspace/adapters/gitstore"
	"github.com/jonathanagustin/conduit/internal/devworkspace/application"
	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHistoryHTTPReadsRealGitWithWorkspaceVisibilityAndExactContext(t *testing.T) {
	store, err := gitstore.Open(gitstore.Options{RootDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(t.Context(), domain.Workspace{ID: "wsp_http_history", Owner: "ada", EnvironmentID: "east", ProposalRevision: "rev-1"}); err != nil {
		t.Fatal(err)
	}
	row, err := store.Get(t.Context(), "wsp_http_history")
	if err != nil {
		t.Fatal(err)
	}
	service := application.New(application.Dependencies{Records: store, Files: store.Files()})
	mux := http.NewServeMux()
	Register(mux, Config{Service: service,
		Authorize: func(r *http.Request) error {
			if r.Header.Get("X-Test-User") == "" {
				return StatusError{Status: 401, Message: "unauthorized"}
			}
			return nil
		},
		Caller: func(r *http.Request) (domain.Caller, error) {
			return domain.Caller{Username: r.Header.Get("X-Test-User")}, nil
		},
		Visible: func(r *http.Request, env string) bool {
			return r.Header.Get("X-Test-User") == "reader" && env == "east"
		},
	})
	for _, tc := range []struct {
		user, revision, head string
		status               int
	}{{"", row.ProposalRevision, row.HeadCommit, 401}, {"other", row.ProposalRevision, row.HeadCommit, 404}, {"ada", "old", row.HeadCommit, 409}, {"ada", row.ProposalRevision, "HEAD", 409}, {"ada", row.ProposalRevision, row.HeadCommit, 200}, {"reader", row.ProposalRevision, row.HeadCommit, 200}} {
		request := httptest.NewRequest(http.MethodGet, "/v1/dev-workspaces/"+row.ID+"/history?revision="+tc.revision+"&head="+tc.head, nil)
		request.Header.Set("X-Test-User", tc.user)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatalf("user=%q status=%d body=%s", tc.user, response.Code, response.Body.String())
		}
		if tc.status == 200 {
			var history domain.CommitHistory
			if json.Unmarshal(response.Body.Bytes(), &history) != nil || history.HeadCommit != row.HeadCommit || history.WorkspaceID != row.ID || len(history.Entries) != 1 || history.Entries[0].Commit != row.HeadCommit {
				t.Fatal("history receipt", response.Body.String())
			}
		}
	}
}

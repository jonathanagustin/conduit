package composition

import (
	"encoding/json"
	activityhttp "github.com/jonathanagustin/conduit/internal/changeactivity/adapters/httpapi"
	activitymemory "github.com/jonathanagustin/conduit/internal/changeactivity/adapters/memory"
	activity "github.com/jonathanagustin/conduit/internal/changeactivity/domain"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	devchange "github.com/jonathanagustin/conduit/internal/devworkspace/adapters/changerequest"
	devdomain "github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDiscussionConnectPreservesOriginalCarrierAndNativeReadAuthority(t *testing.T) {
	_, repository, _ := delegationHub(t)
	submit := devchange.New(controlplane.NewGovernanceService(repository))
	receipt, err := submit.Submit(t.Context(), devdomain.Caller{Username: "alice"}, devdomain.Submission{WorkspaceID: "wsp_discussion", EnvironmentID: "env-a", Revision: "rev-1", SourceDigest: "sha256:discussion", Title: "Read discussion"})
	if err != nil {
		t.Fatal(err)
	}
	store := activitymemory.New()
	comment := activity.Comment{ID: "cmt_00000000000000000000000000000001", RequestID: receipt.ChangeRequestID, Author: "alice", Actor: "robot", CreatedAt: time.Date(2026, 11, 20, 1, 0, 0, 123, time.UTC), Text: "Native immutable discussion", ValueTier: activity.ValueTier}
	if err := store.Append(t.Context(), comment); err != nil {
		t.Fatal(err)
	}
	options := controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"alice"}}
	handler, err := NewHub(repository, options, Options{ChangeDiscussion: store})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		actor, represented string
		status             int
	}{{"alice", "", 200}, {"alice", "bob", 200}, {"robot", "alice", 403}, {"bob", "alice", 403}, {"bob", "", 403}} {
		native := as(t, handler, "/v1/change-requests/"+receipt.ChangeRequestID+"/comments", tc.actor, tc.represented)
		body, _ := json.Marshal(map[string]string{"changeRequestId": receipt.ChangeRequestID})
		r := httptest.NewRequest("POST", strings.TrimPrefix(activityhttp.DiscussionReadConnectRoute, "POST "), strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Connect-Protocol-Version", "1")
		r.Header.Set("X-Platform-Subject", tc.actor)
		if tc.represented != "" {
			r.Header.Set("X-Platform-Service-Account", "true")
			r.Header.Set("X-Platform-On-Behalf-Of", tc.represented)
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		if native.Code != tc.status || out.Code != tc.status {
			t.Fatalf("%s as %s: REST%d %s RPC%d %s", tc.actor, tc.represented, native.Code, native.Body, out.Code, out.Body)
		}
		if out.Code == 200 {
			var got struct {
				Comments []struct{ Author, Actor, Text string }
			}
			if json.Unmarshal(out.Body.Bytes(), &got) != nil || len(got.Comments) != 1 || got.Comments[0].Author != "alice" || got.Comments[0].Actor != "robot" || got.Comments[0].Text != comment.Text {
				t.Fatalf("immutable writer changed with reader: %s", out.Body)
			}
		}
	}
	secure, err := NewHub(repository, controlplane.HandlerOptions{BootstrapAdministrators: []string{"alice"}}, Options{ChangeDiscussion: store})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, strings.TrimPrefix(activityhttp.DiscussionReadConnectRoute, "POST "), strings.NewReader("{broken"))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Connect-Protocol-Version", "1")
	r.Header.Set("X-Platform-Subject", "alice")
	out := httptest.NewRecorder()
	secure.ServeHTTP(out, r)
	if out.Code != 401 {
		t.Fatalf("production accepted development bootstrap or decoded body: %d %s", out.Code, out.Body)
	}
}

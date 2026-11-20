package conduitcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	activityv1 "github.com/jonathanagustin/conduit/gen/go/conduit/changeactivity/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/changeactivity/v1/changeactivityv1connect"
	activitymemory "github.com/jonathanagustin/conduit/internal/changeactivity/adapters/memory"
	activity "github.com/jonathanagustin/conduit/internal/changeactivity/domain"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	devchange "github.com/jonathanagustin/conduit/internal/devworkspace/adapters/changerequest"
	devdomain "github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func executeDiscussionRead(ctx context.Context, server, id, mode, format string, extra ...string) (string, error) {
	var out bytes.Buffer
	root := NewRoot(Runtime{Stdout: &out, Stderr: io.Discard, Getenv: func(string) string { return "" }})
	args := []string{"--server", server, "--development-subject", "reader", "--output", format, "changerequest", "comments", id}
	if mode != "" {
		args = append(args, "--read-transport", mode)
	}
	root.SetArgs(append(args, extra...))
	err := root.ExecuteContext(ctx)
	return out.String(), err
}

func TestDiscussionCLIRealHubPagedParityRESTDefaultAndPrivateRead(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	submit := devchange.New(controlplane.NewGovernanceService(repository))
	receipt, err := submit.Submit(t.Context(), devdomain.Caller{Username: "reader"}, devdomain.Submission{WorkspaceID: "wsp_cli_discussion", EnvironmentID: "env-a", Revision: "rev-1", SourceDigest: "sha256:discussion", Title: "Read comments"})
	if err != nil {
		t.Fatal(err)
	}
	store := activitymemory.New()
	for i := range 17 {
		c := activity.Comment{ID: fmt.Sprintf("cmt_%032x", i+1), RequestID: receipt.ChangeRequestID, Author: "recorded-author", Actor: "recorded-carrier", CreatedAt: time.Date(2026, 11, 20, 1, 0, 0, 123+i, time.UTC), Text: fmt.Sprintf("comment %d: <Unicode 世界>\n\"quoted\"", i), ValueTier: activity.ValueTier}
		if err := store.Append(t.Context(), c); err != nil {
			t.Fatal(err)
		}
	}
	hub, err := composition.NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true}, composition.Options{ChangeDiscussion: store})
	if err != nil {
		t.Fatal(err)
	}
	var rest, rpc atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			rest.Add(1)
		}
		if r.URL.Path == changeactivityv1connect.ChangeRequestDiscussionReadServiceListChangeRequestCommentsProcedure {
			rpc.Add(1)
		}
		hub.ServeHTTP(w, r)
	}))
	defer server.Close()
	for _, format := range []string{"json", "yaml"} {
		a, err := executeDiscussionRead(t.Context(), server.URL, receipt.ChangeRequestID, "", format)
		if err != nil {
			t.Fatal(err)
		}
		if rest.Load() != 3 || rpc.Load() != 0 {
			t.Fatalf("default changed: REST%d RPC%d", rest.Load(), rpc.Load())
		}
		b, err := executeDiscussionRead(t.Context(), server.URL, receipt.ChangeRequestID, "connect", format)
		if err != nil || a != b {
			t.Fatalf("%s parity: %v\nREST%s\nRPC%s", format, err, a, b)
		}
		if rest.Load() != 3 || rpc.Load() != 3 {
			t.Fatalf("transport replay: REST%d RPC%d", rest.Load(), rpc.Load())
		}
		rest.Store(0)
		rpc.Store(0)
	}
	// A different request owner cannot use this read to cross native visibility.
	private, err := submit.Submit(t.Context(), devdomain.Caller{Username: "other"}, devdomain.Submission{WorkspaceID: "wsp_private", EnvironmentID: "env-a", Revision: "rev-1", SourceDigest: "sha256:private", Title: "Private"})
	if err != nil {
		t.Fatal(err)
	}
	if out, err := executeDiscussionRead(t.Context(), server.URL, private.ChangeRequestID, "connect", "json"); err == nil || out != "" {
		t.Fatal("private response", out, err)
	}
	if rest.Load() != 0 || rpc.Load() != 1 {
		t.Fatal("refusal replayed")
	}
}

func discussionWireComment(id string) *activityv1.ChangeRequestComment {
	return &activityv1.ChangeRequestComment{Id: id, ChangeRequestId: "crq_cli", Author: "author", Actor: "actor", CreatedAt: timestamppb.New(time.Date(2026, 11, 20, 1, 0, 0, 123, time.UTC)), Text: "immutable text", ValueTier: activity.ValueTier}
}
func TestDiscussionCLIRejectsMalformedPagesWithoutPartialOutputOrFallback(t *testing.T) {
	for _, name := range []string{"foreign", "invalid-id", "nil-time", "invalid-time", "empty-author", "wrong-tier", "page-tier", "too-many", "short-cursor-page", "long-cursor", "repeat-cursor", "duplicate-id", "backward", "missing-route"} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				if r.Method != "POST" || r.URL.Path != changeactivityv1connect.ChangeRequestDiscussionReadServiceListChangeRequestCommentsProcedure {
					t.Error("unexpected fallback", r.Method, r.URL.Path)
				}
				if name == "missing-route" {
					http.NotFound(w, r)
					return
				}
				var req activityv1.ListChangeRequestCommentsRequest
				data, _ := io.ReadAll(r.Body)
				if proto.Unmarshal(data, &req) != nil || req.ChangeRequestId != "crq_cli" {
					t.Error("request identity lost")
				}
				page := &activityv1.ListChangeRequestCommentsResponse{ValueTier: activity.ValueTier, Comments: []*activityv1.ChangeRequestComment{discussionWireComment("cmt_00000000000000000000000000000001")}}
				switch name {
				case "foreign":
					page.Comments[0].ChangeRequestId = "crq_other"
				case "invalid-id":
					page.Comments[0].Id = "bad"
				case "nil-time":
					page.Comments[0].CreatedAt = nil
				case "invalid-time":
					page.Comments[0].CreatedAt = &timestamppb.Timestamp{Seconds: 1 << 62}
				case "empty-author":
					page.Comments[0].Author = ""
				case "wrong-tier":
					page.Comments[0].ValueTier = "UPSTREAM"
				case "page-tier":
					page.ValueTier = "UPSTREAM"
				case "too-many":
					for range 8 {
						page.Comments = append(page.Comments, discussionWireComment("cmt_00000000000000000000000000000002"))
					}
				case "short-cursor-page":
					page.NextCursor = "next"
				case "long-cursor":
					page.NextCursor = strings.Repeat("x", 1025)
				case "repeat-cursor", "duplicate-id", "backward":
					page.Comments = nil
					if n == 1 || name == "repeat-cursor" {
						for i := range 8 {
							page.Comments = append(page.Comments, discussionWireComment(fmt.Sprintf("cmt_%032x", int(n)*8+i+1)))
						}
						page.NextCursor = "next"
					} else if name == "duplicate-id" {
						page.Comments = []*activityv1.ChangeRequestComment{discussionWireComment("cmt_00000000000000000000000000000009")}
					} else {
						page.Comments = []*activityv1.ChangeRequestComment{discussionWireComment("cmt_00000000000000000000000000000001")}
					}
					if n > 1 && req.Cursor != "next" {
						t.Error("cursor not forwarded")
					}
				}
				data, err := proto.Marshal(page)
				if err != nil {
					t.Error(err)
				}
				w.Header().Set("Content-Type", "application/proto")
				_, _ = w.Write(data)
			}))
			defer server.Close()
			out, err := executeDiscussionRead(t.Context(), server.URL, "crq_cli", "connect", "json")
			if err == nil || out != "" {
				t.Fatal("invalid pages exposed", out, err)
			}
			want := int32(1)
			if name == "repeat-cursor" || name == "duplicate-id" || name == "backward" {
				want = 2
			}
			if calls.Load() != want {
				t.Fatalf("requests %d want%d", calls.Load(), want)
			}
		})
	}
}

func TestDiscussionCLIInvalidModeAndCancellationDoNotReplay(t *testing.T) {
	var calls atomic.Int32
	stop := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-stop:
		}
	}))
	defer server.Close()
	defer close(stop)
	if out, err := executeDiscussionRead(t.Context(), server.URL, "invalid/request", "connect", "json"); err == nil || out != "" || calls.Load() != 0 {
		t.Fatal("invalid request ID made request", out, err)
	}
	if out, err := executeDiscussionRead(t.Context(), server.URL, "crq_cli", "invalid", "json"); err == nil || out != "" || calls.Load() != 0 {
		t.Fatal("invalid mode made request", out, err)
	}
	out, err := executeDiscussionRead(t.Context(), server.URL, "crq_cli", "connect", "json", "--timeout", "150ms")
	if err == nil || out != "" || calls.Load() != 1 {
		t.Fatal("timeout replayed", out, err, calls.Load())
	}
}

func TestDiscussionCLIPreservesTLSBearerAndEmptyArray(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer discussion-test-token" || r.Header.Get("X-Platform-Subject") != "" || r.Method != "POST" || r.URL.Path != changeactivityv1connect.ChangeRequestDiscussionReadServiceListChangeRequestCommentsProcedure {
			t.Error("credential or route changed")
		}
		data, err := proto.Marshal(&activityv1.ListChangeRequestCommentsResponse{ValueTier: activity.ValueTier})
		if err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/proto")
		_, _ = w.Write(data)
	}))
	defer server.Close()
	token := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(token, []byte("discussion-test-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	root := NewRoot(Runtime{Stdout: &out, Stderr: io.Discard, Getenv: func(string) string { return "" }})
	root.SetArgs([]string{"--server", server.URL, "--ca-file", certificatePEM(t, "discussion-hub", server), "--bearer-token-file", token, "--output", "json", "changerequest", "comments", "crq_cli", "--read-transport", "connect"})
	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	var comments []activity.Comment
	if err := json.Unmarshal(out.Bytes(), &comments); err != nil || comments == nil || len(comments) != 0 || calls.Load() != 1 {
		t.Fatal("empty list or credential transport lost", out.String(), err)
	}
}

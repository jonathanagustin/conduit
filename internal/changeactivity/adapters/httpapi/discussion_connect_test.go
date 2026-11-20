package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	activityv1 "github.com/jonathanagustin/conduit/gen/go/conduit/changeactivity/v1"
	"github.com/jonathanagustin/conduit/internal/changeactivity/adapters/memory"
	"github.com/jonathanagustin/conduit/internal/changeactivity/application"
	"github.com/jonathanagustin/conduit/internal/changeactivity/domain"
	"github.com/jonathanagustin/conduit/internal/changeactivity/ports"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/descriptorpb"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

type discussionToken struct{}
type countedDiscussion struct {
	ports.Store
	reads int
	err   error
}

func (s *countedDiscussion) List(ctx context.Context, id string) ([]domain.Comment, error) {
	s.reads++
	if s.err != nil {
		return nil, s.err
	}
	return s.Store.List(ctx, id)
}

type readAuthority struct{ denied bool }

func (a readAuthority) RequireRead(ctx context.Context, id string) error {
	if ctx.Value(discussionToken{}) != "original-carrier" {
		return errors.New("PRIVATE carrier lost")
	}
	if a.denied {
		return domain.ErrForbidden
	}
	return nil
}
func (readAuthority) RequireAnnotation(context.Context, string) (ports.Writer, error) {
	return ports.Writer{}, domain.ErrForbidden
}
func discussionTestMux(t *testing.T, store ports.Store) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	err := Register(mux, Config{BindLabels: func(*http.Request) (LabelMutator, error) { return nil, errors.New("unused") }, BindDiscussion: func(r *http.Request) (*application.Service, error) {
		actor := r.Header.Get("X-Test-Actor")
		if actor == "" {
			return nil, StatusError{Status: 401, Message: "PRIVATE authentication"}
		}
		return application.New(store, readAuthority{denied: actor != "reader"})
	}})
	if err != nil {
		t.Fatal(err)
	}
	return mux
}
func discussionWire(mux http.Handler, actor, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), discussionToken{}, "original-carrier"))
	r.Header.Set("X-Test-Actor", actor)
	if method == "POST" {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Connect-Protocol-Version", "1")
	}
	out := httptest.NewRecorder()
	mux.ServeHTTP(out, r)
	return out
}
func TestDiscussionConnectMatchesEveryNativePageAndCarrierField(t *testing.T) {
	if domain.MaxPageSize != 8 {
		t.Fatal("review typed fixed-page contract before changing native bound")
	}
	store := &countedDiscussion{Store: memory.New()}
	for i := range 17 {
		c := domain.Comment{ID: fmt.Sprintf("cmt_%032x", i), RequestID: "crq_page", Author: "alice", Actor: "robot", CreatedAt: time.Date(2026, 11, 20, 1, i/3, 0, 123456789, time.FixedZone("fixture", 6*3600)), Text: fmt.Sprintf("line %d: <review> π & \"quoted\"\n", i), ValueTier: domain.ValueTier}
		if err := store.Append(t.Context(), c); err != nil {
			t.Fatal(err)
		}
	}
	mux := discussionTestMux(t, store)
	cursor := ""
	seen := map[string]bool{}
	for pageNumber := 0; pageNumber < 3; pageNumber++ {
		native := discussionWire(mux, "reader", "GET", "/v1/change-requests/crq_page/comments?cursor="+url.QueryEscape(cursor), "")
		body, _ := json.Marshal(map[string]string{"changeRequestId": "crq_page", "cursor": cursor})
		wire := discussionWire(mux, "reader", "POST", strings.TrimPrefix(DiscussionReadConnectRoute, "POST "), string(body))
		if native.Code != 200 || wire.Code != 200 {
			t.Fatalf("REST%d %s RPC%d %s", native.Code, native.Body, wire.Code, wire.Body)
		}
		var expected application.CommentPage
		actual := &activityv1.ListChangeRequestCommentsResponse{}
		if json.Unmarshal(native.Body.Bytes(), &expected) != nil || protojson.Unmarshal(wire.Body.Bytes(), actual) != nil {
			t.Fatal("decode page")
		}
		if actual.NextCursor != expected.NextCursor || actual.ValueTier != expected.ValueTier || len(actual.Comments) != len(expected.Comments) || len(actual.Comments) > 8 {
			t.Fatalf("page metadata changed: %v %+v", actual, expected)
		}
		for i, c := range actual.Comments {
			e := expected.Comments[i]
			if c.Id != e.ID || c.ChangeRequestId != e.RequestID || c.Author != e.Author || c.Actor != e.Actor || c.Text != e.Text || c.ValueTier != e.ValueTier || c.CreatedAt == nil || !c.CreatedAt.AsTime().Equal(e.CreatedAt) || seen[c.Id] {
				t.Fatalf("native comment changed: %v %+v", c, e)
			}
			seen[c.Id] = true
		}
		if pageNumber == 0 {
			before := store.reads
			bad, _ := json.Marshal(map[string]string{"changeRequestId": "crq_other", "cursor": actual.NextCursor})
			if x := discussionWire(mux, "reader", "POST", strings.TrimPrefix(DiscussionReadConnectRoute, "POST "), string(bad)); x.Code != 400 {
				t.Fatalf("rebound cursor: %d %s", x.Code, x.Body)
			}
			if store.reads != before {
				t.Fatal("rebound cursor read store")
			}
		}
		cursor = actual.NextCursor
	}
	if cursor != "" || len(seen) != 17 || store.reads != 6 {
		t.Fatalf("incomplete/duplicated walk: cursor=%s seen=%d reads=%d", cursor, len(seen), store.reads)
	}
}
func TestDiscussionConnectRefusalsAndSanitizedBackend(t *testing.T) {
	store := &countedDiscussion{Store: memory.New()}
	mux := discussionTestMux(t, store)
	path := strings.TrimPrefix(DiscussionReadConnectRoute, "POST ")
	for _, tc := range []struct {
		actor, body string
		status      int
	}{
		{"", "{broken", 401}, {"viewer", `{"changeRequestId":"crq_page"}`, 403}, {"reader", `{}`, 400}, {"reader", `{"changeRequestId":"crq_page","cursor":"%%%"}`, 400},
	} {
		x := discussionWire(mux, tc.actor, "POST", path, tc.body)
		if x.Code != tc.status || store.reads != 0 || strings.Contains(x.Body.String(), "PRIVATE") {
			t.Fatalf("refusal: %d reads=%d %s", x.Code, store.reads, x.Body)
		}
	}
	store.err = errors.New("PRIVATE database path")
	x := discussionWire(mux, "reader", "POST", path, `{"changeRequestId":"crq_page"}`)
	if x.Code != 503 || store.reads != 1 || strings.Contains(x.Body.String(), "PRIVATE") {
		t.Fatalf("backend: %d %s", x.Code, x.Body)
	}
}
func TestDiscussionReadContractCoversNativeFieldsAndHasNoSideEffects(t *testing.T) {
	native := reflect.TypeFor[domain.Comment]()
	fields := (&activityv1.ChangeRequestComment{}).ProtoReflect().Descriptor().Fields()
	if fields.Len() != native.NumField() {
		t.Fatal("native discussion field drift")
	}
	for i := 0; i < native.NumField(); i++ {
		tag := native.Field(i).Tag.Get("json")
		found := false
		for n := 0; n < fields.Len(); n++ {
			if fields.Get(n).JSONName() == tag {
				found = true
			}
		}
		if !found {
			t.Fatalf("native field omitted: %s", tag)
		}
	}
	services := activityv1.File_conduit_changeactivity_v1_change_request_discussion_proto.Services()
	if services.Len() != 1 || services.Get(0).Methods().Len() != 1 {
		t.Fatal("unexpected discussion command surface")
	}
	opts := services.Get(0).Methods().Get(0).Options().(*descriptorpb.MethodOptions)
	if opts.GetIdempotencyLevel() != descriptorpb.MethodOptions_NO_SIDE_EFFECTS {
		t.Fatal("read-only procedure lost")
	}
}

func TestDiscussionConnectRefusesUnrepresentableStoredTimeWithoutPartialRows(t *testing.T) {
	store := memory.New()
	c := domain.Comment{ID: "cmt_00000000000000000000000000000001", RequestID: "crq_page", Author: "alice", Actor: "robot", CreatedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), Text: "private stored text", ValueTier: domain.ValueTier}
	if err := store.Append(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	out := discussionWire(discussionTestMux(t, store), "reader", "POST", strings.TrimPrefix(DiscussionReadConnectRoute, "POST "), `{"changeRequestId":"crq_page"}`)
	if out.Code != 503 || strings.Contains(out.Body.String(), "private stored text") || strings.Contains(out.Body.String(), "cmt_") {
		t.Fatalf("invalid timestamp returned partial record: %d %s", out.Code, out.Body)
	}
}
func TestDiscussionReadIsUnavailableWithoutNativeStoreBinding(t *testing.T) {
	mux := http.NewServeMux()
	RegisterDiscussionReadConnect(mux, nil)
	out := discussionWire(mux, "reader", "POST", strings.TrimPrefix(DiscussionReadConnectRoute, "POST "), `{"changeRequestId":"crq_page"}`)
	if out.Code != 404 {
		t.Fatalf("unconfigured read fabricated a page: %d %s", out.Code, out.Body)
	}
}

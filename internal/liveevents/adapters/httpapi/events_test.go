package httpapi

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/liveevents/adapters/filejournal"
	"github.com/jonathanagustin/conduit/internal/liveevents/adapters/schema"
	"github.com/jonathanagustin/conduit/internal/liveevents/application"
	"github.com/jonathanagustin/conduit/internal/liveevents/domain"
)

type operatorKey struct{}
type authorization struct {
	mu     sync.Mutex
	errors map[string]error
}

func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func name(ctx context.Context) string { value, _ := ctx.Value(operatorKey{}).(string); return value }
func (auth *authorization) Snapshot(ctx context.Context) (domain.Identity, error) {
	auth.mu.Lock()
	defer auth.mu.Unlock()
	owner := name(ctx)
	if owner == "" {
		return domain.Identity{}, domain.ErrUnauthorized
	}
	if err := auth.errors[owner]; err != nil {
		return domain.Identity{}, err
	}
	return domain.Identity{Owner: hash(owner), Represented: hash(""), Authorization: hash(owner + "-grants")}, nil
}
func (auth *authorization) Visible(ctx context.Context, _ domain.Identity, subject domain.Subject) (bool, error) {
	return name(ctx) == "alice" && subject.ID == "pln_visible" || name(ctx) == "bob" && subject.ID == "pln_hidden", nil
}
func (auth *authorization) authorize(request *http.Request) (context.Context, error) {
	ctx := context.WithValue(request.Context(), operatorKey{}, request.Header.Get("X-Test-Operator"))
	_, err := auth.Snapshot(ctx)
	return ctx, err
}
func (auth *authorization) deny(owner string, err error) {
	auth.mu.Lock()
	auth.errors[owner] = err
	auth.mu.Unlock()
}

type readiness struct {
	mu  sync.Mutex
	err error
}

func (ready *readiness) Check(context.Context) error {
	ready.mu.Lock()
	defer ready.mu.Unlock()
	return ready.err
}

type fixture struct {
	service *application.Service
	journal *filejournal.Journal
	auth    *authorization
	ready   *readiness
	handler *handler
}

func setup(t *testing.T) *fixture {
	t.Helper()
	journal, err := filejournal.Open(filepath.Join(t.TempDir(), "journal.json"))
	if err != nil {
		t.Fatal(err)
	}
	validator, err := schema.New()
	if err != nil {
		t.Fatal(err)
	}
	auth := &authorization{errors: map[string]error{}}
	ready := &readiness{}
	service, err := application.New(application.Config{Journal: journal, Authorizer: auth, Validator: validator, Readiness: ready})
	if err != nil {
		t.Fatal(err)
	}
	served, err := New(Config{Service: service, Authorize: auth.authorize})
	if err != nil {
		t.Fatal(err)
	}
	handler := served.(*handler)
	handler.heartbeat = 10 * time.Millisecond
	handler.poll = 5 * time.Millisecond
	handler.lifetime = 2 * time.Second
	return &fixture{service: service, journal: journal, auth: auth, ready: ready, handler: handler}
}
func request(owner, cursor string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "http://conduit.test/v1/events", nil)
	req.Header.Set("X-Test-Operator", owner)
	if cursor != "" {
		req.Header.Set("Last-Event-ID", cursor)
	}
	return req
}
func publish(t *testing.T, fixture *fixture, id string, position uint64) {
	t.Helper()
	err := fixture.service.PublishCommittedBatch(context.Background(), []domain.Record{{Position: position, Subject: domain.Subject{Type: "plan", ID: id}, Content: domain.Content{OccurredAt: time.Now().UTC(), Kind: "plan.changed", Resource: domain.Resource{Type: "plan", ID: id, Revision: position}, Data: json.RawMessage(`{"status":"RUNNING"}`)}}})
	if err != nil {
		t.Fatal(err)
	}
}
func server(t *testing.T, fixture *fixture) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(Route, fixture.handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}
func connect(t *testing.T, server *httptest.Server, owner, cursor string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/events", nil)
	req.Header.Set("X-Test-Operator", owner)
	if cursor != "" {
		req.Header.Set("Last-Event-ID", cursor)
	}
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}
func readEvent(t *testing.T, reader *bufio.Reader) domain.Envelope {
	t.Helper()
	var cursor, kind, body string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		line = strings.TrimSuffix(line, "\n")
		switch {
		case strings.HasPrefix(line, "id: "):
			cursor = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			kind = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			body = strings.TrimPrefix(line, "data: ")
		case line == "" && body != "":
			var envelope domain.Envelope
			if err := json.Unmarshal([]byte(body), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Cursor != cursor || envelope.Kind != kind {
				t.Fatal("SSE framing disagrees with envelope")
			}
			return envelope
		}
	}
}

func TestSSEErrorsAuthenticateBeforeAllocationAndHeaders(t *testing.T) {
	for _, test := range []struct {
		label, owner, cursor, origin, query string
		status                              int
	}{{"anonymous", "", "", "", "", 401}, {"forbidden", "blocked", "", "", "", 403}, {"cursor", "alice", "unknown", "", "", 409}, {"foreign-origin", "alice", "", "https://foreign.test", "", 403}, {"anonymous-foreign", "", "", "https://foreign.test", "", 401}, {"query-cursor", "alice", "", "", "cursor=PRIVATE_TOKEN", 400}} {
		t.Run(test.label, func(t *testing.T) {
			fixture := setup(t)
			fixture.auth.deny("blocked", domain.ErrForbidden)
			srv := server(t, fixture)
			req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/events", nil)
			req.Header.Set("X-Test-Operator", test.owner)
			if test.cursor != "" {
				req.Header.Set("Last-Event-ID", test.cursor)
			}
			req.Header.Set("Origin", test.origin)
			req.URL.RawQuery = test.query
			response, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, _ := io.ReadAll(response.Body)
			if response.StatusCode != test.status || response.Header.Get("X-Conduit-Stream-ID") != "" || strings.Contains(string(body), "PRIVATE_TOKEN") {
				t.Fatalf("status=%d headers=%v body=%s", response.StatusCode, response.Header, body)
			}
			state, _ := fixture.journal.Snapshot(context.Background())
			if len(state.Streams) != 0 {
				t.Fatal("rejected request allocated stream state")
			}
		})
	}
}

func TestSSEReadyFilteringAndStableReplay(t *testing.T) {
	fixture := setup(t)
	srv := server(t, fixture)
	alice := connect(t, srv, "alice", "")
	bob := connect(t, srv, "bob", "")
	if alice.StatusCode != 200 || alice.Header.Get("Content-Type") != "text/event-stream" || alice.Header.Get("Cache-Control") != "no-store" || alice.Header.Get("X-Accel-Buffering") != "no" || alice.Header.Get("X-Conduit-Event-Sequence") != "0" {
		t.Fatalf("invalid ready response: %v", alice.Header)
	}
	publish(t, fixture, "pln_hidden", 1)
	publish(t, fixture, "pln_visible", 1)
	first := readEvent(t, bufio.NewReader(alice.Body))
	hidden := readEvent(t, bufio.NewReader(bob.Body))
	if first.Sequence != 1 || first.Resource.ID != "pln_visible" || hidden.Sequence != 1 || hidden.Resource.ID != "pln_hidden" {
		t.Fatal("hidden source affected visible sequence")
	}
	alice.Body.Close()
	bob.Body.Close()
	publish(t, fixture, "pln_visible", 2)
	resumed := connect(t, srv, "alice", first.Cursor)
	if resumed.Header.Get("X-Conduit-Event-Sequence") != "1" || resumed.Header.Get("X-Conduit-Stream-ID") != first.StreamID {
		t.Fatal("resume ready changed epoch")
	}
	second := readEvent(t, bufio.NewReader(resumed.Body))
	resumed.Body.Close()
	replayed := connect(t, srv, "alice", first.Cursor)
	again := readEvent(t, bufio.NewReader(replayed.Body))
	if second.Sequence != 2 || again.Cursor != second.Cursor || again.Sequence != second.Sequence {
		t.Fatal("replay changed stable event cursor")
	}
}

func TestSSEHeartbeatRechecksPermissionAndLifetime(t *testing.T) {
	fixture := setup(t)
	fixture.handler.lifetime = 100 * time.Millisecond
	srv := server(t, fixture)
	response := connect(t, srv, "alice", "")
	reader := bufio.NewReader(response.Body)
	line, err := reader.ReadString('\n')
	if err != nil || line != ": keepalive\n" {
		t.Fatalf("heartbeat=%q %v", line, err)
	}
	fixture.auth.deny("alice", domain.ErrForbidden)
	rest, err := io.ReadAll(reader)
	if err != nil || strings.Contains(string(rest), "data:") {
		t.Fatalf("revocation leaked data: %q %v", rest, err)
	}
	newResponse := connect(t, srv, "alice", "")
	if newResponse.StatusCode != 403 {
		t.Fatal("revoked caller reconnected")
	}
	fixture.auth.deny("alice", nil)
	start := time.Now()
	fresh := connect(t, srv, "alice", "")
	_, _ = io.Copy(io.Discard, fresh.Body)
	if elapsed := time.Since(start); elapsed < 80*time.Millisecond || elapsed > time.Second {
		t.Fatalf("lifetime not enforced: %v", elapsed)
	}
}

func TestSSESourceFailureIsBoundedAndNotAnEvent(t *testing.T) {
	fixture := setup(t)
	var calls atomic.Int32
	fixture.handler.cfg.Refresh = func(context.Context) error {
		if calls.Add(1) > 1 {
			return errors.New("PRIVATE_SOURCE_ERROR")
		}
		return nil
	}
	srv := server(t, fixture)
	response := connect(t, srv, "alice", "")
	body, err := io.ReadAll(response.Body)
	if err != nil || strings.Contains(string(body), "PRIVATE_SOURCE_ERROR") || strings.Contains(string(body), "data:") {
		t.Fatalf("source fault escaped: %s %v", body, err)
	}
	next := connect(t, srv, "alice", "")
	if next.StatusCode != 503 || next.Header.Get("X-Conduit-Stream-ID") != "" {
		t.Fatal("failed source reported ready")
	}
}

type failedFlushWriter struct {
	header                     http.Header
	body                       strings.Builder
	status, flushes, deadlines int
}

func (writer *failedFlushWriter) Header() http.Header              { return writer.header }
func (writer *failedFlushWriter) WriteHeader(status int)           { writer.status = status }
func (writer *failedFlushWriter) Write(body []byte) (int, error)   { return writer.body.Write(body) }
func (writer *failedFlushWriter) SetWriteDeadline(time.Time) error { writer.deadlines++; return nil }
func (writer *failedFlushWriter) FlushError() error {
	writer.flushes++
	if writer.flushes > 1 {
		return errors.New("PRIVATE_FLUSH_ERROR")
	}
	return nil
}

func TestSSEFailedFlushKeepsCursorReplayable(t *testing.T) {
	fixture := setup(t)
	ctx := context.WithValue(context.Background(), operatorKey{}, "alice")
	root, err := fixture.service.Open(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	cursor := root.Ready.Cursor
	root.Close()
	publish(t, fixture, "pln_visible", 1)
	writer := &failedFlushWriter{header: http.Header{}}
	fixture.handler.ServeHTTP(writer, request("alice", cursor))
	if writer.status != 200 || writer.flushes != 2 || writer.deadlines < 2 || strings.Contains(writer.body.String(), "PRIVATE_FLUSH_ERROR") {
		t.Fatal("flush failure framing or deadlines invalid")
	}
	srv := server(t, fixture)
	response := connect(t, srv, "alice", cursor)
	replayed := readEvent(t, bufio.NewReader(response.Body))
	if replayed.Sequence != 1 || !strings.Contains(writer.body.String(), replayed.Cursor) {
		t.Fatal("failed flush lost replay position")
	}
}

func TestSSERequiresNativeAuthorizationAndStreamingWriter(t *testing.T) {
	fixture := setup(t)
	if _, err := New(Config{Service: fixture.service}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("missing authorizer accepted")
	}
	if _, err := New(Config{Authorize: fixture.auth.authorize}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("missing service accepted")
	}
	recorder := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recorder, request("alice", ""))
	if recorder.Code != 503 {
		t.Fatal("writer without deadline support accepted")
	}
	state, _ := fixture.journal.Snapshot(context.Background())
	if len(state.Streams) != 0 {
		t.Fatal("unsupported writer allocated subscriber")
	}
}

func TestSSERejectsAmbiguousOrBodyBearingRequestsBeforeSourceReads(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*http.Request)
		status int
	}{
		{"duplicate-origin", func(r *http.Request) {
			r.Header.Add("Origin", "http://conduit.test")
			r.Header.Add("Origin", "https://other.test")
		}, 403},
		{"duplicate-cursor", func(r *http.Request) { r.Header.Add("Last-Event-ID", "one"); r.Header.Add("Last-Event-ID", "two") }, 400},
		{"empty-query", func(r *http.Request) { r.URL.ForceQuery = true }, 400},
		{"unknown-body-length", func(r *http.Request) { r.Body = io.NopCloser(strings.NewReader("private")); r.ContentLength = -1 }, 400},
		{"cross-site-fetch", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403},
		{"command", func(r *http.Request) { r.Method = http.MethodPost }, 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := setup(t)
			fixture.handler.cfg.Refresh = func(context.Context) error { t.Fatal("invalid request read sources"); return nil }
			req := request("alice", "")
			tc.mutate(req)
			writer := &failedFlushWriter{header: http.Header{}}
			fixture.handler.ServeHTTP(writer, req)
			if writer.status != tc.status || writer.header.Get("X-Conduit-Stream-ID") != "" {
				t.Fatalf("status=%d headers=%v", writer.status, writer.header)
			}
			state, _ := fixture.journal.Snapshot(context.Background())
			if len(state.Streams) != 0 {
				t.Fatal("invalid request allocated stream")
			}
		})
	}
}

type stalledWriter struct {
	failedFlushWriter
	deadline time.Time
	timedOut bool
	cleared  bool
}

func (w *stalledWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	w.cleared = deadline.IsZero()
	return nil
}
func (w *stalledWriter) FlushError() error { return nil }
func (w *stalledWriter) Write([]byte) (int, error) {
	if w.deadline.IsZero() {
		return 0, errors.New("missing write deadline")
	}
	timer := time.NewTimer(time.Until(w.deadline))
	defer timer.Stop()
	<-timer.C
	w.timedOut = true
	return 0, context.DeadlineExceeded
}
func TestSSEStalledClientHasBoundedWriteAndReplay(t *testing.T) {
	fixture := setup(t)
	fixture.handler.writeWindow = 20 * time.Millisecond
	ctx := context.WithValue(context.Background(), operatorKey{}, "alice")
	root, err := fixture.service.Open(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	cursor := root.Ready.Cursor
	root.Close()
	publish(t, fixture, "pln_visible", 1)
	writer := &stalledWriter{failedFlushWriter: failedFlushWriter{header: http.Header{}}}
	started := time.Now()
	fixture.handler.ServeHTTP(writer, request("alice", cursor))
	if elapsed := time.Since(started); elapsed > time.Second || !writer.timedOut || writer.cleared {
		t.Fatalf("stalled write not bounded or deadline cleared before response finalization: %v %+v", elapsed, writer)
	}
	srv := server(t, fixture)
	response := connect(t, srv, "alice", cursor)
	frame := readEvent(t, bufio.NewReader(response.Body))
	if frame.Sequence != 1 {
		t.Fatal("stalled client lost replay")
	}
}

func TestSSERetentionGapRequiresExplicitCatchupBeforeReadyHeaders(t *testing.T) {
	fixture := setup(t)
	fixture.handler.cfg.Refresh = func(context.Context) error { return domain.ErrSourceRetentionGap }
	srv := server(t, fixture)
	response := connect(t, srv, "alice", "")
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != 409 || response.Header.Get("X-Conduit-Stream-ID") != "" || strings.Contains(string(body), "retention gap") {
		t.Fatalf("reset not sanitized before ready: %d %s", response.StatusCode, body)
	}
	state, _ := fixture.journal.Snapshot(context.Background())
	if len(state.Streams) != 0 {
		t.Fatal("retention reset allocated a ready stream")
	}
}

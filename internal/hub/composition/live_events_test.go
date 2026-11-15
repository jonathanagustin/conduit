package composition

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	liveapp "github.com/jonathanagustin/conduit/internal/liveevents/application"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/liveevents/adapters/filejournal"
	live "github.com/jonathanagustin/conduit/internal/liveevents/domain"
	packaging "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	packaginghttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/httpapi"
)

type livePackagingReader struct {
	mu     sync.Mutex
	events []packaging.StepEvent
	err    error
	calls  int
}

func (*livePackagingReader) Configured() bool { return true }
func (r *livePackagingReader) List(context.Context) ([]packaging.RunStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	return []packaging.RunStatus{{RunID: "run_dev", Configured: true, TargetPlanRef: "dev/pln_dev"}}, nil
}
func (r *livePackagingReader) Read(_ context.Context, id string) (packaging.RunStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return packaging.RunStatus{}, r.err
	}
	if id != "run_dev" {
		return packaging.RunStatus{}, packaginghttp.ErrNotFound
	}
	return packaging.RunStatus{RunID: id, Configured: true, TargetPlanRef: "dev/pln_dev"}, nil
}
func (r *livePackagingReader) Events(context.Context, string) ([]packaging.StepEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]packaging.StepEvent(nil), r.events...), r.err
}

func liveNativeFixture(t *testing.T) (*controlplane.FileRepository, string) {
	t.Helper()
	root := t.TempDir()
	repository, err := controlplane.NewFileRepository(filepath.Join(root, "native"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct{ user, environment string }{{"alice", "dev"}, {"bob", "prod"}} {
		if _, err := repository.PutTeam(context.Background(), controlplane.Team{Name: pair.user, MemberUsers: []string{pair.user}, Revision: 1}); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.PutResourceGrant(context.Background(), controlplane.ResourceGrant{ID: "grant_" + pair.user, Team: pair.user, ResourceRID: "environment/" + pair.environment, Roles: []string{"environment:operator"}, Revision: 1}); err != nil {
			t.Fatal(err)
		}
		plan := controlplane.PlanRecord{ID: "pln_" + pair.environment, Status: controlplane.PlanAwaitingApproval, RequiredApprovals: 1}
		plan.Plan.Environment = pair.environment
		if _, err := repository.CreatePlan(context.Background(), plan); err != nil {
			t.Fatal(err)
		}
	}
	return repository, filepath.Join(root, "delivery.json")
}
func liveHubServer(t *testing.T, repository controlplane.Repository, journalPath string, reader packaginghttp.Reader) *httptest.Server {
	t.Helper()
	journal, err := filejournal.Open(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true}, Options{LiveEventJournal: journal, PackagingStatus: reader})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}
func liveConnect(t *testing.T, server *httptest.Server, user, cursor string) *http.Response {
	t.Helper()
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/events", nil)
	request.Header.Set("X-Platform-Subject", user)
	if cursor != "" {
		request.Header.Set("Last-Event-ID", cursor)
	}
	client := *server.Client()
	client.Timeout = 12 * time.Second
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}
func liveReadFrame(t *testing.T, reader *bufio.Reader) live.Envelope {
	t.Helper()
	var id, kind string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "id: ") {
			id = strings.TrimSpace(strings.TrimPrefix(line, "id: "))
		}
		if strings.HasPrefix(line, "event: ") {
			kind = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
		}
		if strings.HasPrefix(line, "data: ") {
			var envelope live.Envelope
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Cursor != id || envelope.Kind != kind {
				t.Fatal("SSE framing mismatch")
			}
			return envelope
		}
	}
}
func TestLiveHubCommittedFanoutApprovalReportPackagingAndRestart(t *testing.T) {
	repository, path := liveNativeFixture(t)
	packagingReader := &livePackagingReader{}
	server := liveHubServer(t, repository, path, packagingReader)
	alice := liveConnect(t, server, "alice", "")
	bob := liveConnect(t, server, "bob", "")
	if alice.StatusCode != 200 || bob.StatusCode != 200 || alice.Header.Get("X-Conduit-Event-Sequence") != "0" {
		t.Fatalf("not ready: %d %d", alice.StatusCode, bob.StatusCode)
	}
	// Exercise the actual governed approval HTTP route, not a precommit callback.
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/plans/pln_dev/approvals", strings.NewReader(`{"rationale":"reviewed fixture"}`))
	req.Header.Set("X-Platform-Subject", "alice")
	req.Header.Set("Content-Type", "application/json")
	approved, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, approved.Body)
	approved.Body.Close()
	if approved.StatusCode != 200 {
		t.Fatalf("approval status=%d", approved.StatusCode)
	}
	if _, err := repository.UpdatePlan(context.Background(), "pln_prod", func(p *controlplane.PlanRecord) error { p.Status = controlplane.PlanCancelled; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PutEnvironmentReport(context.Background(), controlplane.EnvironmentReport{Environment: "dev"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateChangeRequest(context.Background(), controlplane.ChangeRequest{ID: "crq_dev", Environment: "dev", Author: controlplane.Principal{Username: "alice"}}); err != nil {
		t.Fatal(err)
	}
	packagingReader.mu.Lock()
	packagingReader.events = []packaging.StepEvent{{Sequence: 1, At: time.Now().UTC(), Step: packaging.StepAssess, Outcome: string(packaging.OutcomePass)}}
	packagingReader.mu.Unlock()
	expected := map[string]bool{live.IndexID(live.Subject{Type: "plan", ID: "pln_dev"}): true, live.IndexID(live.Subject{Type: "change-request", ID: "crq_dev"}): true, live.IndexID(live.Subject{Type: "environment", ID: "dev"}): true, "run_dev": true}
	reader := bufio.NewReader(alice.Body)
	var last live.Envelope
	for sequence := uint64(1); sequence <= 4; sequence++ {
		frame := liveReadFrame(t, reader)
		if frame.Sequence != sequence || !expected[frame.Resource.ID] {
			t.Fatalf("unexpected alice frame: %+v", frame)
		}
		delete(expected, frame.Resource.ID)
		last = frame
	}
	other := liveReadFrame(t, bufio.NewReader(bob.Body))
	if other.Sequence != 1 || other.Resource.ID != live.IndexID(live.Subject{Type: "plan", ID: "pln_prod"}) {
		t.Fatalf("cross-operator leak or sequence gap: %+v", other)
	}
	alice.Body.Close()
	bob.Body.Close()
	server.Close()
	if _, err := repository.UpdatePlan(context.Background(), "pln_dev", func(p *controlplane.PlanRecord) error { p.Status = controlplane.PlanCancelled; return nil }); err != nil {
		t.Fatal(err)
	}
	restarted := liveHubServer(t, repository, path, packagingReader)
	resumed := liveConnect(t, restarted, "alice", last.Cursor)
	if resumed.StatusCode != 200 || resumed.Header.Get("X-Conduit-Stream-ID") != last.StreamID || resumed.Header.Get("X-Conduit-Event-Sequence") != "4" {
		t.Fatalf("restart lost ready cursor: %d %v", resumed.StatusCode, resumed.Header)
	}
	next := liveReadFrame(t, bufio.NewReader(resumed.Body))
	if next.Sequence != 5 || next.Resource.ID != live.IndexID(live.Subject{Type: "plan", ID: "pln_dev"}) {
		t.Fatalf("restart lost committed write: %+v", next)
	}
	wrongOwner := liveConnect(t, restarted, "bob", last.Cursor)
	if wrongOwner.StatusCode != 409 {
		t.Fatal("cursor became cross-operator authority")
	}
}

func TestLiveHubRefusesAnonymousNoGrantAndUnavailableSources(t *testing.T) {
	repository, path := liveNativeFixture(t)
	for _, configuredJournal := range []bool{false, true} {
		options := Options{}
		if configuredJournal {
			journal, err := filejournal.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			options.LiveEventJournal = journal
		}
		handler, err := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true}, options)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			user   string
			status int
		}{{"", 401}, {"outsider", 403}, {"alice", 503}} {
			req := httptest.NewRequest(http.MethodGet, "http://localhost/v1/events", nil)
			req.Header.Set("X-Platform-Subject", tc.user)
			writer := httptest.NewRecorder()
			// A recorder lacks write deadlines. Use the real server for source readiness.
			if configuredJournal && tc.user == "alice" {
				server := httptest.NewServer(handler)
				response := liveConnect(t, server, tc.user, "")
				if response.StatusCode != tc.status {
					t.Fatal(response.StatusCode)
				}
				response.Body.Close()
				server.Close()
				continue
			}
			handler.ServeHTTP(writer, req)
			if writer.Code != tc.status {
				t.Fatalf("journal=%v user=%q status=%d", configuredJournal, tc.user, writer.Code)
			}
		}
	}
}

func TestLiveHubRevalidatesCredentialAndNativeMembership(t *testing.T) {
	repository, path := liveNativeFixture(t)
	reader := &livePackagingReader{}
	server := liveHubServer(t, repository, path, reader)
	response := liveConnect(t, server, "alice", "")
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	if _, err := repository.PutTeam(context.Background(), controlplane.Team{Name: "alice", Revision: 2}); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil || strings.Contains(string(body), "data:") {
		t.Fatalf("revoked stream leaked data: %q %v", body, err)
	}
	denied := liveConnect(t, server, "alice", "")
	if denied.StatusCode != 403 {
		t.Fatal("revoked member reconnected")
	}
	// A configured resolver is called again, rather than using the ordinary
	// once-per-request carrier cache. This models token expiry on a live request.
	service := controlplane.NewGovernanceService(repository)
	var calls int
	options := controlplane.HandlerOptions{OperatorIdentityResolver: func(*http.Request) (controlplane.IdentityClaims, error) {
		calls++
		if calls > 1 {
			return controlplane.IdentityClaims{}, errors.New("expired")
		}
		return controlplane.IdentityClaims{Username: "root"}, nil
	}, BootstrapAdministrators: []string{"root"}}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/v1/events", nil)
	ctx := context.WithValue(request.Context(), liveRequestKey{}, request)
	if _, err := liveOperatorState(ctx, service, options); err != nil {
		t.Fatal(err)
	}
	if _, err := liveOperatorState(ctx, service, options); !errors.Is(err, live.ErrUnauthorized) {
		t.Fatal("expired credential reused")
	}
	uri, _ := url.Parse("spiffe://platform.delivery/operator/root")
	cert := &x509.Certificate{NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(-time.Second), URIs: []*url.URL{uri}}
	request.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{cert}}}
	if _, err := liveOperatorState(ctx, service, controlplane.HandlerOptions{BootstrapAdministrators: []string{"root"}}); !errors.Is(err, live.ErrUnauthorized) {
		t.Fatal("expired TLS operator accepted")
	}
	cert.NotAfter = time.Now().Add(time.Hour)
	agentURI, _ := url.Parse("spiffe://platform.delivery/environment/dev/agent/spoke")
	cert.URIs = []*url.URL{agentURI}
	if _, err := liveOperatorState(ctx, service, controlplane.HandlerOptions{BootstrapAdministrators: []string{"root"}}); !errors.Is(err, live.ErrUnauthorized) {
		t.Fatal("Agent certificate became operator stream identity")
	}
}

func TestLiveHubSourceFailureCannotAllocateOrInventReadiness(t *testing.T) {
	repository, path := liveNativeFixture(t)
	reader := &livePackagingReader{err: errors.New("PRIVATE_SOURCE_ERROR")}
	server := liveHubServer(t, repository, path, reader)
	failed := liveConnect(t, server, "alice", "")
	body, err := io.ReadAll(failed.Body)
	failed.Body.Close()
	if err != nil || failed.StatusCode != 503 || failed.Header.Get("X-Conduit-Stream-ID") != "" || strings.Contains(string(body), "PRIVATE_SOURCE_ERROR") {
		t.Fatalf("source failure escaped: %d %s %v", failed.StatusCode, body, err)
	}
	// Recover the configured source without deleting checkpoints or inventing steps.
	reader.mu.Lock()
	reader.err = nil
	reader.mu.Unlock()
	recovered := liveConnect(t, server, "alice", "")
	if recovered.StatusCode != 200 || recovered.Header.Get("X-Conduit-Event-Sequence") != "0" {
		t.Fatalf("source recovery not ready: %d %v", recovered.StatusCode, recovered.Header)
	}
}

type controlledLiveDrain struct {
	started, release chan struct{}
	calls            atomic.Int32
}

func (d *controlledLiveDrain) DrainCommitted(ctx context.Context, _ string, _ int) (liveapp.DrainResult, error) {
	if d.calls.Add(1) == 1 {
		close(d.started)
	}
	select {
	case <-d.release:
		return liveapp.DrainResult{}, nil
	case <-ctx.Done():
		return liveapp.DrainResult{}, ctx.Err()
	}
}
func TestLiveRefreshCoalescesWithoutBorrowingOperatorCancellation(t *testing.T) {
	root, stop := context.WithCancel(context.Background())
	defer stop()
	drain := &controlledLiveDrain{started: make(chan struct{}), release: make(chan struct{})}
	refresh := newLiveRefresh(root, drain, []string{"native"})
	operator, cancel := context.WithCancel(context.Background())
	defer cancel()
	completed := make(chan error, 1)
	go func() { completed <- refresh(operator) }()
	select {
	case <-drain.started:
	case <-time.After(time.Second):
		t.Fatal("source did not start")
	}
	cancel()
	if err := refresh(operator); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter=%v", err)
	}
	select {
	case err := <-completed:
		t.Fatalf("operator cancelled shared source: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(drain.release)
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("source did not finish")
	}
	if err := refresh(context.Background()); err != nil || drain.calls.Load() != 1 {
		t.Fatalf("source polls not coalesced: %v calls=%d", err, drain.calls.Load())
	}
	stop()
	if err := refresh(context.Background()); !errors.Is(err, live.ErrUnavailable) {
		t.Fatal("shutdown returned cached readiness")
	}
}

func TestLiveHubDelegationRetainsCarrierBootstrapBoundary(t *testing.T) {
	repository, _ := liveNativeFixture(t)
	if _, err := repository.PutTeam(context.Background(), controlplane.Team{Name: "service", MemberUsers: []string{"service"}, Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PutResourceGrant(context.Background(), controlplane.ResourceGrant{ID: "delegation", Team: "service", ResourceRID: "platform/root", Operations: []string{controlplane.OperationActOnBehalf}, Revision: 1}); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		represented string
		status      int
	}{{"root", 403}, {"alice", 503}} {
		req := httptest.NewRequest(http.MethodGet, "http://localhost/v1/events", nil)
		req.Header.Set("X-Platform-Subject", "service")
		req.Header.Set("X-Platform-Service-Account", "true")
		req.Header.Set("X-Platform-On-Behalf-Of", tc.represented)
		writer := httptest.NewRecorder()
		handler.ServeHTTP(writer, req)
		if writer.Code != tc.status {
			t.Fatalf("represented=%s status=%d", tc.represented, writer.Code)
		}
	}
}

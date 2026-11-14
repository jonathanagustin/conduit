package conduitcli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	debugfile "github.com/jonathanagustin/conduit/internal/debugsessions/adapters/filestore"
	debugdomain "github.com/jonathanagustin/conduit/internal/debugsessions/domain"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	"github.com/jonathanagustin/conduit/internal/operator"
	"go.yaml.in/yaml/v4"
)

func TestDebugRequestCLIUsesCurrentHubAuthorityAndSurvivesRestart(t *testing.T) {
	directory := t.TempDir()
	coreDirectory := filepath.Join(directory, "core")
	journalDirectory := filepath.Join(directory, "journal")
	repository, err := controlplane.NewFileRepository(coreDirectory)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := debugfile.New(journalDirectory)
	if err != nil {
		t.Fatal(err)
	}
	bytesState, err := os.ReadFile("../../examples/example-foundation.json")
	if err != nil {
		t.Fatal(err)
	}
	var state controlplane.DesiredState
	if err := json.Unmarshal(bytesState, &state); err != nil {
		t.Fatal(err)
	}
	namespace := state.Entities[0].Namespace
	state.Environment.Namespaces = nil
	seen := map[string]bool{}
	for _, entity := range state.Entities {
		if !seen[entity.Namespace] {
			seen[entity.Namespace] = true
			state.Environment.Namespaces = append(state.Environment.Namespaces, controlplane.NamespaceTarget{RID: "ri.namespace." + entity.Namespace, Name: entity.Namespace})
		}
	}
	if _, err := controlplane.NewLifecycle(repository).CreatePlan(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	governance := controlplane.NewGovernanceService(repository)
	if _, err := governance.PutTeam(t.Context(), controlplane.Team{Name: "operators", DisplayName: "Operators", AdminUsers: []string{"operator"}, MemberUsers: []string{"operator"}}); err != nil {
		t.Fatal(err)
	}
	for _, resource := range []string{"environment/" + state.Environment.ID, "environment/" + state.Environment.ID + "/namespace/" + namespace} {
		if _, err := governance.PutGrant(t.Context(), controlplane.ResourceGrant{ResourceRID: resource, Team: "operators", Roles: []string{"environment:debug-requester"}}); err != nil {
			t.Fatal(err)
		}
	}
	before, err := repository.ListPlans(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	makeServer := func() *httptest.Server {
		handler, err := composition.NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"}}, composition.Options{DebugSessionJournal: journal})
		if err != nil {
			t.Fatal(err)
		}
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/v1/debug-session-requests") {
				requests.Add(1)
			}
			handler.ServeHTTP(w, r)
		}))
	}
	server := makeServer()
	t.Cleanup(func() { server.Close(); _ = journal.Close() })
	run := func(subject string, args ...string) (string, error) {
		return runMembershipCLI(t, server.URL, "", subject, args...)
	}
	base := []string{"terminal", "request", "create", "--environment", state.Environment.ID, "--namespace", namespace, "--terminal-profile", "requested-profile", "--lifetime", "30m"}
	output, err := run("operator", base...)
	if err != nil {
		t.Fatal(err)
	}
	var created debugdomain.Event
	if err := json.Unmarshal([]byte(output), &created); err != nil {
		t.Fatal(err)
	}
	if !debugdomain.ValidID(created.SessionID) || created.Owner != "operator" || created.Actor != "operator" || created.State != debugdomain.Requested || created.ExpiresAt.Sub(created.CreatedAt) != 30*time.Minute {
		t.Fatal(output)
	}
	replayArgs := append(append([]string{}, base...), "--request-id", created.SessionID)
	output, err = run("operator", replayArgs...)
	if err != nil {
		t.Fatal(err)
	}
	var reused debugdomain.Event
	if err := json.Unmarshal([]byte(output), &reused); err != nil || reused != created {
		t.Fatal("idempotency changed record", output, err)
	}
	if output, err := run("outsider", "terminal", "request", "get", created.SessionID); err == nil || output != "" {
		t.Fatal("unauthorized read", output, err)
	}
	counter := requests.Load()
	for _, args := range [][]string{
		append(append([]string{}, base...), "--output", "table"),
		{"terminal", "request", "create", "-e", state.Environment.ID, "--namespace", namespace, "--terminal-profile", "requested", "--lifetime", "500ms"},
		append(append([]string{}, base...), "--space-id", "spc_not_a_scope"),
	} {
		if output, err := run("operator", args...); err == nil || output != "" {
			t.Fatal("invalid mutation accepted", args, output, err)
		}
	}
	if requests.Load() != counter {
		t.Fatal("invalid local input contacted mutation endpoint")
	}
	if _, err := run("operator", "terminal", "request", "revoke", created.SessionID); err != nil {
		t.Fatal(err)
	}
	server.Close()
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	repository, err = controlplane.NewFileRepository(coreDirectory)
	if err != nil {
		t.Fatal(err)
	}
	journal, err = debugfile.New(journalDirectory)
	if err != nil {
		t.Fatal(err)
	}
	server = makeServer()
	output, err = run("operator", "terminal", "request", "history", created.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	var history []debugdomain.Event
	if err := json.Unmarshal([]byte(output), &history); err != nil || len(history) != 2 || history[0] != created || history[1].State != debugdomain.Revoked {
		t.Fatal(output, err)
	}
	if _, err := run("operator", "terminal", "request", "delete", created.SessionID); err != nil {
		t.Fatal(err)
	}
	if output, err := run("operator", "terminal", "request", "get", created.SessionID); err == nil || output != "" {
		t.Fatal("deleted request returned as live metadata", output, err)
	}
	after, err := repository.ListPlans(t.Context(), "")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("metadata changed delivery Plans", err)
	}
}

type debugProfileService struct {
	*fakeService
	writes int
}

func (s *debugProfileService) CreateDebugRequest(context.Context, operator.DebugRequestDraft) (debugdomain.Event, error) {
	s.writes++
	return debugdomain.Event{}, nil
}
func (s *debugProfileService) ListDebugRequests(context.Context, debugdomain.Scope, string, int) (operator.DebugRequestPage, error) {
	return operator.DebugRequestPage{}, nil
}
func (s *debugProfileService) GetDebugRequest(context.Context, string) (debugdomain.Event, error) {
	return debugdomain.Event{}, nil
}
func (s *debugProfileService) DebugRequestHistory(context.Context, string) ([]debugdomain.Event, error) {
	return nil, nil
}
func (s *debugProfileService) MutateDebugRequest(context.Context, string, string) (debugdomain.Event, error) {
	s.writes++
	return debugdomain.Event{}, nil
}
func TestProfileOutputIsRecheckedBeforeDebugMetadataMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	document := profileDocument{Version: 1, Current: "saved", Profiles: map[string]connectionProfile{"saved": {Name: "saved", Server: "https://hub.example.test", Output: "table", SPIFFEEndpoint: "unix:///run/spire/agent.sock", HubSPIFFEID: "spiffe://platform.delivery/hub"}}}
	body, err := yaml.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	service := &debugProfileService{fakeService: &fakeService{}}
	var output bytes.Buffer
	root := NewRoot(Runtime{Stdout: &output, Stderr: &output, Getenv: func(name string) string {
		if name == "CONDUIT_CONFIG_FILE" {
			return path
		}
		return ""
	}, ServiceFactory: func(context.Context, ConnectionConfig) (Service, func(), error) { return service, func() {}, nil }})
	root.SetArgs([]string{"terminal", "request", "create", "-e", "env", "--namespace", "app", "--terminal-profile", "requested"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "json or yaml") {
		t.Fatal(err)
	}
	if service.writes != 0 {
		t.Fatal("profile output validation happened after mutation")
	}
}

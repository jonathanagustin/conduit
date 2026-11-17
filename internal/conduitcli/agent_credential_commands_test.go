package conduitcli

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	"github.com/jonathanagustin/conduit/internal/identity/adapters/revocationmemory"
	"github.com/jonathanagustin/conduit/internal/identity/domain"
)

// Exercise Cobra, the normal connection factory, the operator HTTP adapter,
// the assembled Hub's authorization and the deny-list it actually enforces.
func TestAgentCredentialCommandsUseHubAuthorityAndTheEnforcedDenyList(t *testing.T) {
	t.Parallel()
	repository := controlplane.NewMemoryRepository()
	store := revocationmemory.New()
	governance := controlplane.NewGovernanceService(repository)
	if _, err := governance.PutTeam(t.Context(), controlplane.Team{Name: "readers", DisplayName: "Readers", AdminUsers: []string{"alice"}, MemberUsers: []string{"alice"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(t.Context(), controlplane.ResourceGrant{ResourceRID: "environment/prod", Team: "readers", Operations: []string{controlplane.OperationReadResource}}); err != nil {
		t.Fatal(err)
	}
	handler, err := composition.NewHub(repository, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"},
	}, composition.Options{AgentCredentials: store})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	run := func(subject string, args ...string) (string, error) {
		var output, diagnostics bytes.Buffer
		root := NewRoot(Runtime{Stdout: &output, Stderr: &diagnostics, Getenv: func(string) string { return "" }})
		root.SetArgs(append([]string{"--server", server.URL, "--development-subject", subject}, args...))
		err := root.Execute()
		return output.String(), err
	}

	output, err := run("admin", "agent-credential", "revoke", "prod", "--agent-id", "primary", "--reason", "key leaked")
	if err != nil {
		t.Fatal(err)
	}
	var agent domain.AgentRevocation
	if err := json.Unmarshal([]byte(output), &agent); err != nil {
		t.Fatal(err)
	}
	if agent.Scope != domain.RevocationAgent || agent.AgentID != "primary" || agent.CreatedBy != "admin" || agent.NotBefore.IsZero() || agent.Reason != "key leaked" {
		t.Fatalf("revocation = %s", output)
	}
	if _, err := run("admin", "agent-credential", "revoke", "prod", "--serial", "0A:1B", "--reason", "one certificate"); err != nil {
		t.Fatal(err)
	}
	if _, err := run("admin", "agent-credential", "revoke", "prod", "--all-agents", "--not-before", "2026-11-17T12:00:00Z", "--reason", "incident"); err != nil {
		t.Fatal(err)
	}
	// What the CLI wrote is what the Hub's enforcement reads.
	held, err := store.Revocations(t.Context(), "prod")
	if err != nil || len(held) != 3 {
		t.Fatalf("deny-list = %#v, %v; want three revocations", held, err)
	}
	scopes := map[string]domain.AgentRevocation{}
	for _, revocation := range held {
		scopes[revocation.Scope] = revocation
	}
	if scopes[domain.RevocationSerial].Serial != "a1b" || scopes[domain.RevocationEnvironment].NotBefore.Format("2006-01-02T15:04:05Z") != "2026-11-17T12:00:00Z" {
		t.Fatalf("stored revocations = %#v", held)
	}

	// A reader may look and may not change.
	output, err = run("alice", "agent-credential", "revocations", "prod")
	if err != nil {
		t.Fatal(err)
	}
	var listed []domain.AgentRevocation
	if err := json.Unmarshal([]byte(output), &listed); err != nil || len(listed) != 3 {
		t.Fatalf("reader's list = %s, %v", output, err)
	}
	if output, err = run("alice", "agent-credential", "list", "prod"); err != nil || strings.TrimSpace(output) != "[]" {
		t.Fatalf("credentials of an Environment no Agent has reached = %q, %v", output, err)
	}
	for _, args := range [][]string{
		{"agent-credential", "revoke", "prod", "--agent-id", "secondary", "--reason", "not mine to make"},
		{"agent-credential", "unrevoke", "prod", agent.ID},
		{"agent-credential", "revocations", "staging"},
	} {
		output, err := run("alice", args...)
		if err == nil || !strings.Contains(err.Error(), "403") || output != "" {
			t.Fatalf("denial discarded for %v: %q, %v", args, output, err)
		}
	}

	// Mistakes are refused before any request is made.
	for name, args := range map[string][]string{
		"nothing selected":      {"agent-credential", "revoke", "prod", "--reason", "r"},
		"two things selected":   {"agent-credential", "revoke", "prod", "--agent-id", "primary", "--all-agents", "--reason", "r"},
		"no reason":             {"agent-credential", "revoke", "prod", "--all-agents"},
		"serial is not hex":     {"agent-credential", "revoke", "prod", "--serial", "zz", "--reason", "r"},
		"instant is not a time": {"agent-credential", "revoke", "prod", "--all-agents", "--not-before", "yesterday", "--reason", "r"},
	} {
		if output, err := run("admin", args...); err == nil || output != "" {
			t.Errorf("%s was accepted: %q", name, output)
		}
	}

	output, err = run("admin", "agent-credential", "unrevoke", "prod", agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	var removed domain.AgentRevocation
	if err := json.Unmarshal([]byte(output), &removed); err != nil || removed.ID != agent.ID {
		t.Fatalf("unrevoke returned %s, %v", output, err)
	}
	if held, _ := store.Revocations(t.Context(), "prod"); len(held) != 2 {
		t.Fatalf("after unrevoke the deny-list holds %d, want 2 (the reader's refused attempts changed nothing)", len(held))
	}
	if _, err := run("admin", "agent-credential", "unrevoke", "prod", agent.ID); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("lifting a revocation twice: %v", err)
	}
}

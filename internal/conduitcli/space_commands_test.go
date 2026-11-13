package conduitcli

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	spacememory "github.com/jonathanagustin/conduit/internal/spaces/adapters/memory"
	"github.com/jonathanagustin/conduit/internal/spaces/domain"
)

// Exercise Cobra, the normal connection factory, operator service/HTTP adapter,
// actual Hub authentication/authorization, and persisted native assignment.
func TestSpaceCommandsUseHubAuthorityAndPersistedBindings(t *testing.T) {
	t.Parallel()
	repository := controlplane.NewMemoryRepository()
	store := spacememory.New()
	governance := controlplane.NewGovernanceService(repository)
	if _, err := governance.PutTeam(t.Context(), controlplane.Team{Name: "readers", DisplayName: "Readers", AdminUsers: []string{"alice"}, MemberUsers: []string{"alice"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(t.Context(), controlplane.ResourceGrant{ResourceRID: "environment/visible", Team: "readers", Operations: []string{controlplane.OperationReadResource}}); err != nil {
		t.Fatal(err)
	}
	handler, err := composition.NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}}, composition.Options{Spaces: store})
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
	output, err := run("admin", "space", "create", "Visible", "--description", "group metadata")
	if err != nil {
		t.Fatal(err)
	}
	var first domain.Space
	if err := json.Unmarshal([]byte(output), &first); err != nil {
		t.Fatal(err)
	}
	if first.Name != "Visible" || first.Description != "group metadata" || first.CreatedBy != "admin" || first.ValueTier != "CONDUIT_NATIVE" || !domain.ValidID(first.ID) {
		t.Fatalf("native identity/provenance lost: %s", output)
	}
	output, err = run("admin", "space", "create", "Hidden")
	if err != nil {
		t.Fatal(err)
	}
	var second domain.Space
	if err := json.Unmarshal([]byte(output), &second); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct{ space, kind, id string }{{first.ID, "environment", "visible"}, {first.ID, "PRODUCT", "private:product"}, {second.ID, "ENVIRONMENT", "hidden"}} {
		if _, err := run("admin", "space", "bind", entry.space, entry.kind, entry.id); err != nil {
			t.Fatal(err)
		}
	}
	output, err = run("alice", "space", "list")
	if err != nil {
		t.Fatal(err)
	}
	var visible []domain.Space
	if err := json.Unmarshal([]byte(output), &visible); err != nil {
		t.Fatal(err)
	}
	if len(visible) != 1 || visible[0] != first {
		t.Fatalf("visibility lost: %s", output)
	}
	output, err = run("alice", "space", "get", first.ID)
	if err != nil {
		t.Fatal(err)
	}
	var read domain.Space
	if err := json.Unmarshal([]byte(output), &read); err != nil || read != first {
		t.Fatalf("get differs: %s, %v", output, err)
	}
	output, err = run("alice", "space", "bindings", first.ID)
	if err != nil {
		t.Fatal(err)
	}
	var bindings []domain.Binding
	if err := json.Unmarshal([]byte(output), &bindings); err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 || bindings[0].SpaceID != first.ID || bindings[0].Resource.ID != "visible" || bindings[0].CreatedBy != "admin" || bindings[0].ValueTier != "CONDUIT_NATIVE" {
		t.Fatalf("independent resource authorization/provenance lost: %s", output)
	}
	for _, args := range [][]string{{"space", "create", "Denied"}, {"space", "get", second.ID}, {"space", "bindings", second.ID}, {"space", "bind", second.ID, "ENVIRONMENT", "visible"}} {
		output, err := run("alice", args...)
		if err == nil || !strings.Contains(err.Error(), "403") || output != "" {
			t.Fatalf("denial discarded for %v: %q, %v", args, output, err)
		}
	}
	if _, err := run("admin", "space", "bind", second.ID, "ENVIRONMENT", "visible"); err == nil || !strings.Contains(err.Error(), "409") {
		t.Fatalf("immutable assignment conflict discarded: %v", err)
	}
	saved, err := store.GetBinding(t.Context(), domain.Resource{Kind: domain.Environment, ID: "visible"})
	if err != nil || saved.SpaceID != first.ID {
		t.Fatalf("denied mutation changed assignment: %#v, %v", saved, err)
	}
	all, err := store.ListSpaces(t.Context())
	if err != nil || len(all) != 2 {
		t.Fatalf("denied create persisted: %#v, %v", all, err)
	}
}

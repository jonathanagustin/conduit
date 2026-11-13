package conduitcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	spacememory "github.com/jonathanagustin/conduit/internal/spaces/adapters/memory"
	"github.com/jonathanagustin/conduit/internal/spaces/domain"
)

func TestSpaceSelectionPaginatesOnHubAndPublishesForAnAuthorizedGuest(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	store := spacememory.New()
	handler, err := composition.NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}}, composition.Options{Spaces: store})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "profiles.yaml")
	run := func(subject string, args ...string) (string, error) {
		var output bytes.Buffer
		root := NewRoot(Runtime{Stdout: &output, Stderr: &output, Getenv: func(key string) string {
			if key == "CONDUIT_CONFIG_FILE" {
				return configPath
			}
			return ""
		}})
		root.SetArgs(append([]string{"--server", server.URL, "--development-subject", subject}, args...))
		err := root.Execute()
		return output.String(), err
	}
	createSpace := func(name string) domain.Space {
		output, err := run("admin", "space", "create", name)
		if err != nil {
			t.Fatal(err)
		}
		var value domain.Space
		if err := json.Unmarshal([]byte(output), &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	first, second := createSpace("Shared"), createSpace("Other")
	raw, err := os.ReadFile("../../examples/example-foundation.json")
	if err != nil {
		t.Fatal(err)
	}
	var desired controlplane.DesiredState
	if err := json.Unmarshal(raw, &desired); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		desired.Environment.ID = fmt.Sprintf("space-env-%02d", i)
		if _, err := controlplane.NewLifecycle(repository).CreatePlan(t.Context(), desired); err != nil {
			t.Fatal(err)
		}
		selected := first
		if i == 24 {
			selected = second
		}
		if _, err := run("admin", "space", "bind", selected.ID, "ENVIRONMENT", desired.Environment.ID); err != nil {
			t.Fatal(err)
		}
	}
	output, err := run("admin", "--space-id", first.ID, "environment", "list")
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Environments []controlplane.EnvironmentCatalogRecord
	}
	if err := json.Unmarshal([]byte(output), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Environments) != 24 {
		t.Fatalf("selection or a later page was lost: %d %s", len(listed.Environments), output)
	}
	for _, record := range listed.Environments {
		if record.Environment.ID == "space-env-24" {
			t.Fatal("other Space survived server selection")
		}
	}
	governance := controlplane.NewGovernanceService(repository)
	if _, err := governance.PutTeam(t.Context(), controlplane.Team{Name: "guests", DisplayName: "Guests", AdminUsers: []string{"external-guest"}, MemberUsers: []string{"external-guest"}}); err != nil {
		t.Fatal(err)
	}
	for _, grant := range []controlplane.ResourceGrant{
		{ResourceRID: "environment/space-env-00", Team: "guests", Operations: []string{controlplane.OperationReadResource}},
		{ResourceRID: "global/artifact", Team: "guests", Operations: []string{controlplane.OperationCreateArtifact}},
		{ResourceRID: "global/product", Team: "guests", Operations: []string{controlplane.OperationCreateProduct, controlplane.OperationCreateRelease, controlplane.OperationReadResource}},
	} {
		if _, err := governance.PutGrant(t.Context(), grant); err != nil {
			t.Fatal(err)
		}
	}
	output, err = run("external-guest", "--space-id", first.ID, "environment", "list")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(output), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Environments) != 1 || listed.Environments[0].Environment.ID != "space-env-00" {
		t.Fatalf("Space selection granted Environment access: %s", output)
	}
	manifest := filepath.Join(t.TempDir(), "manifest.yml")
	writeManifest := func(name string) {
		t.Helper()
		body := `manifest-version: "1.0"
product-group: com.example
product-name: ` + name + `
product-version: 1.2.3
product-type: terraform.v1
extensions:
  terraform-repository-url: oci://registry.example/terraform/network:1.2.3@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
		if err := os.WriteFile(manifest, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest("guest-network")
	if _, err := run("external-guest", "--space-id", first.ID, "product-release", "create", "--manifest", manifest); err != nil {
		t.Fatal(err)
	}
	binding, err := store.GetBinding(t.Context(), domain.Resource{Kind: domain.Product, ID: "com.example:guest-network"})
	if err != nil || binding.SpaceID != first.ID || binding.CreatedBy != "external-guest" {
		t.Fatalf("guest publication grouping/provenance: %#v, %v", binding, err)
	}
	helmManifest, _, _ := writeValuesFlagFixture(t, "helm-chart.v1")
	if _, err := run("external-guest", "--space-id", first.ID, "product-release", "create", "--manifest", helmManifest); err != nil {
		t.Fatal(err)
	}
	helmBinding, err := store.GetBinding(t.Context(), domain.Resource{Kind: domain.Product, ID: "com.example:application"})
	if err != nil || helmBinding.SpaceID != first.ID || helmBinding.CreatedBy != "external-guest" {
		t.Fatalf("Helm publication dropped native selection: %#v, %v", helmBinding, err)
	}
	writeManifest("guest-network")
	if _, err := run("admin", "--space-id", second.ID, "product-release", "create", "--manifest", manifest); err == nil || !strings.Contains(err.Error(), "409") {
		t.Fatalf("explicit selection silently moved existing Product: %v", err)
	}
	preserved, err := store.GetBinding(t.Context(), domain.Resource{Kind: domain.Product, ID: "com.example:guest-network"})
	if err != nil || preserved != binding {
		t.Fatalf("conflict changed initial assignment/provenance: %#v, %v", preserved, err)
	}
	for _, team := range []controlplane.Team{{Name: "read-only", DisplayName: "Read only", AdminUsers: []string{"reader"}, MemberUsers: []string{"reader"}}, {Name: "automation", DisplayName: "Automation", AdminUsers: []string{"robot"}, MemberUsers: []string{"robot", "admin"}}} {
		if _, err := governance.PutTeam(t.Context(), team); err != nil {
			t.Fatal(err)
		}
	}
	for _, grant := range []controlplane.ResourceGrant{{ResourceRID: "environment/space-env-00", Team: "read-only", Operations: []string{controlplane.OperationReadResource}}, {ResourceRID: "platform/root", Team: "automation", Operations: []string{controlplane.OperationActOnBehalf}}} {
		if _, err := governance.PutGrant(t.Context(), grant); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest("reader-network")
	if _, err := run("reader", "--space-id", first.ID, "product-release", "create", "--manifest", manifest); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("readable Space granted publication: %v", err)
	}
	if _, err := store.GetBinding(t.Context(), domain.Resource{Kind: domain.Product, ID: "com.example:reader-network"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unauthorized publisher reserved grouping: %v", err)
	}
	delegated := httptest.NewRequest(http.MethodGet, "/v1/environments?spaceId="+first.ID, nil)
	delegated.Header.Set("X-Platform-Subject", "robot")
	delegated.Header.Set("X-Platform-Service-Account", "true")
	delegated.Header.Set("X-Platform-On-Behalf-Of", "admin")
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, delegated)
	if denied.Code != http.StatusForbidden || strings.Contains(denied.Body.String(), "space-env-") {
		t.Fatalf("selected listing inherited bootstrap authority: %d %s", denied.Code, denied.Body)
	}
	writeManifest("denied-network")
	if _, err := run("external-guest", "--space-id", second.ID, "product-release", "create", "--manifest", manifest); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("guest published into inaccessible Space: %v", err)
	}
	if _, err := store.GetBinding(t.Context(), domain.Resource{Kind: domain.Product, ID: "com.example:denied-network"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("denied publication reserved grouping: %v", err)
	}
	publications, err := repository.ListProductPublications(t.Context(), "com.example:denied-network")
	if err != nil || len(publications) != 0 {
		t.Fatalf("denied publication persisted: %#v, %v", publications, err)
	}
}

func TestConfiguredSpacePersistsAndExplicitSelectionOverridesIt(t *testing.T) {
	const first = "space_0123456789abcdef0123456789abcdef"
	const second = "space_fedcba9876543210fedcba9876543210"
	path := filepath.Join(t.TempDir(), "profile.yaml")
	execute := func(envSpace string, factory serviceFactory, args ...string) (string, error) {
		var output bytes.Buffer
		root := NewRoot(Runtime{Stdout: &output, Stderr: &output, Getenv: func(key string) string {
			switch key {
			case "CONDUIT_CONFIG_FILE":
				return path
			case "CONDUIT_SPACE_ID":
				return envSpace
			}
			return ""
		}, ServiceFactory: factory})
		root.SetArgs(args)
		err := root.Execute()
		return output.String(), err
	}
	output, err := execute("", nil, "configure", "--server", "https://hub.example.test", "--bearer-token-file", "/run/operator/token", "--space-id", first)
	if err != nil || !strings.Contains(output, first) {
		t.Fatalf("configure dropped Space: %s, %v", output, err)
	}
	contents, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(contents), first) {
		t.Fatalf("persisted profile dropped Space: %s, %v", contents, err)
	}
	for _, test := range []struct {
		name, env string
		flags     []string
		want      string
	}{{name: "profile", want: first}, {name: "explicit", flags: []string{"--space-id", second}, want: second}, {name: "environment", env: second, want: second}, {name: "explicit empty", flags: []string{"--space-id", ""}, want: ""}, {name: "flag overrides environment", env: second, flags: []string{"--space-id", first}, want: first}} {
		t.Run(test.name, func(t *testing.T) {
			factory := func(_ context.Context, config ConnectionConfig) (Service, func(), error) {
				if config.SpaceID != test.want {
					t.Fatalf("selection=%q, want %q", config.SpaceID, test.want)
				}
				return &fakeService{}, func() {}, nil
			}
			if _, err := execute(test.env, factory, append(test.flags, "environment", "list")...); err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := execute("", nil, "configure", "--server", "https://hub.example.test", "--bearer-token-file", "/run/operator/token", "--space-id", "not-a-space"); err == nil {
		t.Fatal("invalid native selection accepted into profile")
	}
}

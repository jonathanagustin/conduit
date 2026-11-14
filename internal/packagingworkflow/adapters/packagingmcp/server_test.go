package packagingmcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/packagingtools"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/fixtures"
)

func TestInProcessRoundTripRunsThePackagingTools(t *testing.T) {
	source := []byte("name: static-web\nmemory: 128Mi\n")
	files, err := fixtures.Render(source, domain.DigestBytes(source), fixtures.TemplateVersion, fixtures.ToolVersions)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# site\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	server := NewServer(packagingtools.New(packagingtools.WithRepoRoot(root)))
	session, err := server.Connect(context.Background(), serverTransport)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "packaging-tools-test", Version: "1.0.0"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	listed, err := clientSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range listed.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{ToolInspect, ToolValidate, ToolGates, ToolInspectRepo, ToolReadFile, ToolValidateDockerfile} {
		if !names[want] {
			t.Fatalf("missing tool %s in %#v", want, names)
		}
	}

	gated, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name: ToolGates,
		Arguments: map[string]any{
			"source": string(source), "zarf": string(files.Zarf), "cr": string(files.CR),
		},
	})
	if err != nil || gated.IsError {
		t.Fatalf("dry_run_gates = %#v %v", gated, err)
	}
	var validation domain.Validation
	decodeStructured(t, gated, &validation)
	if validation.Outcome != domain.OutcomePass {
		t.Fatalf("gate = %#v", validation)
	}

	manifest := `apiVersion: v1
kind: Service
metadata:
  name: static-web
`
	validated, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      ToolValidate,
		Arguments: map[string]any{"manifests": manifest},
	})
	if err != nil || validated.IsError {
		t.Fatalf("validate_manifests = %#v %v", validated, err)
	}
	var report domain.ManifestReport
	decodeStructured(t, validated, &report)
	if !report.OK {
		t.Fatalf("service-only report = %#v", report)
	}

	inspected, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      ToolInspect,
		Arguments: map[string]any{"ref": ""},
	})
	if err == nil && (inspected == nil || !inspected.IsError) {
		t.Fatal("inspect_image accepted an empty ref")
	}

	repo, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{Name: ToolInspectRepo})
	if err != nil || repo.IsError {
		t.Fatalf("inspect_repo = %#v %v", repo, err)
	}
	var inspect domain.RepoInspect
	decodeStructured(t, repo, &inspect)
	found := false
	for _, entry := range inspect.Tree {
		if entry == "README.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("tree = %#v", inspect.Tree)
	}

	read, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name: ToolReadFile, Arguments: map[string]any{"path": "README.md"},
	})
	if err != nil || read.IsError {
		t.Fatalf("read_file = %#v %v", read, err)
	}
	var file domain.RepoFile
	decodeStructured(t, read, &file)
	if file.Content != "# site\n" {
		t.Fatalf("content = %q", file.Content)
	}

	denied, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name: ToolReadFile, Arguments: map[string]any{"path": "../secret"},
	})
	if err == nil && (denied == nil || !denied.IsError) {
		t.Fatal("read_file accepted a path outside the repository")
	}

	checked, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      ToolValidateDockerfile,
		Arguments: map[string]any{"dockerfile": "FROM alpine:3.20\nUSER root\n", "dockerignore": ""},
	})
	if err != nil || checked.IsError {
		t.Fatalf("validate_dockerfile = %#v %v", checked, err)
	}
	var docker domain.ManifestReport
	decodeStructured(t, checked, &docker)
	if docker.OK {
		t.Fatalf("expected dockerfile errors, got %#v", docker)
	}
}

func decodeStructured(t *testing.T, result *mcp.CallToolResult, dest any) {
	t.Helper()
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}

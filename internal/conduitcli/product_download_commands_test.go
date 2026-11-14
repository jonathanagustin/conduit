package conduitcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	assetmemory "github.com/jonathanagustin/conduit/internal/productassets/adapters/memory"
	assetapp "github.com/jonathanagustin/conduit/internal/productassets/application"
	assets "github.com/jonathanagustin/conduit/internal/productassets/domain"
	spacememory "github.com/jonathanagustin/conduit/internal/spaces/adapters/memory"
	spaces "github.com/jonathanagustin/conduit/internal/spaces/domain"
)

func downloadDeclaration() assets.Manifest {
	return assets.Manifest{APIVersion: assets.SchemaVersion, ProductGroup: "io.conduit", ProductName: "cli", Version: "1.2.3", Assets: []assets.Asset{{Name: "cli-linux.tar.gz", Target: "linux/amd64", URI: "https://example.org/cli.tar.gz", SHA256: strings.Repeat("a", 64)}}}
}
func TestDownloadCommandsReachActualHubAndPreserveNativeAssetMetadata(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	store := assetmemory.New()
	grouping := spacememory.New()
	handler, e := composition.NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}}, composition.Options{ProductAssets: store, Spaces: grouping})
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	run := func(input string, args ...string) (string, error) {
		var out, diagnostics bytes.Buffer
		root := NewRoot(Runtime{Stdin: strings.NewReader(input), Stdout: &out, Stderr: &diagnostics, Getenv: func(string) string { return "" }})
		root.SetArgs(append([]string{"--server", server.URL, "--development-subject", "admin"}, args...))
		e := root.Execute()
		return out.String(), e
	}
	out, e := run("", "space", "create", "CLI grouping")
	if e != nil {
		t.Fatal(e)
	}
	var space spaces.Space
	if e := json.Unmarshal([]byte(out), &space); e != nil {
		t.Fatal(e)
	}
	manifest := downloadDeclaration()
	body, _ := json.Marshal(manifest)
	out, e = run(string(body), "--space-id", space.ID, "product-release", "download-assets", "publish", "-")
	if e != nil {
		t.Fatal(e)
	}
	var ack assetapp.PublicationResult
	if e := json.Unmarshal([]byte(out), &ack); e != nil {
		t.Fatal(e)
	}
	expectedDigest, _ := manifest.Digest()
	if ack.MavenCoordinate != manifest.Coordinate() || ack.Digest != expectedDigest || ack.Status != "CATALOGUED_NOT_EXECUTABLE" {
		t.Fatal(out)
	}
	binding, e := grouping.GetBinding(t.Context(), spaces.Resource{Kind: spaces.Product, ID: manifest.Product()})
	if e != nil || binding.SpaceID != space.ID || binding.CreatedBy != "admin" {
		t.Fatal("selection did not reach publication", binding, e)
	}
	out, e = run("", "product-release", "download-assets", "get", manifest.Product(), manifest.Version)
	if e != nil {
		t.Fatal(e)
	}
	var read assetapp.Published
	if e := json.Unmarshal([]byte(out), &read); e != nil {
		t.Fatal(e)
	}
	if read.Metadata.Digest != expectedDigest || read.Metadata.PreparedBy != "admin" || read.Executable || read.Metadata.Manifest.Assets[0].Target != "linux/amd64" {
		t.Fatal(out)
	}
	out, e = run("", "product-release", "download-assets", "list", manifest.Product())
	if e != nil {
		t.Fatal(e)
	}
	var listed []assetapp.Published
	if e := json.Unmarshal([]byte(out), &listed); e != nil || len(listed) != 1 || listed[0].Metadata.Digest != expectedDigest {
		t.Fatal(out, e)
	}
	manifest.Assets[0].SHA256 = strings.Repeat("b", 64)
	body, _ = json.Marshal(manifest)
	out, e = run(string(body), "product-release", "download-assets", "publish", "-")
	if e == nil || out != "" {
		t.Fatal("conflicting publication produced success output", out, e)
	}
}
func TestDownloadDryRunAndInvalidInputsNeverContactTheHub(t *testing.T) {
	m := downloadDeclaration()
	valid, _ := json.Marshal(m)
	tests := []struct {
		name, input string
		args        []string
		success     bool
	}{
		{"dry run", string(valid), []string{"--dry-run"}, true},
		{"malformed", `{"private":"PVT_DOWNLOAD_INPUT"`, nil, false},
		{"second document", string(valid) + ` {}`, nil, false},
		{"oversize", string(valid) + strings.Repeat(" ", assets.MaxDocumentBytes), nil, false},
		{"unsupported output", string(valid), []string{"--output", "csv"}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			var out, diagnostics bytes.Buffer
			root := NewRoot(Runtime{Stdin: strings.NewReader(test.input), Stdout: &out, Stderr: &diagnostics, Getenv: func(string) string { return "" }, ServiceFactory: func(context.Context, ConnectionConfig) (Service, func(), error) {
				calls++
				return nil, nil, errors.New("unexpected connection")
			}})
			root.SetArgs(append([]string{"product-release", "download-assets", "publish", "-"}, test.args...))
			e := root.Execute()
			if calls != 0 || (e == nil) != test.success {
				t.Fatal("unexpected contact or result", calls, e)
			}
			if !test.success && (out.Len() != 0 || strings.Contains(e.Error(), "PVT_DOWNLOAD_INPUT")) {
				t.Fatal("invalid declaration produced output or leaked diagnostics", out.String(), e)
			}
			if test.success && !strings.Contains(out.String(), "NOT_PUBLISHED") {
				t.Fatal(out.String())
			}
		})
	}
}

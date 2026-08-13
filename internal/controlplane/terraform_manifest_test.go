package controlplane

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestTerraformPublicationIsImmutableDurableAndVisibleWithoutBecomingExecutable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	repository, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	digest := "sha256:" + strings.Repeat("a", 64)
	manifest := TerraformProductManifest{
		ManifestVersion: "1.0",
		ProductGroup:    "com.example",
		ProductName:     "network",
		ProductVersion:  "1.2.3",
		ProductType:     "terraform.v1",
	}
	manifest.Extensions.TerraformRepositoryURL = "oci://registry.example/terraform/network:1.2.3@" + digest

	response := requestJSON(t, handler, http.MethodPost, "/v1/product-publications/terraform", manifest)
	if response.Code != http.StatusCreated {
		t.Fatalf("publish = %d %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodPost, "/v1/product-publications/terraform", manifest)
	if response.Code != http.StatusCreated {
		t.Fatalf("idempotent publish = %d %s", response.Code, response.Body.String())
	}

	catalog, err := ListProductCatalog(context.Background(), repository)
	if err != nil || len(catalog) != 1 || len(catalog[0].Publications) != 1 || len(catalog[0].Releases) != 0 {
		t.Fatalf("catalog=%#v err=%v", catalog, err)
	}
	if catalog[0].Publications[0].Kind != "terraform" || catalog[0].Publications[0].Release != nil {
		t.Fatalf("publication=%#v", catalog[0].Publications[0])
	}

	reopened, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	publications, err := reopened.ListProductPublications(context.Background(), "com.example:network")
	if err != nil || len(publications) != 1 || publications[0].MavenCoordinate != "com.example:network:1.2.3" {
		t.Fatalf("reopened publications=%#v err=%v", publications, err)
	}

	changed := manifest
	changed.Extensions.TerraformRepositoryURL = "oci://registry.example/terraform/network:1.2.3@sha256:" + strings.Repeat("b", 64)
	response = requestJSON(t, NewHandler(reopened, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true}),
		http.MethodPost, "/v1/product-publications/terraform", changed)
	if response.Code != http.StatusConflict {
		t.Fatalf("mutable republish = %d %s", response.Code, response.Body.String())
	}
}

func TestValidateTerraformProductManifestRejectsFloatingReference(t *testing.T) {
	manifest := TerraformProductManifest{
		ManifestVersion: "1.0", ProductGroup: "com.example", ProductName: "network",
		ProductVersion: "1.2.3", ProductType: "terraform.v1",
	}
	manifest.Extensions.TerraformRepositoryURL = "oci://registry.example/terraform/network:1.2.3"
	if err := ValidateTerraformProductManifest(manifest); err == nil || !strings.Contains(err.Error(), "@sha256") {
		t.Fatalf("error=%v", err)
	}
}

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
)

// The Node test regenerates this golden file from explicitly synthetic bytes.
// This side proves those generated native publication inputs meet the actual
// Hub boundary and become executable Releases with the exact reviewed assets.
// No registry push, cluster mutation or released artifact is involved.
func TestPreparedSelfProductsImportThroughTheNativeHub(t *testing.T) {
	contents, err := os.ReadFile("../../testdata/self-product/synthetic-prepared.json")
	if err != nil {
		t.Fatal(err)
	}
	var prepared struct {
		PublicationState string
		Products         []struct {
			Component, MavenCoordinate string
			Channels                   []string
			Publication                struct {
				Method, Path string
				Body         json.RawMessage
			}
		}
		CLI struct {
			Disposition, CataloguePublication string
			Assets                            []json.RawMessage
		}
	}
	if err := json.Unmarshal(contents, &prepared); err != nil {
		t.Fatal(err)
	}
	if prepared.PublicationState != "NOT_PUBLISHED" || len(prepared.Products) != 2 || len(prepared.CLI.Assets) != 6 || prepared.CLI.Disposition != "DOWNLOAD_INVENTORY_ONLY" || prepared.CLI.CataloguePublication != "UNSUPPORTED_BY_CURRENT_SERVICE_PRODUCT_API" {
		t.Fatal("synthetic preparation scope changed without updating its integration proof")
	}
	repository := controlplane.NewMemoryRepository()
	handler, err := composition.NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"fixture-publisher"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, product := range prepared.Products {
		var bundle controlplane.HelmProductReleaseBundle
		decoder := json.NewDecoder(bytes.NewReader(product.Publication.Body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&bundle); err != nil {
			t.Fatal(err)
		}
		if err := controlplane.ValidateHelmProductReleaseBundle(bundle); err != nil {
			t.Fatal(err)
		}
		expectedImageDigest, expectedChartDigest := "sha256:"+string(bytes.Repeat([]byte("2"), 64)), "sha256:"+string(bytes.Repeat([]byte("5"), 64))
		if product.Component == "spoke-agent" {
			expectedImageDigest = "sha256:" + string(bytes.Repeat([]byte("3"), 64))
			expectedChartDigest = "sha256:" + string(bytes.Repeat([]byte("6"), 64))
		}
		request := httptest.NewRequest(product.Publication.Method, product.Publication.Path, bytes.NewReader(product.Publication.Body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Platform-Subject", "fixture-publisher")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("prepared %s input rejected: %d %s", product.Component, response.Code, response.Body)
		}
		publications, err := repository.ListProductPublications(t.Context(), bundle.Manifest.ProductGroup+":"+bundle.Manifest.ProductName)
		if err != nil || len(publications) != 1 {
			t.Fatalf("prepared Product absent: %#v, %v", publications, err)
		}
		publication := publications[0]
		if publication.MavenCoordinate != product.MavenCoordinate || publication.Kind != "helm-chart" || publication.Release == nil {
			t.Fatalf("native descriptor failed to become an executable release: %#v", publication)
		}
		release := publication.Release
		if release.Chart.Digest != expectedChartDigest || len(release.Images) != 1 || release.Images[0].Digest != expectedImageDigest || release.Images[0].Repository != "ghcr.io/jonathanagustin/conduit" || release.Images[0].Tag != product.Component+"-0.17.5" {
			t.Fatalf("descriptor changed exact artifact coordinates: %#v", release)
		}
		if !slices.Equal(publication.ReleaseChannels, []string{"DEV", "RELEASE_CANDIDATE", "RELEASE"}) || !slices.Equal(publication.ReleaseChannels, product.Channels) {
			t.Fatalf("prepared stable channel differs: %#v", publication.ReleaseChannels)
		}
		digest := sha256.Sum256(bundle.DefaultConfig)
		if publication.DefaultConfig == nil || publication.DefaultConfig.Digest != "sha256:"+hex.EncodeToString(digest[:]) || publication.ConfigSchema == nil {
			t.Fatal("publication did not bind the exact default configuration and schema")
		}
		if release.VulnerabilityAssessment != nil || release.ArtifactAvailability != nil {
			t.Fatal("input receipts were promoted into an unperformed scan/availability assessment")
		}
	}
}

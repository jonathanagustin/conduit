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
	assetmemory "github.com/jonathanagustin/conduit/internal/productassets/adapters/memory"
	assetdomain "github.com/jonathanagustin/conduit/internal/productassets/domain"
)

// The Node test regenerates this golden file from explicitly synthetic bytes.
// This side proves those generated native publication inputs meet the actual
// Hub boundary: Hub/Spoke become exact executable Releases, while CLI assets
// remain publisher-declared downloads without an executable Release.
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
			Disposition, CataloguePublication, MavenCoordinate string
			Assets                                             []json.RawMessage
			Publication                                        struct {
				Method, Path string
				Body         json.RawMessage
			}
		}
	}
	if err := json.Unmarshal(contents, &prepared); err != nil {
		t.Fatal(err)
	}
	if prepared.PublicationState != "NOT_PUBLISHED" || len(prepared.Products) != 2 || len(prepared.CLI.Assets) != 6 || prepared.CLI.Disposition != "DOWNLOAD_ASSET_DECLARATION" || prepared.CLI.CataloguePublication != "CONDUIT_NATIVE_DOWNLOAD_ASSETS" {
		t.Fatal("synthetic preparation scope changed without updating its integration proof")
	}
	repository := controlplane.NewMemoryRepository()
	assetStore := assetmemory.New()
	handler, err := composition.NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"fixture-publisher"}}, composition.Options{ProductAssets: assetStore})
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
	var downloadManifest assetdomain.Manifest
	decoder := json.NewDecoder(bytes.NewReader(prepared.CLI.Publication.Body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&downloadManifest); err != nil {
		t.Fatal(err)
	}
	if err := downloadManifest.Validate(); err != nil {
		t.Fatal(err)
	}
	if downloadManifest.Coordinate() != prepared.CLI.MavenCoordinate || len(downloadManifest.Assets) != 6 {
		t.Fatal("CLI declaration lost catalogue identity or target coverage")
	}
	request := httptest.NewRequest(prepared.CLI.Publication.Method, prepared.CLI.Publication.Path, bytes.NewReader(prepared.CLI.Publication.Body))
	request.Header.Set("X-Platform-Subject", "fixture-publisher")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("prepared CLI input rejected: %d %s", response.Code, response.Body)
	}
	metadata, err := assetStore.Get(t.Context(), downloadManifest.Coordinate())
	if err != nil {
		t.Fatal(err)
	}
	expectedDigest, err := downloadManifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Digest != expectedDigest || metadata.PreparedBy != "fixture-publisher" || metadata.DeclarationTier != "PUBLISHER_DECLARED" {
		t.Fatal("CLI preparation became an unsupported verification claim", metadata)
	}
	publications, err := repository.ListProductPublications(t.Context(), downloadManifest.Product())
	if err != nil || len(publications) != 1 || publications[0].Kind != assetdomain.Kind || publications[0].Release != nil || publications[0].PublicationDigest != expectedDigest {
		t.Fatal("CLI declaration failed receipt binding or became executable", publications, err)
	}

}

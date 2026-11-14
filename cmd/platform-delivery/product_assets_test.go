package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	assets "github.com/jonathanagustin/conduit/internal/productassets/domain"
)

func TestDownloadAssetStartupPersistsIndependentMetadata(t *testing.T) {
	directory := t.TempDir()
	getenv := func(name string) string {
		if name == "DATA_DIRECTORY" {
			return directory
		}
		return ""
	}
	store, closeStore, e := openProductAssets(t.Context(), getenv)
	if e != nil {
		t.Fatal(e)
	}
	manifest := assets.Manifest{APIVersion: assets.SchemaVersion, ProductGroup: "io.conduit", ProductName: "cli", Version: "1.2.3", Assets: []assets.Asset{{Name: "cli.tar.gz", URI: "https://example.org/cli.tar.gz", SHA256: strings.Repeat("a", 64)}}}
	record, e := assets.NewPrepared(manifest, "publisher", time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if _, e := store.Prepare(t.Context(), record); e != nil {
		t.Fatal(e)
	}
	closeStore()
	reopened, closeReopened, e := openProductAssets(t.Context(), getenv)
	if e != nil {
		t.Fatal(e)
	}
	defer closeReopened()
	saved, e := reopened.Get(t.Context(), manifest.Coordinate())
	if e != nil || !reflect.DeepEqual(saved, record) {
		t.Fatal("startup store lost metadata", saved, e)
	}
}
func TestDownloadAssetStartupSanitizesDatabaseFailures(t *testing.T) {
	if _, _, e := openProductAssets(t.Context(), nil); e == nil {
		t.Fatal("nil configuration accepted")
	}
	if _, _, e := openProductAssets(t.Context(), func(string) string { return "unknown" }); e == nil {
		t.Fatal("unknown repository accepted")
	}
	secret := filepath.Join(t.TempDir(), "dsn")
	if e := os.WriteFile(secret, []byte("postgres://operator:PVT_ASSET_DSN@127.0.0.1:1/conduit?sslmode=disable&connect_timeout=1\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	_, _, e := openProductAssets(t.Context(), func(name string) string {
		if name == "REPOSITORY_KIND" {
			return "postgres"
		}
		if name == "POSTGRES_DSN_FILE" {
			return secret
		}
		return ""
	})
	if e == nil || strings.Contains(e.Error(), "PVT_ASSET_DSN") {
		t.Fatal("database credentials exposed", e)
	}
}

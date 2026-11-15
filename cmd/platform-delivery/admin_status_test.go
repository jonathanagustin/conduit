package main

import (
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"testing"
)

func TestAdminFilesystemUsesOnlyConfiguredLocalRepository(t *testing.T) {
	if got := adminStorageForRepository(controlplane.NewMemoryRepository(), func(key string) string {
		if key == "DATA_DIRECTORY" {
			return "/private/unrelated"
		}
		return ""
	}); len(got) != 0 {
		t.Fatal("non-file backend exposed an unrelated filesystem")
	}
	dir := t.TempDir()
	repository, err := controlplane.NewFileRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	probes := adminStorageForRepository(repository, func(key string) string {
		if key == "DATA_DIRECTORY" {
			return dir
		}
		return ""
	})
	if len(probes) != 1 || probes["data"] == nil || probes["registry"] != nil || probes["objectStore"] != nil {
		t.Fatalf("invented remote storage measurement: %+v", probes)
	}
}

func TestAdminStorageRequiresExplicitRemoteFilesystemPaths(t *testing.T) {
	dir := t.TempDir()
	probes := adminStorageForRepository(controlplane.NewMemoryRepository(), func(key string) string {
		if key == "ADMIN_REGISTRY_DATA_DIRECTORY" || key == "ADMIN_OBJECT_STORE_DATA_DIRECTORY" {
			return dir
		}
		return ""
	})
	if len(probes) != 2 || probes["data"] != nil || probes["registry"] == nil || probes["objectStore"] == nil {
		t.Fatalf("explicit filesystem probes: %+v", probes)
	}
}

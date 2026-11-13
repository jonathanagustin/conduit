package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/spaces/domain"
)

func TestFileSpaceComponentReopensWithTheRepositoryDirectory(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	settings := map[string]string{"REPOSITORY_KIND": "file", "DATA_DIRECTORY": directory}
	getenv := func(name string) string { return settings[name] }
	store, closeStore, err := openSpaces(t.Context(), getenv)
	if err != nil {
		t.Fatal(err)
	}
	value, err := domain.New("space_0123456789abcdef0123456789abcdef", "Persisted", "", "operator", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSpace(t.Context(), value); err != nil {
		t.Fatal(err)
	}
	closeStore()
	reopened, closeReopened, err := openSpaces(t.Context(), getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer closeReopened()
	saved, err := reopened.GetSpace(t.Context(), value.ID)
	if err != nil || saved != value {
		t.Fatalf("Space component did not survive reopen: %#v, %v", saved, err)
	}
}
func TestSpaceComponentConfigurationFailsWithoutDisclosingDSN(t *testing.T) {
	t.Parallel()
	if _, _, err := openSpaces(t.Context(), nil); err == nil {
		t.Fatal("missing environment reader accepted")
	}
	if _, _, err := openSpaces(t.Context(), func(string) string { return "unknown" }); err == nil {
		t.Fatal("unknown durability mode accepted")
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "dsn")
	if err := os.WriteFile(path, []byte("invalid-connection private-provider-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := map[string]string{"REPOSITORY_KIND": "postgres", "POSTGRES_DSN_FILE": path}
	if _, _, err := openSpaces(t.Context(), func(name string) string { return settings[name] }); err == nil || strings.Contains(err.Error(), "private-provider-secret") {
		t.Fatalf("initialization did not safely reject credentials: %v", err)
	}
}

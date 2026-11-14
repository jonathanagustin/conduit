package main

import (
	discussion "github.com/jonathanagustin/conduit/internal/changeactivity/domain"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiscussionStartupPersistsMetadataAndSanitizesDatabaseFailures(t *testing.T) {
	root := t.TempDir()
	getenv := func(name string) string {
		if name == "DATA_DIRECTORY" {
			return root
		}
		return ""
	}
	store, closeStore, err := openChangeDiscussion(t.Context(), getenv)
	if err != nil {
		t.Fatal(err)
	}
	c := discussion.Comment{ID: "cmt_" + strings.Repeat("1", 32), RequestID: "crq_startup", Author: "author", Actor: "carrier", CreatedAt: time.Now().UTC(), Text: "discussion", ValueTier: discussion.ValueTier}
	if err := store.Append(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	closeStore()
	reopened, closeReopened, err := openChangeDiscussion(t.Context(), getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer closeReopened()
	rows, err := reopened.List(t.Context(), c.RequestID)
	if err != nil || len(rows) != 1 || rows[0] != c {
		t.Fatal("discussion lost after restart", rows, err)
	}
	if _, _, err := openChangeDiscussion(t.Context(), nil); err == nil {
		t.Fatal("nil configuration accepted")
	}
	if _, _, err := openChangeDiscussion(t.Context(), func(string) string { return "unsupported" }); err == nil {
		t.Fatal("unknown repository accepted")
	}
	secret := filepath.Join(t.TempDir(), "dsn")
	if err := os.WriteFile(secret, []byte("postgres://operator:PVT_DISCUSSION_DSN@127.0.0.1:1/conduit?sslmode=disable&connect_timeout=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = openChangeDiscussion(t.Context(), func(name string) string {
		if name == "REPOSITORY_KIND" {
			return "postgres"
		}
		if name == "POSTGRES_DSN_FILE" {
			return secret
		}
		return ""
	})
	if err == nil || strings.Contains(err.Error(), "PVT_DISCUSSION_DSN") {
		t.Fatal("database diagnostics exposed credentials", err)
	}
}

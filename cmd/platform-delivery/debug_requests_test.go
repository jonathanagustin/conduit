package main

import (
	"github.com/jonathanagustin/conduit/internal/debugsessions/domain"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDebugJournalStartupPersistsMetadataAndSanitizesDatabaseFailures(t *testing.T) {
	directory := t.TempDir()
	getenv := func(name string) string {
		if name == "DATA_DIRECTORY" {
			return directory
		}
		return ""
	}
	journal, closeJournal, err := openDebugSessionJournal(t.Context(), getenv)
	if err != nil {
		t.Fatal(err)
	}
	e, err := domain.New("dsr_abcdefghijklmnopqrstuvwxyz", domain.Target{Environment: "env", Namespace: "app", Profile: "requested"}, domain.Identity{Actor: "actor", Owner: "owner"}, time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(t.Context(), 0, e); err != nil {
		t.Fatal(err)
	}
	closeJournal()
	reopened, closeReopened, err := openDebugSessionJournal(t.Context(), getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer closeReopened()
	events, err := reopened.History(t.Context(), e.SessionID)
	if err != nil || len(events) != 1 || events[0] != e {
		t.Fatal(events, err)
	}
	if _, _, err := openDebugSessionJournal(t.Context(), nil); err == nil {
		t.Fatal("accepted nil configuration")
	}
	if _, _, err := openDebugSessionJournal(t.Context(), func(string) string { return "unknown" }); err == nil {
		t.Fatal("accepted unknown backend")
	}
	secret := filepath.Join(t.TempDir(), "dsn")
	if err := os.WriteFile(secret, []byte("postgres://operator:PVT_DEBUG_DSN@127.0.0.1:1/conduit?sslmode=disable&connect_timeout=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = openDebugSessionJournal(t.Context(), func(name string) string {
		switch name {
		case "REPOSITORY_KIND":
			return "postgres"
		case "POSTGRES_DSN_FILE":
			return secret
		}
		return ""
	})
	if err == nil || strings.Contains(err.Error(), "PVT_DEBUG_DSN") {
		t.Fatal("database diagnostics exposed credentials", err)
	}
}

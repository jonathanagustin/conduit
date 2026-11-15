package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	live "github.com/jonathanagustin/conduit/internal/liveevents/domain"
)

func TestLiveEventJournalIsExplicitPrivateAndSingleOwner(t *testing.T) {
	off, close, err := openLiveEventJournal(func(string) string { return "" })
	if err != nil || off != nil {
		t.Fatal("unconfigured journal created")
	}
	close()
	if _, _, err := openLiveEventJournal(nil); err == nil {
		t.Fatal("missing environment reader accepted")
	}
	if _, _, err := openLiveEventJournal(func(string) string { return "relative.json" }); err == nil {
		t.Fatal("relative journal accepted")
	}
	path := filepath.Join(t.TempDir(), "journal.json")
	env := func(string) string { return path }
	first, closeFirst, err := openLiveEventJournal(env)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFirst()
	if _, closeOther, err := openLiveEventJournal(env); err == nil {
		closeOther()
		t.Fatal("two owners opened journal")
	}
	if err := first.Update(context.Background(), func(s *live.State) error { s.SourcePosition = 12; return nil }); err != nil {
		t.Fatal(err)
	}
	closeFirst()
	closeFirst()
	restored, closeRestored, err := openLiveEventJournal(env)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRestored()
	state, err := restored.Snapshot(context.Background())
	if err != nil || state.SourcePosition != 12 {
		t.Fatalf("restart lost journal: %+v %v", state, err)
	}
	for _, file := range []string{path, path + ".lock"} {
		info, err := os.Stat(file)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("private file %s: %v %v", file, info, err)
		}
	}
}
func TestLiveEventJournalRejectsUnsafeLockOrCorruptState(t *testing.T) {
	for _, tc := range []string{"public-lock", "symlink-lock", "corrupt-journal"} {
		t.Run(tc, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events.json")
			switch tc {
			case "public-lock":
				if err := os.WriteFile(path+".lock", nil, 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink-lock":
				if err := os.Symlink(filepath.Join(t.TempDir(), "elsewhere"), path+".lock"); err != nil {
					t.Fatal(err)
				}
			case "corrupt-journal":
				if err := os.WriteFile(path, []byte(`{"version":1}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, close, err := openLiveEventJournal(func(string) string { return path }); err == nil {
				close()
				t.Fatal("unsafe journal accepted")
			}
		})
	}
}

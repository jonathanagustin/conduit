package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/jonathanagustin/conduit/internal/liveevents/adapters/filejournal"
	"github.com/jonathanagustin/conduit/internal/liveevents/ports"
)

// A delivery journal is opt-in and private to this replica. Keep an advisory
// lock on a separate inode because the journal itself is atomically renamed.
// Never unlink the lock: another opener could then lock a different inode.
func openLiveEventJournal(getenv func(string) string) (ports.Journal, func(), error) {
	empty := func() {}
	if getenv == nil {
		return nil, empty, errors.New("environment reader is required")
	}
	path := strings.TrimSpace(getenv("LIVE_EVENT_JOURNAL_FILE"))
	if path == "" {
		return nil, empty, nil
	}
	if !filepath.IsAbs(path) {
		return nil, empty, errors.New("LIVE_EVENT_JOURNAL_FILE must be an absolute replica-local path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, empty, errors.New("create live-event journal directory failed")
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, empty, errors.New("open live-event journal ownership lock failed")
	}
	fail := func() (ports.Journal, func(), error) {
		_ = lock.Close()
		return nil, empty, errors.New("live-event journal lock must be private, regular and owned by this process")
	}
	info, err := lock.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return fail()
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fail()
	}
	var once sync.Once
	close := func() { once.Do(func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); _ = lock.Close() }) }
	journal, err := filejournal.Open(path)
	if err != nil {
		close()
		return nil, empty, errors.New("open live-event journal failed")
	}
	return journal, close, nil
}

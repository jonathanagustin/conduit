//go:build !windows

package source

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestRejectFIFO(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := Read(root, "pipe"); err == nil {
		t.Fatal("accepted FIFO")
	}
}

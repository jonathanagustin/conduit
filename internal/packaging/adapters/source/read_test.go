package source

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadBindsExactBytes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	f, err := Read(root, "README.md")
	if err != nil {
		t.Fatal(err)
	}
	if f.Text != "hello" || f.SHA256 != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("%+v", f)
	}
}
func TestRejectPathsAndContent(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, name := range []string{".", "../secret", "a/../b", "/secret", "C:/secret", "a\\b"} {
		if _, err := Read(root, name); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	for name, content := range map[string][]byte{"big": []byte(strings.Repeat("x", MaxFileBytes+1)), "binary": {0xff}, "nul": {0}} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(root, name); err == nil {
			t.Errorf("accepted %s", name)
		}
	}
	if _, err := Read(root, "missing"); err == nil {
		t.Fatal("accepted missing")
	}
	if _, err := Read(nil, "README.md"); err == nil {
		t.Fatal("accepted nil root")
	}
}
func TestRejectSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := Read(root, "link"); err == nil {
		t.Fatal("read outside authorized root")
	}
}

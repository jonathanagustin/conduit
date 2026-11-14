// Package source reads bounded evidence from an explicitly selected source tree.
// It never executes repository code or follows paths outside that tree.
package source

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"unicode/utf8"
)

const MaxFileBytes = 64 * 1024

// File is source evidence, not a model assessment or validation certificate.
type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Text   string `json:"text"`
}

// Read uses os.Root's traversal-resistant open. The caller must supply an
// authorized, immutable checkout and keep the root open for the operation.
// The digest binds the bytes actually read, not a claimed Git revision.
func Read(root *os.Root, name string) (File, error) {
	if root == nil {
		return File{}, fmt.Errorf("source root is required")
	}
	if name == "" || name == "." || len(name) > 512 || path.IsAbs(name) || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\:\x00\r\n") {
		return File{}, fmt.Errorf("invalid source path")
	}
	before, err := root.Stat(name)
	if err != nil {
		return File{}, err
	}
	if !before.Mode().IsRegular() {
		return File{}, fmt.Errorf("source must be a regular file")
	}
	// Nonblocking open prevents a swapped FIFO from hanging before the second stat.
	f, err := root.OpenFile(name, os.O_RDONLY|nonblockFlag, 0)
	if err != nil {
		return File{}, fmt.Errorf("open source: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return File{}, err
	}
	if !info.Mode().IsRegular() {
		return File{}, fmt.Errorf("source must be a regular file")
	}
	if info.Size() > MaxFileBytes {
		return File{}, fmt.Errorf("source exceeds byte limit")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return File{}, err
	}
	if len(data) > MaxFileBytes {
		return File{}, fmt.Errorf("source exceeds byte limit")
	}
	if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return File{}, fmt.Errorf("source must be UTF-8 text")
	}
	digest := sha256.Sum256(data)
	return File{Path: name, SHA256: hex.EncodeToString(digest[:]), Text: string(data)}, nil
}

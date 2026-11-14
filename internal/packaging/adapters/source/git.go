package source

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var objectID = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

type Snapshot struct {
	Revision string       `json:"revision"`
	Files    []PinnedFile `json:"files"`
}
type PinnedFile struct {
	File
	Blob string `json:"blob"`
}

type boundedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		return 0, fmt.Errorf("git output exceeds limit")
	}
	return b.buffer.Write(p)
}

// gitRead uses only object inspection, never checkout, filters, hooks, or network.
func gitRead(ctx context.Context, root string, limit int, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	argv := append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-C", root}, args...)
	cmd := exec.CommandContext(ctx, "git", argv...)
	env := []string{}
	for _, v := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(v), "GIT_") {
			env = append(env, v)
		}
	}
	cmd.Env = append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_NO_REPLACE_OBJECTS=1", "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0")
	out := &boundedBuffer{limit: limit}
	stderr := &boundedBuffer{limit: 4096}
	cmd.Stdout = out
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("read Git object: %w", err)
	}
	return out.buffer.Bytes(), nil
}

// ReadCommit reads exact committed blobs, independent of index/worktree changes.
// The repository and explicitly selected files must already be authorized.
func ReadCommit(ctx context.Context, root, revision string, names []string) (Snapshot, error) {
	s := Snapshot{Revision: revision, Files: []PinnedFile{}}
	if !objectID.MatchString(revision) {
		return s, fmt.Errorf("full lowercase commit object ID required")
	}
	if len(names) < 1 || len(names) > 16 {
		return s, fmt.Errorf("select 1-16 source files")
	}
	kind, err := gitRead(ctx, root, 64, "cat-file", "-t", revision)
	if err != nil || string(kind) != "commit\n" {
		return s, fmt.Errorf("revision must identify an existing local commit")
	}
	seen := map[string]bool{}
	for _, name := range names {
		if name == "" || name == "." || len(name) > 512 || path.IsAbs(name) || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\:\x00\r\n") || seen[name] {
			return Snapshot{}, fmt.Errorf("invalid or duplicate source path")
		}
		seen[name] = true
		// Literal pathspec prevents wildcard or magic path interpretation.
		meta, err := gitRead(ctx, root, 256, "ls-tree", "--format=%(objectmode) %(objecttype) %(objectname)", revision, "--", ":(literal)"+name)
		if err != nil {
			return Snapshot{}, err
		}
		fields := strings.Fields(string(meta))
		if len(fields) != 3 || (fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" || !objectID.MatchString(fields[2]) {
			return Snapshot{}, fmt.Errorf("source must identify one regular committed file")
		}
		data, err := gitRead(ctx, root, MaxFileBytes, "cat-file", "blob", fields[2])
		if err != nil {
			return Snapshot{}, err
		}
		if !utf8.Valid(data) || bytes.ContainsRune(data, 0) {
			return Snapshot{}, fmt.Errorf("source must be UTF-8 text")
		}
		digest := sha256.Sum256(data)
		s.Files = append(s.Files, PinnedFile{File: File{Path: name, SHA256: hex.EncodeToString(digest[:]), Text: string(data)}, Blob: fields[2]})
	}
	return s, nil
}

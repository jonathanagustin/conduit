package main

import (
	"encoding/json"
	"fmt"
	"github.com/jonathanagustin/conduit/internal/packaging/adapters/source"
	"io"
	"os"
)

// evidence requires an explicit root and file list; it never recursively scans
// hidden files or credentials. The caller chooses which source to disclose.
func evidence(args []string, out io.Writer) error {
	if len(args) < 2 || len(args) > 17 {
		return fmt.Errorf("evidence requires a source root and 1-16 relative text paths")
	}
	root, err := os.OpenRoot(args[0])
	if err != nil {
		return err
	}
	defer root.Close()
	files := make([]source.File, 0, len(args)-1)
	seen := map[string]bool{}
	for _, name := range args[1:] {
		if seen[name] {
			return fmt.Errorf("duplicate source path")
		}
		seen[name] = true
		f, err := source.Read(root, name)
		if err != nil {
			return err
		}
		files = append(files, f)
	}
	return json.NewEncoder(out).Encode(struct {
		Status string        `json:"status"`
		Files  []source.File `json:"files"`
	}{"source-evidence-not-assessed", files})
}

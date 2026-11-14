// Package sourcefile writes the packaging source the existing run reads.
package sourcefile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Writer stores one source file under Dir/sources/<runID>.txt.
type Writer struct {
	Dir string
}

func (writer Writer) Prepare(_ context.Context, runID string, source []byte) error {
	if strings.TrimSpace(writer.Dir) == "" {
		return nil
	}
	if runID == "" || runID != filepath.Base(runID) || strings.Contains(runID, "..") {
		return fmt.Errorf("run id is not a file name")
	}
	dir := filepath.Join(writer.Dir, "sources")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, runID+".txt"), source, 0o644)
}

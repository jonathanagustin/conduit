package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow"
)

func TestSDKModulePinMatchesGoMod(t *testing.T) {
	module := readGoMod(t)
	needle := packagingworkflow.SDKModule + " " + packagingworkflow.SDKVersion
	if !strings.Contains(module, needle) {
		t.Fatalf("go.mod does not require %s", needle)
	}
}

func readGoMod(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		path := filepath.Join(dir, "go.mod")
		body, err := os.ReadFile(path)
		if err == nil {
			return string(body)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

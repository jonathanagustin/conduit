package optimization

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestOptimizerHasNoDeliveryAuthorityImports(t *testing.T) {
	patterns := []string{"*.go", "../../cmd/platform-optimizer/*.go"}
	for _, pattern := range patterns {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			for _, imported := range parsed.Imports {
				name, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				for _, forbidden := range []string{"/internal/controlplane", "/internal/agent", "/internal/conduitcli"} {
					if strings.Contains(name, forbidden) {
						t.Errorf("%s imports delivery-authority package %s", path, name)
					}
				}
			}
		}
	}
}

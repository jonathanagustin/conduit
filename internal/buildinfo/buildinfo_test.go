package buildinfo

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Not parallel: it stamps the package variables the way a release build does.
// The defaults are deliberately distinct here because Commit and BuildDate
// both default to "unknown", and against those two a Current() that swapped
// them would look correct.
func TestCurrentReportsThePackageVariables(t *testing.T) {
	previousVersion, previousCommit, previousBuildDate := Version, Commit, BuildDate
	t.Cleanup(func() { Version, Commit, BuildDate = previousVersion, previousCommit, previousBuildDate })
	Version, Commit, BuildDate = "1.2.3", "abcdef0", "2026-08-06T00:00:00Z"

	info := Current()
	if info.Version != "1.2.3" {
		t.Errorf("Version = %q", info.Version)
	}
	if info.Commit != "abcdef0" {
		t.Errorf("Commit = %q", info.Commit)
	}
	if info.BuildDate != "2026-08-06T00:00:00Z" {
		t.Errorf("BuildDate = %q", info.BuildDate)
	}
}

// The defaults have to be recognisable as "not a release". A blank version
// renders as an empty field in the CLI and in every log line, which reads as a
// display bug rather than as an unstamped binary.
func TestDevelopmentDefaultsAreExplicit(t *testing.T) {
	for name, value := range map[string]string{"Version": Version, "Commit": Commit, "BuildDate": BuildDate} {
		if strings.TrimSpace(value) == "" {
			t.Errorf("%s has no development default", name)
		}
	}
}

// -ldflags -X names a symbol by its full import path. Renaming one of these
// variables, or moving the package, does not fail the build: the flag simply
// stops matching, and every release binary reports "dev" and "unknown" while
// looking perfectly healthy. This reads the Containerfiles and requires each
// name they stamp to still exist here.
func TestContainerfilesStampVariablesThatExist(t *testing.T) {
	t.Parallel()
	declared := map[string]bool{"Version": true, "Commit": true, "BuildDate": true}
	pattern := regexp.MustCompile(`-X github\.com/jonathanagustin/conduit/internal/buildinfo\.([A-Za-z]+)=`)

	stamped := map[string]bool{}
	for _, name := range []string{"Containerfile", "Containerfile.agent"} {
		path := filepath.Join("..", "..", name)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		matches := pattern.FindAllStringSubmatch(string(contents), -1)
		if len(matches) == 0 {
			t.Errorf("%s stamps no buildinfo variables; release binaries would report the development defaults", name)
			continue
		}
		for _, match := range matches {
			stamped[match[1]] = true
			if !declared[match[1]] {
				t.Errorf("%s stamps buildinfo.%s, which this package does not declare", name, match[1])
			}
		}
	}
	// And the reverse: a variable nobody stamps is one that will always read
	// as a development build.
	for name := range declared {
		if !stamped[name] {
			t.Errorf("buildinfo.%s is declared but no Containerfile stamps it", name)
		}
	}
}

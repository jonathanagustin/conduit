package rubixsubstrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// fixtureRoute mirrors one entry in deploy/rubix-substrate/http-contract.json.
type fixtureRoute struct {
	Service    string   `json:"service"`
	Endpoint   string   `json:"endpoint"`
	Method     string   `json:"method"`
	Path       string   `json:"path"`
	Request    string   `json:"request"`
	Response   string   `json:"response"`
	PathParams []string `json:"pathParams"`
}

type fixture struct {
	Endpoints []fixtureRoute `json:"endpoints"`
}

func loadFixture(t *testing.T) fixture {
	t.Helper()
	// internal/rubixsubstrate -> repo root is two directories up.
	path := filepath.Join("..", "..", "deploy", "rubix-substrate", "http-contract.json")
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read committed contract fixture: %v", err)
	}
	var parsed fixture
	if err := json.Unmarshal(bytes, &parsed); err != nil {
		t.Fatalf("parse committed contract fixture: %v", err)
	}
	return parsed
}

// TestFixtureSignaturesAreModeled proves the recovered request/response beans
// named per endpoint in the fixture are all modeled types (or the boolean
// scalar), and that each endpoint's declared path parameters are exactly the
// {name} segments in its path. This binds the recovered service signatures to
// the beans and the paths, so neither can drift silently.
func TestFixtureSignaturesAreModeled(t *testing.T) {
	parsed := loadFixture(t)
	modeled := map[string]bool{"": true, "boolean": true}
	for name := range objectRegistry() {
		modeled[name] = true
	}
	for name := range unionRegistry() {
		modeled[name] = true
	}
	for name := range aliasRegistry() {
		modeled[name] = true
	}

	for _, ep := range parsed.Endpoints {
		if !modeled[ep.Request] {
			t.Errorf("%s.%s request bean %q is not modeled", ep.Service, ep.Endpoint, ep.Request)
		}
		if !modeled[ep.Response] {
			t.Errorf("%s.%s response bean %q is not modeled", ep.Service, ep.Endpoint, ep.Response)
		}
		var inPath []string
		for _, seg := range splitSegments(ep.Path) {
			if len(seg) > 2 && seg[0] == '{' && seg[len(seg)-1] == '}' {
				inPath = append(inPath, seg[1:len(seg)-1])
			}
		}
		declared := append([]string(nil), ep.PathParams...)
		if !equalSets(inPath, declared) {
			t.Errorf("%s.%s path params %v != declared %v", ep.Service, ep.Endpoint, inPath, declared)
		}
	}
}

func splitSegments(path string) []string {
	var out []string
	cur := ""
	for _, r := range path {
		if r == '/' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func TestTableIsInternallyConsistent(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatalf("route table failed its own invariants: %v", err)
	}
	if got := len(Routes); got != 14 {
		t.Fatalf("recovered contract has fourteen operations, table has %d", got)
	}
	if got := len(ServicesInOrder()); got != 5 {
		t.Fatalf("recovered contract spans five services, table has %d", got)
	}
}

// TestTableMatchesCommittedFixture is the drift gate: the in-code table and the
// committed ground-truth fixture must be the same set, exactly. Editing one
// without the other fails here, so the recovered contract cannot silently drift.
func TestTableMatchesCommittedFixture(t *testing.T) {
	parsed := loadFixture(t)

	fixtureKeys := make([]string, 0, len(parsed.Endpoints))
	for _, route := range parsed.Endpoints {
		fixtureKeys = append(fixtureKeys, route.Service+"|"+route.Endpoint+"|"+route.Method+"|"+route.Path)
	}
	sort.Strings(fixtureKeys)

	tableKeys := SortedKeys()
	if len(tableKeys) != len(fixtureKeys) {
		t.Fatalf("route count drift: table has %d, fixture has %d", len(tableKeys), len(fixtureKeys))
	}
	for i := range tableKeys {
		if tableKeys[i] != fixtureKeys[i] {
			t.Fatalf("route drift at %d:\n  table:   %s\n  fixture: %s", i, tableKeys[i], fixtureKeys[i])
		}
	}
}

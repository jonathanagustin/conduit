package udsschema

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Regenerate the embedded structures after changing a pin:
//
//	go test ./internal/udsschema -run TestStructuresAreDerivedFromThePinnedUpstreamSchemas -update
var update = flag.Bool("update", false, "rewrite the embedded *.structure.json files from the retained upstream schemas")

const repositoryRoot = "../.."

type pinnedSchema struct {
	name string
	// manifest and entry locate the recorded SHA-256 of the upstream bytes.
	manifest, entry string
	upstream        string
	version         string
	structure       string
	embedded        []byte
}

func pins() []pinnedSchema {
	return []pinnedSchema{
		{
			name:     "package",
			manifest: "workspace-schemas/v1/manifest.json", entry: "uds-package.schema.json",
			upstream: "workspace-schemas/v1/uds-package.schema.json", version: CoreVersion,
			structure: "package-v1alpha1.structure.json", embedded: packageStructure,
		},
		{
			name:     "bundle",
			manifest: "workspace-schemas/upstream/manifest.json", entry: "uds-cli/v0.41.0/uds.schema.json",
			upstream: "workspace-schemas/upstream/uds-cli/v0.41.0/uds.schema.json", version: CLIVersion,
			structure: "bundle-v0.41.0.structure.json", embedded: bundleStructure,
		},
	}
}

func readRepositoryFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return contents
}

func sha256Hex(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}

// structureOf removes annotation keywords from a JSON Schema and keeps every
// keyword that decides whether a document is valid. Keys inside a properties,
// definitions or patternProperties map are names, not keywords, so a property
// that happens to be called "description" survives.
func structureOf(node any) any {
	switch value := node.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, child := range value {
			switch key {
			case "description", "title", "examples", "$comment", "$schema":
			case "properties", "definitions", "$defs", "patternProperties":
				names, ok := child.(map[string]any)
				if !ok {
					out[key] = child
					continue
				}
				kept := make(map[string]any, len(names))
				for name, schema := range names {
					kept[name] = structureOf(schema)
				}
				out[key] = kept
			case "enum", "const", "default", "required":
				out[key] = child
			default:
				out[key] = structureOf(child)
			}
		}
		return out
	case []any:
		out := make([]any, len(value))
		for index, child := range value {
			out[index] = structureOf(child)
		}
		return out
	default:
		return node
	}
}

func deriveStructure(t *testing.T, upstream []byte) []byte {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(upstream))
	decoder.UseNumber()
	var document map[string]any
	if err := decoder.Decode(&document); err != nil {
		t.Fatalf("decode upstream schema: %v", err)
	}
	structure := structureOf(document).(map[string]any)
	// The dialect is not an annotation: it decides how the rest is read.
	structure["$schema"] = document["$schema"]
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(structure); err != nil {
		t.Fatalf("encode structure: %v", err)
	}
	return out.Bytes()
}

func TestStructuresAreDerivedFromThePinnedUpstreamSchemas(t *testing.T) {
	for _, pin := range pins() {
		t.Run(pin.name, func(t *testing.T) {
			var manifest struct {
				Schemas []struct {
					File, Version, Sha256, Source, License, LicenseSha256 string
				} `json:"schemas"`
			}
			if err := json.Unmarshal(readRepositoryFile(t, pin.manifest), &manifest); err != nil {
				t.Fatalf("decode %s: %v", pin.manifest, err)
			}
			upstream := readRepositoryFile(t, pin.upstream)
			recorded := false
			for _, entry := range manifest.Schemas {
				if entry.File != pin.entry {
					continue
				}
				recorded = true
				if entry.Version != pin.version {
					t.Errorf("%s records version %s; this package targets %s", pin.manifest, entry.Version, pin.version)
				}
				if !strings.Contains(entry.Source, "/v"+pin.version+"/") {
					t.Errorf("source %s does not name the tag v%s", entry.Source, pin.version)
				}
				if got := sha256Hex(upstream); got != entry.Sha256 {
					t.Fatalf("%s is %s, but %s records %s", pin.upstream, got, pin.manifest, entry.Sha256)
				}
				licence := readRepositoryFile(t, filepath.ToSlash(filepath.Join(filepath.Dir(pin.manifest), entry.License)))
				if got := sha256Hex(licence); got != entry.LicenseSha256 {
					t.Errorf("licence for %s is %s, recorded %s", pin.entry, got, entry.LicenseSha256)
				}
			}
			if !recorded {
				t.Fatalf("%s has no entry for %s", pin.manifest, pin.entry)
			}

			derived := deriveStructure(t, upstream)
			if *update {
				if err := os.WriteFile(pin.structure, derived, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			if !bytes.Equal(derived, pin.embedded) {
				t.Fatalf("%s is not what %s derives to; run this test with -update and review the diff", pin.structure, pin.upstream)
			}
			for _, annotation := range []string{`"description":`, `"title":`, `"examples":`} {
				// A property may be named "description"; only a string-valued
				// one is the annotation keyword.
				if bytes.Contains(pin.embedded, []byte(annotation+` "`)) {
					t.Errorf("%s still carries upstream %s text", pin.structure, annotation)
				}
			}
		})
	}
}

// The pin is only useful if it is the version actually run. The Package schema
// must be the UDS Core version the bring-up installs and the evidence was
// gathered on; the bundle schema must be the one the repository's own bundle
// declares.
func TestPinsMatchTheVersionsTheRepositoryRuns(t *testing.T) {
	if up := readRepositoryFile(t, "scripts/uds-up.sh"); !bytes.Contains(up, []byte(CoreVersion)) {
		t.Errorf("scripts/uds-up.sh does not install UDS Core %s", CoreVersion)
	}
	directive := "# yaml-language-server: $schema=https://raw.githubusercontent.com/defenseunicorns/uds-cli/v" + CLIVersion + "/uds.schema.json"
	if bundle := readRepositoryFile(t, "deploy/zarf/uds-bundle.yaml"); !bytes.HasPrefix(bundle, []byte(directive)) {
		t.Errorf("deploy/zarf/uds-bundle.yaml is not pinned to uds-cli v%s", CLIVersion)
	}
}

func compileUpstream(t *testing.T, path string) *jsonschema.Schema {
	t.Helper()
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(readRepositoryFile(t, path)))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("https://conduit.invalid/upstream", document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("https://conduit.invalid/upstream")
	if err != nil {
		t.Fatalf("compile %s: %v", path, err)
	}
	return schema
}

// Stripping annotations must not change a single verdict. Every document in
// this package's corpus is judged by the untouched upstream schema and by the
// embedded structure, and the two must agree.
func TestStructureGivesTheSameVerdictAsTheUpstreamSchema(t *testing.T) {
	packageUpstream := compileUpstream(t, "workspace-schemas/v1/uds-package.schema.json")
	bundleUpstream := compileUpstream(t, "workspace-schemas/upstream/uds-cli/v0.41.0/uds.schema.json")
	check := func(name string, upstream *jsonschema.Schema, structure string, raw string) {
		document, violations := decode([]byte(raw))
		if violations != nil {
			t.Fatalf("%s: corpus document does not decode: %v", name, violations)
		}
		fromUpstream := upstream.Validate(document) == nil
		fromStructure := len(validate(structure, document)) == 0
		if fromUpstream != fromStructure {
			t.Errorf("%s: upstream says valid=%t, the embedded structure says valid=%t", name, fromUpstream, fromStructure)
		}
	}
	for name, raw := range packageCorpus {
		check("package/"+name, packageUpstream, "package", raw.document)
	}
	for name, raw := range bundleCorpus {
		check("bundle/"+name, bundleUpstream, "bundle", raw.document)
	}
}

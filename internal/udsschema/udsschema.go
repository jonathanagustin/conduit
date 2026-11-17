// Package udsschema validates the two UDS documents Conduit generates -- the
// UDS Core Package custom resource and the uds-cli UDSBundle -- against the
// schemas of the upstream versions Conduit targets.
//
// There is one pin per document, and it is this package:
//
//   - Package: UDS Core 1.17.1, schemas/package-v1alpha1.schema.json, retained
//     byte-for-byte in workspace-schemas/v1/uds-package.schema.json.
//   - UDSBundle: uds-cli v0.41.0, uds.schema.json, retained byte-for-byte in
//     workspace-schemas/upstream/uds-cli/v0.41.0/uds.schema.json. It is the
//     schema deploy/zarf/uds-bundle.yaml names in its language-server directive.
//
// What is compiled into the binary is not those files. It is their structure:
// the two *.structure.json files beside this one, which are the upstream
// schemas with the annotation keywords (description, title, examples, nested
// $schema) removed and every validation keyword kept. The test in this package
// regenerates both from the retained upstream bytes, after checking those bytes
// against the recorded SHA-256, and fails if either differs. So the structure
// cannot drift from the pin, and a change of pin is a reviewed change to three
// files rather than an edit to one.
//
// Two reasons for the indirection. The Spoke and Hub images are built from cmd/
// and internal/ only, so nothing outside internal/ can be embedded. And the
// upstream repositories carry an AGPL-3.0 root licence whose application to
// these files has not been adjudicated (docs/operations/workspace-schemas.md);
// field names, types and enumerations are what interoperating requires, and
// the descriptive text is not.
//
// Both upstream schemas close their objects with additionalProperties: false,
// so a misspelled or unknown field is a violation, not a silent no-op.
package udsschema

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// The upstream versions the embedded structures were derived from.
const (
	CoreVersion = "1.17.1"
	CLIVersion  = "0.41.0"

	// PackageAPIVersion and PackageKind identify the UDS Core Package resource.
	PackageAPIVersion = "uds.dev/v1alpha1"
	PackageKind       = "Package"
	// BundleKind identifies a uds-cli bundle. A bundle has no apiVersion.
	BundleKind = "UDSBundle"
)

//go:embed package-v1alpha1.structure.json
var packageStructure []byte

//go:embed bundle-v0.41.0.structure.json
var bundleStructure []byte

// Violation is one way a document departs from its pinned schema.
type Violation struct {
	// Path is the dotted location of the offending value, "" for the document.
	Path    string
	Message string
}

func (violation Violation) String() string {
	if violation.Path == "" {
		return violation.Message
	}
	return violation.Path + ": " + violation.Message
}

var compiled = sync.OnceValues(func() (map[string]*jsonschema.Schema, error) {
	schemas := make(map[string]*jsonschema.Schema, 2)
	for name, structure := range map[string][]byte{"package": packageStructure, "bundle": bundleStructure} {
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(structure))
		if err != nil {
			return nil, fmt.Errorf("decode pinned %s schema: %w", name, err)
		}
		location := "https://conduit.invalid/udsschema/" + name
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource(location, document); err != nil {
			return nil, fmt.Errorf("load pinned %s schema: %w", name, err)
		}
		schema, err := compiler.Compile(location)
		if err != nil {
			return nil, fmt.Errorf("compile pinned %s schema: %w", name, err)
		}
		schemas[name] = schema
	}
	return schemas, nil
})

// ValidatePackage checks one YAML or JSON document against the UDS Core
// Package schema, and checks the envelope the schema itself leaves open:
// apiVersion, kind, and a name and namespace.
func ValidatePackage(raw []byte) []Violation {
	document, violations := decode(raw)
	if violations != nil {
		return violations
	}
	if value, _ := document["apiVersion"].(string); value != PackageAPIVersion {
		violations = append(violations, Violation{Path: "apiVersion", Message: "must be " + PackageAPIVersion})
	}
	if value, _ := document["kind"].(string); value != PackageKind {
		violations = append(violations, Violation{Path: "kind", Message: "must be " + PackageKind})
	}
	metadata, _ := document["metadata"].(map[string]any)
	for _, field := range []string{"name", "namespace"} {
		if value, _ := metadata[field].(string); strings.TrimSpace(value) == "" {
			violations = append(violations, Violation{Path: "metadata." + field, Message: "is required"})
		}
	}
	return append(violations, validate("package", document)...)
}

// ValidateBundle checks one YAML or JSON document against the uds-cli
// UDSBundle schema.
func ValidateBundle(raw []byte) []Violation {
	document, violations := decode(raw)
	if violations != nil {
		return violations
	}
	return validate("bundle", document)
}

// decode reads exactly one document into JSON-shaped values. A stream of
// several documents is refused: validating the first and ignoring the rest
// would pass a file whose second document nobody looked at.
func decode(raw []byte) (map[string]any, []Violation) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, []Violation{{Message: "document is empty"}}
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	var first any
	if err := decoder.Decode(&first); err != nil {
		return nil, []Violation{{Message: "document is not valid YAML"}}
	}
	var second any
	if err := decoder.Decode(&second); err == nil {
		return nil, []Violation{{Message: "expected one document, found several"}}
	}
	// Through JSON, so the validator sees JSON types and nothing YAML-specific
	// (a timestamp, a non-string key) reaches it.
	encoded, err := json.Marshal(first)
	if err != nil {
		return nil, []Violation{{Message: "document has no JSON form: " + err.Error()}}
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		return nil, []Violation{{Message: "document has no JSON form: " + err.Error()}}
	}
	document, ok := value.(map[string]any)
	if !ok {
		return nil, []Violation{{Message: "document must be a mapping"}}
	}
	return document, nil
}

func validate(name string, document map[string]any) []Violation {
	schemas, err := compiled()
	if err != nil {
		// The structures are compiled into the binary and compiled again by
		// this package's tests, so this is unreachable in a built tree. If it
		// is ever reached, refusing is the only safe answer.
		return []Violation{{Message: err.Error()}}
	}
	err = schemas[name].Validate(document)
	if err == nil {
		return nil
	}
	var failure *jsonschema.ValidationError
	if !errors.As(err, &failure) {
		return []Violation{{Message: err.Error()}}
	}
	seen := map[Violation]struct{}{}
	var violations []Violation
	// The leaves of the detailed output say what failed and where; everything
	// above a leaf only says that something below it did.
	var collect func(jsonschema.OutputUnit)
	collect = func(unit jsonschema.OutputUnit) {
		if len(unit.Errors) > 0 {
			for _, child := range unit.Errors {
				collect(child)
			}
			return
		}
		if unit.Error == nil {
			return
		}
		path := strings.ReplaceAll(strings.TrimPrefix(unit.InstanceLocation, "/"), "/", ".")
		violation := Violation{Path: path, Message: unit.Error.String()}
		if _, duplicate := seen[violation]; !duplicate {
			seen[violation] = struct{}{}
			violations = append(violations, violation)
		}
	}
	collect(*failure.DetailedOutput())
	if len(violations) == 0 {
		violations = []Violation{{Message: failure.Error()}}
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Path != violations[j].Path {
			return violations[i].Path < violations[j].Path
		}
		return violations[i].Message < violations[j].Message
	})
	return violations
}

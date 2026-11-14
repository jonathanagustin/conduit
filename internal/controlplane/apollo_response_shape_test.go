package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/apollo"
)

// The response-shape gate.
//
// Four gates certify the Apollo public surface and between them none of them
// looks at a response body. validate-public-api.rb validates the snapshot
// document and touches no Go code. verify-apollo-routes.sh byte-compares the
// generated transport, which is strong for DTO and router fidelity and silent
// about semantics. verify-public-api-adapter.rb greps source text -- its
// "conformance declaration" check is satisfied by an operationId appearing in a
// comment. verify-apollo-interface.mjs governs the GraphQL SDL and never runs
// the HTTP handler.
//
// So "13/13 operations" meant thirteen operationIds were mentioned somewhere.
// This drives them and reads what they answer with.
//
// What it establishes, stated narrowly because the gap it fills was created by
// a number that promised more than it checked:
//
//   - No response carries a property the pinned schema does not declare. The
//     generated types come from the snapshot, so a strict decode into them
//     fails on an invented or renamed field.
//   - Every value of an enum-constrained property is a member of that enum,
//     read from the snapshot rather than restated here.
//
// What it does NOT establish, and the issue that asked for it overstated this:
// it cannot catch a fabricated value that happens to be a legal enum member.
// Conduit serves environmentType "production" and status "managed" as
// unconditional constants for every environment, and both are members of their
// enums -- this gate passes them. Values that no publisher stated are a
// provenance question, not a shape question, and shape is all a schema can
// answer.

const apolloSnapshot = "../../docs/reference-platform/snapshots/" +
	"reference-openapi-c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml"

var (
	// A named schema in components, at four spaces, e.g. "    UpgradeStrategy:".
	snapshotSchemaName = regexp.MustCompile(`^ {4}([A-Za-z][A-Za-z0-9]*):\s*$`)
	// A property naming another schema, e.g. "        upgradeStrategy:" then
	// "          $ref: '#/components/schemas/UpgradeStrategy'".
	snapshotPropertyName = regexp.MustCompile(`^ {6,}([a-z][A-Za-z0-9]*):\s*$`)
	snapshotRef          = regexp.MustCompile(`\$ref:\s*'#/components/schemas/([A-Za-z][A-Za-z0-9]*)'`)
	snapshotEnumMember   = regexp.MustCompile(`^\s+- (.+?)\s*$`)
)

// enumConstrainedProperties reads the snapshot and returns, for each property
// name that references an enum schema, the values that schema permits.
//
// Line-read rather than YAML-parsed, which is this repository's established
// technique for the pinned snapshot: the document cannot drift under the check
// because it is pinned by digest in its own path, and a YAML dependency would
// buy nothing a regexp does not already get from a file that never changes.
func enumConstrainedProperties(t *testing.T) map[string][]string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Clean(apolloSnapshot))
	if err != nil {
		t.Fatalf("read the pinned snapshot: %v", err)
	}
	lines := strings.Split(string(contents), "\n")

	// Pass one: which named schemas are enums, and what do they permit.
	enums := map[string][]string{}
	for index, line := range lines {
		name := snapshotSchemaName.FindStringSubmatch(line)
		if name == nil {
			continue
		}
		for cursor := index + 1; cursor < len(lines); cursor++ {
			next := lines[cursor]
			if snapshotSchemaName.MatchString(next) {
				break // the next schema; this one had no enum
			}
			if strings.TrimSpace(next) != "enum:" {
				continue
			}
			var members []string
			for member := cursor + 1; member < len(lines); member++ {
				match := snapshotEnumMember.FindStringSubmatch(lines[member])
				if match == nil {
					break
				}
				members = append(members, strings.Trim(match[1], `"'`))
			}
			if len(members) > 0 {
				enums[name[1]] = members
			}
			break
		}
	}
	if len(enums) == 0 {
		t.Fatal("the snapshot parse found no enum schemas at all; the gate would pass vacuously")
	}

	// Pass two: which property names reference one of them.
	constrained := map[string][]string{}
	for index, line := range lines {
		property := snapshotPropertyName.FindStringSubmatch(line)
		if property == nil || index+1 >= len(lines) {
			continue
		}
		ref := snapshotRef.FindStringSubmatch(lines[index+1])
		if ref == nil {
			continue
		}
		if members, isEnum := enums[ref[1]]; isEnum {
			constrained[property[1]] = members
		}
	}
	if len(constrained) == 0 {
		t.Fatal("no property references an enum schema; the walk below would check nothing")
	}
	return constrained
}

// assertEnumValues walks a decoded response and fails on any value of an
// enum-constrained property that the snapshot does not permit.
func assertEnumValues(t *testing.T, operation string, value any, constrained map[string][]string, path string) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			here := path + "." + key
			if members, isConstrained := constrained[key]; isConstrained {
				if served, isString := nested.(string); isString && !contains(members, served) {
					t.Errorf("%s served %s = %q, which is not one of %v",
						operation, here, served, members)
				}
			}
			assertEnumValues(t, operation, nested, constrained, here)
		}
	case []any:
		for index, nested := range typed {
			assertEnumValues(t, operation, nested, constrained, path+"[]")
			_ = index
		}
	}
}

// TestApolloResponsesMatchThePinnedSchemas drives the read operations and
// checks what they answer with against the snapshot.
func TestApolloResponsesMatchThePinnedSchemas(t *testing.T) {
	constrained := enumConstrainedProperties(t)
	handler := Handler()

	state := foundation()
	state.Environment.Namespaces = []NamespaceTarget{{
		RID: "ri.namespace.main.namespace.123", Name: "example", OntologyCountValid: true,
	}}
	if response := requestJSON(t, handler, http.MethodPost, "/v1/plans", state); response.Code != http.StatusCreated {
		t.Fatalf("seed the catalog: %d %s", response.Code, response.Body.String())
	}
	publish := map[string]any{"extensions": map[string]any{
		"helmChart": map[string]any{
			"helmChartName": "example-api", "helmChartVersion": "0.1.0",
			"helmRepositoryUrl": "https://charts.example.test",
		},
		"artifacts": []any{}, "productDependencies": []any{},
	}}
	if response := requestJSON(t, handler, http.MethodPost,
		"/api/v0/products/helmChart/app.example:example-api:0.1.0/publish", publish); response.Code != http.StatusOK {
		t.Fatalf("seed a published Release: %d %s", response.Code, response.Body.String())
	}

	environmentRID := ""
	{
		response := requestJSON(t, handler, http.MethodGet, "/api/v0/environments", nil)
		var listed struct {
			Environments []struct {
				RID string `json:"rid"`
			} `json:"environments"`
		}
		decodeResponse(t, response, http.StatusOK, &listed)
		if len(listed.Environments) == 0 {
			t.Fatal("the seed produced no environment, so most operations below would read nothing")
		}
		environmentRID = listed.Environments[0].RID
	}

	// The decode target is the type openapi.gen.go generated from this
	// operation's declared response schema, so a strict decode into it is the
	// snapshot checking the response.
	for _, operation := range []struct {
		id, path string
		into     any
	}{
		{"listProducts", "/api/v0/products", &apollo.ListProductsResponse{}},
		{"getProduct", "/api/v0/products/app.example:example-api", &apollo.Product{}},
		{"listReleases", "/api/v0/products/app.example:example-api/releases", &apollo.ListReleasesResponse{}},
		{"getRelease", "/api/v0/products/app.example:example-api/releases/0.1.0", &apollo.Release{}},
		{"listEnvironments", "/api/v0/environments", &apollo.ListEnvironmentsResponse{}},
		{"getEnvironment", "/api/v0/environments/" + environmentRID, &apollo.Environment{}},
		{"listInstallations", "/api/v0/environments/" + environmentRID + "/installations", &apollo.ListInstallationsResponse{}},
		{"listNamespaces", "/api/v0/namespaces", &apollo.ListNamespacesResponse{}},
	} {
		t.Run(operation.id, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, operation.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("%s = %d %s", operation.id, response.Code, response.Body.String())
			}

			// Strict decode into the generated type. An invented or renamed
			// property has nowhere to go and fails here; this is the half that
			// no existing gate performed at all.
			strict := json.NewDecoder(strings.NewReader(response.Body.String()))
			strict.DisallowUnknownFields()
			if err := strict.Decode(operation.into); err != nil {
				t.Errorf("%s answered with a shape the pinned schema does not declare: %v\nbody: %s",
					operation.id, err, response.Body.String())
			}

			// The enum walk needs the raw shape: the generated enum types are
			// string aliases, so a decode normalises nothing and checks nothing
			// about the values themselves.
			var raw any
			if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
				t.Fatalf("%s did not answer with JSON: %v", operation.id, err)
			}
			assertEnumValues(t, operation.id, raw, constrained, operation.id)
		})
	}
}

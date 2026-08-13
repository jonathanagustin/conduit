package main

import (
	"strings"
	"testing"
)

func TestGoNamePreservesPlatformInitialisms(t *testing.T) {
	cases := map[string]string{
		"apolloEnvironmentId":                     "ApolloEnvironmentID",
		"blockSetVersionIdToSpacesStatus":         "BlockSetVersionIDToSpacesStatus",
		"ReleaseCreationError_apolloPublishError": "ReleaseCreationErrorApolloPublishError",
		"cbacMarkings":                            "CBACMarkings",
		"slsVersion":                              "SLSVersion",
		"artifactUri":                             "ArtifactURI",
	}
	for input, want := range cases {
		if got := goName(input); got != want {
			t.Errorf("goName(%q) = %q, want %q", input, got, want)
		}
	}
}

func primitive(name string) conjureType { return conjureType{Type: "primitive", Primitive: name} }
func reference(name string) conjureType {
	return conjureType{Type: "reference", Reference: &typeName{Name: name}}
}
func optional(item conjureType) conjureType {
	return conjureType{Type: "optional", Optional: &itemType{ItemType: item}}
}
func list(item conjureType) conjureType {
	return conjureType{Type: "list", List: &itemType{ItemType: item}}
}
func mapping(key, value conjureType) conjureType {
	return conjureType{Type: "map", Map: &mapType{KeyType: key, ValueType: value}}
}

// goType decides what every wire type becomes, and a wrong mapping is a type
// that does not round-trip against the upstream contract. The mapping is not
// one-to-one -- five Conjure primitives all become string -- so a table is the
// only way to see that each is deliberate rather than a default.
func TestGoTypeMapsEveryPrimitiveAndComposite(t *testing.T) {
	t.Parallel()
	gen := &generator{
		byName: map[string]typeDefinition{},
		roots:  map[string]bool{},
	}

	for name, test := range map[string]struct {
		given conjureType
		want  string
	}{
		// Identifier-shaped primitives are all strings: the distinction is in
		// the upstream contract, not in Go.
		"string":      {primitive("STRING"), "string"},
		"rid":         {primitive("RID"), "string"},
		"uuid":        {primitive("UUID"), "string"},
		"datetime":    {primitive("DATETIME"), "string"},
		"bearertoken": {primitive("BEARERTOKEN"), "string"},
		"boolean":     {primitive("BOOLEAN"), "bool"},
		// Widths are pinned: INTEGER is 32-bit and SAFELONG is 64, and
		// widening either silently changes what round-trips.
		"integer":  {primitive("INTEGER"), "int32"},
		"safelong": {primitive("SAFELONG"), "int64"},
		"double":   {primitive("DOUBLE"), "float64"},
		"binary":   {primitive("BINARY"), "[]byte"},
		// ANY is deliberately raw so unknown payloads survive untouched.
		"any": {primitive("ANY"), "json.RawMessage"},

		"list":         {list(primitive("STRING")), "[]string"},
		"map":          {mapping(primitive("STRING"), primitive("INTEGER")), "map[string]int32"},
		"nested list":  {list(list(primitive("RID"))), "[][]string"},
		"map of lists": {mapping(primitive("UUID"), list(primitive("BOOLEAN"))), "map[string][]bool"},

		// An optional scalar is a pointer, so absent and zero stay distinct.
		"optional string": {optional(primitive("STRING")), "*string"},
		// An optional list or map is not: both already have a nil form, and a
		// pointer to a slice is a shape nobody wants to marshal.
		"optional list": {optional(list(primitive("STRING"))), "[]string"},
		"optional map":  {optional(mapping(primitive("STRING"), primitive("STRING"))), "map[string]string"},
		// Optional ANY is already nilable as raw JSON.
		"optional any": {optional(primitive("ANY")), "json.RawMessage"},
	} {
		if got := gen.goType(test.given, false); got != test.want {
			t.Errorf("%s: goType = %q, want %q", name, got, test.want)
		}
	}
}

// A reference is resolved through the bounded set: an alias collapses to
// whatever it aliases and an enum to a string, unless the name is a root the
// generator emits as its own type. Emitting a name the generated package does
// not declare produces source that will not compile.
func TestGoTypeResolvesReferencesThroughTheBoundedSet(t *testing.T) {
	t.Parallel()
	aliasOf := func(target conjureType) typeDefinition {
		definition := typeDefinition{Type: "alias"}
		definition.Alias = &struct {
			TypeName typeName    `json:"typeName"`
			Alias    conjureType `json:"alias"`
		}{TypeName: typeName{Name: "ApolloPlanRid"}, Alias: target}
		return definition
	}
	enum := typeDefinition{Type: "enum"}
	object := typeDefinition{Type: "object"}

	gen := &generator{
		byName: map[string]typeDefinition{
			"ApolloPlanRid":  aliasOf(primitive("RID")),
			"ApolloStatus":   enum,
			"ApolloManifest": object,
			"RootAlias":      aliasOf(primitive("STRING")),
		},
		roots: map[string]bool{"RootAlias": true},
	}

	for name, test := range map[string]struct {
		given string
		want  string
	}{
		"an alias collapses to what it aliases": {"ApolloPlanRid", "string"},
		"an enum collapses to a string":         {"ApolloStatus", "string"},
		// An object is emitted as its own type, so the reference keeps its name.
		"an object keeps its name": {"ApolloManifest", "ApolloManifest"},
		// A root is emitted even when it is an alias, so it keeps its name too.
		"a root alias keeps its name": {"RootAlias", "RootAlias"},
		// A name outside the bounded set is emitted as written; the selection
		// is what guarantees it exists.
		"an unknown name is emitted as written": {"ApolloUnbounded", "ApolloUnbounded"},
	} {
		if got := gen.goType(reference(test.given), false); got != test.want {
			t.Errorf("%s: goType(%s) = %q, want %q", name, test.given, got, test.want)
		}
	}
}

// typeUsesJSON decides whether the generated file imports encoding/json.
// Getting it wrong either leaves an unused import, which will not compile, or
// omits a needed one, which also will not compile -- so it has to follow the
// same resolution goType does.
func TestTypeUsesJSONFollowsAliasesAndComposites(t *testing.T) {
	t.Parallel()
	aliasToAny := typeDefinition{Type: "alias"}
	aliasToAny.Alias = &struct {
		TypeName typeName    `json:"typeName"`
		Alias    conjureType `json:"alias"`
	}{TypeName: typeName{Name: "AnyAlias"}, Alias: primitive("ANY")}

	gen := &generator{byName: map[string]typeDefinition{"AnyAlias": aliasToAny}, roots: map[string]bool{}}

	for name, test := range map[string]struct {
		given conjureType
		want  bool
	}{
		"a bare ANY":               {primitive("ANY"), true},
		"another primitive":        {primitive("STRING"), false},
		"an alias to ANY":          {reference("AnyAlias"), true},
		"a reference to nothing":   {reference("Unknown"), false},
		"an optional ANY":          {optional(primitive("ANY")), true},
		"a list of ANY":            {list(primitive("ANY")), true},
		"a map keyed by ANY":       {mapping(primitive("ANY"), primitive("STRING")), true},
		"a map valued by ANY":      {mapping(primitive("STRING"), primitive("ANY")), true},
		"a map of neither":         {mapping(primitive("STRING"), primitive("RID")), false},
		"a list of something else": {list(primitive("BOOLEAN")), false},
	} {
		if got := gen.typeUsesJSON(test.given); got != test.want {
			t.Errorf("%s: typeUsesJSON = %v, want %v", name, got, test.want)
		}
	}
}

// splitWords is what makes initialism replacement possible: it has to break a
// camelCase identifier at the right points, including the acronym-then-word
// case, or the pieces never match an entry in the table.
func TestSplitWordsBreaksCamelCaseIncludingAcronyms(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		given string
		want  string
	}{
		"empty":     {"", ""},
		"one word":  {"apollo", "apollo"},
		"two words": {"apolloPlan", "apollo,Plan"},
		// An acronym followed by a word breaks before the last capital, so
		// "RIDValue" is RID and Value rather than one run.
		"acronym then word":  {"ridValue", "rid,Value"},
		"trailing acronym":   {"planRID", "plan,RID"},
		"acronym then word2": {"RIDValue", "RID,Value"},
		"single capital":     {"A", "A"},
		"all capitals":       {"RID", "RID"},
	} {
		got := strings.Join(splitWords(test.given), ",")
		if got != test.want {
			t.Errorf("%s: splitWords(%q) = %q, want %q", name, test.given, got, test.want)
		}
	}
}

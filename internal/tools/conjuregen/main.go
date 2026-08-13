// Command conjuregen generates the deliberately small, type-only Go binding
// used at Platform's Foundry interoperability boundary. It is not a general
// Conjure implementation: services and errors are rejected, and Conjure ANY
// is retained as json.RawMessage so opaque external payloads round-trip.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

type typeName struct {
	Name    string `json:"name"`
	Package string `json:"package"`
}

type conjureType struct {
	Type      string    `json:"type"`
	Primitive string    `json:"primitive,omitempty"`
	Reference *typeName `json:"reference,omitempty"`
	Optional  *itemType `json:"optional,omitempty"`
	List      *itemType `json:"list,omitempty"`
	Map       *mapType  `json:"map,omitempty"`
}

type itemType struct {
	ItemType conjureType `json:"itemType"`
}

type mapType struct {
	KeyType   conjureType `json:"keyType"`
	ValueType conjureType `json:"valueType"`
}

type field struct {
	FieldName string      `json:"fieldName"`
	Type      conjureType `json:"type"`
}

type typeDefinition struct {
	Type  string `json:"type"`
	Alias *struct {
		TypeName typeName    `json:"typeName"`
		Alias    conjureType `json:"alias"`
	} `json:"alias,omitempty"`
	Enum *struct {
		TypeName typeName `json:"typeName"`
		Values   []struct {
			Value string `json:"value"`
		} `json:"values"`
	} `json:"enum,omitempty"`
	Object *struct {
		TypeName typeName `json:"typeName"`
		Fields   []field  `json:"fields"`
	} `json:"object,omitempty"`
	Union *struct {
		TypeName typeName `json:"typeName"`
		Union    []field  `json:"union"`
	} `json:"union,omitempty"`
}

type sourceMetadata struct {
	Repository        string `json:"repository"`
	Commit            string `json:"commit"`
	NPMPackage        string `json:"npmPackage"`
	NPMVersion        string `json:"npmVersion"`
	NPMTarballSHA256  string `json:"npmTarballSha256"`
	TypeScriptVersion string `json:"typescriptVersion"`
}

type contractMetadata struct {
	Version       string `json:"version"`
	AdapterID     string `json:"adapterId"`
	ManagedMode   string `json:"managedMode"`
	UniversalMode string `json:"universalMode"`
}

type definition struct {
	Version    int               `json:"version"`
	Errors     []json.RawMessage `json:"errors"`
	Types      []typeDefinition  `json:"types"`
	Services   []json.RawMessage `json:"services"`
	Extensions struct {
		Platform struct {
			Contract      contractMetadata `json:"contract"`
			Source        sourceMetadata   `json:"source"`
			RootTypes     []string         `json:"rootTypes"`
			OpaqueTypes   []string         `json:"opaqueTypes"`
			JavaInventory []string         `json:"javaInventory"`
		} `json:"platform"`
	} `json:"extensions"`
}

type generator struct {
	definition  definition
	byName      map[string]typeDefinition
	roots       map[string]bool
	packageName string
}

func main() {
	input := flag.String("input", "", "bounded Conjure IR input")
	output := flag.String("output", "", "generated Go package directory")
	packageName := flag.String("package", "wire", "generated Go package name")
	flag.Parse()
	if *input == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "usage: conjuregen -input <contract.conjure.json> -output <package-dir> [-package <name>]")
		os.Exit(2)
	}
	content, err := os.ReadFile(*input)
	check(err)
	var def definition
	check(json.Unmarshal(content, &def))
	if def.Version != 1 || len(def.Errors) != 0 || len(def.Services) != 0 {
		check(fmt.Errorf("only type-only Conjure IR version 1 is supported"))
	}
	if *packageName == "" {
		check(fmt.Errorf("generated package name is required"))
	}
	g := generator{definition: def, byName: make(map[string]typeDefinition), roots: make(map[string]bool), packageName: *packageName}
	for _, name := range def.Extensions.Platform.RootTypes {
		g.roots[name] = true
	}
	for _, value := range def.Types {
		name := definitionName(value)
		if name == "" || g.byName[name].Type != "" {
			check(fmt.Errorf("missing or duplicate type name %q", name))
		}
		g.byName[name] = value
	}
	contractHash := sha256.Sum256(content)
	check(os.MkdirAll(*output, 0o755))
	g.write(filepath.Join(*output, "doc.conjure.go"), g.docFile(hex.EncodeToString(contractHash[:])))
	g.write(filepath.Join(*output, "aliases.conjure.go"), g.aliasFile())
	g.write(filepath.Join(*output, "enums.conjure.go"), g.enumFile())
	g.write(filepath.Join(*output, "objects.conjure.go"), g.objectFile())
	g.write(filepath.Join(*output, "unions.conjure.go"), g.unionFile())
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func definitionName(value typeDefinition) string {
	switch value.Type {
	case "alias":
		return value.Alias.TypeName.Name
	case "enum":
		return value.Enum.TypeName.Name
	case "object":
		return value.Object.TypeName.Name
	case "union":
		return value.Union.TypeName.Name
	default:
		return ""
	}
}

func (g *generator) header(importJSON bool) *bytes.Buffer {
	return g.headerWithPackageComment(importJSON, "")
}

func (g *generator) headerWithPackageComment(importJSON bool, packageComment string) *bytes.Buffer {
	var out bytes.Buffer
	out.WriteString("// Code generated by Platform's bounded Conjure generator. DO NOT EDIT.\n\n")
	for line := range strings.SplitSeq(packageComment, "\n") {
		if packageComment == "" {
			break
		}
		fmt.Fprintf(&out, "// %s\n", line)
	}
	fmt.Fprintf(&out, "package %s\n", g.packageName)
	if importJSON {
		out.WriteString("\nimport \"encoding/json\"\n")
	}
	return &out
}

func (g *generator) docFile(hash string) []byte {
	// The package comment lives here rather than in a hand-written doc.go
	// beside it: this file is generated, and a comment a regeneration can
	// silently drop is one that will eventually be wrong.
	out := g.headerWithPackageComment(false,
		"Package "+g.packageName+" holds the Foundry Apollo transport types this repository\n"+
			"generates from a pinned upstream commit. Nothing here is written by hand and\n"+
			"nothing here is part of Platform's own model; foundryinterop adapts these to it.")
	source := g.definition.Extensions.Platform.Source
	contract := g.definition.Extensions.Platform.Contract
	fmt.Fprintf(out, "\nconst ContractSHA256 = %q\n", hash)
	fmt.Fprintf(out, "const ContractVersion = %q\n", contract.Version)
	fmt.Fprintf(out, "const AdapterID = %q\n", contract.AdapterID)
	fmt.Fprintf(out, "const ManagedMode = %q\n", contract.ManagedMode)
	fmt.Fprintf(out, "const UniversalMode = %q\n", contract.UniversalMode)
	fmt.Fprintf(out, "const SourceRepository = %q\n", source.Repository)
	fmt.Fprintf(out, "const SourceCommit = %q\n", source.Commit)
	fmt.Fprintf(out, "const SourceNPMPackage = %q\n", source.NPMPackage)
	fmt.Fprintf(out, "const SourceNPMVersion = %q\n", source.NPMVersion)
	fmt.Fprintf(out, "const SourceNPMTarballSHA256 = %q\n", source.NPMTarballSHA256)
	out.WriteString("\nvar StructuralTypeInventory = []string{\n")
	for _, name := range g.definition.Extensions.Platform.JavaInventory {
		fmt.Fprintf(out, "\t%q,\n", name)
	}
	out.WriteString("}\n")
	return out.Bytes()
}

func (g *generator) aliasFile() []byte {
	values := g.ofKind("alias")
	needsJSON := false
	for _, value := range values {
		if g.typeUsesJSON(value.Alias.Alias) {
			needsJSON = true
		}
	}
	out := g.header(needsJSON)
	for _, value := range values {
		fmt.Fprintf(out, "\ntype %s %s\n", goName(value.Alias.TypeName.Name), g.goType(value.Alias.Alias, false))
	}
	return out.Bytes()
}

func (g *generator) enumFile() []byte {
	values := g.ofKind("enum")
	out := g.header(false)
	for _, value := range values {
		name := goName(value.Enum.TypeName.Name)
		fmt.Fprintf(out, "\ntype %s string\n\nconst (\n", name)
		for _, enumValue := range value.Enum.Values {
			fmt.Fprintf(out, "\t%s%s %s = %q\n", name, goName(enumValue.Value), name, enumValue.Value)
		}
		out.WriteString(")\n")
	}
	return out.Bytes()
}

func (g *generator) objectFile() []byte {
	values := g.ofKind("object")
	needsJSON := false
	for _, value := range values {
		for _, field := range value.Object.Fields {
			if g.typeUsesJSON(field.Type) {
				needsJSON = true
			}
		}
	}
	out := g.header(needsJSON)
	for _, value := range values {
		fmt.Fprintf(out, "\ntype %s struct {\n", goName(value.Object.TypeName.Name))
		for _, field := range value.Object.Fields {
			optionalField := field.Type.Type == "optional"
			jsonName := field.FieldName
			if optionalField {
				jsonName += ",omitempty"
			}
			fmt.Fprintf(out, "\t%s %s `json:%q`\n", goName(field.FieldName), g.goType(field.Type, false), jsonName)
		}
		out.WriteString("}\n")
	}
	return out.Bytes()
}

func (g *generator) unionFile() []byte {
	values := g.ofKind("union")
	out := g.header(len(values) != 0)
	for _, value := range values {
		name := goName(value.Union.TypeName.Name)
		fmt.Fprintf(out, "\n// %s retains the complete tagged union so unknown variants round-trip.\n", name)
		fmt.Fprintf(out, "type %s json.RawMessage\n", name)
		for _, variant := range value.Union.Union {
			variantName := goName(variant.FieldName)
			fmt.Fprintf(out, "\ntype %s%s struct {\n", name, variantName)
			fmt.Fprintln(out, "\tType string `json:\"type\"`")
			fmt.Fprintf(out, "\t%s %s `json:%q`\n", variantName, g.goType(variant.Type, false), variant.FieldName)
			out.WriteString("}\n")
		}
	}
	return out.Bytes()
}

func (g *generator) ofKind(kind string) []typeDefinition {
	values := make([]typeDefinition, 0)
	for _, value := range g.definition.Types {
		if value.Type == kind {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool { return definitionName(values[i]) < definitionName(values[j]) })
	return values
}

func (g *generator) goType(value conjureType, nestedOptional bool) string {
	switch value.Type {
	case "primitive":
		switch value.Primitive {
		case "ANY":
			return "json.RawMessage"
		case "STRING", "RID", "UUID", "DATETIME", "BEARERTOKEN":
			return "string"
		case "BOOLEAN":
			return "bool"
		case "INTEGER":
			return "int32"
		case "SAFELONG":
			return "int64"
		case "DOUBLE":
			return "float64"
		case "BINARY":
			return "[]byte"
		default:
			check(fmt.Errorf("unsupported primitive %q", value.Primitive))
		}
	case "reference":
		name := value.Reference.Name
		if !g.roots[name] {
			if target, ok := g.byName[name]; ok {
				switch target.Type {
				case "alias":
					return g.goType(target.Alias.Alias, nestedOptional)
				case "enum":
					return "string"
				}
			}
		}
		return goName(name)
	case "optional":
		item := value.Optional.ItemType
		if item.Type == "primitive" && item.Primitive == "ANY" {
			return "json.RawMessage"
		}
		if item.Type == "list" || item.Type == "map" {
			return g.goType(item, true)
		}
		return "*" + g.goType(item, true)
	case "list":
		return "[]" + g.goType(value.List.ItemType, false)
	case "map":
		return "map[" + g.goType(value.Map.KeyType, false) + "]" + g.goType(value.Map.ValueType, false)
	default:
		check(fmt.Errorf("unsupported Conjure type %q", value.Type))
	}
	return ""
}

func (g *generator) typeUsesJSON(value conjureType) bool {
	switch value.Type {
	case "primitive":
		return value.Primitive == "ANY"
	case "reference":
		if target, ok := g.byName[value.Reference.Name]; ok && target.Type == "alias" {
			return g.typeUsesJSON(target.Alias.Alias)
		}
	case "optional":
		return g.typeUsesJSON(value.Optional.ItemType)
	case "list":
		return g.typeUsesJSON(value.List.ItemType)
	case "map":
		return g.typeUsesJSON(value.Map.KeyType) || g.typeUsesJSON(value.Map.ValueType)
	}
	return false
}

var initialisms = map[string]string{
	"Api": "API", "Cbac": "CBAC", "Id": "ID", "Ids": "IDs", "Rid": "RID", "Rids": "RIDs",
	"Sls": "SLS", "Tofu": "TOFU", "Uri": "URI", "Url": "URL", "Uuid": "UUID",
}

func goName(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '_' || r == '-' || r == '.' })
	var result strings.Builder
	for _, part := range parts {
		words := splitWords(part)
		for _, word := range words {
			word = strings.ToUpper(word[:1]) + word[1:]
			if replacement, ok := initialisms[word]; ok {
				word = replacement
			}
			result.WriteString(word)
		}
	}
	return result.String()
}

func splitWords(value string) []string {
	if value == "" {
		return nil
	}
	var words []string
	start := 0
	runes := []rune(value)
	for i := 1; i < len(runes); i++ {
		if unicode.IsUpper(runes[i]) && (unicode.IsLower(runes[i-1]) || (i+1 < len(runes) && unicode.IsLower(runes[i+1]))) {
			words = append(words, string(runes[start:i]))
			start = i
		}
	}
	return append(words, string(runes[start:]))
}

func (g *generator) write(path string, content []byte) {
	formatted, err := format.Source(content)
	if err != nil {
		check(fmt.Errorf("format %s: %w\n%s", path, err, content))
	}
	check(os.WriteFile(path, formatted, 0o644))
}

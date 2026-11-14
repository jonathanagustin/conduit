// Package domain is the ontology catalog: object types, links, and the one
// governed action projected from Conduit's own records.
//
// The JSON field names follow the published ontology v2 REST documents so a
// client written against those documents can read this Hub. Nothing here is a
// generated transport, and the catalog is not a copy of another product's
// schema. Interface types are part of the response and stay empty: no two of
// these object types share a domain interface.
package domain

const (
	OntologyName = "conduit"
	ontologyRID  = "ri.conduit.main.ontology.conduit"

	ModeValidateOnly       = "VALIDATE_ONLY"
	ModeValidateAndExecute = "VALIDATE_AND_EXECUTE"
	ActionCreateChange     = "createChangeRequest"
	ResultValid            = "VALID"
	ResultInvalid          = "INVALID"
)

// TypeSpec is one object type the catalog can project.
type TypeSpec struct {
	APIName       string
	DisplayName   string
	Plural        string
	Description   string
	PrimaryKey    string
	TitleProperty string
	Icon          string
	Color         string
	Properties    []PropSpec
}

// PropSpec is one string property.
type PropSpec struct {
	APIName     string
	DisplayName string
}

// LinkSpec is one link, named from each end and sharing one rid so a client
// can pair the sides.
type LinkSpec struct {
	RID      string
	From     string
	FromName string
	FromCard string
	To       string
	ToName   string
	ToCard   string
}

// Types is the object catalog, in a stable order.
func Types() []TypeSpec {
	return []TypeSpec{
		{APIName: "Environment", DisplayName: "Environment", Plural: "Environments",
			Description: "A delivery environment and the generation it declares.",
			PrimaryKey:  "id", TitleProperty: "id", Icon: "application", Color: "#4C90F0",
			Properties: []PropSpec{
				{"id", "Identifier"}, {"generation", "Generation"},
				{"connection", "Connection"}, {"releaseChannel", "Default release channel"},
			}},
		{APIName: "Spoke", DisplayName: "Spoke", Plural: "Spokes",
			Description: "The agent that reports an environment.",
			PrimaryKey:  "id", TitleProperty: "id", Icon: "graph", Color: "#3DCC91",
			Properties: []PropSpec{{"id", "Identifier"}, {"environment", "Environment"}}},
		{APIName: "WorkProfile", DisplayName: "Work profile", Plural: "Work profiles",
			Description: "The operator-authored profile of an environment.",
			PrimaryKey:  "environmentId", TitleProperty: "displayName", Icon: "person", Color: "#AD99FF",
			Properties: []PropSpec{
				{"environmentId", "Environment"}, {"displayName", "Display name"},
				{"description", "Description"}, {"accreditation", "Accreditation"},
				{"ownerTeam", "Owner team"},
			}},
		{APIName: "ReleaseChannel", DisplayName: "Release channel", Plural: "Release channels",
			Description: "A release channel declared for an environment.",
			PrimaryKey:  "id", TitleProperty: "name", Icon: "tag", Color: "#F5A623",
			Properties: []PropSpec{
				{"id", "Identifier"}, {"environment", "Environment"},
				{"name", "Name"}, {"description", "Description"}, {"archived", "Archived"},
			}},
		{APIName: "Plan", DisplayName: "Plan", Plural: "Plans",
			Description: "A reviewed plan for one environment.",
			PrimaryKey:  "id", TitleProperty: "id", Icon: "flows", Color: "#48AFF0",
			Properties: []PropSpec{
				{"id", "Identifier"}, {"environment", "Environment"},
				{"status", "Status"}, {"generation", "Generation"},
				{"changeRequestId", "Change request"},
			}},
		{APIName: "ChangeRequest", DisplayName: "Change request", Plural: "Change requests",
			Description: "A governed change request. Creating one does not approve it.",
			PrimaryKey:  "id", TitleProperty: "title", Icon: "document", Color: "#D9822B",
			Properties: []PropSpec{
				{"id", "Identifier"}, {"environment", "Environment"},
				{"planId", "Plan"}, {"title", "Title"}, {"status", "Status"},
				{"author", "Author"},
			}},
		{APIName: "Artifact", DisplayName: "Artifact", Plural: "Artifacts",
			Description: "A chart or image referenced by a plan's desired release.",
			PrimaryKey:  "id", TitleProperty: "repository", Icon: "cube", Color: "#15B371",
			Properties: []PropSpec{
				{"id", "Identifier"}, {"product", "Product"}, {"version", "Version"},
				{"repository", "Repository"}, {"tag", "Tag"}, {"digest", "Digest"},
			}},
	}
}

// Links is every link the catalog projects, from both ends.
func Links() []LinkSpec {
	return []LinkSpec{
		{"ri.conduit.main.link-type.environment-spoke", "Environment", "reportingSpoke", "MANY", "Spoke", "environments", "MANY"},
		{"ri.conduit.main.link-type.environment-profile", "Environment", "workProfile", "ONE", "WorkProfile", "environment", "ONE"},
		{"ri.conduit.main.link-type.environment-channel", "Environment", "releaseChannels", "MANY", "ReleaseChannel", "environment", "ONE"},
		{"ri.conduit.main.link-type.environment-plan", "Environment", "plans", "MANY", "Plan", "environment", "ONE"},
		{"ri.conduit.main.link-type.environment-change", "Environment", "changeRequests", "MANY", "ChangeRequest", "environment", "ONE"},
		{"ri.conduit.main.link-type.plan-change", "Plan", "changeRequest", "ONE", "ChangeRequest", "plan", "ONE"},
		{"ri.conduit.main.link-type.plan-artifact", "Plan", "artifacts", "MANY", "Artifact", "plans", "MANY"},
	}
}

// KnownType reports whether apiName is a projected object type.
func KnownType(apiName string) bool {
	for _, spec := range Types() {
		if spec.APIName == apiName {
			return true
		}
	}
	return false
}

// KnownLink reports whether apiName is a link name on either end.
func KnownLink(apiName string) bool {
	for _, spec := range Links() {
		if spec.FromName == apiName || spec.ToName == apiName {
			return true
		}
	}
	return false
}

// KnownProperty reports whether the object type declares the property.
func KnownProperty(apiName, property string) bool {
	for _, spec := range Types() {
		if spec.APIName != apiName {
			continue
		}
		for _, prop := range spec.Properties {
			if prop.APIName == property {
				return true
			}
		}
		return false
	}
	return false
}

func typeSpec(apiName string) (TypeSpec, bool) {
	for _, spec := range Types() {
		if spec.APIName == apiName {
			return spec, true
		}
	}
	return TypeSpec{}, false
}

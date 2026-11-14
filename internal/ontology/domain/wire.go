package domain

// Ontology is the single catalog this Hub serves.
type Ontology struct {
	APIName     string `json:"apiName"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	RID         string `json:"rid"`
}

// DataType is a string property type.
type DataType struct {
	Type string `json:"type"`
}

// PropertyV2 is one property on an object type.
type PropertyV2 struct {
	RID         string   `json:"rid"`
	DisplayName string   `json:"displayName"`
	Description string   `json:"description,omitempty"`
	DataType    DataType `json:"dataType"`
}

// IconV2 is the marker a client may draw for an object type.
type IconV2 struct {
	Type  string `json:"type"`
	Color string `json:"color"`
	Name  string `json:"name"`
}

// ObjectTypeV2 is one object type.
type ObjectTypeV2 struct {
	APIName           string                `json:"apiName"`
	DisplayName       string                `json:"displayName"`
	Status            string                `json:"status"`
	PluralDisplayName string                `json:"pluralDisplayName"`
	Icon              IconV2                `json:"icon"`
	PrimaryKey        string                `json:"primaryKey"`
	Properties        map[string]PropertyV2 `json:"properties"`
	RID               string                `json:"rid"`
	TitleProperty     string                `json:"titleProperty"`
	Description       string                `json:"description,omitempty"`
	Aliases           []string              `json:"aliases,omitempty"`
	Datasources       []any                 `json:"datasources,omitempty"`
}

// LinkSideV2 is one end of a link. The other end shares linkTypeRid.
type LinkSideV2 struct {
	APIName           string `json:"apiName"`
	DisplayName       string `json:"displayName"`
	Status            string `json:"status"`
	ObjectTypeAPIName string `json:"objectTypeApiName"`
	Cardinality       string `json:"cardinality"`
	LinkTypeRID       string `json:"linkTypeRid"`
}

// ObjectTypeFull is an object type plus the link sides that start on it.
type ObjectTypeFull struct {
	ObjectType                ObjectTypeV2   `json:"objectType"`
	LinkTypes                 []LinkSideV2   `json:"linkTypes"`
	ImplementsInterfaces      []string       `json:"implementsInterfaces"`
	ImplementsInterfaces2     map[string]any `json:"implementsInterfaces2"`
	SharedPropertyTypeMapping map[string]any `json:"sharedPropertyTypeMapping"`
}

// ActionParameterV2 is one action parameter.
type ActionParameterV2 struct {
	DisplayName string   `json:"displayName"`
	Description string   `json:"description,omitempty"`
	DataType    DataType `json:"dataType"`
	Required    bool     `json:"required"`
}

// ActionOperationV2 names the object type an action writes.
type ActionOperationV2 struct {
	Type              string `json:"type"`
	ObjectTypeAPIName string `json:"objectTypeApiName"`
}

// ActionTypeV2 is the governed create-change-request action.
type ActionTypeV2 struct {
	APIName     string                       `json:"apiName"`
	Description string                       `json:"description"`
	DisplayName string                       `json:"displayName"`
	Status      string                       `json:"status"`
	Parameters  map[string]ActionParameterV2 `json:"parameters"`
	RID         string                       `json:"rid"`
	Operations  []ActionOperationV2          `json:"operations"`
}

// FullMetadata is the ontology and every type it contains.
type FullMetadata struct {
	Ontology            Ontology                  `json:"ontology"`
	ObjectTypes         map[string]ObjectTypeFull `json:"objectTypes"`
	ActionTypes         map[string]ActionTypeV2   `json:"actionTypes"`
	QueryTypes          map[string]any            `json:"queryTypes"`
	InterfaceTypes      map[string]any            `json:"interfaceTypes"`
	SharedPropertyTypes map[string]any            `json:"sharedPropertyTypes"`
	ValueTypes          map[string]any            `json:"valueTypes"`
}

// ListResponse is a page of values.
type ListResponse[T any] struct {
	Data          []T    `json:"data"`
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// OntologyDocument is the one ontology this Hub publishes.
func OntologyDocument() Ontology {
	return Ontology{
		APIName:     OntologyName,
		DisplayName: "Conduit",
		Description: "Environments, spokes, work profiles, release channels, plans, change requests, and artifacts.",
		RID:         ontologyRID,
	}
}

// KnownOntology reports whether the path segment names this catalog.
func KnownOntology(name string) bool {
	return name == OntologyName || name == ontologyRID
}

// Metadata builds the full metadata document. The shape does not depend on
// which rows the caller may see.
func Metadata() FullMetadata {
	objects := map[string]ObjectTypeFull{}
	for _, spec := range Types() {
		properties := map[string]PropertyV2{}
		for _, prop := range spec.Properties {
			kind := prop.Kind
			if kind == "" {
				kind = "string"
			}
			properties[prop.APIName] = PropertyV2{
				RID:         "ri.conduit.main.object-type." + spec.APIName + ".property." + prop.APIName,
				DisplayName: prop.DisplayName, DataType: DataType{Type: kind},
			}
		}
		objects[spec.APIName] = objectTypeFull(spec, properties)
	}
	return FullMetadata{
		Ontology:            OntologyDocument(),
		ObjectTypes:         objects,
		ActionTypes:         publishedActions(),
		QueryTypes:          map[string]any{},
		InterfaceTypes:      map[string]any{},
		SharedPropertyTypes: map[string]any{},
		ValueTypes:          map[string]any{},
	}
}

func objectTypeFull(spec TypeSpec, properties map[string]PropertyV2) ObjectTypeFull {
	return ObjectTypeFull{
		ObjectType: ObjectTypeV2{
			APIName: spec.APIName, DisplayName: spec.DisplayName, Status: "ACTIVE",
			Description: spec.Description, PluralDisplayName: spec.Plural,
			Icon:       IconV2{Type: "blueprint", Color: spec.Color, Name: spec.Icon},
			PrimaryKey: spec.PrimaryKey, Properties: properties,
			RID: "ri.conduit.main.object-type." + spec.APIName, TitleProperty: spec.TitleProperty,
		},
		LinkTypes:                 linkSides(spec.APIName),
		ImplementsInterfaces:      []string{},
		ImplementsInterfaces2:     map[string]any{},
		SharedPropertyTypeMapping: map[string]any{},
	}
}

// ObjectTypeFullByName returns one object type with its links.
func ObjectTypeFullByName(apiName string) (ObjectTypeFull, bool) {
	full, ok := Metadata().ObjectTypes[apiName]
	return full, ok
}

// ObjectTypeByName returns one object type.
func ObjectTypeByName(apiName string) (ObjectTypeV2, bool) {
	full, ok := Metadata().ObjectTypes[apiName]
	if !ok {
		return ObjectTypeV2{}, false
	}
	return full.ObjectType, true
}

// ActionByName returns the one action, or false.
func ActionByName(apiName string) (ActionTypeV2, bool) {
	action, ok := Metadata().ActionTypes[apiName]
	return action, ok
}

func linkSides(apiName string) []LinkSideV2 {
	sides := []LinkSideV2{}
	for _, spec := range Links() {
		if spec.From == apiName {
			sides = append(sides, LinkSideV2{
				APIName: spec.FromName, DisplayName: displayLink(spec.FromName), Status: "ACTIVE",
				ObjectTypeAPIName: spec.From, Cardinality: spec.FromCard, LinkTypeRID: spec.RID,
			})
		}
		if spec.To == apiName {
			sides = append(sides, LinkSideV2{
				APIName: spec.ToName, DisplayName: displayLink(spec.ToName), Status: "ACTIVE",
				ObjectTypeAPIName: spec.To, Cardinality: spec.ToCard, LinkTypeRID: spec.RID,
			})
		}
	}
	return sides
}

func displayLink(apiName string) string {
	switch apiName {
	case "reportingSpoke":
		return "Reporting spoke"
	case "environments":
		return "Environments"
	case "workProfile":
		return "Work profile"
	case "releaseChannels":
		return "Release channels"
	case "plans":
		return "Plans"
	case "changeRequests":
		return "Change requests"
	case "changeRequest":
		return "Change request"
	case "artifacts":
		return "Artifacts"
	case "workspaces":
		return "Workspaces"
	default:
		return apiName
	}
}

func actionType() ActionTypeV2 {
	required := map[string]bool{}
	for _, name := range RequiredActionParameters() {
		required[name] = true
	}
	labels := []struct{ name, display, description string }{
		{"environment", "Environment", "Environment the change request targets."},
		{"targetKind", "Target kind", "Governance target kind, such as ENVIRONMENT."},
		{"targetId", "Target identifier", "Identifier of the governance target."},
		{"title", "Title", "Title stored on the change request."},
		{"description", "Description", "Description stored on the change request."},
		{"changeType", "Change type", "A change type the governance taxonomy already accepts."},
		{"changeId", "Change identifier", "Identifier of the change inside the request."},
		{"installationId", "Installation", "Installation referenced by change types that require one."},
		{"productId", "Product", "Product referenced by change types that require one."},
	}
	parameters := map[string]ActionParameterV2{}
	for _, label := range labels {
		parameters[label.name] = ActionParameterV2{
			DisplayName: label.display, Description: label.description,
			DataType: DataType{Type: "string"}, Required: required[label.name],
		}
	}
	return ActionTypeV2{
		APIName: ActionCreateChange, DisplayName: "Create change request", Status: "ACTIVE",
		Description: "Proposes a change request through the existing approval path. VALIDATE_ONLY does not store one.",
		Parameters:  parameters,
		RID:         "ri.conduit.main.action-type." + ActionCreateChange,
		Operations:  []ActionOperationV2{{Type: "createObject", ObjectTypeAPIName: "ChangeRequest"}},
	}
}

func publishedActions() map[string]ActionTypeV2 {
	actions := map[string]ActionTypeV2{ActionCreateChange: actionType()}
	for _, spec := range []ActionTypeV2{
		{APIName: "workspace_create", DisplayName: "Create workspace", Description: "Creates an authorized workspace. It does not approve or deploy."},
		{APIName: "workspace_stop", DisplayName: "Stop workspace", Description: "Stops the local preview runtime."},
		{APIName: "workspace_delete", DisplayName: "Delete workspace", Description: "Deletes a stopped workspace after confirmation."},
		{APIName: "workspace_submit", DisplayName: "Submit workspace", Description: "Opens a change request. It does not approve that request."},
	} {
		spec.Status = "ACTIVE"
		spec.RID = "ri.conduit.main.action-type." + spec.APIName
		spec.Parameters = map[string]ActionParameterV2{}
		spec.Operations = []ActionOperationV2{}
		actions[spec.APIName] = spec
	}
	return actions
}

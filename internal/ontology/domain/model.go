package domain

import "errors"

// Object is one projected record. Properties use the names in the type spec.
type Object struct {
	Type       string
	PrimaryKey string
	Properties map[string]string
}

// Link is one directed edge. APIName is the link name on the source type.
type Link struct {
	APIName  string
	FromType string
	FromKey  string
	ToType   string
	ToKey    string
}

// Catalog is the snapshot a read was allowed to see.
type Catalog struct {
	Objects []Object
	Links   []Link
}

// ObjectSet is a base, filter, or search-around set.
type ObjectSet struct {
	Type       string
	ObjectType string
	Where      *Where
	Inner      *ObjectSet
	Link       string
}

// Where is an equality, conjunction, disjunction, or negation.
type Where struct {
	Type    string
	Field   string
	Value   string
	Clauses []Where
}

// Page is one page of object records.
type Page struct {
	Objects       []Object
	NextPageToken string
	TotalCount    int
}

// ApplyRequest is the one governed action this catalog exposes.
type ApplyRequest struct {
	Action     string
	Parameters map[string]string
	Mode       string
}

// ApplyResult is the validation outcome. Stored is set only when a change
// request was written, which happens on VALIDATE_AND_EXECUTE and never on
// VALIDATE_ONLY.
type ApplyResult struct {
	Valid              bool
	ParameterMessages  map[string]string
	SubmissionMessage  string
	ChangeRequestID    string
	ChangeRequestState string
	Stored             bool
}

var (
	ErrUnknownType   = errors.New("unknown object type")
	ErrUnknownAction = errors.New("unknown action")
	ErrBadQuery      = errors.New("unsupported object set")
)

// RequiredActionParameters are the parameters createChangeRequest demands.
func RequiredActionParameters() []string {
	return []string{"environment", "targetKind", "targetId", "changeType"}
}

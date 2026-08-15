package domain

// ChangeDiffEntry is one path a change edits, and what it edits it from and to.
// Before is absent when the path did not exist.
type ChangeDiffEntry struct {
	Before any `json:"before,omitempty"`
	After  any `json:"after,omitempty"`
}

// ReferencedResource is a resource a change touches, flattened to the facts a
// policy is allowed to see: what it is, what it is called, how it is labelled,
// and what the author already holds over it.
type ReferencedResource struct {
	RID         string            `json:"rid"`
	Name        string            `json:"name"`
	Labels      map[string]string `json:"labels"`
	AuthorRoles []string          `json:"authorRoles"`
	Resource    map[string]any    `json:"resource,omitempty"`
}

// ChangeFacts is everything an approval policy may read about one change.
//
// It is deliberately not the Change Request's own entity-change record. The
// record carries identifiers and installation coordinates that a policy has no
// business branching on, and it belongs to the context that owns the request's
// lifecycle. This is the projection of it that Governance judges: a type, the
// request as submitted, its declared origin, what it edits, and what it
// touches. Anything a policy needs that is not here is a deliberate omission
// rather than an oversight.
type ChangeFacts struct {
	Type    string
	Request map[string]any
	// Metadata records the origin of the change so a policy can tell automation
	// apart from an operator editing the same resource. It is the input the
	// published is_trusted_module_request and is_trusted_import_request
	// predicates read.
	Metadata            map[string]any
	Diff                map[string]ChangeDiffEntry
	ReferencedResources map[string]ReferencedResource
}

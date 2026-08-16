// Package schema is the wire shape of the Knative resources Conduit projects.
//
// The types mirror serving.knative.dev/v1 as Knative publishes it
// (https://knative.dev/docs/serving/reference/serving-api/), so a client that
// knows Knative recognizes what Conduit returns. They are deliberately a subset:
// only the fields Conduit can fill from its own delivery data, plus the
// identifying envelope (apiVersion, kind, metadata) that makes the object a
// Knative object at all. A field Conduit cannot back is left off the struct
// rather than serialized as a zero value -- the domain.Mapping records why, and
// the projection refuses it if asked.
//
// omitempty is load-bearing here for the same reason: an absent field reads as
// "Conduit does not project this," which is true, where an emitted zero would
// read as a Knative default Conduit never chose.
package schema

// APIVersion is the single Knative Serving GA group/version this interface
// speaks. It is not a guess: v1 is the GA version Knative documents for the core
// resources.
const APIVersion = "serving.knative.dev/v1"

const (
	KindRevision      = "Revision"
	KindConfiguration = "Configuration"
	KindService       = "Service"
	KindRoute         = "Route"
)

// ObjectMeta is the Kubernetes metadata envelope, narrowed to what Conduit
// fills. Name is required for any object; labels carry the Knative ownership
// links (a Revision names its Configuration), which Conduit knows because a
// Release names its Product.
type ObjectMeta struct {
	Name      string            `json:"name"`
	Labels    map[string]string `json:"labels,omitempty"`
	Namespace string            `json:"namespace,omitempty"`
}

// Container is the subset of the Kubernetes container spec a Knative Revision
// carries that Conduit can fill. Conduit knows the image -- a Release names its
// OCI image artifacts by digest -- and nothing else about the running pod, so
// nothing else is here.
type Container struct {
	Image string `json:"image"`
}

// RevisionSpec is the immutable snapshot's spec. TimeoutSeconds is filled from
// the Release's resource-readiness timeout; containerConcurrency and the rest of
// PodSpec are refused, so they are absent rather than zero.
type RevisionSpec struct {
	Containers     []Container `json:"containers"`
	TimeoutSeconds *int        `json:"timeoutSeconds,omitempty"`
}

// Revision is Knative's "immutable snapshot of code and configuration." Conduit
// projects a Release as one: both are the immutable, versioned unit of their
// system, which is the correspondence that makes this honest rather than a
// re-labelling.
type Revision struct {
	APIVersion string       `json:"apiVersion"`
	Kind       string       `json:"kind"`
	Metadata   ObjectMeta   `json:"metadata"`
	Spec       RevisionSpec `json:"spec"`
}

// ConfigurationStatus carries the two revision pointers Knative defines on a
// Configuration. Conduit fills them from a Product's release history: the newest
// Release is the latest created, and the current channel head is the latest
// ready. Either may be empty when Conduit cannot determine it, in which case it
// is omitted rather than pointed at a guess.
type ConfigurationStatus struct {
	LatestCreatedRevisionName string `json:"latestCreatedRevisionName,omitempty"`
	LatestReadyRevisionName   string `json:"latestReadyRevisionName,omitempty"`
}

// Configuration is Knative's "floating HEAD of a linear history of Revisions."
// Conduit projects a Product as one: a Product is exactly a linear history of
// versioned Releases, and its channel head is the floating HEAD.
type Configuration struct {
	APIVersion string              `json:"apiVersion"`
	Kind       string              `json:"kind"`
	Metadata   ObjectMeta          `json:"metadata"`
	Status     ConfigurationStatus `json:"status"`
}

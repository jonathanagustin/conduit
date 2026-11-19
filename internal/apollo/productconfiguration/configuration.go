package productconfiguration

import (
	"fmt"

	"go.yaml.in/yaml/v4"
)

// ReloadTypeInstall is the existing managed-file shorthand reload behavior.
const ReloadTypeInstall = "install"

// DefaultEndpointPath is the unstated endpoint path.
const DefaultEndpointPath = "/"

// Configuration is configuration.yml.
type Configuration struct {
	Resources   *ResourceRequirements `yaml:"resources,omitempty" json:"resources,omitempty"`
	Replication *Replication          `yaml:"replication,omitempty" json:"replication,omitempty"`
	// Volumes sizes the volumes the manifest declared under volumes-v2. A key
	// with no manifest declaration is rejected rather than ignored.
	Volumes   map[string]VolumeConfiguration `yaml:"volumes,omitempty" json:"volumes,omitempty"`
	Endpoints *Endpoints                     `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
	// Conf is the Product's own configuration tree. It is intentionally
	// untyped: its shape belongs to the Product, and the only parts of it
	// Platform understands are the substitution language inside its strings and
	// the reserved `tags` key that feeds discovery.
	Conf             map[string]any             `yaml:"conf,omitempty" json:"conf,omitempty"`
	ManagedFiles     map[string]ManagedFile     `yaml:"managed_files,omitempty" json:"managedFiles,omitempty"`
	GeneratedSecrets map[string]GeneratedSecret `yaml:"generated_secrets,omitempty" json:"generatedSecrets,omitempty"`
	Discovery        *Discovery                 `yaml:"discovery,omitempty" json:"discovery,omitempty"`
	Other            map[string]yaml.Node       `yaml:",inline" json:"-"`
}

// ResourceRequirements is the requests/limits pair. Kubernetes owns the
// quantity grammar, so the values stay strings and are validated, not parsed
// into a lossy numeric type.
type ResourceRequirements struct {
	Requests *ResourceQuantities `yaml:"requests,omitempty" json:"requests,omitempty"`
	Limits   *ResourceQuantities `yaml:"limits,omitempty" json:"limits,omitempty"`
}

// ResourceQuantities carries CPU and memory. Both are strings because the
// specification's own example writes `cpu: 8` and `memory: 10Gi` in the same
// block, and a numeric CPU must survive alongside a suffixed memory value.
type ResourceQuantities struct {
	CPU    string `yaml:"cpu,omitempty" json:"cpu,omitempty"`
	Memory string `yaml:"memory,omitempty" json:"memory,omitempty"`
}

// Replication carries the desired number of pods. Desired is a pointer
// because zero is the documented way to stop a workload, and that has to be
// distinguishable from "the document says nothing".
type Replication struct {
	Desired *int `yaml:"desired,omitempty" json:"desired,omitempty"`
}

// VolumeConfiguration sizes one declared volume. It is a union: exactly
// one underlying configuration may be present, and it must match the volume type
// the manifest declared.
//
// DesiredSize is modelled only so the validator can say what is wrong. The
// Product Specification's worked example writes `desired-size` directly under
// the volume name, which the normative Volumes definition does not allow;
// accepting it silently would let a Product publish a size the platform then
// ignores.
type VolumeConfiguration struct {
	DurableStorageConfiguration *DurableStorageConfiguration `yaml:"durable-storage-configuration,omitempty" json:"durableStorageConfiguration,omitempty"`
	DesiredSize                 string                       `yaml:"desired-size,omitempty" json:"-"`
}

type DurableStorageConfiguration struct {
	DesiredSize string `yaml:"desired-size" json:"desiredSize"`
}

// MaximumVolumeSize is the largest volume the platform will provision.
const MaximumVolumeSize = "10T"

// Endpoints names the ports a Product listens on. SLSStatusEndpoint names
// the definition that serves the status resource the platform polls, which is
// why it is a reference rather than a second port declaration.
type Endpoints struct {
	Definitions       map[string]EndpointDefinition `yaml:"definitions,omitempty" json:"definitions,omitempty"`
	SLSStatusEndpoint string                        `yaml:"sls-status-endpoint,omitempty" json:"slsStatusEndpoint,omitempty"`
}

// EndpointDefinition is one listening port. ExternalPort belongs here
// rather than on the manifest's TCP extension so that the port a client dials
// and the port the container binds are stated in one place.
type EndpointDefinition struct {
	DesiredPort  int    `yaml:"desired-port" json:"desiredPort"`
	Path         string `yaml:"path,omitempty" json:"path,omitempty"`
	ExternalPort int    `yaml:"external-port,omitempty" json:"externalPort,omitempty"`
}

// EffectivePath is the path the endpoint serves, defaulting to the root.
func (endpoint EndpointDefinition) EffectivePath() string {
	if endpoint.Path == "" {
		return DefaultEndpointPath
	}
	return endpoint.Path
}

// ManagedFile is one file the platform renders into the container. Type
// selects the encoder; ReloadType decides whether a change is applied by
// rewriting the file in place or only by reinstalling.
type ManagedFile struct {
	Type       string `yaml:"type" json:"type"`
	Content    any    `yaml:"content,omitempty" json:"content,omitempty"`
	ReloadType string `yaml:"reload-type,omitempty" json:"reloadType,omitempty"`
	// Shorthand records that the entry was written as `name.yml: some-config`.
	// It is kept because the shorthand fixes the reload type to install, and a
	// later reader cannot tell that from the expanded fields alone.
	Shorthand bool `yaml:"-" json:"shorthand,omitempty"`
}

var managedFileKeys = map[string]struct{}{
	"type": {}, "content": {}, "reload-type": {},
}

// UnmarshalYAML accepts both spellings of a managed file entry: the mapping
// form, and the shorthand where the value is the conf key and the file
// extension is the type. Strictness is applied by hand because decoding a node
// does not carry the decoder's KnownFields setting, and silently dropping a
// misspelled `reload_type` would leave a Product believing it reloads without
// a restart.
func (file *ManagedFile) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		var content string
		if err := node.Decode(&content); err != nil {
			return err
		}
		*file = ManagedFile{Content: content, ReloadType: ReloadTypeInstall, Shorthand: true}
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: managed file must be a mapping or a conf key", node.Line)
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		key := node.Content[index].Value
		if _, known := managedFileKeys[key]; !known {
			return fmt.Errorf("line %d: field %s not found in managed file", node.Content[index].Line, key)
		}
	}
	type rawManagedFile struct {
		Type       string `yaml:"type"`
		Content    any    `yaml:"content"`
		ReloadType string `yaml:"reload-type"`
	}
	var raw rawManagedFile
	decodeNode := node
	if len(node.Content)%2 != 0 {
		trimmed := *node
		trimmed.Content = node.Content[:len(node.Content)-1]
		decodeNode = &trimmed
	}
	if err := decodeNode.Decode(&raw); err != nil {
		return err
	}
	*file = ManagedFile{Type: raw.Type, Content: raw.Content, ReloadType: raw.ReloadType}
	return nil
}

// GeneratedSecret declares a secret the platform generates once per
// installation. Length carries the size for every type that has one — a string
// length, a byte count, or a key size — because that is how the specification
// spells all three.
type GeneratedSecret struct {
	SecretType    string `yaml:"secret-type" json:"secretType"`
	Length        int    `yaml:"length,omitempty" json:"length,omitempty"`
	AllowChars    string `yaml:"allow-chars,omitempty" json:"allowChars,omitempty"`
	DisallowChars string `yaml:"disallow-chars,omitempty" json:"disallowChars,omitempty"`
}

// Discovery is how a Product publishes itself to, and finds, its peers.
// Produces and Consumes are keyed by the name a substitution refers to, which
// is why they are maps rather than lists with a name field.
type Discovery struct {
	// ProduceToAdditionalStacks widens every produced entry, not one of them.
	// The specification is explicit that it has no per-entry form.
	ProduceToAdditionalStacks []string                     `yaml:"produce-to-additional-stacks,omitempty" json:"produceToAdditionalStacks,omitempty"`
	Produces                  map[string]DiscoveryProduces `yaml:"produces,omitempty" json:"produces,omitempty"`
	Consumes                  map[string]DiscoveryConsumes `yaml:"consumes,omitempty" json:"consumes,omitempty"`
}

// DiscoveryProduces is one entry a Product publishes. The payload is open
// because its contents are the Product's own contract with its consumers;
// Platform models only the fields that drive URI augmentation.
type DiscoveryProduces struct {
	Role   string `yaml:"role,omitempty" json:"role,omitempty"`
	Port   int    `yaml:"port,omitempty" json:"port,omitempty"`
	Path   string `yaml:"path,omitempty" json:"path,omitempty"`
	Scheme string `yaml:"scheme,omitempty" json:"scheme,omitempty"`
	// Payload keeps every other key of the entry so a round trip preserves what
	// the Product publishes.
	Payload map[string]yaml.Node `yaml:",inline" json:"-"`
}

// DiscoveryConsumes selects producers to resolve. Select picks one field
// out of the producer's payload, which is what makes a consumed reference
// usable directly as a client's uris list.
type DiscoveryConsumes struct {
	Role   string   `yaml:"role,omitempty" json:"role,omitempty"`
	Select string   `yaml:"select,omitempty" json:"select,omitempty"`
	Stack  string   `yaml:"stack,omitempty" json:"stack,omitempty"`
	Tags   []string `yaml:"tags,omitempty" json:"tags,omitempty"`
}

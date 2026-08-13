package controlplane

import "encoding/json"

// ManagedInstallationConfiguration models resolved installation inputs without
// embedding secret material. Shape values stay JSON because each Product owns
// its input-shape schema; the delivery plane owns resolution and provenance.
type ManagedInstallationConfiguration struct {
	CBACMarkings       []string                       `json:"cbacMarkings,omitempty"`
	InputMapping       map[string]json.RawMessage     `json:"inputMapping,omitempty"`
	PresetInputMapping map[string]json.RawMessage     `json:"presetInputMapping,omitempty"`
	SecretInputMapping map[string]SecretResolvedInput `json:"secretInputMapping,omitempty"`
}

type UniversalInstallationConfiguration struct {
	EnrollmentAllowlist []string                         `json:"enrollmentAllowlist,omitempty"`
	EnrollmentDenylist  []string                         `json:"enrollmentDenylist,omitempty"`
	ManagedConfig       ManagedInstallationConfiguration `json:"managedConfig"`
	NamespaceDenylist   []string                         `json:"namespaceDenylist,omitempty"`
}

type SecretResolvedInput struct {
	Type                       string                                 `json:"type"`
	EdgePipelineMagritteSource *SecretResolvedEdgePipelineSourceInput `json:"edgePipelineMagritteSource,omitempty"`
	Markings                   *SecretAndKeyName                      `json:"markings,omitempty"`
	NamedCredential            *SecretName                            `json:"namedCredential,omitempty"`
}

type SecretResolvedEdgePipelineSourceInput struct {
	Config     json.RawMessage `json:"config"`
	SecretName string          `json:"secretName"`
	TaskConfig json.RawMessage `json:"taskConfig,omitempty"`
}

type SecretAndKeyName struct {
	SecretName    string `json:"secretName"`
	SecretKeyName string `json:"secretKeyName"`
}

type SecretName struct {
	SecretName string `json:"secretName"`
}

const (
	IgnoreNamespaceExplicitlyDenied  = "NAMESPACE_EXPLICITLY_DENIED"
	IgnoreEnrollmentExplicitlyDenied = "ENROLLMENT_EXPLICITLY_DENIED"
	IgnoreEnrollmentNotAllowed       = "ENROLLMENT_NOT_ALLOWED"
	IgnoreNoOwningEnrollment         = "NO_OWNING_ENROLLMENT"
	IgnoreNamespaceTrashed           = "NAMESPACE_TRASHED"
	IgnoreInvalidOntologyCount       = "INVALID_ONTOLOGY_COUNT"
	IgnoreAlreadyOnTarget            = "ALREADY_ON_TARGET_VERSION_AND_CONFIG"
)

// Package plan owns the Plan: the resolved, ordered instruction a Hub hands to
// a Spoke, saying exactly what to install where and under what constraints.
//
// A Plan names its Environment by identifier rather than embedding one. That is
// not an accident of the model and it is worth stating, because the opposite
// reading has already been guessed at once: a Plan is a durable, persisted,
// wire-served record that outlives any particular view of the Environment it
// targets. Embedding the Environment would freeze a copy of its configuration
// into every Plan, so a Plan replayed after the Environment changed would
// describe a world that no longer exists — and two Plans for one Environment
// would each carry their own answer to what that Environment is. The identifier
// is the seam, and Environments resolves it.
//
// Delivery therefore depends on Catalog and Environments, and neither depends
// back. A Plan step carries the artifacts a Release ships and the registry
// access an Environment grants; it is the point where catalogue content and
// environment configuration are combined into one instruction. That direction
// is inherent to what a Plan is.
//
// Every type here is serialized. The JSON is a persistence and wire contract —
// Plans are stored and handed to Agents — so field names and tags are not free
// to change, and plan_test.go pins the shape.
package plan

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/catalog"
	"github.com/jonathanagustin/conduit/internal/environments"
)

// Secret mutation actions.
const (
	SecretCreate  = "CREATE"
	SecretReplace = "REPLACE"
	SecretVerify  = "VERIFY"
	SecretDelete  = "DELETE"
)

// Access is one resolved secret an installation may read: which requirement it
// satisfies, which assignment answered it, and at which rotation revision.
type Access struct {
	Requirement          string   `json:"requirement"`
	Assignment           string   `json:"assignment"`
	Revision             int      `json:"revision"`
	ProviderRef          string   `json:"providerRef"`
	KubernetesSecretName string   `json:"kubernetesSecretName"`
	Keys                 []string `json:"keys"`
}

// Mutation is an Environment-side operation over externally owned secret
// material. Current and Previous are both carried so a Spoke can tell a
// rotation from a first write without consulting anything else.
type Mutation struct {
	Position  int     `json:"position"`
	Action    string  `json:"action"`
	Namespace string  `json:"namespace"`
	Current   *Access `json:"current,omitempty"`
	Previous  *Access `json:"previous,omitempty"`
}

// Override suspends the ordinary maintenance schedule for a bounded period.
type Override struct {
	InstallationID string    `json:"installationId,omitempty"`
	Type           string    `json:"type"`
	StartsAt       time.Time `json:"startsAt"`
	EndsAt         time.Time `json:"endsAt"`
	Reason         string    `json:"reason"`
	ApprovedBy     string    `json:"approvedBy"`
}

// MaintenanceConstraint is every window bearing on one step at once. The
// windows are Catalog's type because a Release may declare one and an
// Environment may declare another, and a step has to carry both.
type MaintenanceConstraint struct {
	RequiresDowntime bool                             `json:"requiresDowntime,omitempty"`
	Downtime         *catalog.WeeklyMaintenanceWindow `json:"downtime,omitempty"`
	NoDowntime       *catalog.WeeklyMaintenanceWindow `json:"noDowntime,omitempty"`
	Product          *catalog.WeeklyMaintenanceWindow `json:"product,omitempty"`
	Overrides        []Override                       `json:"overrides,omitempty"`
}

// UpgradeRationale records why a target was chosen when it was chosen through
// the vulnerability-recall exception. Its presence is the evidence that the
// narrower selection path ran; the two scores are what it compared.
type UpgradeRationale struct {
	Reason           string  `json:"reason"`
	CurrentVersion   string  `json:"currentVersion"`
	TargetVersion    string  `json:"targetVersion"`
	CurrentRiskScore float64 `json:"currentRiskScore"`
	TargetRiskScore  float64 `json:"targetRiskScore"`
}

// IgnoredNamespace records a namespace the Plan deliberately did not touch, and
// why. Silence would be indistinguishable from having missed it.
type IgnoredNamespace struct {
	Entity       string `json:"entity"`
	NamespaceRID string `json:"namespaceRid"`
	Reason       string `json:"reason"`
}

// Step is one installation's worth of work.
type Step struct {
	Position       int                                   `json:"position"`
	Action         string                                `json:"action"`
	InstallationID string                                `json:"installationId"`
	Entity         string                                `json:"entity"`
	Product        string                                `json:"product"`
	Version        string                                `json:"version"`
	ReleaseDigest  string                                `json:"releaseDigest"`
	ReleaseChannel string                                `json:"releaseChannel,omitempty"`
	Namespace      string                                `json:"namespace"`
	NamespaceRID   string                                `json:"namespaceRid,omitempty"`
	ConfigDigest   string                                `json:"configDigest"`
	Values         json.RawMessage                       `json:"values"`
	Chart          catalog.Artifact                      `json:"chart"`
	Images         []catalog.Artifact                    `json:"images"`
	RegistryAccess []environments.RegistryAccess         `json:"registryAccess,omitempty"`
	SecretAccess   []Access                              `json:"secretAccess,omitempty"`
	HealthQuorum   string                                `json:"healthQuorum,omitempty"`
	RampedRollout  *environments.RampedRolloutConstraint `json:"rampedRollout,omitempty"`
	RecallRollOff  bool                                  `json:"recallRollOff,omitempty"`
	RecallIDs      []string                              `json:"recallIds,omitempty"`
	DependsOn      []string                              `json:"dependsOn,omitempty"`
	// UpgradeRationale is present only when the target Release was chosen through
	// the vulnerability-recall exception, so its presence records that the
	// narrower selection path ran and carries the two risk scores it compared.
	UpgradeRationale *UpgradeRationale                `json:"upgradeRationale,omitempty"`
	Maintenance      MaintenanceConstraint            `json:"maintenance,omitempty"`
	HelmExecution    environments.HelmExecutionPolicy `json:"helmExecution"`
	ResourceKind     string                           `json:"resourceKind,omitempty"`
	ResourceName     string                           `json:"resourceName,omitempty"`
	// HelmReleaseName is the resolved Helm release name. Read it through
	// HelmRelease so a Plan persisted before this field existed still resolves.
	HelmReleaseName string `json:"helmReleaseName,omitempty"`
}

// HelmRelease is the Helm release name the Spoke must manage for this step. It
// defaults to the Entity name, which is what every Plan authored before the
// override existed relies on.
func (step Step) HelmRelease() string {
	if name := strings.TrimSpace(step.HelmReleaseName); name != "" {
		return name
	}
	return step.Entity
}

// Plan is the whole instruction: which Environment, at which generation, and
// the ordered steps.
//
// Environment is the Environment's identifier, not the Environment. See the
// package comment for why.
type Plan struct {
	Environment              string             `json:"environment"`
	Generation               string             `json:"generation"`
	AutoSuppressionThreshold int                `json:"autoSuppressionThreshold"`
	SecretMutations          []Mutation         `json:"secretMutations,omitempty"`
	Steps                    []Step             `json:"steps"`
	IgnoredNamespaces        []IgnoredNamespace `json:"ignoredNamespaces,omitempty"`
}

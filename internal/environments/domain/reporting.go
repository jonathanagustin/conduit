package domain

import "time"

const (
	HealthHealthy       = "HEALTHY"
	HealthUnhealthy     = "UNHEALTHY"
	HealthIndeterminate = "INDETERMINATE"
	HealthUnknown       = "UNKNOWN"
	HealthRepairing     = "REPAIRING"
	HealthWarning       = "WARNING"
	HealthError         = "ERROR"
	HealthTerminal      = "TERMINAL"

	RolloutUpToDate      = "UP_TO_DATE"
	RolloutInGracePeriod = "IN_GRACE_PERIOD"
	RolloutBlocked       = "BLOCKED"
	RolloutRecalled      = "RECALLED"
	RolloutOutOfDate     = "OUT_OF_DATE"

	ConfigApplied       = "APPLIED"
	ConfigDrifted       = "DRIFTED"
	ConfigUnavailable   = "UNAVAILABLE"
	ConfigIndeterminate = "INDETERMINATE"

	LifecycleInstalled   = "INSTALLED"
	LifecycleUninstalled = "UNINSTALLED"
	LifecycleUnknown     = "UNKNOWN"
)

// HealthCheck is a normalized Environment observation. Foreign reporting
// protocols translate into this shape before persistence.
type HealthCheck struct {
	ID      string         `json:"id"`
	Message string         `json:"message"`
	Params  map[string]any `json:"params"`
	Result  string         `json:"result"`
}

type EntityContainerHealth struct {
	ContainerID string        `json:"containerId"`
	Liveness    bool          `json:"liveness"`
	Readiness   bool          `json:"readiness"`
	Result      string        `json:"result"`
	Checks      []HealthCheck `json:"checks,omitempty"`
}

type EntityNodeHealth struct {
	NodeID         string                  `json:"nodeId"`
	ProductVersion string                  `json:"productVersion"`
	Result         string                  `json:"result"`
	Containers     []EntityContainerHealth `json:"containers"`
}

type EntityHealthDetails struct {
	Quorum string             `json:"quorum"`
	Nodes  []EntityNodeHealth `json:"nodes"`
}

type ReportedEntity struct {
	InstallationID string               `json:"installationId"`
	Product        string               `json:"product"`
	Version        string               `json:"version"`
	ReleaseDigest  string               `json:"releaseDigest"`
	Namespace      string               `json:"namespace"`
	ConfigDigest   string               `json:"configDigest"`
	Health         string               `json:"health"`
	RolloutStatus  string               `json:"rolloutStatus"`
	ConfigStatus   string               `json:"configStatus"`
	Lifecycle      string               `json:"lifecycle"`
	ObservedAt     time.Time            `json:"observedAt"`
	HealthDetails  *EntityHealthDetails `json:"healthDetails,omitempty"`
}

type ReportedSecret struct {
	Assignment           string   `json:"assignment"`
	Namespace            string   `json:"namespace"`
	Revision             int      `json:"revision"`
	ProviderRef          string   `json:"providerRef"`
	KubernetesSecretName string   `json:"kubernetesSecretName"`
	Keys                 []string `json:"keys"`
}

type Report struct {
	Environment string           `json:"environment"`
	AgentID     string           `json:"agentId"`
	PlanID      string           `json:"planId,omitempty"`
	ObservedAt  time.Time        `json:"observedAt"`
	Entities    []ReportedEntity `json:"entities"`
	Secrets     []ReportedSecret `json:"secrets,omitempty"`
	Revision    uint64           `json:"revision"`
}

// PlanStepProjection is the minimum reviewed desired-state information a
// reporting adapter needs. It intentionally excludes approvals and execution.
type PlanStepProjection struct {
	InstallationID string
	Entity         string
	Product        string
	Version        string
	Namespace      string
}

type PlanProjection struct {
	ID    string
	Steps []PlanStepProjection
}

// CompatibilityProjection is the reviewed subset exposed to a foreign API.
// It contains no transport DTOs and no executable authority.
//
// Recalls and promotions are deliberately NOT carried here, though they were
// once. This projection is pushed into a compatibility surface's memory by the
// process that accepted a desired state, and a Hub runs more than one replica:
// the replica that never handled that POST -- and every replica after a restart
// -- would have answered "not recalled" for a Release Conduit had withdrawn.
// Recalls and promotions are read from durable desired state instead, through
// the compatibility surface's own lifecycle port.
type CompatibilityProjection struct {
	ID         string
	Entities   []ProjectedEntity
	Namespaces []ProjectedNamespace
}

// ProjectedPromotion is one passed channel boundary, narrowed to what decides
// which Channels a Release has reached. The narrowing matches catalogPromotions
// in the control plane, which reads exactly these fields for the same purpose.
type ProjectedPromotion struct {
	Product     string
	Version     string
	FromChannel string
	ToChannel   string
	Passed      bool
}

// ProjectedRecall is a withdrawal, narrowed to what a compatibility surface may
// state about it. The version bounds rather than a resolved list, because which
// versions a recall covers is the recall domain's rule and restating it here
// would be a second copy of it.
type ProjectedRecall struct {
	Product                string
	StartingVersion        string
	EndingVersionExclusive string
	ExcludedVersions       []string
	SingleVersion          bool
	Reason                 string
	Reverted               bool
}

type ProjectedEntity struct {
	Name           string
	Product        string
	Version        string
	ReleaseChannel string
}

type ProjectedNamespace struct {
	RID  string
	Name string
}

// WorseHealth returns the more severe normalized health result.
func WorseHealth(left, right string) string {
	severity := map[string]int{
		HealthHealthy: 0, HealthRepairing: 1, HealthWarning: 2, HealthError: 3, HealthTerminal: 4,
	}
	if severity[right] > severity[left] {
		return right
	}
	return left
}

package agent

import (
	"context"
	"time"
)

// NetworkPolicyBackend is the provider-neutral execution port for a
// Plan-authorized, namespaced network policy. A backend is an execution
// adapter; it is never a desired-state or deletion authority.
type NetworkPolicyBackend interface {
	Apply(context.Context, NetworkPolicyIntent) (NetworkPolicyReceipt, error)
	Observe(context.Context, NetworkPolicyReceipt) (NetworkPolicyObservation, error)
}

// NetworkPolicyIntent contains only immutable identity and policy data. Secret
// material and lease tokens are deliberately not representable.
type NetworkPolicyIntent struct {
	Environment string              `json:"environment"`
	EntityID    string              `json:"entityId"`
	Namespace   string              `json:"namespace"`
	PlanID      string              `json:"planId"`
	Attempt     uint64              `json:"attempt"`
	Egress      []NetworkEgressRule `json:"egress"`
}

// NetworkEgressRule expresses a DNS-identity destination and the L4/L7 traffic
// allowed to it. FQDN is an exact DNS name; FQDNPattern supports only Cilium's
// leading wildcard form (for example, *.example.com).
type NetworkEgressRule struct {
	FQDN        string            `json:"fqdn,omitempty"`
	FQDNPattern string            `json:"fqdnPattern,omitempty"`
	Ports       []NetworkPortRule `json:"ports"`
}

type NetworkPortRule struct {
	Port     uint16            `json:"port"`
	Protocol string            `json:"protocol"`
	HTTP     []NetworkHTTPRule `json:"http,omitempty"`
}

type NetworkHTTPRule struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Host   string `json:"host,omitempty"`
}

type NetworkPolicyReceipt struct {
	Backend      string `json:"backend"`
	Namespace    string `json:"namespace"`
	Name         string `json:"name"`
	Environment  string `json:"environment"`
	EntityID     string `json:"entityId"`
	PlanID       string `json:"planId"`
	Attempt      uint64 `json:"attempt"`
	PolicyDigest string `json:"policyDigest"`
}

type EvidenceState string

const (
	EvidenceHealthy EvidenceState = "HEALTHY"
	EvidenceError   EvidenceState = "ERROR"
	EvidenceUnknown EvidenceState = "UNKNOWN"
)

// NetworkPolicyObservation intentionally reports enforcement and telemetry as
// independent axes. A healthy policy does not prove Hubble coverage, and an
// unavailable Hubble stream does not prove that no traffic was denied.
type NetworkPolicyObservation struct {
	Enforcement       EvidenceState `json:"enforcement"`
	Telemetry         EvidenceState `json:"telemetry"`
	PolicyRevision    uint64        `json:"policyRevision,omitempty"`
	RealizedEndpoints int           `json:"realizedEndpoints"`
	DeniedFlowCount   int           `json:"deniedFlowCount"`
	DeniedFlows       []DeniedFlow  `json:"deniedFlows,omitempty"`
	FlowsIncomplete   bool          `json:"flowsIncomplete,omitempty"`
	Message           string        `json:"message,omitempty"`
}

// CiliumEndpointRealization is a scoped observation of one selected endpoint.
// The adapter requires every selected endpoint to be ready, enforcing egress,
// and at a positive policy revision before reporting enforcement healthy.
type CiliumEndpointRealization struct {
	Name                  string `json:"name"`
	Ready                 bool   `json:"ready"`
	EgressPolicyEnforcing bool   `json:"egressPolicyEnforcing"`
	PolicyRevision        uint64 `json:"policyRevision"`
}

// DeniedFlow is bounded, credential-free Hubble evidence. It intentionally
// omits IP addresses, Kubernetes labels, headers, bodies, and user identity.
type DeniedFlow struct {
	ObservedAt time.Time `json:"observedAt"`
	Protocol   string    `json:"protocol"`
	DNSQuery   string    `json:"dnsQuery,omitempty"`
	HTTPMethod string    `json:"httpMethod,omitempty"`
	HTTPPath   string    `json:"httpPath,omitempty"`
	Reason     string    `json:"reason,omitempty"`
}

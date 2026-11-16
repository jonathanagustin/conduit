package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jonathanagustin/conduit/internal/apollo/workloadmetadata"
)

const (
	ciliumBackendName = "cilium-v2"
	ciliumAPIVersion  = "cilium.io/v2"
	ciliumKind        = "CiliumNetworkPolicy"

	planIDAnnotation       = "platform.palantir.dev/plan.id"
	planAttemptAnnotation  = "platform.palantir.dev/plan.attempt"
	policyDigestAnnotation = "platform.palantir.dev/network-policy.digest"
	maxDeniedFlowEvidence  = 20

	// Kubernetes label values cannot contain the colons in an Apollo Entity ID.
	// These identity labels therefore carry a collision-resistant digest of the
	// exact public Apollo metadata value. The full values remain annotations.
	CiliumEnvironmentIdentityLabel = "platform.palantir.dev/apollo-environment"
	CiliumEntityIdentityLabel      = "platform.palantir.dev/apollo-entity"
)

var (
	dnsLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	httpMethod      = regexp.MustCompile(`^[A-Z]+$`)
)

// CiliumPolicyStore is the namespaced storage boundary. Implementations must
// reject CiliumClusterwideNetworkPolicy and must not use cluster-admin
// credentials. Discovery is separate so a missing CRD fails before mutation.
type CiliumPolicyStore interface {
	HasNamespacedPolicyCRD(context.Context) (bool, error)
	ApplyNamespaced(context.Context, string, []byte) error
	GetNamespaced(context.Context, string, string) ([]byte, error)
}

// HubbleFlowObserver queries an already-scoped Hubble endpoint. Query
// implementations must match both identity labels; the backend checks the
// immutable scope before issuing the query.
type HubbleFlowObserver interface {
	DeniedFlows(context.Context, HubbleFlowQuery) ([]DeniedFlow, error)
}

// CiliumRealizationObserver is deliberately separate from the CNP store. The
// current CNP CRD exposes policy validity, not endpoint enforcement. A
// production implementation must make a namespace- and selector-scoped query
// over CiliumEndpoint state and return evidence bound to this exact policy.
type CiliumRealizationObserver interface {
	ObserveRealization(context.Context, CiliumRealizationQuery) (CiliumRealizationEvidence, error)
}

type CiliumRealizationQuery struct {
	Namespace    string `json:"namespace"`
	PolicyName   string `json:"policyName"`
	PolicyUID    string `json:"policyUid"`
	Generation   int64  `json:"generation"`
	PolicyDigest string `json:"policyDigest"`
	Environment  string `json:"environment"`
	EntityID     string `json:"entityId"`
}

type CiliumRealizationEvidence struct {
	PolicyUID    string                      `json:"policyUid"`
	Generation   int64                       `json:"generation"`
	PolicyDigest string                      `json:"policyDigest"`
	Endpoints    []CiliumEndpointRealization `json:"endpoints"`
}

type HubbleFlowQuery struct {
	Namespace   string `json:"namespace"`
	Environment string `json:"environment"`
	EntityID    string `json:"entityId"`
	Verdict     string `json:"verdict"`
}

// CiliumBackend owns only policy objects in one Environment namespace.
type CiliumBackend struct {
	Environment string
	Namespace   string
	Store       CiliumPolicyStore
	Realizer    CiliumRealizationObserver
	Hubble      HubbleFlowObserver
}

func (backend CiliumBackend) Apply(ctx context.Context, intent NetworkPolicyIntent) (NetworkPolicyReceipt, error) {
	if err := backend.validateScope(intent); err != nil {
		return NetworkPolicyReceipt{}, err
	}
	if backend.Store == nil {
		return NetworkPolicyReceipt{}, fmt.Errorf("Cilium namespaced policy store is not configured")
	}
	present, err := backend.Store.HasNamespacedPolicyCRD(ctx)
	if err != nil {
		return NetworkPolicyReceipt{}, fmt.Errorf("discover namespaced CiliumNetworkPolicy CRD: %w", err)
	}
	if !present {
		return NetworkPolicyReceipt{}, fmt.Errorf("namespaced CiliumNetworkPolicy CRD is unavailable")
	}
	digest, err := networkPolicyIntentDigest(intent)
	if err != nil {
		return NetworkPolicyReceipt{}, err
	}
	name := "platform-" + strings.TrimPrefix(digest, "sha256:")[:24]
	manifest, err := renderCiliumPolicy(intent, name, digest)
	if err != nil {
		return NetworkPolicyReceipt{}, err
	}
	if err := backend.Store.ApplyNamespaced(ctx, intent.Namespace, manifest); err != nil {
		return NetworkPolicyReceipt{}, fmt.Errorf("apply namespaced CiliumNetworkPolicy: %w", err)
	}
	return NetworkPolicyReceipt{
		Backend: ciliumBackendName, Namespace: intent.Namespace, Name: name,
		Environment: intent.Environment, EntityID: intent.EntityID,
		PlanID: intent.PlanID, Attempt: intent.Attempt, PolicyDigest: digest,
	}, nil
}

func (backend CiliumBackend) Observe(ctx context.Context, receipt NetworkPolicyReceipt) (NetworkPolicyObservation, error) {
	if err := backend.validateReceipt(receipt); err != nil {
		return NetworkPolicyObservation{}, err
	}
	if backend.Store == nil {
		return NetworkPolicyObservation{}, fmt.Errorf("Cilium namespaced policy store is not configured")
	}
	present, err := backend.Store.HasNamespacedPolicyCRD(ctx)
	if err != nil {
		return NetworkPolicyObservation{}, fmt.Errorf("discover namespaced CiliumNetworkPolicy CRD: %w", err)
	}
	if !present {
		return NetworkPolicyObservation{}, fmt.Errorf("namespaced CiliumNetworkPolicy CRD is unavailable")
	}
	raw, err := backend.Store.GetNamespaced(ctx, receipt.Namespace, receipt.Name)
	if err != nil {
		return NetworkPolicyObservation{}, fmt.Errorf("read namespaced CiliumNetworkPolicy: %w", err)
	}
	metadata, err := observeCiliumAdmission(raw, receipt)
	if err != nil {
		return NetworkPolicyObservation{}, err
	}
	observation := NetworkPolicyObservation{Enforcement: EvidenceUnknown, Telemetry: EvidenceUnknown}
	if backend.Realizer == nil {
		observation.Message = appendObservationMessage(observation.Message, "Cilium endpoint realization is not configured")
	} else {
		evidence, realizationErr := backend.Realizer.ObserveRealization(ctx, CiliumRealizationQuery{
			Namespace: receipt.Namespace, PolicyName: receipt.Name, PolicyUID: metadata.UID,
			Generation: metadata.Generation, PolicyDigest: receipt.PolicyDigest,
			Environment: receipt.Environment, EntityID: receipt.EntityID,
		})
		if realizationErr != nil {
			observation.Message = appendObservationMessage(observation.Message, "Cilium endpoint realization is unavailable")
		} else if err := applyCiliumRealization(&observation, evidence, metadata, receipt); err != nil {
			return NetworkPolicyObservation{}, err
		}
	}
	if backend.Hubble == nil {
		observation.Telemetry = EvidenceUnknown
		observation.Message = appendObservationMessage(observation.Message, "Hubble telemetry is not configured")
		return observation, nil
	}
	flows, err := backend.Hubble.DeniedFlows(ctx, HubbleFlowQuery{
		Namespace: receipt.Namespace, Environment: receipt.Environment,
		EntityID: receipt.EntityID, Verdict: "DROPPED",
	})
	if err != nil {
		observation.Telemetry = EvidenceUnknown
		observation.Message = appendObservationMessage(observation.Message, "Hubble telemetry is unavailable")
		return observation, nil
	}
	observation.Telemetry = EvidenceHealthy
	observation.DeniedFlowCount = len(flows)
	if len(flows) > maxDeniedFlowEvidence {
		observation.FlowsIncomplete = true
		flows = flows[:maxDeniedFlowEvidence]
	}
	observation.DeniedFlows = sanitizeDeniedFlows(flows)
	return observation, nil
}

func (backend CiliumBackend) validateScope(intent NetworkPolicyIntent) error {
	if strings.TrimSpace(backend.Environment) == "" || strings.TrimSpace(backend.Namespace) == "" {
		return fmt.Errorf("Cilium backend requires an Environment and namespace scope")
	}
	if intent.Environment != backend.Environment {
		return fmt.Errorf("network policy Environment %q is outside backend Environment %q", intent.Environment, backend.Environment)
	}
	if intent.Namespace != backend.Namespace {
		return fmt.Errorf("network policy namespace %q is outside backend namespace %q", intent.Namespace, backend.Namespace)
	}
	if strings.TrimSpace(intent.EntityID) == "" {
		return fmt.Errorf("network policy requires an Apollo Entity ID")
	}
	if len(intent.EntityID) > workloadmetadata.ApolloEntityIDMaximumLength || !strings.HasPrefix(intent.EntityID, "aeid:"+intent.Environment+":") {
		return fmt.Errorf("network policy Apollo Entity ID is not bound to Environment %q", intent.Environment)
	}
	if strings.TrimSpace(intent.PlanID) == "" || intent.Attempt == 0 {
		return fmt.Errorf("network policy requires a Plan ID and positive lease attempt")
	}
	if len(intent.Egress) == 0 {
		return fmt.Errorf("network policy requires at least one egress rule")
	}
	for index, rule := range intent.Egress {
		if err := validateNetworkEgressRule(rule); err != nil {
			return fmt.Errorf("egress rule %d: %w", index, err)
		}
	}
	return nil
}

func (backend CiliumBackend) validateReceipt(receipt NetworkPolicyReceipt) error {
	if receipt.Backend != ciliumBackendName {
		return fmt.Errorf("network policy receipt backend %q is not %q", receipt.Backend, ciliumBackendName)
	}
	if receipt.Environment != backend.Environment || receipt.Namespace != backend.Namespace {
		return fmt.Errorf("network policy receipt is outside backend scope")
	}
	if receipt.Name == "" || receipt.EntityID == "" || receipt.PlanID == "" || receipt.Attempt == 0 || !strings.HasPrefix(receipt.PolicyDigest, "sha256:") {
		return fmt.Errorf("network policy receipt is incomplete")
	}
	return nil
}

func validateNetworkEgressRule(rule NetworkEgressRule) error {
	if (rule.FQDN == "") == (rule.FQDNPattern == "") {
		return fmt.Errorf("exactly one of fqdn and fqdnPattern is required")
	}
	if rule.FQDN != "" && !validDNSName(rule.FQDN) {
		return fmt.Errorf("fqdn %q is not a normalized DNS name", rule.FQDN)
	}
	if rule.FQDNPattern != "" {
		if !strings.HasPrefix(rule.FQDNPattern, "*.") || !validDNSName(strings.TrimPrefix(rule.FQDNPattern, "*.")) {
			return fmt.Errorf("fqdnPattern %q must be a leading wildcard over a normalized DNS name", rule.FQDNPattern)
		}
	}
	if len(rule.Ports) == 0 {
		return fmt.Errorf("at least one port is required")
	}
	for _, port := range rule.Ports {
		protocol := strings.ToUpper(port.Protocol)
		if port.Port == 0 || (protocol != "TCP" && protocol != "UDP") {
			return fmt.Errorf("port requires a positive number and TCP or UDP protocol")
		}
		if len(port.HTTP) > 0 && protocol != "TCP" {
			return fmt.Errorf("HTTP L7 rules require TCP")
		}
		for _, http := range port.HTTP {
			if !httpMethod.MatchString(http.Method) || !strings.HasPrefix(http.Path, "/") || strings.ContainsAny(http.Path, "\r\n") {
				return fmt.Errorf("HTTP rule requires an uppercase method and absolute single-line path")
			}
			if http.Host != "" && !validDNSName(http.Host) {
				return fmt.Errorf("HTTP host %q is not a normalized DNS name", http.Host)
			}
		}
	}
	return nil
}

func validDNSName(name string) bool {
	if name == "" || len(name) > 253 || name != strings.ToLower(name) || strings.HasSuffix(name, ".") {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if !dnsLabelPattern.MatchString(label) {
			return false
		}
	}
	return true
}

func networkPolicyIntentDigest(intent NetworkPolicyIntent) (string, error) {
	raw, err := json.Marshal(intent)
	if err != nil {
		return "", fmt.Errorf("encode network policy intent: %w", err)
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

type ciliumPolicy struct {
	APIVersion string               `json:"apiVersion"`
	Kind       string               `json:"kind"`
	Metadata   ciliumPolicyMetadata `json:"metadata"`
	Spec       ciliumPolicySpec     `json:"spec"`
}

type ciliumPolicyMetadata struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Annotations map[string]string `json:"annotations"`
}

type ciliumPolicySpec struct {
	EndpointSelector ciliumEndpointSelector `json:"endpointSelector"`
	Egress           []ciliumEgressRule     `json:"egress"`
}

type ciliumEndpointSelector struct {
	MatchLabels map[string]string `json:"matchLabels"`
}

type ciliumEgressRule struct {
	ToFQDNs []ciliumFQDNSelector `json:"toFQDNs"`
	ToPorts []ciliumPortPolicy   `json:"toPorts"`
}

type ciliumFQDNSelector struct {
	MatchName    string `json:"matchName,omitempty"`
	MatchPattern string `json:"matchPattern,omitempty"`
}

type ciliumPortPolicy struct {
	Ports []ciliumPort `json:"ports"`
	Rules *ciliumRules `json:"rules,omitempty"`
}

type ciliumPort struct {
	Port     string `json:"port"`
	Protocol string `json:"protocol"`
}

type ciliumRules struct {
	HTTP []NetworkHTTPRule `json:"http"`
}

func renderCiliumPolicy(intent NetworkPolicyIntent, name, digest string) ([]byte, error) {
	egress := make([]ciliumEgressRule, 0, len(intent.Egress))
	for _, rule := range intent.Egress {
		fqdn := ciliumFQDNSelector{MatchName: rule.FQDN, MatchPattern: rule.FQDNPattern}
		ports := make([]ciliumPortPolicy, 0, len(rule.Ports))
		for _, port := range rule.Ports {
			entry := ciliumPortPolicy{Ports: []ciliumPort{{Port: strconv.Itoa(int(port.Port)), Protocol: strings.ToUpper(port.Protocol)}}}
			if len(port.HTTP) > 0 {
				entry.Rules = &ciliumRules{HTTP: port.HTTP}
			}
			ports = append(ports, entry)
		}
		egress = append(egress, ciliumEgressRule{ToFQDNs: []ciliumFQDNSelector{fqdn}, ToPorts: ports})
	}
	policy := ciliumPolicy{
		APIVersion: ciliumAPIVersion, Kind: ciliumKind,
		Metadata: ciliumPolicyMetadata{Name: name, Namespace: intent.Namespace, Annotations: map[string]string{
			workloadmetadata.ApolloEnvironmentIDAnnotation: intent.Environment,
			workloadmetadata.ApolloEntityIDAnnotation:      intent.EntityID,
			planIDAnnotation: intent.PlanID, planAttemptAnnotation: strconv.FormatUint(intent.Attempt, 10),
			policyDigestAnnotation: digest,
		}},
		Spec: ciliumPolicySpec{EndpointSelector: ciliumEndpointSelector{MatchLabels: CiliumWorkloadIdentityLabels(intent.Environment, intent.EntityID)}, Egress: egress},
	}
	return json.Marshal(policy)
}

// CiliumWorkloadIdentityLabels returns Kubernetes-safe selector labels bound
// to the exact Apollo Environment and Entity metadata. The workload
// post-renderer must stamp this same pair before a Cilium policy can be used.
func CiliumWorkloadIdentityLabels(environment, entityID string) map[string]string {
	return map[string]string{
		CiliumEnvironmentIdentityLabel: identityLabelValue(environment),
		CiliumEntityIdentityLabel:      identityLabelValue(entityID),
	}
}

func identityLabelValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	// Kubernetes label values are capped at 63 characters. "sha256-" plus 56
	// hex characters retains 224 bits while remaining valid.
	return "sha256-" + hex.EncodeToString(sum[:])[:56]
}

type ciliumObservedPolicy struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name        string            `json:"name"`
		Namespace   string            `json:"namespace"`
		UID         string            `json:"uid"`
		Generation  int64             `json:"generation"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Status struct {
		Conditions []ciliumPolicyCondition `json:"conditions"`
	} `json:"status"`
}

type ciliumPolicyCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

type ciliumPolicyMetadataObservation struct {
	UID        string
	Generation int64
}

func observeCiliumAdmission(raw []byte, receipt NetworkPolicyReceipt) (ciliumPolicyMetadataObservation, error) {
	var policy ciliumObservedPolicy
	if err := json.Unmarshal(raw, &policy); err != nil {
		return ciliumPolicyMetadataObservation{}, fmt.Errorf("decode CiliumNetworkPolicy status: %w", err)
	}
	if policy.APIVersion != ciliumAPIVersion || policy.Kind != ciliumKind || policy.Metadata.Name != receipt.Name || policy.Metadata.Namespace != receipt.Namespace {
		return ciliumPolicyMetadataObservation{}, fmt.Errorf("observed CiliumNetworkPolicy identity does not match receipt")
	}
	expected := receiptAnnotations(receipt)
	if !containsAnnotations(policy.Metadata.Annotations, expected) {
		return ciliumPolicyMetadataObservation{}, fmt.Errorf("observed CiliumNetworkPolicy annotations do not match receipt")
	}
	if policy.Metadata.UID == "" || policy.Metadata.Generation <= 0 {
		return ciliumPolicyMetadataObservation{}, fmt.Errorf("CiliumNetworkPolicy is missing UID or generation")
	}
	valid := false
	for _, condition := range policy.Status.Conditions {
		if condition.Type != "Valid" {
			continue
		}
		if condition.Status != "True" {
			return ciliumPolicyMetadataObservation{}, fmt.Errorf("CiliumNetworkPolicy Valid condition is %q", condition.Status)
		}
		valid = true
	}
	if !valid {
		return ciliumPolicyMetadataObservation{}, fmt.Errorf("CiliumNetworkPolicy has no Valid condition")
	}
	return ciliumPolicyMetadataObservation{UID: policy.Metadata.UID, Generation: policy.Metadata.Generation}, nil
}

func applyCiliumRealization(observation *NetworkPolicyObservation, evidence CiliumRealizationEvidence, metadata ciliumPolicyMetadataObservation, receipt NetworkPolicyReceipt) error {
	if evidence.PolicyUID != metadata.UID || evidence.Generation != metadata.Generation || evidence.PolicyDigest != receipt.PolicyDigest {
		return fmt.Errorf("Cilium endpoint realization does not match policy UID, generation, and digest")
	}
	if len(evidence.Endpoints) == 0 {
		return fmt.Errorf("Cilium endpoint realization selected no endpoints")
	}
	observation.Enforcement = EvidenceHealthy
	observation.RealizedEndpoints = len(evidence.Endpoints)
	for _, endpoint := range evidence.Endpoints {
		if endpoint.Name == "" || !endpoint.Ready || !endpoint.EgressPolicyEnforcing || endpoint.PolicyRevision == 0 {
			return fmt.Errorf("Cilium endpoint %q is not ready with egress policy enforcement", endpoint.Name)
		}
		if observation.PolicyRevision == 0 || endpoint.PolicyRevision < observation.PolicyRevision {
			observation.PolicyRevision = endpoint.PolicyRevision
		}
	}
	return nil
}

func receiptAnnotations(receipt NetworkPolicyReceipt) map[string]string {
	return map[string]string{
		workloadmetadata.ApolloEnvironmentIDAnnotation: receipt.Environment,
		workloadmetadata.ApolloEntityIDAnnotation:      receipt.EntityID,
		planIDAnnotation: receipt.PlanID, planAttemptAnnotation: strconv.FormatUint(receipt.Attempt, 10),
		policyDigestAnnotation: receipt.PolicyDigest,
	}
}

func containsAnnotations(got, expected map[string]string) bool {
	for key, value := range expected {
		if got[key] != value {
			return false
		}
	}
	return true
}

func sanitizeDeniedFlows(flows []DeniedFlow) []DeniedFlow {
	result := make([]DeniedFlow, len(flows))
	for index, flow := range flows {
		result[index] = DeniedFlow{
			ObservedAt: flow.ObservedAt,
			Protocol:   boundedText(flow.Protocol, 32), DNSQuery: boundedText(flow.DNSQuery, 253),
			HTTPMethod: boundedText(flow.HTTPMethod, 16), HTTPPath: boundedText(flow.HTTPPath, 256),
			Reason: boundedText(flow.Reason, 256),
		}
	}
	return result
}

func boundedText(value string, limit int) string {
	value = strings.Map(func(character rune) rune {
		if character == '\r' || character == '\n' || character == '\x00' {
			return -1
		}
		return character
	}, value)
	if len(value) > limit {
		return value[:limit]
	}
	return value
}

func appendObservationMessage(existing, message string) string {
	if existing == "" {
		return message
	}
	return existing + "; " + message
}

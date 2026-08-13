package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

type fakeCiliumStore struct {
	crd       bool
	discovery error
	appliedNS string
	manifest  []byte
	observed  []byte
	applyCall int
}

func (store *fakeCiliumStore) HasNamespacedPolicyCRD(context.Context) (bool, error) {
	return store.crd, store.discovery
}

func (store *fakeCiliumStore) ApplyNamespaced(_ context.Context, namespace string, manifest []byte) error {
	store.applyCall++
	store.appliedNS = namespace
	store.manifest = append([]byte(nil), manifest...)
	return nil
}

func (store *fakeCiliumStore) GetNamespaced(_ context.Context, namespace, _ string) ([]byte, error) {
	if namespace != store.appliedNS && store.appliedNS != "" {
		return nil, fmt.Errorf("unexpected namespace %s", namespace)
	}
	return append([]byte(nil), store.observed...), nil
}

type fakeHubble struct {
	query HubbleFlowQuery
	flows []DeniedFlow
	err   error
}

type fakeCiliumRealizer struct {
	query    CiliumRealizationQuery
	evidence CiliumRealizationEvidence
	err      error
}

func (realizer *fakeCiliumRealizer) ObserveRealization(_ context.Context, query CiliumRealizationQuery) (CiliumRealizationEvidence, error) {
	realizer.query = query
	return realizer.evidence, realizer.err
}

func (hubble *fakeHubble) DeniedFlows(_ context.Context, query HubbleFlowQuery) ([]DeniedFlow, error) {
	hubble.query = query
	return append([]DeniedFlow(nil), hubble.flows...), hubble.err
}

func testNetworkPolicyIntent() NetworkPolicyIntent {
	return NetworkPolicyIntent{
		Environment: "dev-use1", EntityID: "aeid:dev-use1:helm-chart:planeconnection",
		Namespace: "planeconnection", PlanID: "pln_0123456789abcdef", Attempt: 3,
		Egress: []NetworkEgressRule{{
			FQDN:  "api.example.com",
			Ports: []NetworkPortRule{{Port: 443, Protocol: "TCP", HTTP: []NetworkHTTPRule{{Method: "GET", Path: "/health", Host: "api.example.com"}}}},
		}},
	}
}

func TestCiliumApplyFailsClosedWithoutCRD(t *testing.T) {
	store := &fakeCiliumStore{}
	backend := CiliumBackend{Environment: "dev-use1", Namespace: "planeconnection", Store: store}
	if _, err := backend.Apply(context.Background(), testNetworkPolicyIntent()); err == nil || !strings.Contains(err.Error(), "CRD is unavailable") {
		t.Fatalf("Apply error = %v, want missing CRD", err)
	}
	if store.applyCall != 0 {
		t.Fatalf("policy was mutated despite missing CRD: %d calls", store.applyCall)
	}
}

func TestCiliumApplyRejectsCrossNamespaceBeforeDiscovery(t *testing.T) {
	store := &fakeCiliumStore{crd: true, discovery: errors.New("must not be called")}
	backend := CiliumBackend{Environment: "dev-use1", Namespace: "planeconnection", Store: store}
	intent := testNetworkPolicyIntent()
	intent.Namespace = "foundry"
	if _, err := backend.Apply(context.Background(), intent); err == nil || !strings.Contains(err.Error(), "outside backend namespace") {
		t.Fatalf("Apply error = %v, want namespace rejection", err)
	}
	if store.applyCall != 0 {
		t.Fatalf("cross-namespace policy was applied: %d calls", store.applyCall)
	}
}

func TestCiliumApplyRendersNamespacedIdentityAndAttemptBoundPolicy(t *testing.T) {
	store := &fakeCiliumStore{crd: true}
	backend := CiliumBackend{Environment: "dev-use1", Namespace: "planeconnection", Store: store}
	receipt, err := backend.Apply(context.Background(), testNetworkPolicyIntent())
	if err != nil {
		t.Fatal(err)
	}
	if store.appliedNS != "planeconnection" || strings.Contains(string(store.manifest), "CiliumClusterwideNetworkPolicy") {
		t.Fatalf("policy escaped namespaced boundary: ns=%q manifest=%s", store.appliedNS, store.manifest)
	}
	var policy ciliumPolicy
	if err := json.Unmarshal(store.manifest, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.Kind != ciliumKind || policy.Metadata.Namespace != "planeconnection" {
		t.Fatalf("wrong policy identity: %#v", policy)
	}
	labels := policy.Spec.EndpointSelector.MatchLabels
	wantLabels := CiliumWorkloadIdentityLabels(receipt.Environment, receipt.EntityID)
	if labels[CiliumEnvironmentIdentityLabel] != wantLabels[CiliumEnvironmentIdentityLabel] || labels[CiliumEntityIdentityLabel] != wantLabels[CiliumEntityIdentityLabel] {
		t.Fatalf("selector labels = %#v, want Apollo-bound labels %#v", labels, wantLabels)
	}
	annotations := policy.Metadata.Annotations
	if annotations[controlplane.ApolloEnvironmentIDAnnotation] != receipt.Environment || annotations[controlplane.ApolloEntityIDAnnotation] != receipt.EntityID {
		t.Fatalf("exact Apollo annotations = %#v", annotations)
	}
	if annotations[planIDAnnotation] != receipt.PlanID || annotations[planAttemptAnnotation] != "3" || annotations[policyDigestAnnotation] != receipt.PolicyDigest {
		t.Fatalf("attempt binding annotations = %#v", annotations)
	}
	if strings.Contains(string(store.manifest), "token") || strings.Contains(string(store.manifest), "secret") {
		t.Fatalf("policy manifest contains material-looking field: %s", store.manifest)
	}
	if got := policy.Spec.Egress[0].ToPorts[0].Rules.HTTP[0]; got.Method != "GET" || got.Path != "/health" {
		t.Fatalf("HTTP rule = %#v", got)
	}
}

func TestCiliumIntentValidationRejectsUnsafeFQDNAndL7(t *testing.T) {
	tests := []struct {
		name string
		edit func(*NetworkPolicyIntent)
		want string
	}{
		{name: "both DNS forms", edit: func(intent *NetworkPolicyIntent) { intent.Egress[0].FQDNPattern = "*.example.com" }, want: "exactly one"},
		{name: "unbounded wildcard", edit: func(intent *NetworkPolicyIntent) { intent.Egress[0].FQDN = ""; intent.Egress[0].FQDNPattern = "*" }, want: "leading wildcard"},
		{name: "non-normalized DNS", edit: func(intent *NetworkPolicyIntent) { intent.Egress[0].FQDN = "API.Example.com" }, want: "normalized DNS"},
		{name: "HTTP over UDP", edit: func(intent *NetworkPolicyIntent) { intent.Egress[0].Ports[0].Protocol = "UDP" }, want: "require TCP"},
		{name: "relative HTTP path", edit: func(intent *NetworkPolicyIntent) { intent.Egress[0].Ports[0].HTTP[0].Path = "health" }, want: "absolute"},
		{name: "foreign Apollo entity", edit: func(intent *NetworkPolicyIntent) { intent.EntityID = "aeid:prod-use1:helm-chart:planeconnection" }, want: "not bound"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			intent := testNetworkPolicyIntent()
			test.edit(&intent)
			backend := CiliumBackend{Environment: intent.Environment, Namespace: intent.Namespace, Store: &fakeCiliumStore{crd: true}}
			if _, err := backend.Apply(context.Background(), intent); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Apply error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestCiliumObserveFailsClosedWithoutRealizationStatus(t *testing.T) {
	store := &fakeCiliumStore{crd: true}
	backend := CiliumBackend{Environment: "dev-use1", Namespace: "planeconnection", Store: store}
	receipt, err := backend.Apply(context.Background(), testNetworkPolicyIntent())
	if err != nil {
		t.Fatal(err)
	}
	store.observed = observedPolicyJSON(t, receipt, false)
	if _, err := backend.Observe(context.Background(), receipt); err == nil || !strings.Contains(err.Error(), "no Valid condition") {
		t.Fatalf("Observe error = %v, want missing Valid condition", err)
	}
}

func TestCiliumObserveCurrentCNPShapeLeavesEnforcementUnknownWithoutRealizer(t *testing.T) {
	store := &fakeCiliumStore{crd: true}
	backend := CiliumBackend{Environment: "dev-use1", Namespace: "planeconnection", Store: store}
	receipt, err := backend.Apply(context.Background(), testNetworkPolicyIntent())
	if err != nil {
		t.Fatal(err)
	}
	store.observed = observedPolicyJSON(t, receipt, true)
	observation, err := backend.Observe(context.Background(), receipt)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Enforcement != EvidenceUnknown || observation.PolicyRevision != 0 || observation.RealizedEndpoints != 0 {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestCiliumObserveRequiresExactReadyEndpointRealization(t *testing.T) {
	store := &fakeCiliumStore{crd: true}
	backend := CiliumBackend{Environment: "dev-use1", Namespace: "planeconnection", Store: store}
	receipt, err := backend.Apply(context.Background(), testNetworkPolicyIntent())
	if err != nil {
		t.Fatal(err)
	}
	store.observed = observedPolicyJSON(t, receipt, true)
	realizer := realizedEndpoints(receipt)
	backend.Realizer = realizer
	observation, err := backend.Observe(context.Background(), receipt)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Enforcement != EvidenceHealthy || observation.PolicyRevision != 9 || observation.RealizedEndpoints != 2 {
		t.Fatalf("observation = %#v", observation)
	}
	if realizer.query.PolicyUID != "cnp-uid-1" || realizer.query.Generation != 4 || realizer.query.PolicyDigest != receipt.PolicyDigest || realizer.query.EntityID != receipt.EntityID {
		t.Fatalf("realization query not exact-bound: %#v", realizer.query)
	}

	realizer.evidence.Endpoints[1].EgressPolicyEnforcing = false
	if _, err := backend.Observe(context.Background(), receipt); err == nil || !strings.Contains(err.Error(), "not ready with egress") {
		t.Fatalf("Observe error = %v, want non-enforcing endpoint failure", err)
	}
	realizer.evidence.Endpoints[1].EgressPolicyEnforcing = true
	realizer.evidence.PolicyDigest = "sha256:" + strings.Repeat("0", 64)
	if _, err := backend.Observe(context.Background(), receipt); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("Observe error = %v, want realization binding failure", err)
	}
}

func TestCiliumObserveKeepsUnavailableHubbleUnknown(t *testing.T) {
	store := &fakeCiliumStore{crd: true}
	hubble := &fakeHubble{err: errors.New("relay unavailable")}
	backend := CiliumBackend{Environment: "dev-use1", Namespace: "planeconnection", Store: store, Hubble: hubble}
	receipt, err := backend.Apply(context.Background(), testNetworkPolicyIntent())
	if err != nil {
		t.Fatal(err)
	}
	store.observed = observedPolicyJSON(t, receipt, true)
	backend.Realizer = realizedEndpoints(receipt)
	observation, err := backend.Observe(context.Background(), receipt)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Enforcement != EvidenceHealthy || observation.Telemetry != EvidenceUnknown || observation.DeniedFlowCount != 0 {
		t.Fatalf("unavailable telemetry was conflated: %#v", observation)
	}
}

func TestCiliumObserveReportsBoundedDeniedDNSFlowEvidence(t *testing.T) {
	store := &fakeCiliumStore{crd: true}
	const expectedEvidenceLimit = 20
	flows := make([]DeniedFlow, expectedEvidenceLimit+3)
	for index := range flows {
		flows[index] = DeniedFlow{ObservedAt: time.Unix(int64(index), 0).UTC(), Protocol: "DNS", DNSQuery: "blocked.example.com", Reason: "POLICY_DENIED\nunsafe"}
	}
	hubble := &fakeHubble{flows: flows}
	backend := CiliumBackend{Environment: "dev-use1", Namespace: "planeconnection", Store: store, Hubble: hubble}
	receipt, err := backend.Apply(context.Background(), testNetworkPolicyIntent())
	if err != nil {
		t.Fatal(err)
	}
	store.observed = observedPolicyJSON(t, receipt, true)
	backend.Realizer = realizedEndpoints(receipt)
	observation, err := backend.Observe(context.Background(), receipt)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Telemetry != EvidenceHealthy || observation.DeniedFlowCount != len(flows) || len(observation.DeniedFlows) != expectedEvidenceLimit || !observation.FlowsIncomplete {
		t.Fatalf("denied-flow evidence = %#v", observation)
	}
	if observation.DeniedFlows[0].DNSQuery != "blocked.example.com" || strings.Contains(observation.DeniedFlows[0].Reason, "\n") {
		t.Fatalf("denied DNS evidence was not safely normalized: %#v", observation.DeniedFlows[0])
	}
	if hubble.query.Namespace != receipt.Namespace || hubble.query.Environment != receipt.Environment || hubble.query.EntityID != receipt.EntityID || hubble.query.Verdict != "DROPPED" {
		t.Fatalf("Hubble query was not exact-scoped: %#v", hubble.query)
	}
}

func TestCiliumObserveAcceptsPinnedCurrentCRDConditionFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/cilium-network-policy-valid.json")
	if err != nil {
		t.Fatal(err)
	}
	receipt := NetworkPolicyReceipt{
		Backend: ciliumBackendName, Namespace: "planeconnection", Name: "platform-current-fixture",
		Environment: "dev-use1", EntityID: "aeid:dev-use1:helm-chart:planeconnection",
		PlanID: "pln_fixture", Attempt: 2, PolicyDigest: "sha256:" + strings.Repeat("a", 64),
	}
	store := &fakeCiliumStore{crd: true, observed: raw}
	backend := CiliumBackend{Environment: receipt.Environment, Namespace: receipt.Namespace, Store: store}
	observation, err := backend.Observe(context.Background(), receipt)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Enforcement != EvidenceUnknown {
		t.Fatalf("CNP Valid condition alone claimed enforcement: %#v", observation)
	}
}

func realizedEndpoints(receipt NetworkPolicyReceipt) *fakeCiliumRealizer {
	return &fakeCiliumRealizer{evidence: CiliumRealizationEvidence{
		PolicyUID: "cnp-uid-1", Generation: 4, PolicyDigest: receipt.PolicyDigest,
		Endpoints: []CiliumEndpointRealization{
			{Name: "pod-a", Ready: true, EgressPolicyEnforcing: true, PolicyRevision: 12},
			{Name: "pod-b", Ready: true, EgressPolicyEnforcing: true, PolicyRevision: 9},
		},
	}}
}

func observedPolicyJSON(t *testing.T, receipt NetworkPolicyReceipt, valid bool) []byte {
	t.Helper()
	policy := ciliumObservedPolicy{APIVersion: ciliumAPIVersion, Kind: ciliumKind}
	policy.Metadata.Name = receipt.Name
	policy.Metadata.Namespace = receipt.Namespace
	policy.Metadata.UID = "cnp-uid-1"
	policy.Metadata.Generation = 4
	policy.Metadata.Annotations = receiptAnnotations(receipt)
	if valid {
		policy.Status.Conditions = []ciliumPolicyCondition{{Type: "Valid", Status: "True", Reason: "PolicyImported"}}
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

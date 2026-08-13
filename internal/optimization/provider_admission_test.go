package optimization

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestProviderCaptureReceiptIsSanitizedReadOnlyAndScopeBound(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	coordinate := OptimizerCapabilityCoordinate{Kind: OptimizerCapabilityProvider, Name: "prometheus", Version: "fixture", ArtifactDigest: digest('e'), Architecture: "any", Capabilities: []string{"metrics.read"}}
	originalCapabilities := append([]string(nil), coordinate.Capabilities...)
	receipt, err := NewProviderCaptureReceipt(coordinate, "dev-use2", "plan-7", "revision-17", digest('a'), digest('b'), "collector:authorized", []string{"metrics.latency", "metrics.saturation"}, "GET", "aggregate-range", now, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(coordinate.Capabilities, originalCapabilities) {
		t.Fatal("provider capture construction mutated caller-owned coordinate")
	}
	if err := VerifyProviderCaptureReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	if err := VerifyProviderCaptureForAdmission(receipt, coordinate, "dev-use2", "plan-7"); err != nil {
		t.Fatal(err)
	}
	store := FileProviderCaptureStore{Directory: t.TempDir()}
	if err := store.Save(receipt); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Get(receipt.ID)
	if err != nil || loaded.ID != receipt.ID {
		t.Fatalf("stored provider capture = %#v, %v", loaded, err)
	}
	tampered := receipt
	tampered.CollectorIdentity = "collector:other"
	tampered.ID = providerCaptureReceiptID(tampered)
	if err := store.Save(tampered); err != nil {
		t.Fatal(err)
	}
	conflict := tampered
	conflict.CollectorIdentity = "collector:conflict"
	conflict.ID = tampered.ID
	if err := store.Save(conflict); err == nil {
		t.Fatal("overwrote immutable provider capture bytes")
	}
	sourceDigest, err := digestJSON(receipt.SourceRevision)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := ProviderCaptureAdmissionEvidence(receipt, sourceDigest, now.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Gate != OptimizerAdmissionSanitizedLiveCapture || evidence.ReceiptDigest != "sha256:"+strings.TrimPrefix(receipt.ID, "pcap_") || evidence.ValidUntil != now.Add(30*time.Minute).Format(time.RFC3339Nano) {
		t.Fatalf("capture admission evidence = %#v", evidence)
	}
	gates, err := RequiredOptimizerCapabilityAdmissionGates(coordinate.Kind)
	if err != nil {
		t.Fatal(err)
	}
	otherEvidence := make([]OptimizerCapabilityAdmissionEvidence, 0, len(gates)-1)
	for _, gate := range gates {
		if gate == OptimizerAdmissionSanitizedLiveCapture {
			continue
		}
		otherEvidence = append(otherEvidence, OptimizerCapabilityAdmissionEvidence{Gate: gate, ReceiptDigest: digest('1'), SourceRevisionDigest: digest('2'), ObservedAt: now.Format(time.RFC3339Nano), ValidUntil: now.Add(30 * time.Minute).Format(time.RFC3339Nano)})
	}
	admission, err := NewProviderCapabilityAdmissionRecord(coordinate, otherEvidence, receipt, sourceDigest, "dev-use2", "plan-7", "reviewer:test", "provider capture bound", now, now.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyOptimizerCapabilityAdmissionRecord(admission); err != nil {
		t.Fatal(err)
	}
	callerSupplied := append([]OptimizerCapabilityAdmissionEvidence(nil), otherEvidence...)
	callerSupplied = append(callerSupplied, OptimizerCapabilityAdmissionEvidence{Gate: OptimizerAdmissionSanitizedLiveCapture, ReceiptDigest: digest('f'), SourceRevisionDigest: digest('f'), ObservedAt: now.Format(time.RFC3339Nano), ValidUntil: now.Add(30 * time.Minute).Format(time.RFC3339Nano)})
	if _, err := NewProviderCapabilityAdmissionRecord(coordinate, callerSupplied, receipt, sourceDigest, "dev-use2", "plan-7", "reviewer:test", "provider capture bound", now, now.Add(30*time.Minute)); err == nil {
		t.Fatal("accepted caller-authored sanitized live capture gate")
	}
	encoded := string(mustJSON(t, receipt))
	for _, forbidden := range []string{"Authorization", "Cookie", "userId", "responseBody", "secret"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("capture receipt contains forbidden body/credential field %q: %s", forbidden, encoded)
		}
	}
}

func TestProviderCaptureReceiptRejectsMutationForeignScopeAndNonGET(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	coordinate := OptimizerCapabilityCoordinate{Kind: OptimizerCapabilityProvider, Name: "hubble", Version: "fixture", ArtifactDigest: digest('f'), Architecture: "any", Capabilities: []string{"network.aggregate.read"}}
	receipt, err := NewProviderCaptureReceipt(coordinate, "dev-use2", "", "revision-1", digest('a'), digest('b'), "collector:authorized", []string{"network.flows"}, "GET", "aggregate-window", now, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	mutated := receipt
	mutated.ReadOnly = false
	if err := VerifyProviderCaptureReceipt(mutated); err == nil {
		t.Fatal("accepted mutable provider capture")
	}
	foreign := receipt
	foreign.Environment = "other"
	foreign.ID = providerCaptureReceiptID(foreign)
	if err := VerifyProviderCaptureForAdmission(foreign, coordinate, "dev-use2", ""); err == nil {
		t.Fatal("accepted foreign provider capture scope")
	}
	badMethod := receipt
	badMethod.Method = "POST"
	badMethod.ID = providerCaptureReceiptID(badMethod)
	if err := VerifyProviderCaptureReceipt(badMethod); err == nil {
		t.Fatal("accepted non-GET provider capture")
	}
	sourceDigest, err := digestJSON(receipt.SourceRevision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ProviderCaptureAdmissionEvidence(receipt, sourceDigest, now.Add(2*time.Hour)); err == nil {
		t.Fatal("allowed admission evidence to outlive provider capture")
	}
	if _, err := ProviderCaptureAdmissionEvidence(receipt, digest('c'), now.Add(30*time.Minute)); err == nil {
		t.Fatal("accepted source revision digest unrelated to capture")
	}
}

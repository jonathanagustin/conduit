package optimization

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOptimizerCapabilityLifecycleDisablesSelectionAndPreservesHistory(t *testing.T) {
	coordinate := OptimizerCapabilityCoordinate{Kind: OptimizerCapabilityBackend, Name: "or-tools-cp-sat", Version: "9.15.0", ArtifactDigest: digest('a'), Architecture: "amd64", Capabilities: []string{DiscreteScheduleCapability}}
	start := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	shadow, err := NewOptimizerCapabilityLifecycleRecord(coordinate, OptimizerCapabilityShadow, "operator:test", "begin bounded candidate evaluation", start, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyCapabilityUse(shadow, coordinate, true); err != nil {
		t.Fatal(err)
	}
	if err := VerifyCapabilityUse(shadow, coordinate, false); err == nil {
		t.Fatal("shadow capability influenced selection")
	}
	admission := testOptimizerCapabilityAdmission(t, coordinate, "reviewer:test", start)
	enabled, err := NewOptimizerCapabilityLifecycleRecordWithAdmission(coordinate, OptimizerCapabilityEnabled, "operator:test", "admission gates passed", start.Add(time.Second), &shadow, admission)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyCapabilityUse(enabled, coordinate, false); err == nil {
		t.Fatal("historical enabled event bypassed current admission lookup")
	}
	disabled, err := NewOptimizerCapabilityLifecycleRecord(coordinate, OptimizerCapabilityDisabled, "operator:test", "operational failure threshold exceeded", start.Add(2*time.Second), &enabled)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyCapabilityUse(disabled, coordinate, true); err == nil {
		t.Fatal("disabled capability remained available")
	}

	store := FileOptimizerCapabilityLifecycleStore{Directory: filepath.Join(t.TempDir(), "lifecycle")}
	if err := store.Save(shadow); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAdmission(admission); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(enabled); err != nil {
		t.Fatal(err)
	}
	if err := store.VerifyCurrentUseAt(enabled, coordinate, false, start.Add(2*time.Second)); err != nil {
		t.Fatalf("current enabled capability was unavailable: %v", err)
	}
	if err := store.Save(disabled); err != nil {
		t.Fatal(err)
	}
	if err := store.VerifyCurrentUseAt(enabled, coordinate, false, start.Add(3*time.Second)); err == nil {
		t.Fatal("stale enabled lifecycle event remained selectable after disable")
	}
	if err := store.VerifyCurrentUseAt(disabled, coordinate, true, start.Add(3*time.Second)); err == nil {
		t.Fatal("current disabled capability remained available for shadow use")
	}
	loaded, err := store.Get(shadow.ID)
	if err != nil || loaded.ID != shadow.ID || loaded.Status != OptimizerCapabilityShadow {
		t.Fatalf("historical lifecycle record = %#v, %v", loaded, err)
	}
}

func TestOptimizerCapabilityLifecycleFailsClosedOnInvalidTransitions(t *testing.T) {
	coordinate := OptimizerCapabilityCoordinate{Kind: OptimizerCapabilityProvider, Name: "opencost", Version: "candidate", ArtifactDigest: digest('b'), Architecture: "any", Capabilities: []string{"cost.allocation.read"}}
	start := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	if _, err := NewOptimizerCapabilityLifecycleRecord(coordinate, OptimizerCapabilityEnabled, "operator:test", "skip shadow", start, nil); err == nil {
		t.Fatal("accepted initial enabled capability")
	}
	shadow, err := NewOptimizerCapabilityLifecycleRecord(coordinate, OptimizerCapabilityShadow, "operator:test", "evaluate", start, nil)
	if err != nil {
		t.Fatal(err)
	}
	retired, err := NewOptimizerCapabilityLifecycleRecord(coordinate, OptimizerCapabilityRetired, "operator:test", "retire", start.Add(time.Second), &shadow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewOptimizerCapabilityLifecycleRecord(coordinate, OptimizerCapabilityShadow, "operator:test", "resurrect", start.Add(2*time.Second), &retired); err == nil {
		t.Fatal("accepted transition from retired capability")
	}
	foreign := coordinate
	foreign.ArtifactDigest = digest('c')
	if _, err := NewOptimizerCapabilityLifecycleRecord(foreign, OptimizerCapabilityDisabled, "operator:test", "foreign", start.Add(2*time.Second), &shadow); err == nil {
		t.Fatal("accepted coordinate change during transition")
	}
	tampered := shadow
	tampered.CanSelect = true
	if err := VerifyOptimizerCapabilityLifecycleRecord(tampered); err == nil {
		t.Fatal("accepted lifecycle authority tampering")
	}

	store := FileOptimizerCapabilityLifecycleStore{Directory: filepath.Join(t.TempDir(), "branching")}
	if err := store.Save(shadow); err != nil {
		t.Fatal(err)
	}
	admission := testOptimizerCapabilityAdmission(t, coordinate, "reviewer:test", start)
	if err := store.SaveAdmission(admission); err != nil {
		t.Fatal(err)
	}
	enabled, err := NewOptimizerCapabilityLifecycleRecordWithAdmission(coordinate, OptimizerCapabilityEnabled, "operator:test", "enable", start.Add(time.Second), &shadow, admission)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(enabled); err != nil {
		t.Fatal(err)
	}
	branch, err := NewOptimizerCapabilityLifecycleRecord(coordinate, OptimizerCapabilityDisabled, "operator:test", "stale branch", start.Add(2*time.Second), &shadow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(branch); err == nil {
		t.Fatal("accepted a lifecycle transition from a stale predecessor")
	}
}

func TestOptimizerCapabilityAdmissionFailsClosed(t *testing.T) {
	coordinate := OptimizerCapabilityCoordinate{Kind: OptimizerCapabilityBackend, Name: "or-tools-cp-sat", Version: "9.15.0", ArtifactDigest: digest('d'), Architecture: "amd64+arm64", Capabilities: []string{DiscreteScheduleCapability}}
	start := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	admission := testOptimizerCapabilityAdmission(t, coordinate, "reviewer:test", start)
	if err := VerifyOptimizerCapabilityAdmissionRecord(admission); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(*OptimizerCapabilityAdmissionRecord)
	}{
		{"missing gate", func(record *OptimizerCapabilityAdmissionRecord) { record.Evidence = record.Evidence[1:] }},
		{"duplicate gate", func(record *OptimizerCapabilityAdmissionRecord) { record.Evidence[1].Gate = record.Evidence[0].Gate }},
		{"foreign receipt", func(record *OptimizerCapabilityAdmissionRecord) { record.Evidence[0].ReceiptDigest = "not-a-digest" }},
		{"future evidence", func(record *OptimizerCapabilityAdmissionRecord) {
			record.Evidence[0].ObservedAt = start.Add(time.Second).Format(time.RFC3339Nano)
		}},
		{"expired evidence", func(record *OptimizerCapabilityAdmissionRecord) {
			record.Evidence[0].ValidUntil = start.Add(30 * time.Minute).Format(time.RFC3339Nano)
		}},
		{"unbounded validity", func(record *OptimizerCapabilityAdmissionRecord) { record.ValidUntil = record.AssessedAt }},
		{"excessive validity", func(record *OptimizerCapabilityAdmissionRecord) {
			record.ValidUntil = start.Add(maximumCapabilityAdmissionValidity + time.Second).Format(time.RFC3339Nano)
		}},
		{"mutation claim", func(record *OptimizerCapabilityAdmissionRecord) { record.MutatedState = true }},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			forged := admission
			forged.Evidence = append([]OptimizerCapabilityAdmissionEvidence(nil), admission.Evidence...)
			testCase.mutate(&forged)
			if err := VerifyOptimizerCapabilityAdmissionRecord(forged); err == nil {
				t.Fatal("accepted malformed optimizer capability admission")
			}
		})
	}

	shadow, err := NewOptimizerCapabilityLifecycleRecord(coordinate, OptimizerCapabilityShadow, "operator:test", "evaluate", start, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewOptimizerCapabilityLifecycleRecord(coordinate, OptimizerCapabilityEnabled, "operator:test", "prose-only enable", start.Add(time.Second), &shadow); err == nil {
		t.Fatal("accepted ENABLED transition without immutable admission evidence")
	}
	if _, err := NewOptimizerCapabilityLifecycleRecordWithAdmission(coordinate, OptimizerCapabilityEnabled, admission.Assessor, "self-approved", start.Add(time.Second), &shadow, admission); err == nil {
		t.Fatal("accepted one identity as admission assessor and lifecycle actor")
	}
	store := FileOptimizerCapabilityLifecycleStore{Directory: filepath.Join(t.TempDir(), "admission")}
	if err := store.Save(shadow); err != nil {
		t.Fatal(err)
	}
	enabled, err := NewOptimizerCapabilityLifecycleRecordWithAdmission(coordinate, OptimizerCapabilityEnabled, "operator:test", "enable", start.Add(time.Second), &shadow, admission)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(enabled); err == nil {
		t.Fatal("stored enabled lifecycle event without its admission record")
	}
	if err := store.SaveAdmission(admission); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(enabled); err != nil {
		t.Fatal(err)
	}
	foreignCoordinate := coordinate
	foreignCoordinate.ArtifactDigest = digest('e')
	if err := store.VerifyCurrentUseAt(enabled, foreignCoordinate, false, start.Add(2*time.Second)); err == nil {
		t.Fatal("foreign optimizer capability coordinate used admission evidence")
	}
	admissionPath := filepath.Join(store.Directory, "optimizer-capability-admission", admission.ID+".json")
	encodedAdmission, err := os.ReadFile(admissionPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(admissionPath); err != nil {
		t.Fatal(err)
	}
	if err := store.VerifyCurrentUseAt(enabled, coordinate, false, start.Add(2*time.Second)); err == nil {
		t.Fatal("missing optimizer capability admission remained selectable")
	}
	if err := os.WriteFile(admissionPath, encodedAdmission, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.VerifyCurrentUseAt(enabled, coordinate, false, start.Add(2*time.Hour)); err == nil {
		t.Fatal("expired optimizer capability admission remained selectable")
	}
}

func TestProviderSelectionRequiresProviderCoordinateAndCurrentAdmission(t *testing.T) {
	coordinate := OptimizerCapabilityCoordinate{Kind: OptimizerCapabilityProvider, Name: "prometheus", Version: "fixture", ArtifactDigest: digest('e'), Architecture: "any", Capabilities: []string{"metrics.read"}}
	start := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	shadow, err := NewOptimizerCapabilityLifecycleRecord(coordinate, OptimizerCapabilityShadow, "operator:test", "shadow", start, nil)
	if err != nil {
		t.Fatal(err)
	}
	admission := testOptimizerCapabilityAdmission(t, coordinate, "reviewer:test", start)
	enabled, err := NewOptimizerCapabilityLifecycleRecordWithAdmission(coordinate, OptimizerCapabilityEnabled, "operator:test", "enable", start.Add(time.Second), &shadow, admission)
	if err != nil {
		t.Fatal(err)
	}
	store := FileOptimizerCapabilityLifecycleStore{Directory: filepath.Join(t.TempDir(), "provider")}
	if err := store.Save(shadow); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAdmission(admission); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(enabled); err != nil {
		t.Fatal(err)
	}
	if err := store.VerifyProviderSelection(coordinate, enabled, start.Add(2*time.Second)); err != nil {
		t.Fatalf("current provider admission rejected: %v", err)
	}
	backendCoordinate := coordinate
	backendCoordinate.Kind = OptimizerCapabilityBackend
	if err := store.VerifyProviderSelection(backendCoordinate, enabled, start.Add(2*time.Second)); err == nil {
		t.Fatal("accepted backend coordinate through provider selection gate")
	}
	if err := store.VerifyProviderSelection(coordinate, shadow, start.Add(2*time.Second)); err == nil {
		t.Fatal("accepted shadow provider for selection")
	}
}

func TestDiscreteBackendSelectionRequiresExactCurrentAdmission(t *testing.T) {
	backend := ExternalProcessDiscreteSolverBackend{BackendDescriptor: SolverBackendDescriptor{
		Name: "cp-sat", Version: "9.15.0", ExecutableDigest: digest('f'), Architecture: "amd64+arm64",
		Capabilities: []string{DiscreteScheduleCapability},
	}}
	coordinate := OptimizerCapabilityCoordinate{
		Kind: OptimizerCapabilityBackend, Name: "cp-sat", Version: "9.15.0", ArtifactDigest: digest('f'),
		Architecture: "amd64+arm64", Capabilities: []string{DiscreteScheduleCapability},
	}
	start := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	shadow, err := NewOptimizerCapabilityLifecycleRecord(coordinate, OptimizerCapabilityShadow, "operator:test", "shadow", start, nil)
	if err != nil {
		t.Fatal(err)
	}
	admission := testOptimizerCapabilityAdmission(t, coordinate, "reviewer:test", start)
	enabled, err := NewOptimizerCapabilityLifecycleRecordWithAdmission(coordinate, OptimizerCapabilityEnabled, "operator:test", "enable", start.Add(time.Second), &shadow, admission)
	if err != nil {
		t.Fatal(err)
	}
	store := FileOptimizerCapabilityLifecycleStore{Directory: filepath.Join(t.TempDir(), "backend-selection")}
	if err := store.Save(shadow); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAdmission(admission); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(enabled); err != nil {
		t.Fatal(err)
	}
	if err := store.VerifyDiscreteBackendSelection(backend, enabled, start.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	foreign := backend
	foreign.BackendDescriptor.ExecutableDigest = digest('0')
	if err := store.VerifyDiscreteBackendSelection(foreign, enabled, start.Add(2*time.Second)); err == nil {
		t.Fatal("foreign discrete backend executable used admitted lifecycle")
	}
	native := NativeDiscreteSolverBackend{ExecutableDigest: digest('9'), Architecture: "amd64"}
	if err := store.VerifyDiscreteBackendSelection(native, enabled, start.Add(2*time.Second)); err == nil {
		t.Fatal("native reference backend was treated as an external admitted capability")
	}
}

func TestContinuousBackendSelectionRequiresExactCurrentAdmission(t *testing.T) {
	backend := fixtureSolverBackend{}
	descriptor := backend.Descriptor()
	coordinate := OptimizerCapabilityCoordinate{
		Kind: OptimizerCapabilityBackend, Name: descriptor.Name, Version: descriptor.Version,
		ArtifactDigest: descriptor.ExecutableDigest, Architecture: descriptor.Architecture,
		Capabilities: append([]string(nil), descriptor.Capabilities...),
	}
	start := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	shadow, err := NewOptimizerCapabilityLifecycleRecord(coordinate, OptimizerCapabilityShadow, "operator:test", "shadow", start, nil)
	if err != nil {
		t.Fatal(err)
	}
	store := FileOptimizerCapabilityLifecycleStore{Directory: filepath.Join(t.TempDir(), "continuous")}
	if err := store.Save(shadow); err != nil {
		t.Fatal(err)
	}
	admission := testOptimizerCapabilityAdmission(t, coordinate, "reviewer:test", start)
	if err := store.SaveAdmission(admission); err != nil {
		t.Fatal(err)
	}
	enabled, err := NewOptimizerCapabilityLifecycleRecordWithAdmission(coordinate, OptimizerCapabilityEnabled, "operator:test", "enable", start.Add(time.Second), &shadow, admission)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(enabled); err != nil {
		t.Fatal(err)
	}
	if err := store.VerifyContinuousBackendSelection(backend, enabled, start.Add(2*time.Second)); err != nil {
		t.Fatalf("current continuous backend admission rejected: %v", err)
	}
	if err := store.VerifyContinuousBackendSelection(NativeSolverBackend{}, enabled, start.Add(2*time.Second)); err == nil {
		t.Fatal("accepted native reference through external admission gate")
	}
	disabled, err := NewOptimizerCapabilityLifecycleRecord(coordinate, OptimizerCapabilityDisabled, "operator:test", "disable", start.Add(2*time.Second), &enabled)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(disabled); err != nil {
		t.Fatal(err)
	}
	if err := store.VerifyContinuousBackendSelection(backend, disabled, start.Add(3*time.Second)); err == nil {
		t.Fatal("accepted disabled continuous backend")
	}
}

func testOptimizerCapabilityAdmission(t *testing.T, coordinate OptimizerCapabilityCoordinate, assessor string, assessedAt time.Time) OptimizerCapabilityAdmissionRecord {
	t.Helper()
	gates, err := RequiredOptimizerCapabilityAdmissionGates(coordinate.Kind)
	if err != nil {
		t.Fatal(err)
	}
	evidence := make([]OptimizerCapabilityAdmissionEvidence, len(gates))
	for index, gate := range gates {
		evidence[index] = OptimizerCapabilityAdmissionEvidence{
			Gate: gate, ReceiptDigest: digest(byte('1' + index)),
			SourceRevisionDigest: digest(byte('a' + index%6)),
			ObservedAt:           assessedAt.Add(-time.Second).Format(time.RFC3339Nano),
			ValidUntil:           assessedAt.Add(time.Hour).Format(time.RFC3339Nano),
		}
	}
	record, err := NewOptimizerCapabilityAdmissionRecord(coordinate, evidence, assessor, "all closed admission gates independently verified", assessedAt, assessedAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return record
}

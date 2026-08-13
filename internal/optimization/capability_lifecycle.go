package optimization

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

const (
	OptimizerCapabilityAdmissionAPIVersion = "optimization.platform.example/capability-admission/v1alpha1"
	OptimizerCapabilityLifecycleAPIVersion = "optimization.platform.example/capability-lifecycle/v1alpha2"
)

const maximumCapabilityLifecycleRecords = 10_000

const maximumCapabilityAdmissionValidity = 90 * 24 * time.Hour

var ErrCapabilityLifecycleNotFound = errors.New("optimizer capability lifecycle not found")

const (
	OptimizerCapabilityBackend  = "BACKEND"
	OptimizerCapabilityProvider = "PROVIDER"

	OptimizerCapabilityShadow   = "SHADOW"
	OptimizerCapabilityEnabled  = "ENABLED"
	OptimizerCapabilityDisabled = "DISABLED"
	OptimizerCapabilityRetired  = "RETIRED"
)

const (
	OptimizerAdmissionArtifactIdentity              = "ARTIFACT_IDENTITY"
	OptimizerAdmissionNativeMultiArchitecture       = "NATIVE_MULTI_ARCHITECTURE"
	OptimizerAdmissionIndependentConstraintVerifier = "INDEPENDENT_CONSTRAINT_VERIFICATION"
	OptimizerAdmissionReproducibleOrBoundedBuild    = "REPRODUCIBLE_OR_BOUNDED_BUILD"
	OptimizerAdmissionSandboxIsolation              = "SANDBOX_ISOLATION"
	OptimizerAdmissionSignedAttestedDistribution    = "SIGNED_ATTESTED_DISTRIBUTION"
	OptimizerAdmissionReadOnlyAuthority             = "READ_ONLY_AUTHORITY"
	OptimizerAdmissionFixtureConformance            = "FIXTURE_CONFORMANCE"
	OptimizerAdmissionFreshnessFailureConformance   = "FRESHNESS_FAILURE_CONFORMANCE"
	OptimizerAdmissionSanitizedLiveCapture          = "SANITIZED_LIVE_CAPTURE"
	OptimizerAdmissionShadowCalibration             = "SHADOW_CALIBRATION"
)

// OptimizerCapabilityCoordinate is immutable supply-chain identity. Name or
// version alone never authorizes execution because two builds of the same
// semantic version may have different behavior and closure.
type OptimizerCapabilityCoordinate struct {
	Kind           string   `json:"kind"`
	Name           string   `json:"name"`
	Version        string   `json:"version"`
	ArtifactDigest string   `json:"artifactDigest"`
	Architecture   string   `json:"architecture"`
	Capabilities   []string `json:"capabilities"`
}

// OptimizerCapabilityAdmissionEvidence binds one closed admission gate to a
// content-addressed, sanitized receipt. It deliberately stores no raw provider
// body, credential, signature material, or tenant identifier.
type OptimizerCapabilityAdmissionEvidence struct {
	Gate                 string `json:"gate"`
	ReceiptDigest        string `json:"receiptDigest"`
	SourceRevisionDigest string `json:"sourceRevisionDigest"`
	ObservedAt           string `json:"observedAt"`
	ValidUntil           string `json:"validUntil"`
}

// OptimizerCapabilityAdmissionRecord is the independently assessed evidence
// required before an immutable capability coordinate can enter ENABLED. The
// record is finite-lived so stale security, provider, or calibration evidence
// cannot authorize selection indefinitely.
type OptimizerCapabilityAdmissionRecord struct {
	APIVersion   string                                 `json:"apiVersion"`
	Kind         string                                 `json:"kind"`
	ID           string                                 `json:"id"`
	Coordinate   OptimizerCapabilityCoordinate          `json:"coordinate"`
	Evidence     []OptimizerCapabilityAdmissionEvidence `json:"evidence"`
	AssessedAt   string                                 `json:"assessedAt"`
	ValidUntil   string                                 `json:"validUntil"`
	Assessor     string                                 `json:"assessor"`
	Rationale    string                                 `json:"rationale"`
	MutatedState bool                                   `json:"mutatedState"`
}

// OptimizerCapabilityLifecycleRecord is an immutable enable/disable event. It
// grants no delivery authority. PreviousID makes transition history explicit;
// the store verifies that link against the complete previous record.
type OptimizerCapabilityLifecycleRecord struct {
	APIVersion   string                        `json:"apiVersion"`
	Kind         string                        `json:"kind"`
	ID           string                        `json:"id"`
	Coordinate   OptimizerCapabilityCoordinate `json:"coordinate"`
	Status       string                        `json:"status"`
	PreviousID   string                        `json:"previousId,omitempty"`
	AdmissionID  string                        `json:"admissionId,omitempty"`
	EffectiveAt  string                        `json:"effectiveAt"`
	Actor        string                        `json:"actor"`
	Rationale    string                        `json:"rationale"`
	CanSelect    bool                          `json:"canSelect"`
	CanShadow    bool                          `json:"canShadow"`
	MutatedState bool                          `json:"mutatedState"`
}

func RequiredOptimizerCapabilityAdmissionGates(kind string) ([]string, error) {
	var gates []string
	switch kind {
	case OptimizerCapabilityBackend:
		gates = []string{
			OptimizerAdmissionArtifactIdentity,
			OptimizerAdmissionIndependentConstraintVerifier,
			OptimizerAdmissionNativeMultiArchitecture,
			OptimizerAdmissionReproducibleOrBoundedBuild,
			OptimizerAdmissionSandboxIsolation,
			OptimizerAdmissionShadowCalibration,
			OptimizerAdmissionSignedAttestedDistribution,
		}
	case OptimizerCapabilityProvider:
		gates = []string{
			OptimizerAdmissionArtifactIdentity,
			OptimizerAdmissionFixtureConformance,
			OptimizerAdmissionFreshnessFailureConformance,
			OptimizerAdmissionReadOnlyAuthority,
			OptimizerAdmissionSanitizedLiveCapture,
			OptimizerAdmissionShadowCalibration,
		}
	default:
		return nil, errors.New("optimizer capability kind must be BACKEND or PROVIDER")
	}
	sort.Strings(gates)
	return gates, nil
}

func NewOptimizerCapabilityAdmissionRecord(
	coordinate OptimizerCapabilityCoordinate,
	evidence []OptimizerCapabilityAdmissionEvidence,
	assessor, rationale string,
	assessedAt, validUntil time.Time,
) (OptimizerCapabilityAdmissionRecord, error) {
	coordinate.Capabilities = append([]string(nil), coordinate.Capabilities...)
	sort.Strings(coordinate.Capabilities)
	evidence = append([]OptimizerCapabilityAdmissionEvidence(nil), evidence...)
	sort.Slice(evidence, func(i, j int) bool { return evidence[i].Gate < evidence[j].Gate })
	record := OptimizerCapabilityAdmissionRecord{
		APIVersion: OptimizerCapabilityAdmissionAPIVersion,
		Kind:       "OptimizerCapabilityAdmissionRecord",
		Coordinate: coordinate,
		Evidence:   evidence,
		AssessedAt: assessedAt.UTC().Format(time.RFC3339Nano),
		ValidUntil: validUntil.UTC().Format(time.RFC3339Nano),
		Assessor:   assessor,
		Rationale:  rationale,
	}
	if err := validateCapabilityAdmissionRecord(record); err != nil {
		return OptimizerCapabilityAdmissionRecord{}, err
	}
	id, err := capabilityAdmissionID(record)
	if err != nil {
		return OptimizerCapabilityAdmissionRecord{}, err
	}
	record.ID = id
	return record, nil
}

func VerifyOptimizerCapabilityAdmissionRecord(record OptimizerCapabilityAdmissionRecord) error {
	if err := validateCapabilityAdmissionRecord(record); err != nil {
		return err
	}
	want, err := capabilityAdmissionID(record)
	if err != nil {
		return err
	}
	if record.ID != want {
		return errors.New("optimizer capability admission id does not match content")
	}
	return nil
}

func validateCapabilityAdmissionRecord(record OptimizerCapabilityAdmissionRecord) error {
	if record.APIVersion != OptimizerCapabilityAdmissionAPIVersion || record.Kind != "OptimizerCapabilityAdmissionRecord" || (record.ID != "" && !validContentID(record.ID, "optadm_")) || record.MutatedState {
		return errors.New("unsupported optimizer capability admission record")
	}
	coordinateProbe := OptimizerCapabilityLifecycleRecord{
		APIVersion: OptimizerCapabilityLifecycleAPIVersion, Kind: "OptimizerCapabilityLifecycleRecord",
		Coordinate: record.Coordinate, Status: OptimizerCapabilityShadow,
		EffectiveAt: time.Unix(1, 0).UTC().Format(time.RFC3339Nano), Actor: "admission-validator",
		Rationale: "validate immutable capability coordinate", CanShadow: true,
	}
	if err := validateCapabilityLifecycleRecord(coordinateProbe); err != nil {
		return err
	}
	required, err := RequiredOptimizerCapabilityAdmissionGates(record.Coordinate.Kind)
	if err != nil {
		return err
	}
	if len(record.Evidence) != len(required) {
		return errors.New("optimizer capability admission evidence is incomplete")
	}
	assessedAt, assessedErr := time.Parse(time.RFC3339Nano, record.AssessedAt)
	validUntil, validUntilErr := time.Parse(time.RFC3339Nano, record.ValidUntil)
	if assessedErr != nil || validUntilErr != nil || !validUntil.After(assessedAt) || validUntil.Sub(assessedAt) > maximumCapabilityAdmissionValidity {
		return errors.New("optimizer capability admission requires an ordered finite validity window")
	}
	for index, item := range record.Evidence {
		observedAt, observedErr := time.Parse(time.RFC3339Nano, item.ObservedAt)
		evidenceValidUntil, evidenceValidErr := time.Parse(time.RFC3339Nano, item.ValidUntil)
		if item.Gate != required[index] || !validDigest(item.ReceiptDigest) || !validDigest(item.SourceRevisionDigest) || observedErr != nil || evidenceValidErr != nil || observedAt.After(assessedAt) || !evidenceValidUntil.After(observedAt) || evidenceValidUntil.Before(validUntil) {
			return errors.New("optimizer capability admission evidence is invalid, incomplete, or non-canonical")
		}
	}
	if !validObservationText(record.Assessor) || !validObservationText(record.Rationale) {
		return errors.New("optimizer capability admission requires assessor and rationale")
	}
	return nil
}

func capabilityAdmissionID(record OptimizerCapabilityAdmissionRecord) (string, error) {
	record.ID = ""
	digest, err := digestJSON(record)
	if err != nil {
		return "", err
	}
	return "optadm_" + digest[len("sha256:"):], nil
}

func NewOptimizerCapabilityLifecycleRecord(
	coordinate OptimizerCapabilityCoordinate,
	status, actor, rationale string,
	effectiveAt time.Time,
	previous *OptimizerCapabilityLifecycleRecord,
) (OptimizerCapabilityLifecycleRecord, error) {
	return newOptimizerCapabilityLifecycleRecord(coordinate, status, actor, rationale, effectiveAt, previous, nil)
}

func NewOptimizerCapabilityLifecycleRecordWithAdmission(
	coordinate OptimizerCapabilityCoordinate,
	status, actor, rationale string,
	effectiveAt time.Time,
	previous *OptimizerCapabilityLifecycleRecord,
	admission OptimizerCapabilityAdmissionRecord,
) (OptimizerCapabilityLifecycleRecord, error) {
	return newOptimizerCapabilityLifecycleRecord(coordinate, status, actor, rationale, effectiveAt, previous, &admission)
}

func newOptimizerCapabilityLifecycleRecord(
	coordinate OptimizerCapabilityCoordinate,
	status, actor, rationale string,
	effectiveAt time.Time,
	previous *OptimizerCapabilityLifecycleRecord,
	admission *OptimizerCapabilityAdmissionRecord,
) (OptimizerCapabilityLifecycleRecord, error) {
	coordinate.Capabilities = append([]string(nil), coordinate.Capabilities...)
	sort.Strings(coordinate.Capabilities)
	record := OptimizerCapabilityLifecycleRecord{
		APIVersion:  OptimizerCapabilityLifecycleAPIVersion,
		Kind:        "OptimizerCapabilityLifecycleRecord",
		Coordinate:  coordinate,
		Status:      status,
		EffectiveAt: effectiveAt.UTC().Format(time.RFC3339Nano),
		Actor:       actor,
		Rationale:   rationale,
		CanSelect:   status == OptimizerCapabilityEnabled,
		CanShadow:   status == OptimizerCapabilityShadow || status == OptimizerCapabilityEnabled,
	}
	if admission != nil {
		if status != OptimizerCapabilityEnabled {
			return OptimizerCapabilityLifecycleRecord{}, errors.New("optimizer admission evidence may only be bound to ENABLED")
		}
		if err := verifyCapabilityAdmissionForUse(*admission, coordinate, effectiveAt); err != nil {
			return OptimizerCapabilityLifecycleRecord{}, err
		}
		if admission.Assessor == actor {
			return OptimizerCapabilityLifecycleRecord{}, errors.New("optimizer capability admission assessor must be independent from lifecycle actor")
		}
		record.AdmissionID = admission.ID
	}
	if previous != nil {
		if err := VerifyOptimizerCapabilityLifecycleRecord(*previous); err != nil {
			return OptimizerCapabilityLifecycleRecord{}, fmt.Errorf("previous capability lifecycle record: %w", err)
		}
		record.PreviousID = previous.ID
		if err := validateCapabilityTransition(*previous, record); err != nil {
			return OptimizerCapabilityLifecycleRecord{}, err
		}
	} else if status != OptimizerCapabilityShadow {
		return OptimizerCapabilityLifecycleRecord{}, errors.New("a capability lifecycle must begin in SHADOW")
	}
	if err := validateCapabilityLifecycleRecord(record); err != nil {
		return OptimizerCapabilityLifecycleRecord{}, err
	}
	id, err := capabilityLifecycleID(record)
	if err != nil {
		return OptimizerCapabilityLifecycleRecord{}, err
	}
	record.ID = id
	return record, nil
}

func VerifyOptimizerCapabilityLifecycleRecord(record OptimizerCapabilityLifecycleRecord) error {
	if err := validateCapabilityLifecycleRecord(record); err != nil {
		return err
	}
	want, err := capabilityLifecycleID(record)
	if err != nil {
		return err
	}
	if record.ID != want {
		return errors.New("optimizer capability lifecycle id does not match content")
	}
	return nil
}

func validateCapabilityLifecycleRecord(record OptimizerCapabilityLifecycleRecord) error {
	if record.APIVersion != OptimizerCapabilityLifecycleAPIVersion || record.Kind != "OptimizerCapabilityLifecycleRecord" || (record.ID != "" && !validContentID(record.ID, "optcap_")) {
		return errors.New("unsupported optimizer capability lifecycle record")
	}
	coordinate := record.Coordinate
	if coordinate.Kind != OptimizerCapabilityBackend && coordinate.Kind != OptimizerCapabilityProvider {
		return errors.New("optimizer capability kind must be BACKEND or PROVIDER")
	}
	if !validObservationText(coordinate.Name) || !validObservationText(coordinate.Version) || !validDigest(coordinate.ArtifactDigest) || !validObservationText(coordinate.Architecture) || len(coordinate.Capabilities) == 0 || len(coordinate.Capabilities) > 64 {
		return errors.New("optimizer capability coordinate is incomplete")
	}
	for index, capability := range coordinate.Capabilities {
		if !validObservationText(capability) || (index > 0 && coordinate.Capabilities[index-1] >= capability) {
			return errors.New("optimizer capabilities must have unique canonical order")
		}
	}
	switch record.Status {
	case OptimizerCapabilityShadow, OptimizerCapabilityEnabled, OptimizerCapabilityDisabled, OptimizerCapabilityRetired:
	default:
		return errors.New("optimizer capability lifecycle status is invalid")
	}
	if record.Status == OptimizerCapabilityEnabled {
		if !validContentID(record.AdmissionID, "optadm_") {
			return errors.New("enabled optimizer capability requires immutable admission evidence")
		}
	} else if record.AdmissionID != "" {
		return errors.New("only enabled optimizer capability may bind admission evidence")
	}
	if record.CanSelect != (record.Status == OptimizerCapabilityEnabled) || record.CanShadow != (record.Status == OptimizerCapabilityShadow || record.Status == OptimizerCapabilityEnabled) || record.MutatedState {
		return errors.New("optimizer capability authority flags do not match lifecycle status")
	}
	if _, err := time.Parse(time.RFC3339Nano, record.EffectiveAt); err != nil || !validObservationText(record.Actor) || !validObservationText(record.Rationale) {
		return errors.New("optimizer capability lifecycle requires time, actor, and rationale")
	}
	if record.PreviousID != "" && !validContentID(record.PreviousID, "optcap_") {
		return errors.New("optimizer capability previous id is invalid")
	}
	return nil
}

func validateCapabilityTransition(previous, next OptimizerCapabilityLifecycleRecord) error {
	if !reflect.DeepEqual(previous.Coordinate, next.Coordinate) {
		return errors.New("optimizer capability lifecycle cannot change immutable coordinates")
	}
	previousTime, _ := time.Parse(time.RFC3339Nano, previous.EffectiveAt)
	nextTime, err := time.Parse(time.RFC3339Nano, next.EffectiveAt)
	if err != nil || !nextTime.After(previousTime) {
		return errors.New("optimizer capability transition must advance effective time")
	}
	allowed := map[string]map[string]bool{
		OptimizerCapabilityShadow:   {OptimizerCapabilityEnabled: true, OptimizerCapabilityDisabled: true, OptimizerCapabilityRetired: true},
		OptimizerCapabilityEnabled:  {OptimizerCapabilityDisabled: true, OptimizerCapabilityRetired: true},
		OptimizerCapabilityDisabled: {OptimizerCapabilityShadow: true, OptimizerCapabilityRetired: true},
	}
	if !allowed[previous.Status][next.Status] {
		return fmt.Errorf("optimizer capability transition %s -> %s is not allowed", previous.Status, next.Status)
	}
	return nil
}

func capabilityLifecycleID(record OptimizerCapabilityLifecycleRecord) (string, error) {
	record.ID = ""
	digest, err := digestJSON(record)
	if err != nil {
		return "", err
	}
	return "optcap_" + digest[len("sha256:"):], nil
}

// VerifyCapabilityUse admits shadow evaluation only. Proposal-influencing
// selection additionally needs the current immutable lifecycle chain and its
// independently assessed admission record, so callers must use
// FileOptimizerCapabilityLifecycleStore.VerifyCurrentUseAt for that path.
func VerifyCapabilityUse(record OptimizerCapabilityLifecycleRecord, coordinate OptimizerCapabilityCoordinate, shadow bool) error {
	if err := VerifyOptimizerCapabilityLifecycleRecord(record); err != nil {
		return err
	}
	coordinate.Capabilities = append([]string(nil), coordinate.Capabilities...)
	sort.Strings(coordinate.Capabilities)
	if !reflect.DeepEqual(record.Coordinate, coordinate) {
		return errors.New("optimizer capability use does not match lifecycle coordinate")
	}
	if !shadow {
		return errors.New("optimizer capability selection requires current admission evidence")
	}
	if shadow && !record.CanShadow {
		return errors.New("optimizer capability is not available for shadow evaluation")
	}
	return nil
}

type FileOptimizerCapabilityLifecycleStore struct{ Directory string }

func (store FileOptimizerCapabilityLifecycleStore) SaveAdmission(record OptimizerCapabilityAdmissionRecord) error {
	if err := VerifyOptimizerCapabilityAdmissionRecord(record); err != nil {
		return err
	}
	return FileTwinStore(store).save("optimizer-capability-admission", record.ID, record)
}

func (store FileOptimizerCapabilityLifecycleStore) GetAdmission(id string) (OptimizerCapabilityAdmissionRecord, error) {
	var record OptimizerCapabilityAdmissionRecord
	if !validContentID(id, "optadm_") {
		return record, errors.New("invalid optimizer capability admission id")
	}
	if err := FileTwinStore(store).get("optimizer-capability-admission", id, &record); err != nil {
		return record, err
	}
	return record, VerifyOptimizerCapabilityAdmissionRecord(record)
}

func (store FileOptimizerCapabilityLifecycleStore) Save(record OptimizerCapabilityLifecycleRecord) error {
	if err := VerifyOptimizerCapabilityLifecycleRecord(record); err != nil {
		return err
	}
	if record.Status == OptimizerCapabilityEnabled {
		admission, err := store.GetAdmission(record.AdmissionID)
		if err != nil {
			return fmt.Errorf("load optimizer capability admission: %w", err)
		}
		effectiveAt, _ := time.Parse(time.RFC3339Nano, record.EffectiveAt)
		if err := verifyCapabilityAdmissionForUse(admission, record.Coordinate, effectiveAt); err != nil {
			return err
		}
		if admission.Assessor == record.Actor {
			return errors.New("optimizer capability admission assessor must be independent from lifecycle actor")
		}
	}
	if record.PreviousID == "" {
		if record.Status != OptimizerCapabilityShadow {
			return errors.New("optimizer capability lifecycle must start in SHADOW")
		}
		if _, err := store.Current(record.Coordinate); err == nil {
			return errors.New("optimizer capability lifecycle already has a current record")
		} else if !errors.Is(err, ErrCapabilityLifecycleNotFound) {
			return err
		}
	} else {
		previous, err := store.Get(record.PreviousID)
		if err != nil {
			return fmt.Errorf("load previous optimizer capability lifecycle record: %w", err)
		}
		if err := validateCapabilityTransition(previous, record); err != nil {
			return err
		}
		current, err := store.Current(record.Coordinate)
		if err != nil {
			return err
		}
		if current.ID != previous.ID {
			return errors.New("optimizer capability transition does not extend the current record")
		}
	}
	return FileTwinStore(store).save("optimizer-capability-lifecycle", record.ID, record)
}

func (store FileOptimizerCapabilityLifecycleStore) Get(id string) (OptimizerCapabilityLifecycleRecord, error) {
	var record OptimizerCapabilityLifecycleRecord
	if !validContentID(id, "optcap_") {
		return record, errors.New("invalid optimizer capability lifecycle id")
	}
	if err := FileTwinStore(store).get("optimizer-capability-lifecycle", id, &record); err != nil {
		return record, err
	}
	return record, VerifyOptimizerCapabilityLifecycleRecord(record)
}

// Current derives the sole current record from the immutable event chain. It
// never guesses across a branch, missing predecessor, cycle, or corrupt file:
// ambiguity disables the capability until the evidence store is repaired.
func (store FileOptimizerCapabilityLifecycleStore) Current(coordinate OptimizerCapabilityCoordinate) (OptimizerCapabilityLifecycleRecord, error) {
	var zero OptimizerCapabilityLifecycleRecord
	coordinate.Capabilities = append([]string(nil), coordinate.Capabilities...)
	sort.Strings(coordinate.Capabilities)
	probe := OptimizerCapabilityLifecycleRecord{
		APIVersion: OptimizerCapabilityLifecycleAPIVersion, Kind: "OptimizerCapabilityLifecycleRecord",
		Coordinate: coordinate, Status: OptimizerCapabilityShadow, EffectiveAt: time.Unix(1, 0).UTC().Format(time.RFC3339Nano),
		Actor: "lifecycle-reader", Rationale: "validate immutable capability coordinate", CanShadow: true,
	}
	if err := validateCapabilityLifecycleRecord(probe); err != nil {
		return zero, err
	}
	directory := filepath.Join(store.Directory, "optimizer-capability-lifecycle")
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return zero, ErrCapabilityLifecycleNotFound
	}
	if err != nil {
		return zero, err
	}
	if len(entries) > maximumCapabilityLifecycleRecords {
		return zero, errors.New("optimizer capability lifecycle exceeds its record bound")
	}
	records := map[string]OptimizerCapabilityLifecycleRecord{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		record, err := store.Get(id)
		if err != nil {
			return zero, fmt.Errorf("read optimizer capability lifecycle record %q: %w", entry.Name(), err)
		}
		if reflect.DeepEqual(record.Coordinate, coordinate) {
			records[record.ID] = record
		}
	}
	if len(records) == 0 {
		return zero, ErrCapabilityLifecycleNotFound
	}
	if len(records) > maximumCapabilityLifecycleRecords {
		return zero, errors.New("optimizer capability lifecycle exceeds its record bound")
	}
	successor := map[string]string{}
	rootID := ""
	for id, record := range records {
		if record.PreviousID == "" {
			if rootID != "" || record.Status != OptimizerCapabilityShadow {
				return zero, errors.New("optimizer capability lifecycle has multiple or invalid roots")
			}
			rootID = id
			continue
		}
		previous, exists := records[record.PreviousID]
		if !exists {
			return zero, errors.New("optimizer capability lifecycle has a missing predecessor")
		}
		if err := validateCapabilityTransition(previous, record); err != nil {
			return zero, err
		}
		if successor[record.PreviousID] != "" {
			return zero, errors.New("optimizer capability lifecycle is branched")
		}
		successor[record.PreviousID] = id
	}
	if rootID == "" {
		return zero, errors.New("optimizer capability lifecycle has no root")
	}
	currentID := rootID
	visited := map[string]bool{}
	for {
		if visited[currentID] {
			return zero, errors.New("optimizer capability lifecycle contains a cycle")
		}
		visited[currentID] = true
		next := successor[currentID]
		if next == "" {
			break
		}
		currentID = next
	}
	if len(visited) != len(records) {
		return zero, errors.New("optimizer capability lifecycle is disconnected")
	}
	return records[currentID], nil
}

// VerifyCurrentUse is the wall-clock authority-bearing selection gate.
func (store FileOptimizerCapabilityLifecycleStore) VerifyCurrentUse(record OptimizerCapabilityLifecycleRecord, coordinate OptimizerCapabilityCoordinate, shadow bool) error {
	return store.VerifyCurrentUseAt(record, coordinate, shadow, time.Now().UTC())
}

// VerifyCurrentUseAt independently proves the supplied lifecycle event is the
// unique current head and, for selection, resolves a complete unexpired
// admission record from immutable storage. The explicit clock keeps replay and
// tests deterministic while production callers use VerifyCurrentUse.
func (store FileOptimizerCapabilityLifecycleStore) VerifyCurrentUseAt(record OptimizerCapabilityLifecycleRecord, coordinate OptimizerCapabilityCoordinate, shadow bool, at time.Time) error {
	if err := VerifyOptimizerCapabilityLifecycleRecord(record); err != nil {
		return err
	}
	coordinate.Capabilities = append([]string(nil), coordinate.Capabilities...)
	sort.Strings(coordinate.Capabilities)
	if !reflect.DeepEqual(record.Coordinate, coordinate) {
		return errors.New("optimizer capability use does not match lifecycle coordinate")
	}
	if shadow {
		if !record.CanShadow {
			return errors.New("optimizer capability is not available for shadow evaluation")
		}
	} else {
		if !record.CanSelect {
			return errors.New("optimizer capability is not enabled for selection")
		}
		admission, err := store.GetAdmission(record.AdmissionID)
		if err != nil {
			return fmt.Errorf("load optimizer capability admission: %w", err)
		}
		if err := verifyCapabilityAdmissionForUse(admission, coordinate, at); err != nil {
			return err
		}
		if admission.Assessor == record.Actor {
			return errors.New("optimizer capability admission assessor must be independent from lifecycle actor")
		}
	}
	current, err := store.Current(coordinate)
	if err != nil {
		return err
	}
	if current.ID != record.ID {
		return errors.New("optimizer capability use supplied a stale lifecycle record")
	}
	return nil
}

func verifyCapabilityAdmissionForUse(record OptimizerCapabilityAdmissionRecord, coordinate OptimizerCapabilityCoordinate, at time.Time) error {
	if err := VerifyOptimizerCapabilityAdmissionRecord(record); err != nil {
		return err
	}
	coordinate.Capabilities = append([]string(nil), coordinate.Capabilities...)
	sort.Strings(coordinate.Capabilities)
	if !reflect.DeepEqual(record.Coordinate, coordinate) {
		return errors.New("optimizer capability admission does not match immutable coordinate")
	}
	assessedAt, _ := time.Parse(time.RFC3339Nano, record.AssessedAt)
	validUntil, _ := time.Parse(time.RFC3339Nano, record.ValidUntil)
	at = at.UTC()
	if at.Before(assessedAt) || !at.Before(validUntil) {
		return errors.New("optimizer capability admission is not valid at selection time")
	}
	return nil
}

// VerifyDiscreteBackendSelection is the composition-root gate for a concrete
// non-native backend. It binds the executable descriptor to the immutable
// lifecycle coordinate and requires the current admission head before the
// backend can be included in a proposal-influencing solver portfolio. Native is
// the built-in disconnected reference and has no external artifact lifecycle.
func (store FileOptimizerCapabilityLifecycleStore) VerifyDiscreteBackendSelection(backend DiscreteSolverBackend, record OptimizerCapabilityLifecycleRecord, at time.Time) error {
	if backend == nil {
		return errors.New("nil discrete solver backend")
	}
	descriptor := canonicalBackendDescriptor(backend.Descriptor())
	if err := validateBackendDescriptor(descriptor); err != nil {
		return err
	}
	if descriptor.Name == NativeBackendName {
		return errors.New("native reference backend does not use external capability admission")
	}
	coordinate := OptimizerCapabilityCoordinate{
		Kind: OptimizerCapabilityBackend, Name: descriptor.Name, Version: descriptor.Version,
		ArtifactDigest: descriptor.ExecutableDigest, Architecture: descriptor.Architecture,
		Capabilities: append([]string(nil), descriptor.Capabilities...),
	}
	return store.VerifyCurrentUseAt(record, coordinate, false, at)
}

// VerifyContinuousBackendSelection is the composition-root gate for an
// external allocation backend. It mirrors the discrete path so a HiGHS (or
// future LP/MIP) backend cannot bypass the same immutable-coordinate,
// current-admission, and lifecycle checks merely because it uses the
// continuous solver contract.
func (store FileOptimizerCapabilityLifecycleStore) VerifyContinuousBackendSelection(backend SolverBackend, record OptimizerCapabilityLifecycleRecord, at time.Time) error {
	if backend == nil {
		return errors.New("nil continuous solver backend")
	}
	descriptor := canonicalBackendDescriptor(backend.Descriptor())
	if err := validateBackendDescriptor(descriptor); err != nil {
		return err
	}
	if descriptor.Name == NativeBackendName {
		return errors.New("native reference backend does not use external capability admission")
	}
	coordinate := OptimizerCapabilityCoordinate{
		Kind: OptimizerCapabilityBackend, Name: descriptor.Name, Version: descriptor.Version,
		ArtifactDigest: descriptor.ExecutableDigest, Architecture: descriptor.Architecture,
		Capabilities: append([]string(nil), descriptor.Capabilities...),
	}
	return store.VerifyCurrentUseAt(record, coordinate, false, at)
}

// VerifyProviderSelection is the composition-root gate for a provider
// adapter. Provider implementations intentionally do not share a solver
// interface, so the immutable provider coordinate is supplied explicitly and
// still passes through the same current-head, admission, expiry, and actor
// separation checks before its facts may influence a recommendation.
func (store FileOptimizerCapabilityLifecycleStore) VerifyProviderSelection(coordinate OptimizerCapabilityCoordinate, record OptimizerCapabilityLifecycleRecord, at time.Time) error {
	if coordinate.Kind != OptimizerCapabilityProvider {
		return errors.New("provider selection requires a PROVIDER capability coordinate")
	}
	return store.VerifyCurrentUseAt(record, coordinate, false, at)
}

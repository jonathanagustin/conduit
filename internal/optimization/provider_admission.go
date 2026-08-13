package optimization

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

const ProviderCaptureAPIVersion = "optimization.platform.example/provider-capture/v1alpha1"

// ProviderCaptureReceipt is the sanitized, content-addressed evidence needed
// for the SANITIZED_LIVE_CAPTURE admission gate. It records only scope and
// cryptographic identities; raw response bodies, credentials, user IDs, and
// headers are deliberately not representable.
type ProviderCaptureReceipt struct {
	APIVersion        string                        `json:"apiVersion"`
	Kind              string                        `json:"kind"`
	ID                string                        `json:"id"`
	Coordinate        OptimizerCapabilityCoordinate `json:"coordinate"`
	Environment       string                        `json:"environment"`
	Plan              string                        `json:"plan,omitempty"`
	SourceRevision    string                        `json:"sourceRevision"`
	RequestDigest     string                        `json:"requestDigest"`
	ResponseDigest    string                        `json:"responseDigest"`
	CollectorIdentity string                        `json:"collectorIdentity"`
	Scopes            []string                      `json:"scopes"`
	Method            string                        `json:"method"`
	EndpointClass     string                        `json:"endpointClass"`
	ReadOnly          bool                          `json:"readOnly"`
	Redacted          bool                          `json:"redacted"`
	ObservedAt        string                        `json:"observedAt"`
	ValidUntil        string                        `json:"validUntil"`
}

func NewProviderCaptureReceipt(coordinate OptimizerCapabilityCoordinate, environment, plan, sourceRevision, requestDigest, responseDigest, collector string, scopes []string, method, endpointClass string, observedAt, validUntil time.Time) (ProviderCaptureReceipt, error) {
	coordinate.Capabilities = append([]string(nil), coordinate.Capabilities...)
	receipt := ProviderCaptureReceipt{
		APIVersion: ProviderCaptureAPIVersion, Kind: "ProviderCaptureReceipt", Coordinate: coordinate,
		Environment: environment, Plan: plan, SourceRevision: sourceRevision,
		RequestDigest: requestDigest, ResponseDigest: responseDigest, CollectorIdentity: collector,
		Scopes: append([]string(nil), scopes...), Method: method, EndpointClass: endpointClass,
		ReadOnly: true, Redacted: true, ObservedAt: observedAt.UTC().Format(time.RFC3339Nano),
		ValidUntil: validUntil.UTC().Format(time.RFC3339Nano),
	}
	sort.Strings(receipt.Coordinate.Capabilities)
	sort.Strings(receipt.Scopes)
	if err := validateProviderCaptureReceipt(receipt); err != nil {
		return ProviderCaptureReceipt{}, err
	}
	receipt.ID = providerCaptureReceiptID(receipt)
	return receipt, nil
}

func VerifyProviderCaptureReceipt(receipt ProviderCaptureReceipt) error {
	if err := validateProviderCaptureReceipt(receipt); err != nil {
		return err
	}
	if providerCaptureReceiptID(receipt) != receipt.ID {
		return errors.New("provider capture receipt id does not match content")
	}
	return nil
}

func validateProviderCaptureReceipt(receipt ProviderCaptureReceipt) error {
	if receipt.APIVersion != ProviderCaptureAPIVersion || receipt.Kind != "ProviderCaptureReceipt" || (receipt.ID != "" && !validContentID(receipt.ID, "pcap_")) {
		return errors.New("unsupported provider capture receipt")
	}
	if !validProviderCaptureCoordinate(receipt.Coordinate) || !validEvidenceCoordinate(receipt.Environment) || (receipt.Plan != "" && !validEvidenceCoordinate(receipt.Plan)) || !validEvidenceCoordinate(receipt.SourceRevision) || !validDigest(receipt.RequestDigest) || !validDigest(receipt.ResponseDigest) || !validObservationText(receipt.CollectorIdentity) || !validObservationText(receipt.Method) || !validObservationText(receipt.EndpointClass) {
		return errors.New("provider capture receipt has incomplete scope or digest identity")
	}
	if receipt.Method != "GET" || receipt.EndpointClass == "" || !receipt.ReadOnly || !receipt.Redacted || len(receipt.Scopes) == 0 || len(receipt.Scopes) > 32 {
		return errors.New("provider capture receipt is not bounded read-only redacted evidence")
	}
	for i, scope := range receipt.Scopes {
		if !validEvidenceCoordinate(scope) || (i > 0 && receipt.Scopes[i-1] >= scope) {
			return errors.New("provider capture scopes must be unique canonical coordinates")
		}
	}
	observed, err := time.Parse(time.RFC3339Nano, receipt.ObservedAt)
	if err != nil {
		return fmt.Errorf("provider capture observedAt: %w", err)
	}
	validUntil, err := time.Parse(time.RFC3339Nano, receipt.ValidUntil)
	if err != nil || !validUntil.After(observed) || validUntil.Sub(observed) > 90*24*time.Hour {
		return errors.New("provider capture requires a finite validity window")
	}
	return nil
}

func validProviderCaptureCoordinate(coordinate OptimizerCapabilityCoordinate) bool {
	if coordinate.Kind != OptimizerCapabilityProvider || !validObservationText(coordinate.Name) || !validObservationText(coordinate.Version) || !validDigest(coordinate.ArtifactDigest) || !validObservationText(coordinate.Architecture) || len(coordinate.Capabilities) == 0 || len(coordinate.Capabilities) > 64 {
		return false
	}
	for index, capability := range coordinate.Capabilities {
		if !validObservationText(capability) || (index > 0 && coordinate.Capabilities[index-1] >= capability) {
			return false
		}
	}
	return true
}

func providerCaptureReceiptID(receipt ProviderCaptureReceipt) string {
	receipt.ID = ""
	encoded, _ := json.Marshal(receipt)
	digest := sha256.Sum256(encoded)
	return "pcap_" + hex.EncodeToString(digest[:])
}

// VerifyProviderCaptureForAdmission binds sanitized capture evidence to the
// exact provider coordinate and lifecycle admission evidence without allowing
// the capture receipt itself to promote or select the provider.
func VerifyProviderCaptureForAdmission(receipt ProviderCaptureReceipt, coordinate OptimizerCapabilityCoordinate, environment, plan string) error {
	if err := VerifyProviderCaptureReceipt(receipt); err != nil {
		return err
	}
	coordinate.Capabilities = append([]string(nil), coordinate.Capabilities...)
	sort.Strings(coordinate.Capabilities)
	if !reflect.DeepEqual(receipt.Coordinate, coordinate) || receipt.Environment != environment || receipt.Plan != plan {
		return errors.New("provider capture is foreign to the admitted coordinate or scope")
	}
	return nil
}

// ProviderCaptureAdmissionEvidence converts a verified capture into the exact
// lifecycle gate shape. Its validity cannot outlive the capture itself, and
// the receipt digest is always the content-addressed capture identity.
func ProviderCaptureAdmissionEvidence(receipt ProviderCaptureReceipt, sourceRevisionDigest string, validUntil time.Time) (OptimizerCapabilityAdmissionEvidence, error) {
	if err := VerifyProviderCaptureReceipt(receipt); err != nil {
		return OptimizerCapabilityAdmissionEvidence{}, err
	}
	if !validDigest(sourceRevisionDigest) {
		return OptimizerCapabilityAdmissionEvidence{}, errors.New("provider capture admission requires a valid source revision digest")
	}
	expectedSourceRevisionDigest, err := digestJSON(receipt.SourceRevision)
	if err != nil || sourceRevisionDigest != expectedSourceRevisionDigest {
		return OptimizerCapabilityAdmissionEvidence{}, errors.New("provider capture admission source revision digest does not bind to capture")
	}
	observed, _ := time.Parse(time.RFC3339Nano, receipt.ObservedAt)
	captureValidUntil, _ := time.Parse(time.RFC3339Nano, receipt.ValidUntil)
	validUntil = validUntil.UTC()
	if validUntil.Before(observed) || validUntil.After(captureValidUntil) {
		return OptimizerCapabilityAdmissionEvidence{}, errors.New("provider capture admission validity exceeds capture validity")
	}
	return OptimizerCapabilityAdmissionEvidence{
		Gate: OptimizerAdmissionSanitizedLiveCapture, ReceiptDigest: "sha256:" + strings.TrimPrefix(receipt.ID, "pcap_"),
		SourceRevisionDigest: sourceRevisionDigest, ObservedAt: receipt.ObservedAt,
		ValidUntil: validUntil.Format(time.RFC3339Nano),
	}, nil
}

// NewProviderCapabilityAdmissionRecord is the provider-specific composition
// helper. It refuses caller-authored SANITIZED_LIVE_CAPTURE evidence and
// derives that gate from the verified, scope-bound capture instead.
func NewProviderCapabilityAdmissionRecord(coordinate OptimizerCapabilityCoordinate, evidence []OptimizerCapabilityAdmissionEvidence, capture ProviderCaptureReceipt, sourceRevisionDigest, environment, plan, assessor, rationale string, assessedAt, validUntil time.Time) (OptimizerCapabilityAdmissionRecord, error) {
	if err := VerifyProviderCaptureForAdmission(capture, coordinate, environment, plan); err != nil {
		return OptimizerCapabilityAdmissionRecord{}, err
	}
	filtered := make([]OptimizerCapabilityAdmissionEvidence, 0, len(evidence)+1)
	for _, item := range evidence {
		if item.Gate == OptimizerAdmissionSanitizedLiveCapture {
			return OptimizerCapabilityAdmissionRecord{}, errors.New("provider admission must derive sanitized live capture evidence")
		}
		filtered = append(filtered, item)
	}
	captureEvidence, err := ProviderCaptureAdmissionEvidence(capture, sourceRevisionDigest, validUntil)
	if err != nil {
		return OptimizerCapabilityAdmissionRecord{}, err
	}
	filtered = append(filtered, captureEvidence)
	return NewOptimizerCapabilityAdmissionRecord(coordinate, filtered, assessor, rationale, assessedAt, validUntil)
}

// FileProviderCaptureStore persists only the sanitized receipt, never the
// provider response body or transport credentials.
type FileProviderCaptureStore struct{ Directory string }

func (store FileProviderCaptureStore) Save(receipt ProviderCaptureReceipt) error {
	if err := VerifyProviderCaptureReceipt(receipt); err != nil {
		return err
	}
	return FileTwinStore(store).save("provider-captures", receipt.ID, receipt)
}

func (store FileProviderCaptureStore) Get(id string) (ProviderCaptureReceipt, error) {
	var receipt ProviderCaptureReceipt
	if !validContentID(id, "pcap_") {
		return receipt, errors.New("invalid provider capture receipt id")
	}
	if err := FileTwinStore(store).get("provider-captures", id, &receipt); err != nil {
		return receipt, err
	}
	return receipt, VerifyProviderCaptureReceipt(receipt)
}

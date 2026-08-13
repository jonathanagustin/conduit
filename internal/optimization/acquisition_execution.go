package optimization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	AcquisitionExecutionAPIVersion = "optimization.platform.example/acquisition-execution/v1alpha1"

	AcquisitionCallComplete    = "COMPLETE"
	AcquisitionCallPartial     = "PARTIAL"
	AcquisitionCallStale       = "STALE"
	AcquisitionCallUnavailable = "UNAVAILABLE"

	AcquisitionFailureAuthentication = "AUTHENTICATION"
	AcquisitionFailureAuthorization  = "AUTHORIZATION"
	AcquisitionFailureNotFound       = "NOT_FOUND"
	AcquisitionFailureRateLimited    = "RATE_LIMITED"
	AcquisitionFailureTimeout        = "TIMEOUT"
	AcquisitionFailureTransport      = "TRANSPORT"
	AcquisitionFailureProvider       = "PROVIDER"
	AcquisitionFailureUnknown        = "UNKNOWN"

	defaultMaximumAcquisitionResponseBytes = 1 << 20
)

// AcquisitionReadPort is the outbound hexagonal port. Implementations own
// credentials and transport configuration; neither crosses this boundary or
// enters an acquisition receipt.
type AcquisitionReadPort interface {
	Read(context.Context, AcquisitionReadRequest) (AcquisitionReadResponse, error)
}

// AcquisitionReadError lets an adapter expose a bounded operational category
// without returning provider text, credentials, or tenant payloads in durable
// evidence. Unknown error implementations are reduced to UNKNOWN.
type AcquisitionReadError struct {
	Category  string
	Retryable bool
}

func (readError AcquisitionReadError) Error() string {
	return "acquisition read failed: " + readError.Category
}

// AcquisitionFactExtractor is a provider-specific anti-corruption layer. It
// may inspect the bounded response body, but only normalized facts leave the
// boundary. The executor never persists the raw response.
type AcquisitionFactExtractor interface {
	Extract(AcquisitionReadRequest, AcquisitionReadResponse) ([]ExtractedAcquisitionFact, error)
}

type TemporalFactSink interface {
	SaveFact(TemporalFact) error
}

type AcquisitionReadRequest struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	Method      string `json:"method"`
	Endpoint    string `json:"endpoint"`
	Consistency string `json:"consistency"`
}

type AcquisitionReadResponse struct {
	StatusCode     int    `json:"statusCode"`
	ObservedAt     string `json:"observedAt"`
	SourceRevision string `json:"sourceRevision"`
	Body           []byte `json:"-"`
}

type ExtractedAcquisitionFact struct {
	RequirementID string          `json:"requirementId"`
	EntityID      string          `json:"entityId"`
	Attribute     string          `json:"attribute"`
	Value         json.RawMessage `json:"value"`
	Unit          string          `json:"unit"`
	Plane         StatePlane      `json:"plane"`
	Fidelity      Fidelity        `json:"fidelity"`
	ValidFrom     string          `json:"validFrom"`
	ValidUntil    string          `json:"validUntil,omitempty"`
	Uncertainty   string          `json:"uncertainty,omitempty"`
}

type AcquisitionCallExecutionReceipt struct {
	CallID                string   `json:"callId"`
	Source                string   `json:"source"`
	Status                string   `json:"status"`
	ObservedAt            string   `json:"observedAt,omitempty"`
	RecordedAt            string   `json:"recordedAt"`
	SourceRevision        string   `json:"sourceRevision,omitempty"`
	ProviderStatusCode    int      `json:"providerStatusCode,omitempty"`
	FailureCategory       string   `json:"failureCategory,omitempty"`
	Retryable             bool     `json:"retryable,omitempty"`
	RequestDigest         string   `json:"requestDigest"`
	ResponseDigest        string   `json:"responseDigest,omitempty"`
	CollectorIdentity     string   `json:"collectorIdentity"`
	FactIDs               []string `json:"factIds,omitempty"`
	SatisfiedRequirements []string `json:"satisfiedRequirements,omitempty"`
	UnmetRequirements     []string `json:"unmetRequirements,omitempty"`
}

type AcquisitionExecutionReceipt struct {
	APIVersion        string                            `json:"apiVersion"`
	Kind              string                            `json:"kind"`
	ID                string                            `json:"id"`
	Strategy          string                            `json:"strategy"`
	PlannedComplete   bool                              `json:"plannedComplete"`
	Complete          bool                              `json:"complete"`
	StartedAt         string                            `json:"startedAt"`
	FinishedAt        string                            `json:"finishedAt"`
	CollectorIdentity string                            `json:"collectorIdentity"`
	RequirementIDs    []string                          `json:"requirementIds"`
	Calls             []AcquisitionCallExecutionReceipt `json:"calls"`
	FactIDs           []string                          `json:"factIds,omitempty"`
	UnmetRequirements []string                          `json:"unmetRequirements,omitempty"`
}

// AcquisitionExecutor is an application service over read, extraction, and
// immutable fact-store ports. It has no delivery-control-plane dependency and
// cannot approve, issue, lease, or execute a Plan.
type AcquisitionExecutor struct {
	Reader               AcquisitionReadPort
	Extractor            AcquisitionFactExtractor
	Facts                TemporalFactSink
	CollectorIdentity    string
	MaximumResponseBytes int
	Now                  func() time.Time
}

// Execute independently chooses the bounded least-cost call set, admits only
// resolved GET operations, derives runtime freshness from extracted valid time,
// and persists content-addressed facts. Transport failure produces explicit
// UNAVAILABLE evidence; malformed provenance or extraction fails closed.
func (executor AcquisitionExecutor) Execute(ctx context.Context, requirements []StateRequirement, candidates []AcquisitionCall) (AcquisitionExecutionReceipt, error) {
	if ctx == nil {
		return AcquisitionExecutionReceipt{}, errors.New("acquisition execution requires a context")
	}
	if executor.Reader == nil || executor.Extractor == nil || executor.Facts == nil {
		return AcquisitionExecutionReceipt{}, errors.New("acquisition execution requires reader, extractor, and fact sink ports")
	}
	if !validObservationText(executor.CollectorIdentity) {
		return AcquisitionExecutionReceipt{}, errors.New("acquisition execution requires a bounded collector identity")
	}
	if executor.Now == nil {
		executor.Now = time.Now
	}
	maximumResponseBytes := executor.MaximumResponseBytes
	if maximumResponseBytes == 0 {
		maximumResponseBytes = defaultMaximumAcquisitionResponseBytes
	}
	if maximumResponseBytes < 1 || maximumResponseBytes > defaultMaximumAcquisitionResponseBytes {
		return AcquisitionExecutionReceipt{}, fmt.Errorf("maximum acquisition response bytes must be between 1 and %d", defaultMaximumAcquisitionResponseBytes)
	}
	requirementsByID, err := validateExecutableAcquisition(requirements, candidates)
	if err != nil {
		return AcquisitionExecutionReceipt{}, err
	}
	plan, err := OptimizeAcquisitionCalls(requirements, candidates)
	if err != nil {
		return AcquisitionExecutionReceipt{}, err
	}
	startedAt := executor.Now().UTC()
	if startedAt.IsZero() {
		return AcquisitionExecutionReceipt{}, errors.New("acquisition execution clock returned zero time")
	}
	receipt := AcquisitionExecutionReceipt{
		APIVersion:        AcquisitionExecutionAPIVersion,
		Kind:              "AcquisitionExecutionReceipt",
		Strategy:          plan.Strategy,
		PlannedComplete:   plan.Complete,
		StartedAt:         startedAt.Format(time.RFC3339Nano),
		CollectorIdentity: executor.CollectorIdentity,
		UnmetRequirements: append([]string(nil), plan.Unknown...),
	}
	unmet := make(map[string]bool, len(requirements))
	for _, requirement := range requirements {
		unmet[requirement.ID] = true
		receipt.RequirementIDs = append(receipt.RequirementIDs, requirement.ID)
	}
	sort.Strings(receipt.RequirementIDs)
	for _, call := range plan.Calls {
		if err := ctx.Err(); err != nil {
			return AcquisitionExecutionReceipt{}, err
		}
		callReceipt, facts, err := executor.executeCall(ctx, call, requirementsByID, maximumResponseBytes)
		if err != nil {
			return AcquisitionExecutionReceipt{}, err
		}
		for _, fact := range facts {
			if err := executor.Facts.SaveFact(fact); err != nil {
				return AcquisitionExecutionReceipt{}, fmt.Errorf("save acquisition fact %s: %w", fact.ID, err)
			}
			receipt.FactIDs = append(receipt.FactIDs, fact.ID)
		}
		for _, requirementID := range callReceipt.SatisfiedRequirements {
			delete(unmet, requirementID)
		}
		receipt.Calls = append(receipt.Calls, callReceipt)
	}
	receipt.UnmetRequirements = receipt.UnmetRequirements[:0]
	for requirementID := range unmet {
		receipt.UnmetRequirements = append(receipt.UnmetRequirements, requirementID)
	}
	sort.Strings(receipt.FactIDs)
	sort.Strings(receipt.UnmetRequirements)
	receipt.Complete = len(receipt.UnmetRequirements) == 0
	finishedAt := executor.Now().UTC()
	if finishedAt.Before(startedAt) {
		return AcquisitionExecutionReceipt{}, errors.New("acquisition execution clock moved backwards")
	}
	receipt.FinishedAt = finishedAt.Format(time.RFC3339Nano)
	id, err := acquisitionExecutionReceiptID(receipt)
	if err != nil {
		return AcquisitionExecutionReceipt{}, err
	}
	receipt.ID = id
	return receipt, VerifyAcquisitionExecutionReceipt(receipt)
}

func (executor AcquisitionExecutor) executeCall(ctx context.Context, call AcquisitionCall, requirements map[string]StateRequirement, maximumResponseBytes int) (AcquisitionCallExecutionReceipt, []TemporalFact, error) {
	request := AcquisitionReadRequest{ID: call.ID, Source: call.Source, Method: call.Method, Endpoint: call.Endpoint, Consistency: call.Consistency}
	requestDigest, err := acquisitionCallDigest(call)
	if err != nil {
		return AcquisitionCallExecutionReceipt{}, nil, err
	}
	expected := acquisitionCallRequirementIDs(call)
	response, readErr := executor.Reader.Read(ctx, request)
	recordedAt := executor.Now().UTC()
	if recordedAt.IsZero() {
		return AcquisitionCallExecutionReceipt{}, nil, errors.New("acquisition execution clock returned zero time")
	}
	receipt := AcquisitionCallExecutionReceipt{
		CallID:            call.ID,
		Source:            call.Source,
		Status:            AcquisitionCallUnavailable,
		RecordedAt:        recordedAt.Format(time.RFC3339Nano),
		RequestDigest:     requestDigest,
		CollectorIdentity: executor.CollectorIdentity,
		UnmetRequirements: append([]string(nil), expected...),
	}
	if readErr != nil && ctx.Err() != nil {
		return AcquisitionCallExecutionReceipt{}, nil, ctx.Err()
	}
	if readErr != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		receipt.ProviderStatusCode = response.StatusCode
		receipt.FailureCategory, receipt.Retryable = classifyAcquisitionFailure(readErr, response.StatusCode)
		return receipt, nil, nil
	}
	if len(response.Body) == 0 || len(response.Body) > maximumResponseBytes {
		return AcquisitionCallExecutionReceipt{}, nil, fmt.Errorf("acquisition call %q response body must be between 1 and %d bytes", call.ID, maximumResponseBytes)
	}
	observedAt, err := time.Parse(time.RFC3339Nano, response.ObservedAt)
	if err != nil || observedAt.After(recordedAt) {
		return AcquisitionCallExecutionReceipt{}, nil, fmt.Errorf("acquisition call %q observedAt must be RFC3339 and not in the future", call.ID)
	}
	if !validObservationText(response.SourceRevision) {
		return AcquisitionCallExecutionReceipt{}, nil, fmt.Errorf("acquisition call %q requires a bounded source revision", call.ID)
	}
	body := append([]byte(nil), response.Body...)
	responseDigest := sha256Digest(body)
	response.Body = append([]byte(nil), body...)
	extracted, err := executor.Extractor.Extract(request, response)
	if err != nil {
		return AcquisitionCallExecutionReceipt{}, nil, fmt.Errorf("acquisition call %q extractor rejected the response", call.ID)
	}
	facts, satisfied, stale, err := buildAcquisitionFacts(call, requirements, extracted, response, recordedAt, requestDigest, responseDigest, executor.CollectorIdentity)
	if err != nil {
		return AcquisitionCallExecutionReceipt{}, nil, err
	}
	receipt.ObservedAt = observedAt.UTC().Format(time.RFC3339Nano)
	receipt.SourceRevision = response.SourceRevision
	receipt.ProviderStatusCode = response.StatusCode
	receipt.ResponseDigest = responseDigest
	for _, fact := range facts {
		receipt.FactIDs = append(receipt.FactIDs, fact.ID)
	}
	receipt.SatisfiedRequirements = append(receipt.SatisfiedRequirements, satisfied...)
	receipt.UnmetRequirements = difference(expected, satisfied)
	switch {
	case len(receipt.UnmetRequirements) == 0:
		receipt.Status = AcquisitionCallComplete
	case stale && len(extracted) == len(expected):
		receipt.Status = AcquisitionCallStale
	default:
		receipt.Status = AcquisitionCallPartial
	}
	return receipt, facts, nil
}

func validateExecutableAcquisition(requirements []StateRequirement, calls []AcquisitionCall) (map[string]StateRequirement, error) {
	requirementsByID := make(map[string]StateRequirement, len(requirements))
	for _, requirement := range requirements {
		if err := validateRequirement(requirement); err != nil {
			return nil, err
		}
		if int64(requirement.MaximumStalenessSeconds) > (1<<63-1)/int64(time.Second) {
			return nil, fmt.Errorf("requirement %q maximum staleness overflows time duration", requirement.ID)
		}
		if _, exists := requirementsByID[requirement.ID]; exists {
			return nil, fmt.Errorf("duplicate requirement %q", requirement.ID)
		}
		requirementsByID[requirement.ID] = requirement
	}
	for _, call := range calls {
		if !validEvidenceCoordinate(call.ID) || !validEvidenceCoordinate(call.Source) || !validEvidenceCoordinate(call.Method) ||
			!validEvidenceCoordinate(call.Consistency) {
			return nil, fmt.Errorf("acquisition call %q has an invalid bounded identity or consistency", call.ID)
		}
		if call.Method != "GET" {
			return nil, fmt.Errorf("acquisition call %q is not an admitted read-only GET operation", call.ID)
		}
		if !strings.HasPrefix(call.Endpoint, "/") || strings.ContainsAny(call.Endpoint, "{}#") || strings.Contains(call.Endpoint, "://") {
			return nil, fmt.Errorf("acquisition call %q requires a resolved relative endpoint", call.ID)
		}
		if len(call.Observations) == 0 {
			return nil, fmt.Errorf("acquisition call %q has no observation contract", call.ID)
		}
		seen := map[string]bool{}
		for _, observation := range call.Observations {
			requirement, exists := requirementsByID[observation.Requirement]
			if !exists {
				return nil, fmt.Errorf("call %q observes unknown requirement %q", call.ID, observation.Requirement)
			}
			if seen[observation.Requirement] {
				return nil, fmt.Errorf("call %q repeats observation %q", call.ID, observation.Requirement)
			}
			seen[observation.Requirement] = true
			if observation.Plane != requirement.Plane || fidelityRank(observation.Fidelity) == 0 || observation.MaximumStalenessSeconds < 0 {
				return nil, fmt.Errorf("call %q has an invalid observation contract for %q", call.ID, observation.Requirement)
			}
		}
	}
	return requirementsByID, nil
}

func buildAcquisitionFacts(call AcquisitionCall, requirements map[string]StateRequirement, extracted []ExtractedAcquisitionFact, response AcquisitionReadResponse, recordedAt time.Time, requestDigest, responseDigest, collectorIdentity string) ([]TemporalFact, []string, bool, error) {
	declared := make(map[string]FieldObservation, len(call.Observations))
	for _, observation := range call.Observations {
		declared[observation.Requirement] = observation
	}
	facts := make([]TemporalFact, 0, len(extracted))
	satisfied := []string{}
	seen := map[string]bool{}
	stale := false
	responseObservedAt, _ := time.Parse(time.RFC3339Nano, response.ObservedAt)
	for _, value := range extracted {
		observation, exists := declared[value.RequirementID]
		if !exists {
			return nil, nil, false, fmt.Errorf("acquisition call %q extracted undeclared requirement %q", call.ID, value.RequirementID)
		}
		if seen[value.RequirementID] {
			return nil, nil, false, fmt.Errorf("acquisition call %q extracted requirement %q more than once", call.ID, value.RequirementID)
		}
		seen[value.RequirementID] = true
		requirement := requirements[value.RequirementID]
		if value.Plane != observation.Plane || value.Plane != requirement.Plane || value.Fidelity != observation.Fidelity || !fidelitySatisfies(requirement.MinimumFidelity, value.Fidelity) {
			return nil, nil, false, fmt.Errorf("acquisition call %q extracted incompatible plane or fidelity for %q", call.ID, value.RequirementID)
		}
		if !validObservationText(value.EntityID) || !validObservationText(value.Attribute) || !validObservationText(value.Unit) {
			return nil, nil, false, fmt.Errorf("acquisition call %q extracted invalid fact identity for %q", call.ID, value.RequirementID)
		}
		validFrom, err := time.Parse(time.RFC3339Nano, value.ValidFrom)
		if err != nil || validFrom.After(responseObservedAt) {
			return nil, nil, false, fmt.Errorf("acquisition call %q extracted invalid validFrom for %q", call.ID, value.RequirementID)
		}
		fresh := recordedAt.Sub(validFrom) <= time.Duration(requirement.MaximumStalenessSeconds)*time.Second
		if value.ValidUntil != "" {
			validUntil, parseErr := time.Parse(time.RFC3339Nano, value.ValidUntil)
			if parseErr != nil || !validUntil.After(validFrom) {
				return nil, nil, false, fmt.Errorf("acquisition call %q extracted invalid validUntil for %q", call.ID, value.RequirementID)
			}
			fresh = fresh && recordedAt.Before(validUntil)
		}
		fact, err := NewTemporalFact(TemporalFact{
			EntityID: value.EntityID, Attribute: value.Attribute, Value: append(json.RawMessage(nil), value.Value...), Unit: value.Unit,
			Plane: value.Plane, ValidFrom: validFrom.UTC().Format(time.RFC3339Nano), ValidUntil: value.ValidUntil,
			RecordedAt: recordedAt.Format(time.RFC3339Nano), Source: call.ID, SourceRevision: response.SourceRevision,
			RequestDigest: requestDigest, ResponseDigest: responseDigest, CollectorIdentity: collectorIdentity,
			Fidelity: value.Fidelity, Uncertainty: value.Uncertainty,
		})
		if err != nil {
			return nil, nil, false, fmt.Errorf("build acquisition fact for %q: %w", value.RequirementID, err)
		}
		facts = append(facts, fact)
		if fresh {
			satisfied = append(satisfied, value.RequirementID)
		} else {
			stale = true
		}
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
	sort.Strings(satisfied)
	return facts, satisfied, stale, nil
}

func VerifyAcquisitionExecutionReceipt(receipt AcquisitionExecutionReceipt) error {
	if receipt.APIVersion != AcquisitionExecutionAPIVersion || receipt.Kind != "AcquisitionExecutionReceipt" || !validContentID(receipt.ID, "acq_") {
		return errors.New("unsupported or invalid acquisition execution receipt")
	}
	startedAt, err := time.Parse(time.RFC3339Nano, receipt.StartedAt)
	if err != nil {
		return fmt.Errorf("acquisition receipt startedAt: %w", err)
	}
	finishedAt, err := time.Parse(time.RFC3339Nano, receipt.FinishedAt)
	if err != nil || finishedAt.Before(startedAt) {
		return errors.New("acquisition receipt finishedAt must be RFC3339 and not precede startedAt")
	}
	if !validObservationText(receipt.CollectorIdentity) || receipt.Strategy == "" || receipt.Complete != (len(receipt.UnmetRequirements) == 0) {
		return errors.New("acquisition receipt has invalid identity, strategy, or completeness")
	}
	if err := verifyCanonicalStrings(receipt.RequirementIDs); err != nil {
		return fmt.Errorf("acquisition receipt requirements: %w", err)
	}
	if err := verifyCanonicalIDs(receipt.FactIDs, "fact_"); err != nil {
		return fmt.Errorf("acquisition receipt facts: %w", err)
	}
	if err := verifyCanonicalStrings(receipt.UnmetRequirements); err != nil {
		return fmt.Errorf("acquisition receipt unmet requirements: %w", err)
	}
	knownRequirements := make(map[string]bool, len(receipt.RequirementIDs))
	for _, requirementID := range receipt.RequirementIDs {
		knownRequirements[requirementID] = true
	}
	allCallFactIDs := []string{}
	satisfiedRequirements := map[string]bool{}
	for index, call := range receipt.Calls {
		if index > 0 && receipt.Calls[index-1].CallID >= call.CallID {
			return errors.New("acquisition receipt calls must have unique canonical order")
		}
		if !validEvidenceCoordinate(call.CallID) || !validEvidenceCoordinate(call.Source) || call.CollectorIdentity != receipt.CollectorIdentity || !validDigest(call.RequestDigest) {
			return fmt.Errorf("acquisition call receipt %q has invalid identity or request provenance", call.CallID)
		}
		callRecordedAt, err := time.Parse(time.RFC3339Nano, call.RecordedAt)
		if err != nil || callRecordedAt.Before(startedAt) || callRecordedAt.After(finishedAt) {
			return fmt.Errorf("acquisition call receipt %q has invalid recordedAt", call.CallID)
		}
		switch call.Status {
		case AcquisitionCallComplete, AcquisitionCallPartial, AcquisitionCallStale:
			if call.ProviderStatusCode < 200 || call.ProviderStatusCode >= 300 || !validObservationText(call.SourceRevision) || !validDigest(call.ResponseDigest) {
				return fmt.Errorf("acquisition call receipt %q lacks response provenance", call.CallID)
			}
			if call.FailureCategory != "" || call.Retryable {
				return fmt.Errorf("acquisition call receipt %q combines success and failure evidence", call.CallID)
			}
			callObservedAt, err := time.Parse(time.RFC3339Nano, call.ObservedAt)
			if err != nil || callObservedAt.After(callRecordedAt) {
				return fmt.Errorf("acquisition call receipt %q has invalid observedAt", call.CallID)
			}
		case AcquisitionCallUnavailable:
			if call.ProviderStatusCode >= 200 && call.ProviderStatusCode < 300 {
				return fmt.Errorf("unavailable acquisition call receipt %q claims a successful provider status", call.CallID)
			}
			if call.ResponseDigest != "" || call.SourceRevision != "" || call.ObservedAt != "" || len(call.FactIDs) != 0 || len(call.SatisfiedRequirements) != 0 {
				return fmt.Errorf("unavailable acquisition call receipt %q claims observed evidence", call.CallID)
			}
			if !validAcquisitionFailureCategory(call.FailureCategory) {
				return fmt.Errorf("unavailable acquisition call receipt %q lacks a bounded failure category", call.CallID)
			}
		default:
			return fmt.Errorf("acquisition call receipt %q has invalid status", call.CallID)
		}
		if err := verifyCanonicalIDs(call.FactIDs, "fact_"); err != nil {
			return fmt.Errorf("acquisition call receipt %q facts: %w", call.CallID, err)
		}
		if err := verifyCanonicalStrings(call.SatisfiedRequirements); err != nil {
			return fmt.Errorf("acquisition call receipt %q satisfied requirements: %w", call.CallID, err)
		}
		if err := verifyCanonicalStrings(call.UnmetRequirements); err != nil {
			return fmt.Errorf("acquisition call receipt %q unmet requirements: %w", call.CallID, err)
		}
		if call.Status == AcquisitionCallComplete && len(call.UnmetRequirements) != 0 {
			return fmt.Errorf("complete acquisition call receipt %q retains unmet requirements", call.CallID)
		}
		if call.Status != AcquisitionCallComplete && len(call.UnmetRequirements) == 0 {
			return fmt.Errorf("incomplete acquisition call receipt %q lacks unmet requirements", call.CallID)
		}
		callSatisfied := make(map[string]bool, len(call.SatisfiedRequirements))
		for _, requirementID := range call.SatisfiedRequirements {
			callSatisfied[requirementID] = true
		}
		for _, requirementID := range call.UnmetRequirements {
			if callSatisfied[requirementID] {
				return fmt.Errorf("acquisition call receipt %q both satisfies and leaves requirement %q unmet", call.CallID, requirementID)
			}
		}
		allCallFactIDs = append(allCallFactIDs, call.FactIDs...)
		for _, requirementID := range call.SatisfiedRequirements {
			if !knownRequirements[requirementID] {
				return fmt.Errorf("acquisition call receipt %q satisfies unknown requirement %q", call.CallID, requirementID)
			}
			satisfiedRequirements[requirementID] = true
		}
		for _, requirementID := range call.UnmetRequirements {
			if !knownRequirements[requirementID] {
				return fmt.Errorf("acquisition call receipt %q references unknown unmet requirement %q", call.CallID, requirementID)
			}
		}
	}
	sort.Strings(allCallFactIDs)
	if strings.Join(allCallFactIDs, "\x00") != strings.Join(receipt.FactIDs, "\x00") {
		return errors.New("acquisition receipt fact set does not match its call receipts")
	}
	unmetRequirements := map[string]bool{}
	for _, requirementID := range receipt.UnmetRequirements {
		if !knownRequirements[requirementID] {
			return fmt.Errorf("acquisition receipt references unknown unmet requirement %q", requirementID)
		}
		unmetRequirements[requirementID] = true
	}
	for _, requirementID := range receipt.RequirementIDs {
		if satisfiedRequirements[requirementID] == unmetRequirements[requirementID] {
			return fmt.Errorf("acquisition receipt does not classify requirement %q exactly once", requirementID)
		}
	}
	id, err := acquisitionExecutionReceiptID(receipt)
	if err != nil {
		return err
	}
	if id != receipt.ID {
		return errors.New("acquisition execution receipt id does not match content")
	}
	return nil
}

func acquisitionExecutionReceiptID(receipt AcquisitionExecutionReceipt) (string, error) {
	receipt.ID = ""
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return "", fmt.Errorf("encode acquisition execution receipt identity: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "acq_" + hex.EncodeToString(digest[:]), nil
}

func acquisitionCallDigest(call AcquisitionCall) (string, error) {
	call.Observations = append([]FieldObservation(nil), call.Observations...)
	sort.Slice(call.Observations, func(i, j int) bool {
		left, right := call.Observations[i], call.Observations[j]
		if left.Requirement != right.Requirement {
			return left.Requirement < right.Requirement
		}
		if left.Plane != right.Plane {
			return left.Plane < right.Plane
		}
		return left.Fidelity < right.Fidelity
	})
	encoded, err := json.Marshal(call)
	if err != nil {
		return "", fmt.Errorf("encode acquisition call digest: %w", err)
	}
	return sha256Digest(encoded), nil
}

func sha256Digest(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func acquisitionCallRequirementIDs(call AcquisitionCall) []string {
	ids := make([]string, 0, len(call.Observations))
	for _, observation := range call.Observations {
		ids = append(ids, observation.Requirement)
	}
	sort.Strings(ids)
	return ids
}

func difference(all, present []string) []string {
	found := make(map[string]bool, len(present))
	for _, value := range present {
		found[value] = true
	}
	missing := []string{}
	for _, value := range all {
		if !found[value] {
			missing = append(missing, value)
		}
	}
	return missing
}

func classifyAcquisitionFailure(readErr error, statusCode int) (string, bool) {
	if readErr != nil {
		var typedPointer *AcquisitionReadError
		if errors.As(readErr, &typedPointer) && typedPointer != nil && validAcquisitionFailureCategory(typedPointer.Category) {
			return typedPointer.Category, typedPointer.Retryable
		}
		var typedValue AcquisitionReadError
		if errors.As(readErr, &typedValue) && validAcquisitionFailureCategory(typedValue.Category) {
			return typedValue.Category, typedValue.Retryable
		}
		return AcquisitionFailureUnknown, false
	}
	switch statusCode {
	case 401:
		return AcquisitionFailureAuthentication, false
	case 403:
		return AcquisitionFailureAuthorization, false
	case 404:
		return AcquisitionFailureNotFound, false
	case 408, 504:
		return AcquisitionFailureTimeout, true
	case 429:
		return AcquisitionFailureRateLimited, true
	default:
		if statusCode >= 500 {
			return AcquisitionFailureProvider, true
		}
		return AcquisitionFailureProvider, false
	}
}

func validAcquisitionFailureCategory(category string) bool {
	switch category {
	case AcquisitionFailureAuthentication, AcquisitionFailureAuthorization, AcquisitionFailureNotFound,
		AcquisitionFailureRateLimited, AcquisitionFailureTimeout, AcquisitionFailureTransport,
		AcquisitionFailureProvider, AcquisitionFailureUnknown:
		return true
	default:
		return false
	}
}

func verifyCanonicalIDs(values []string, prefix string) error {
	for index, value := range values {
		if !validContentID(value, prefix) {
			return fmt.Errorf("invalid content id %q", value)
		}
		if index > 0 && values[index-1] >= value {
			return errors.New("content ids must have unique canonical order")
		}
	}
	return nil
}

func verifyCanonicalStrings(values []string) error {
	for index, value := range values {
		if !validEvidenceCoordinate(value) {
			return fmt.Errorf("invalid coordinate %q", value)
		}
		if index > 0 && values[index-1] >= value {
			return errors.New("coordinates must have unique canonical order")
		}
	}
	return nil
}

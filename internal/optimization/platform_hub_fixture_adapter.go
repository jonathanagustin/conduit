package optimization

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const platformHubAcquisitionSource = "platform-hub"

// PlatformHubReadFixture is an immutable, credential-free capture presented to
// the fixture adapter. It deliberately has no URL, headers, cookies, client, or
// transport hook: this adapter can exercise shadow acquisition without gaining
// network or control-plane authority.
type PlatformHubReadFixture struct {
	Request         AcquisitionReadRequest `json:"request"`
	StatusCode      int                    `json:"statusCode,omitempty"`
	ObservedAt      string                 `json:"observedAt,omitempty"`
	Body            json.RawMessage        `json:"body,omitempty"`
	FailureCategory string                 `json:"failureCategory,omitempty"`
	Retryable       bool                   `json:"retryable,omitempty"`
}

// PlatformHubFixtureReadAdapter implements only the outbound read port over a
// closed set of supplied fixtures. A separately admitted live transport may be
// added later; it must not be smuggled through this type.
type PlatformHubFixtureReadAdapter struct {
	fixtures map[string]PlatformHubReadFixture
}

func NewPlatformHubFixtureReadAdapter(fixtures []PlatformHubReadFixture) (PlatformHubFixtureReadAdapter, error) {
	adapter := PlatformHubFixtureReadAdapter{fixtures: make(map[string]PlatformHubReadFixture, len(fixtures))}
	for _, fixture := range fixtures {
		if err := validatePlatformHubReadRequest(fixture.Request); err != nil {
			return PlatformHubFixtureReadAdapter{}, err
		}
		if fixture.StatusCode < 0 || fixture.StatusCode > 599 || fixture.StatusCode > 0 && fixture.StatusCode < 100 {
			return PlatformHubFixtureReadAdapter{}, fmt.Errorf("platform Hub fixture %q has invalid status", fixture.Request.ID)
		}
		if fixture.FailureCategory != "" {
			if fixture.StatusCode != 0 || len(fixture.Body) != 0 || !validAcquisitionFailureCategory(fixture.FailureCategory) {
				return PlatformHubFixtureReadAdapter{}, fmt.Errorf("platform Hub fixture %q has invalid failure evidence", fixture.Request.ID)
			}
		} else if fixture.StatusCode == 0 {
			return PlatformHubFixtureReadAdapter{}, fmt.Errorf("platform Hub fixture %q requires a status or bounded failure", fixture.Request.ID)
		}
		key := platformHubRequestKey(fixture.Request)
		if _, exists := adapter.fixtures[key]; exists {
			return PlatformHubFixtureReadAdapter{}, fmt.Errorf("duplicate platform Hub fixture %q", fixture.Request.ID)
		}
		fixture.Body = append(json.RawMessage(nil), fixture.Body...)
		adapter.fixtures[key] = fixture
	}
	return adapter, nil
}

func (adapter PlatformHubFixtureReadAdapter) Read(ctx context.Context, request AcquisitionReadRequest) (AcquisitionReadResponse, error) {
	if err := ctx.Err(); err != nil {
		return AcquisitionReadResponse{}, err
	}
	if err := validatePlatformHubReadRequest(request); err != nil {
		return AcquisitionReadResponse{}, AcquisitionReadError{Category: AcquisitionFailureProvider}
	}
	fixture, exists := adapter.fixtures[platformHubRequestKey(request)]
	if !exists {
		return AcquisitionReadResponse{}, AcquisitionReadError{Category: AcquisitionFailureProvider}
	}
	if fixture.FailureCategory != "" {
		return AcquisitionReadResponse{}, AcquisitionReadError{Category: fixture.FailureCategory, Retryable: fixture.Retryable}
	}
	response := AcquisitionReadResponse{
		StatusCode: fixture.StatusCode,
		ObservedAt: fixture.ObservedAt,
		Body:       append([]byte(nil), fixture.Body...),
	}
	if fixture.StatusCode >= http.StatusOK && fixture.StatusCode < http.StatusMultipleChoices {
		revision, err := platformHubSourceRevision(request, fixture.Body)
		if err != nil {
			return AcquisitionReadResponse{}, AcquisitionReadError{Category: AcquisitionFailureProvider}
		}
		response.SourceRevision = revision
	}
	return response, nil
}

// PlatformHubFactExtractor is the anti-corruption layer for the three
// repository-owned GET projections. It emits only bounded optimizer facts: no
// lease token, approval rationale, failure text, health-check message, raw Plan
// record, raw report, reported secret coordinate, arbitrary Environment config,
// or external Environment reference is retained. The desired projection keeps
// the immutable coordinate, Hub/Spoke ownership settings, and a digest of the
// complete Environment document so omitted fields remain detectable.
type PlatformHubFactExtractor struct{}

func (PlatformHubFactExtractor) Extract(request AcquisitionReadRequest, response AcquisitionReadResponse) ([]ExtractedAcquisitionFact, error) {
	if err := validatePlatformHubReadRequest(request); err != nil {
		return nil, err
	}
	switch request.ID {
	case "hub.environment":
		return extractPlatformHubEnvironment(request, response)
	case "hub.plan":
		return extractPlatformHubPlan(request, response)
	case "hub.reported-state":
		return extractPlatformHubReport(request, response)
	default:
		return nil, errors.New("unsupported platform Hub acquisition call")
	}
}

type platformHubEnvironmentDocument struct {
	Environment json.RawMessage `json:"environment"`
}

type platformHubEnvironmentIdentity struct {
	ID                    string `json:"id"`
	Generation            string `json:"generation"`
	ConnectionSetting     string `json:"connectionSetting"`
	DeclaredStateSource   string `json:"declaredStateSource"`
	DefaultReleaseChannel string `json:"defaultReleaseChannel"`
}

type platformHubEnvironmentValue struct {
	ID                    string `json:"id"`
	Generation            string `json:"generation"`
	ConnectionSetting     string `json:"connectionSetting,omitempty"`
	DeclaredStateSource   string `json:"declaredStateSource,omitempty"`
	DefaultReleaseChannel string `json:"defaultReleaseChannel,omitempty"`
	DocumentDigest        string `json:"documentDigest"`
}

type platformHubPlanDocument struct {
	ID   string `json:"id"`
	Plan struct {
		Environment string `json:"environment"`
		Generation  string `json:"generation"`
	} `json:"plan"`
	Status            string `json:"status"`
	RequiredApprovals int    `json:"requiredApprovals"`
	Approvals         []struct {
		Subject   string `json:"subject"`
		CreatedAt string `json:"createdAt"`
	} `json:"approvals"`
	Lease *struct {
		Attempt   int    `json:"attempt"`
		IssuedAt  string `json:"issuedAt"`
		ExpiresAt string `json:"expiresAt"`
	} `json:"lease"`
	Execution *struct {
		Attempt    int               `json:"attempt"`
		StartedAt  string            `json:"startedAt"`
		FinishedAt string            `json:"finishedAt"`
		Tasks      []json.RawMessage `json:"tasks"`
	} `json:"execution"`
	Cancellation *struct {
		CreatedAt string `json:"createdAt"`
	} `json:"cancellation"`
	UpdatedAt string `json:"updatedAt"`
	Revision  uint64 `json:"revision"`
}

type platformHubPlanAdmissionValue struct {
	Status            string `json:"status"`
	RequiredApprovals int    `json:"requiredApprovals"`
	ApprovalCount     int    `json:"approvalCount"`
	Revision          uint64 `json:"revision"`
}

type platformHubPlanExecutionValue struct {
	Status              string `json:"status"`
	Attempt             int    `json:"attempt,omitempty"`
	LeaseIssuedAt       string `json:"leaseIssuedAt,omitempty"`
	LeaseExpiresAt      string `json:"leaseExpiresAt,omitempty"`
	ExecutionStartedAt  string `json:"executionStartedAt,omitempty"`
	ExecutionFinishedAt string `json:"executionFinishedAt,omitempty"`
	ExecutionTaskCount  int    `json:"executionTaskCount,omitempty"`
	CancelledAt         string `json:"cancelledAt,omitempty"`
	Revision            uint64 `json:"revision"`
}

type platformHubReportDocument struct {
	Environment string `json:"environment"`
	PlanID      string `json:"planId"`
	ObservedAt  string `json:"observedAt"`
	Entities    []struct {
		InstallationID string `json:"installationId"`
		Product        string `json:"product"`
		Version        string `json:"version"`
		ReleaseDigest  string `json:"releaseDigest"`
		Namespace      string `json:"namespace"`
		ConfigDigest   string `json:"configDigest"`
		Health         string `json:"health"`
		RolloutStatus  string `json:"rolloutStatus"`
		ConfigStatus   string `json:"configStatus"`
		Lifecycle      string `json:"lifecycle"`
		ObservedAt     string `json:"observedAt"`
	} `json:"entities"`
	Secrets  []json.RawMessage `json:"secrets"`
	Revision uint64            `json:"revision"`
}

type platformHubReportedEntityValue struct {
	InstallationID string `json:"installationId"`
	Product        string `json:"product"`
	Version        string `json:"version"`
	ReleaseDigest  string `json:"releaseDigest"`
	Namespace      string `json:"namespace"`
	ConfigDigest   string `json:"configDigest"`
	Health         string `json:"health"`
	RolloutStatus  string `json:"rolloutStatus"`
	ConfigStatus   string `json:"configStatus"`
	Lifecycle      string `json:"lifecycle"`
	ObservedAt     string `json:"observedAt"`
}

type platformHubReportValue struct {
	PlanID      string                           `json:"planId,omitempty"`
	ObservedAt  string                           `json:"observedAt"`
	Revision    uint64                           `json:"revision"`
	Entities    []platformHubReportedEntityValue `json:"entities"`
	SecretCount int                              `json:"secretCount"`
}

func extractPlatformHubEnvironment(request AcquisitionReadRequest, response AcquisitionReadResponse) ([]ExtractedAcquisitionFact, error) {
	var document platformHubEnvironmentDocument
	if err := decodePlatformHubJSON(response.Body, &document); err != nil {
		return nil, err
	}
	var identity platformHubEnvironmentIdentity
	if err := decodePlatformHubJSON(document.Environment, &identity); err != nil || !validPlatformHubPathID(identity.ID) || !validPlatformHubRevisionCoordinate(identity.Generation) {
		return nil, errors.New("invalid platform Hub environment projection")
	}
	if !validOptionalPlatformHubText(identity.ConnectionSetting) || !validOptionalPlatformHubText(identity.DeclaredStateSource) || !validOptionalPlatformHubText(identity.DefaultReleaseChannel) {
		return nil, errors.New("invalid platform Hub environment ownership projection")
	}
	wantID, ok := platformHubEndpointID(request.Endpoint, "/v1/environments/", "")
	if !ok || identity.ID != wantID {
		return nil, errors.New("platform Hub environment identity does not match request")
	}
	revision, err := platformHubSourceRevision(request, response.Body)
	if err != nil || response.SourceRevision != revision {
		return nil, errors.New("platform Hub environment revision mismatch")
	}
	canonical, err := canonicalPlatformHubJSON(document.Environment)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(canonical)
	value, _ := json.Marshal(platformHubEnvironmentValue{
		ID: identity.ID, Generation: identity.Generation, ConnectionSetting: identity.ConnectionSetting,
		DeclaredStateSource: identity.DeclaredStateSource, DefaultReleaseChannel: identity.DefaultReleaseChannel,
		DocumentDigest: "sha256:" + hex.EncodeToString(digest[:]),
	})
	return []ExtractedAcquisitionFact{{
		RequirementID: "platform.desired.environment", EntityID: identity.ID, Attribute: "environment", Value: value,
		Unit: "document", Plane: StateDesired, Fidelity: FidelityExact, ValidFrom: response.ObservedAt,
	}}, nil
}

func extractPlatformHubPlan(request AcquisitionReadRequest, response AcquisitionReadResponse) ([]ExtractedAcquisitionFact, error) {
	var document platformHubPlanDocument
	if err := decodePlatformHubJSON(response.Body, &document); err != nil {
		return nil, err
	}
	wantID, ok := platformHubEndpointID(request.Endpoint, "/v1/plans/", "")
	if !ok || document.ID != wantID || !validPlatformHubPathID(document.ID) || !validPlatformHubPathID(document.Plan.Environment) || !validPlatformHubRevisionCoordinate(document.Plan.Generation) || document.Revision == 0 || document.RequiredApprovals < 0 {
		return nil, errors.New("invalid platform Hub plan projection")
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, document.UpdatedAt)
	if err != nil {
		return nil, errors.New("invalid platform Hub plan update time")
	}
	responseObservedAt, err := time.Parse(time.RFC3339Nano, response.ObservedAt)
	if err != nil || updatedAt.After(responseObservedAt) {
		return nil, errors.New("platform Hub plan update time exceeds observation time")
	}
	revision, err := platformHubSourceRevision(request, response.Body)
	if err != nil || response.SourceRevision != revision {
		return nil, errors.New("platform Hub plan revision mismatch")
	}
	if !validPlatformHubPlanStatus(document.Status) {
		return nil, errors.New("invalid platform Hub plan status")
	}
	if err := validatePlatformHubPlanLifecycle(document, responseObservedAt); err != nil {
		return nil, err
	}
	facts := make([]ExtractedAcquisitionFact, 0, 2)
	if document.Status != "RECORDED" {
		value, _ := json.Marshal(platformHubPlanAdmissionValue{Status: document.Status, RequiredApprovals: document.RequiredApprovals, ApprovalCount: len(document.Approvals), Revision: document.Revision})
		facts = append(facts, ExtractedAcquisitionFact{
			RequirementID: "platform.admitted.plan", EntityID: document.ID, Attribute: "admission-status", Value: value,
			Unit: "state", Plane: StateAdmitted, Fidelity: FidelityExact, ValidFrom: updatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	if platformHubPlanHasExecutionState(document) {
		value := platformHubPlanExecutionValue{Status: document.Status, Revision: document.Revision}
		if document.Lease != nil {
			value.Attempt, value.LeaseIssuedAt, value.LeaseExpiresAt = document.Lease.Attempt, document.Lease.IssuedAt, document.Lease.ExpiresAt
		}
		if document.Execution != nil {
			value.Attempt, value.ExecutionStartedAt, value.ExecutionFinishedAt = document.Execution.Attempt, document.Execution.StartedAt, document.Execution.FinishedAt
			value.ExecutionTaskCount = len(document.Execution.Tasks)
		}
		if document.Cancellation != nil {
			value.CancelledAt = document.Cancellation.CreatedAt
		}
		encoded, _ := json.Marshal(value)
		facts = append(facts, ExtractedAcquisitionFact{
			RequirementID: "platform.executing.plan", EntityID: document.ID, Attribute: "execution-status", Value: encoded,
			Unit: "state", Plane: StateExecuting, Fidelity: FidelityExact, ValidFrom: updatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return facts, nil
}

func extractPlatformHubReport(request AcquisitionReadRequest, response AcquisitionReadResponse) ([]ExtractedAcquisitionFact, error) {
	var document platformHubReportDocument
	if err := decodePlatformHubJSON(response.Body, &document); err != nil {
		return nil, err
	}
	wantID, ok := platformHubEndpointID(request.Endpoint, "/v1/environments/", "/reported-state")
	if !ok || document.Environment != wantID || !validPlatformHubPathID(document.Environment) || document.Revision == 0 {
		return nil, errors.New("invalid platform Hub reported-state projection")
	}
	reportObservedAt, err := time.Parse(time.RFC3339Nano, document.ObservedAt)
	if err != nil {
		return nil, errors.New("invalid platform Hub report observation time")
	}
	responseObservedAt, err := time.Parse(time.RFC3339Nano, response.ObservedAt)
	if err != nil || reportObservedAt.After(responseObservedAt) {
		return nil, errors.New("platform Hub report time exceeds observation time")
	}
	if document.PlanID != "" && !validPlatformHubPathID(document.PlanID) {
		return nil, errors.New("invalid platform Hub report plan identity")
	}
	revision, err := platformHubSourceRevision(request, response.Body)
	if err != nil || response.SourceRevision != revision {
		return nil, errors.New("platform Hub report revision mismatch")
	}
	oldest := reportObservedAt
	value := platformHubReportValue{
		PlanID: document.PlanID, ObservedAt: reportObservedAt.UTC().Format(time.RFC3339Nano), Revision: document.Revision,
		Entities: make([]platformHubReportedEntityValue, 0, len(document.Entities)), SecretCount: len(document.Secrets),
	}
	seen := make(map[string]bool, len(document.Entities))
	for _, entity := range document.Entities {
		if !validObservationText(entity.InstallationID) || seen[entity.InstallationID] || !validObservationText(entity.Product) ||
			!validObservationText(entity.Version) || !validObservationText(entity.Namespace) || !validObservationText(entity.Health) ||
			!validObservationText(entity.RolloutStatus) || !validObservationText(entity.ConfigStatus) || !validObservationText(entity.Lifecycle) ||
			!validOptionalPlatformHubText(entity.ReleaseDigest) || !validOptionalPlatformHubText(entity.ConfigDigest) {
			return nil, errors.New("invalid platform Hub reported entity")
		}
		seen[entity.InstallationID] = true
		observedAt, parseErr := time.Parse(time.RFC3339Nano, entity.ObservedAt)
		if parseErr != nil {
			return nil, errors.New("invalid platform Hub entity observation time")
		}
		if observedAt.After(responseObservedAt) || observedAt.After(reportObservedAt) {
			return nil, errors.New("platform Hub entity observation exceeds report time")
		}
		if observedAt.Before(oldest) {
			oldest = observedAt
		}
		value.Entities = append(value.Entities, platformHubReportedEntityValue(entity))
	}
	encoded, _ := json.Marshal(value)
	return []ExtractedAcquisitionFact{{
		RequirementID: "platform.observed.reported-state", EntityID: document.Environment, Attribute: "reported-state", Value: encoded,
		Unit: "document", Plane: StateObserved, Fidelity: FidelityExact, ValidFrom: oldest.UTC().Format(time.RFC3339Nano),
	}}, nil
}

func platformHubPlanHasExecutionState(document platformHubPlanDocument) bool {
	switch document.Status {
	case "LEASED", "RUNNING", "SUCCEEDED", "FAILED":
		return true
	case "CANCELLED":
		return document.Lease != nil || document.Execution != nil
	default:
		return false
	}
}

func validPlatformHubPlanStatus(status string) bool {
	switch status {
	case "AWAITING_APPROVAL", "APPROVED", "LEASED", "RUNNING", "SUCCEEDED", "FAILED", "CANCELLED", "RECORDED":
		return true
	default:
		return false
	}
}

func validatePlatformHubPlanLifecycle(document platformHubPlanDocument, observedAt time.Time) error {
	if document.Status == "RECORDED" {
		if document.RequiredApprovals != 0 || len(document.Approvals) != 0 || document.Lease != nil || document.Execution != nil || document.Cancellation != nil {
			return errors.New("recorded platform Hub plan claims execution lifecycle state")
		}
		return nil
	}
	if document.RequiredApprovals < 1 {
		return errors.New("platform Hub plan requires no admission")
	}
	approvalSubjects := make(map[string]bool, len(document.Approvals))
	for _, approval := range document.Approvals {
		createdAt, err := time.Parse(time.RFC3339Nano, approval.CreatedAt)
		if !validObservationText(approval.Subject) || approvalSubjects[approval.Subject] || err != nil || createdAt.After(observedAt) {
			return errors.New("invalid platform Hub plan approval projection")
		}
		approvalSubjects[approval.Subject] = true
	}
	if document.Lease != nil {
		issuedAt, issuedErr := time.Parse(time.RFC3339Nano, document.Lease.IssuedAt)
		expiresAt, expiresErr := time.Parse(time.RFC3339Nano, document.Lease.ExpiresAt)
		if document.Lease.Attempt < 1 || issuedErr != nil || expiresErr != nil || !expiresAt.After(issuedAt) || issuedAt.After(observedAt) {
			return errors.New("invalid platform Hub plan lease projection")
		}
	}
	if document.Execution != nil {
		startedAt, startedErr := time.Parse(time.RFC3339Nano, document.Execution.StartedAt)
		finishedAt, finishedErr := time.Parse(time.RFC3339Nano, document.Execution.FinishedAt)
		if document.Execution.Attempt < 1 || startedErr != nil || finishedErr != nil || finishedAt.Before(startedAt) || startedAt.After(observedAt) || finishedAt.After(observedAt) {
			return errors.New("invalid platform Hub plan execution projection")
		}
		if document.Lease == nil {
			return errors.New("platform Hub plan execution lacks lease state")
		}
	}
	if document.Cancellation != nil {
		cancelledAt, err := time.Parse(time.RFC3339Nano, document.Cancellation.CreatedAt)
		if err != nil || cancelledAt.After(observedAt) {
			return errors.New("invalid platform Hub plan cancellation projection")
		}
	}
	switch document.Status {
	case "AWAITING_APPROVAL":
		if document.Lease != nil || document.Execution != nil || document.Cancellation != nil {
			return errors.New("pre-execution platform Hub plan claims execution lifecycle state")
		}
		if len(document.Approvals) >= document.RequiredApprovals {
			return errors.New("awaiting platform Hub plan already satisfies approval policy")
		}
	case "APPROVED":
		if document.Lease != nil || document.Execution != nil || document.Cancellation != nil {
			return errors.New("pre-execution platform Hub plan claims execution lifecycle state")
		}
		if len(document.Approvals) < document.RequiredApprovals {
			return errors.New("approved platform Hub plan lacks approval evidence")
		}
	case "LEASED", "RUNNING":
		if document.Lease == nil {
			return errors.New("executing platform Hub plan lacks lease state")
		}
		if len(document.Approvals) < document.RequiredApprovals {
			return errors.New("executing platform Hub plan lacks approval evidence")
		}
	case "SUCCEEDED", "FAILED":
		if document.Lease == nil || document.Execution == nil {
			return errors.New("finished platform Hub plan lacks execution state")
		}
		if len(document.Approvals) < document.RequiredApprovals {
			return errors.New("finished platform Hub plan lacks approval evidence")
		}
	case "CANCELLED":
		if document.Cancellation == nil {
			return errors.New("cancelled platform Hub plan lacks cancellation state")
		}
	}
	return nil
}

func validatePlatformHubReadRequest(request AcquisitionReadRequest) error {
	if request.Source != platformHubAcquisitionSource || request.Method != http.MethodGet {
		return errors.New("platform Hub adapter accepts only platform-hub GET requests")
	}
	var prefix, suffix string
	switch request.ID {
	case "hub.environment":
		prefix = "/v1/environments/"
		if request.Consistency != "repository-read" {
			return errors.New("invalid platform Hub environment consistency")
		}
	case "hub.plan":
		prefix = "/v1/plans/"
		if request.Consistency != "repository-read" {
			return errors.New("invalid platform Hub plan consistency")
		}
	case "hub.reported-state":
		prefix, suffix = "/v1/environments/", "/reported-state"
		if request.Consistency != "exact-spoke-report" {
			return errors.New("invalid platform Hub report consistency")
		}
	default:
		return errors.New("unsupported platform Hub acquisition call")
	}
	if _, ok := platformHubEndpointID(request.Endpoint, prefix, suffix); !ok {
		return errors.New("invalid platform Hub acquisition endpoint")
	}
	return nil
}

func platformHubEndpointID(endpoint, prefix, suffix string) (string, bool) {
	if !strings.HasPrefix(endpoint, prefix) || !strings.HasSuffix(endpoint, suffix) {
		return "", false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(endpoint, prefix), suffix)
	return id, validPlatformHubPathID(id)
}

func platformHubRequestKey(request AcquisitionReadRequest) string {
	return strings.Join([]string{request.ID, request.Source, request.Method, request.Endpoint, request.Consistency}, "\x00")
}

func platformHubSourceRevision(request AcquisitionReadRequest, body []byte) (string, error) {
	switch request.ID {
	case "hub.environment":
		var document platformHubEnvironmentDocument
		if err := decodePlatformHubJSON(body, &document); err != nil {
			return "", err
		}
		var identity platformHubEnvironmentIdentity
		if err := decodePlatformHubJSON(document.Environment, &identity); err != nil || !validPlatformHubPathID(identity.ID) || !validPlatformHubRevisionCoordinate(identity.Generation) {
			return "", errors.New("invalid environment revision source")
		}
		canonical, err := canonicalPlatformHubJSON(document.Environment)
		if err != nil {
			return "", err
		}
		digest := sha256.Sum256(canonical)
		return "environment:" + identity.ID + ":" + identity.Generation + ":sha256:" + hex.EncodeToString(digest[:]), nil
	case "hub.plan":
		var document platformHubPlanDocument
		if err := decodePlatformHubJSON(body, &document); err != nil || !validPlatformHubPathID(document.ID) || document.Revision == 0 {
			return "", errors.New("invalid plan revision source")
		}
		return "plan:" + document.ID + ":r" + strconv.FormatUint(document.Revision, 10), nil
	case "hub.reported-state":
		var document platformHubReportDocument
		if err := decodePlatformHubJSON(body, &document); err != nil || !validPlatformHubPathID(document.Environment) || document.Revision == 0 {
			return "", errors.New("invalid report revision source")
		}
		return "report:" + document.Environment + ":r" + strconv.FormatUint(document.Revision, 10), nil
	default:
		return "", errors.New("unsupported platform Hub revision source")
	}
}

func validPlatformHubRevisionCoordinate(value string) bool {
	return len(value) <= 64 && validObservationText(value)
}

func validOptionalPlatformHubText(value string) bool {
	return value == "" || validObservationText(value)
}

func decodePlatformHubJSON(encoded []byte, destination any) error {
	if len(encoded) == 0 {
		return errors.New("empty platform Hub JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	if err := decoder.Decode(destination); err != nil {
		return errors.New("invalid platform Hub JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("platform Hub JSON contains a trailing value")
	}
	return nil
}

func canonicalPlatformHubJSON(encoded []byte) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, errors.New("invalid platform Hub JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("platform Hub JSON contains a trailing value")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("canonicalize platform Hub JSON")
	}
	return canonical, nil
}

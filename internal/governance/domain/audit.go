package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

const AuditSchemaV3 = "audit.3"

// DecisionAllow and DecisionDeny are the two verdicts an authorizationCheck
// records. They are spelled once because the record is read by matching on
// them, and a surface that filed "allow" would be invisible to every query.
const (
	DecisionAllow = "ALLOW"
	DecisionDeny  = "DENY"
)

const (
	AuditCategoryAPIGatewayRequest   = "apiGatewayRequest"
	AuditCategoryAuthenticationCheck = "authenticationCheck"
	AuditCategoryAuthorizationCheck  = "authorizationCheck"
	AuditCategoryRequestCreate       = "requestCreate"
	AuditCategoryRequestApprove      = "requestApprove"
	AuditCategoryRequestDisapprove   = "requestDisapprove"
	AuditCategoryRequestCancel       = "requestCancel"
	AuditCategoryRequestExecute      = "requestExecute"
	AuditCategoryRequestUpdate       = "requestUpdate"
	AuditCategoryCreateInfra         = "createInfra"
	AuditCategoryConfigureInfra      = "configureInfra"
	AuditCategoryUpgradeInfra        = "upgradeInfra"
)

type auditCategoryContract struct {
	RequiredRequest []string
	RequiredResult  []string
}

// auditCategoryContracts is intentionally bounded to categories emitted or
// consumed by Platform. It is not represented as a complete Foundry schema.
var auditCategoryContracts = map[string]auditCategoryContract{
	AuditCategoryAPIGatewayRequest:   {},
	AuditCategoryAuthenticationCheck: {RequiredResult: []string{"authenticationCheckResult"}},
	AuditCategoryAuthorizationCheck:  {RequiredRequest: []string{"authorizationCheckOperations"}, RequiredResult: []string{"authorizationCheckSucceededTargets", "authorizationCheckFailedTargets"}},
	AuditCategoryRequestCreate:       {RequiredRequest: []string{"createdRequestAffectedResources"}, RequiredResult: []string{"createdRequestIds"}},
	AuditCategoryRequestApprove:      {RequiredRequest: []string{"approvedRequestIds"}},
	AuditCategoryRequestDisapprove:   {RequiredRequest: []string{"disapprovedRequestIds"}},
	AuditCategoryRequestCancel:       {RequiredRequest: []string{"canceledRequestIds"}},
	AuditCategoryRequestExecute:      {RequiredRequest: []string{"executedRequestIds"}},
	AuditCategoryRequestUpdate:       {RequiredRequest: []string{"updatedRequestIds"}},
	AuditCategoryCreateInfra:         {RequiredRequest: []string{"createInfraTargets"}, RequiredResult: []string{"createdInfraResources"}},
	AuditCategoryConfigureInfra:      {RequiredRequest: []string{"configureInfraTargets"}, RequiredResult: []string{"configureInfraRequestId"}},
	AuditCategoryUpgradeInfra:        {RequiredRequest: []string{"upgradedResources"}},
}

// ValidateAuditCategoryEvent enforces the public audit.3 field contract for
// the bounded category subset Platform understands. Unknown categories fail
// closed instead of silently acquiring invented semantics.
func ValidateAuditCategoryEvent(event AuthorizationAuditEvent) error {
	if event.SchemaVersion != AuditSchemaV3 {
		return fmt.Errorf("audit schemaVersion must be %q", AuditSchemaV3)
	}
	if len(event.Categories) == 0 {
		return fmt.Errorf("audit.3 event requires at least one category")
	}
	seen := map[string]struct{}{}
	for _, category := range event.Categories {
		category = strings.TrimSpace(category)
		contract, known := auditCategoryContracts[category]
		if !known {
			return fmt.Errorf("audit category %q is outside Platform's pinned contract", category)
		}
		if _, duplicate := seen[category]; duplicate {
			return fmt.Errorf("duplicate audit category %q", category)
		}
		seen[category] = struct{}{}
		for _, field := range contract.RequiredRequest {
			if _, present := event.RequestFields[field]; !present {
				return fmt.Errorf("audit category %s requires requestFields.%s", category, field)
			}
		}
		for _, field := range contract.RequiredResult {
			if _, present := event.ResultFields[field]; !present {
				return fmt.Errorf("audit category %s requires resultFields.%s", category, field)
			}
		}
	}
	return nil
}

// NewAuthorizationCheckEvent is the audit.3 record of one authorization
// decision, and the only place its shape is decided.
//
// It lives beside the rules rather than beside a transport because more than
// one surface now reaches the same decision, and #241 is what the second one
// cost while it had no record at all: a resource read that the REST routes
// would have refused and filed happened on the notification routes with no
// trace of either the request or the verdict. One builder means a surface that
// reaches the decision cannot reach a different record of it.
//
// The identifier is derived from the decision and the instant rather than
// minted at random, so a store that dedupes on it collapses a retry of the same
// decision instead of filing it twice.
func NewAuthorizationCheckEvent(
	now time.Time, subject, action, resource, decision, reason string, eventContext map[string]any,
) AuthorizationAuditEvent {
	identity := sha256.Sum256([]byte(
		subject + "\x00" + action + "\x00" + resource + "\x00" + decision + "\x00" + now.Format(time.RFC3339Nano)))
	succeeded, failed := []string{}, []string{}
	if decision == DecisionAllow {
		succeeded = []string{resource}
	} else {
		failed = []string{resource}
	}
	return AuthorizationAuditEvent{
		ID: "aud_" + hex.EncodeToString(identity[:16]), SchemaVersion: AuditSchemaV3,
		Categories: []string{AuditCategoryAuthorizationCheck}, TraceID: resource,
		Subject: subject, Action: action, ResourceRID: resource, Decision: decision, Reason: reason,
		RequestFields: map[string]any{
			"authorizationCheckTargets":    []string{resource},
			"authorizationCheckOperations": []string{action},
		},
		ResultFields: map[string]any{
			"authorizationCheckSucceededTargets": succeeded,
			"authorizationCheckFailedTargets":    failed,
			"authorizationCheckResultMessage":    reason,
		},
		Context: eventContext, CreatedAt: now,
	}
}

// NormalizedAuditCategories trims, dedupes and orders the categories an event
// is filed under, so two events recording the same decision compare equal.
func NormalizedAuditCategories(categories []string) []string {
	result := normalizedStrings(categories)
	sort.Strings(result)
	return result
}

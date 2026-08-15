package domain

import (
	"fmt"
	"sort"
	"strings"
)

const AuditSchemaV3 = "audit.3"

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

// NormalizedAuditCategories trims, dedupes and orders the categories an event
// is filed under, so two events recording the same decision compare equal.
func NormalizedAuditCategories(categories []string) []string {
	result := normalizedStrings(categories)
	sort.Strings(result)
	return result
}

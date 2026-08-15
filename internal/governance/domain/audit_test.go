package domain

import "testing"

func TestAudit3CategoryContractRejectsMissingRequiredFields(t *testing.T) {
	t.Parallel()
	event := AuthorizationAuditEvent{
		SchemaVersion: AuditSchemaV3,
		Categories:    []string{AuditCategoryRequestCreate},
		RequestFields: map[string]any{"createdRequestAffectedResources": []string{"environment/dev"}},
		ResultFields:  map[string]any{},
	}
	if err := ValidateAuditCategoryEvent(event); err == nil {
		t.Fatal("requestCreate without createdRequestIds was accepted")
	}
	event.ResultFields["createdRequestIds"] = []string{"crq_example"}
	if err := ValidateAuditCategoryEvent(event); err != nil {
		t.Fatalf("valid bounded audit.3 event rejected: %v", err)
	}
}

func TestAudit3CategoryContractRejectsUnknownCategory(t *testing.T) {
	t.Parallel()
	event := AuthorizationAuditEvent{
		SchemaVersion: AuditSchemaV3, Categories: []string{"inventedApolloBehavior"},
		RequestFields: map[string]any{}, ResultFields: map[string]any{},
	}
	if err := ValidateAuditCategoryEvent(event); err == nil {
		t.Fatal("unknown audit category was accepted")
	}
}

package domain

import (
	"strings"
	"testing"
	"time"
)

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

// The record every surface files for an authorization decision has to satisfy
// the contract above without anyone remembering to check, and it has to put the
// resource on the side of the ledger the verdict says. A DENY filed with the
// target under succeeded would read, to whoever queried the audit afterwards,
// as the allow that never happened.
func TestAnAuthorizationCheckRecordSatisfiesTheContractForBothVerdicts(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 18, 9, 0, 0, 0, time.UTC)
	for _, verdict := range []struct{ decision, succeeded, failed string }{
		{decision: DecisionAllow, succeeded: "environment/secret-env"},
		{decision: DecisionDeny, failed: "environment/secret-env"},
	} {
		event := NewAuthorizationCheckEvent(now, "robot", OperationReadResource, "environment/secret-env",
			verdict.decision, "reason", map[string]any{"onBehalfOf": "alice"})
		if err := ValidateAuditCategoryEvent(event); err != nil {
			t.Fatalf("the record of a %s was rejected by the audit.3 contract: %v", verdict.decision, err)
		}
		succeeded, _ := event.ResultFields["authorizationCheckSucceededTargets"].([]string)
		failed, _ := event.ResultFields["authorizationCheckFailedTargets"].([]string)
		if strings.Join(succeeded, "") != verdict.succeeded || strings.Join(failed, "") != verdict.failed {
			t.Fatalf("a %s filed succeeded=%v failed=%v", verdict.decision, succeeded, failed)
		}
		if event.Subject != "robot" || event.Context["onBehalfOf"] != "alice" {
			t.Fatalf("the record does not name the carrier and its delegation: %#v", event)
		}
	}
	// Two decisions differing only in verdict are two records. The identifier is
	// derived rather than minted at random, and a store that dedupes on it would
	// otherwise keep the first and drop the reversal.
	allow := NewAuthorizationCheckEvent(now, "robot", OperationReadResource, "environment/secret-env",
		DecisionAllow, "reason", nil)
	deny := NewAuthorizationCheckEvent(now, "robot", OperationReadResource, "environment/secret-env",
		DecisionDeny, "reason", nil)
	if allow.ID == deny.ID {
		t.Fatalf("an ALLOW and a DENY collapsed onto one identifier %q", allow.ID)
	}
}

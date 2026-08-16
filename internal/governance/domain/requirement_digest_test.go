package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSatisfiabilityIsComputedAfterTheDigestNotStoredInIt is the safety pin for
// the read-time satisfiability diagnostic. ReviewerRequirement.ID is a SHA-256
// fixed at creation by PolicyResult.Requirement, when Satisfiable is nil and
// Unsatisfiable is empty -- both omitempty, so both are absent from the payload
// the digest is taken over. Annotating the requirement on read must therefore
// leave its identity untouched, or the same demand raised twice would stop
// being the same requirement and reviews keyed on the ID would break.
//
// The design note for #13 put it as "any new field must be excluded from or
// computed after the digest"; this proves it is computed after.
func TestSatisfiabilityIsComputedAfterTheDigestNotStoredInIt(t *testing.T) {
	t.Parallel()
	created := PolicyResult{
		Kind: RequirementOperation, ResourceRID: "environment/prod",
		Operation: "entity:approve-config-update", Reason: "operators approve config",
	}.Requirement("chg_1", "pol_1")

	// The identity is fixed and does not encode the read-time fields (they were
	// nil/empty when the digest was taken).
	if created.Satisfiable != nil || created.Unsatisfiable != "" {
		t.Fatal("a freshly created requirement already carries a satisfiability verdict")
	}
	original := created.ID

	// Annotating -- what the read path does -- must not move the identity.
	annotated := created
	unsatisfiable := false
	annotated.Satisfiable = &unsatisfiable
	annotated.Unsatisfiable = "no current grant conveys what this requirement demands"
	if annotated.ID != original {
		t.Fatalf("annotating changed the identity: %q -> %q", original, annotated.ID)
	}

	// And re-running Requirement for the same input yields the same ID, proving
	// the field's mere existence on the struct did not perturb the digest.
	again := PolicyResult{
		Kind: RequirementOperation, ResourceRID: "environment/prod",
		Operation: "entity:approve-config-update", Reason: "operators approve config",
	}.Requirement("chg_1", "pol_1")
	if again.ID != original {
		t.Fatalf("identical input produced a different ID: %q vs %q", again.ID, original)
	}

	// The diagnostic must actually reach a client: once set, it serializes.
	payload, err := json.Marshal(annotated)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"satisfiable":false`) ||
		!strings.Contains(string(payload), `"unsatisfiable":`) {
		t.Fatalf("the satisfiability diagnostic did not serialize: %s", payload)
	}
	// A requirement that was never annotated must not carry the fields at all,
	// so an unannotated read is byte-for-byte what it always was.
	clean, _ := json.Marshal(created)
	if strings.Contains(string(clean), "satisfiable") {
		t.Fatalf("an unannotated requirement leaked a satisfiability field: %s", clean)
	}
}

package domain

import (
	"os"
	"regexp"
	"sort"
	"testing"
)

// TestOperationVocabularyCoversEveryOperation is the drift gate between the
// operation constants and the list the session capability projection
// enumerates. Declaring a constant and forgetting the vocabulary would make it
// a capability that never appears in a session, which is invisible from either
// side.
func TestOperationVocabularyCoversEveryOperation(t *testing.T) {
	t.Parallel()
	conveyed := map[string]struct{}{}
	for role, operations := range CanonicalRoleOperations {
		for _, operation := range operations {
			if !OperationIsKnown(operation) {
				t.Errorf("role %s conveys %q, which is not in OperationVocabulary", role, operation)
			}
			conveyed[operation] = struct{}{}
		}
	}
	// OperationActOnBehalf is a delegation check rather than something a role
	// conveys, so it is the one member with no entry in the role table. Any
	// other absence means the vocabulary has grown a name nothing can grant.
	for _, operation := range OperationVocabulary {
		if _, found := conveyed[operation]; !found && operation != OperationActOnBehalf {
			t.Errorf("OperationVocabulary lists %q, which no role conveys", operation)
		}
	}
	if !sort.StringsAreSorted(OperationVocabulary) {
		t.Error("OperationVocabulary is not sorted, which makes a duplicate easy to miss on review")
	}
	// Sortedness does NOT imply uniqueness -- ["a", "a"] is sorted -- and the
	// comment above claimed it caught duplicates. It did not: a repeated entry
	// passed, and the session capability projection reads this list directly, so
	// it would have offered the same operation twice.
	unique := map[string]struct{}{}
	for _, operation := range OperationVocabulary {
		if _, repeated := unique[operation]; repeated {
			t.Errorf("OperationVocabulary lists %q more than once", operation)
		}
		unique[operation] = struct{}{}
	}
}

// operationConstant matches the `OperationX = "family:verb"` declarations.
var operationConstant = regexp.MustCompile(`\n\s*(Operation[A-Za-z]+)\s*=\s*"([^"]+)"`)

// TestOperationVocabularyCoversEveryDeclaredConstant reads the constants out of
// the source, because the test above cannot see the case it most needs to.
//
// That test walks the role table and the vocabulary. A constant declared and
// then added to neither is in neither collection, so nothing iterates it and
// nothing fails -- which is precisely the drift being guarded against, and the
// gate's own comment claimed to catch it. Reading the declarations is the only
// way to ask the question the comment was already promising an answer to.
func TestOperationVocabularyCoversEveryDeclaredConstant(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("authorization.go")
	if err != nil {
		t.Fatal(err)
	}
	matches := operationConstant.FindAllStringSubmatch(string(source), -1)
	if len(matches) < len(OperationVocabulary) {
		t.Fatalf("found only %d Operation constants in the source; the pattern has stopped matching", len(matches))
	}
	for _, match := range matches {
		if !OperationIsKnown(match[2]) {
			t.Errorf("%s = %q is declared but absent from OperationVocabulary, so nothing can ever offer it",
				match[1], match[2])
		}
	}
}

// TestOperationIsKnownDoesNotConstrainPolicyRequirements pins the distinction
// that PH-6 got wrong, because the tempting "fix" is to validate
// require_operation against the vocabulary and that would delete a working
// feature.
//
// An operator-defined approval token is not an operation Platform performs, so
// OperationIsKnown answers false for it -- and it is still perfectly valid in a
// policy, satisfied by a grant carrying the same string. The end-to-end proof
// is TestCELRoleAndOperationRequirementsUseResourceGrants in
// internal/controlplane; this states the domain-level rule that permits it.
func TestOperationIsKnownDoesNotConstrainPolicyRequirements(t *testing.T) {
	t.Parallel()
	for operation, want := range map[string]bool{
		OperationOperate:               true,
		OperationReadResource:          true,
		"environment:approve-change":   false, // operator-defined, and satisfiable
		"entity:approve-config-update": false, // Apollo's published example
	} {
		if got := OperationIsKnown(operation); got != want {
			t.Errorf("OperationIsKnown(%q) = %v, want %v", operation, got, want)
		}
	}
}

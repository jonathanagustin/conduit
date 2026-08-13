package controlplane

import (
	"fmt"
	"strings"
	"testing"
)

// Two Entities sharing a namespace can reference the same secret assignment,
// but not with different key subsets: buildSecretMutations keys desired secrets
// on assignment and namespace, so one requirement silently overwrites the other
// and the Plan materializes a Secret missing keys the losing Entity declared --
// while its Plan step still advertises the requirement as satisfied.
func TestConflictingSecretKeysInOneNamespaceAreRefused(t *testing.T) {
	t.Parallel()
	base := func() DesiredState {
		state := foundation()
		state.Environment.Secrets = []ExternalSecretAssignment{{
			Name: "database", Revision: 8, ProviderRef: "vault://platform/dev/database",
			KubernetesSecretName: "database-runtime", Keys: []string{"username", "password", "token"},
		}}
		return state
	}

	// Both foundation Entities live in namespace "example".
	conflicting := base()
	conflicting.Entities[0].SecretRequirements = []SecretRequirement{{
		Name: "workspace-db", Secret: "database", Keys: []string{"username", "password"},
	}}
	conflicting.Entities[1].SecretRequirements = []SecretRequirement{{
		Name: "api-db", Secret: "database", Keys: []string{"password", "token"},
	}}
	errs := Validate(conflicting)
	if len(errs) == 0 {
		t.Fatal("two Entities disagreeing about one Secret's keys were accepted")
	}
	joined := fmt.Sprint(errs)
	if !strings.Contains(joined, "database") || !strings.Contains(joined, "example") {
		t.Fatalf("error does not name the secret and namespace: %v", errs)
	}

	// The same keys, in a different order, are the same claim.
	agreeing := base()
	agreeing.Entities[0].SecretRequirements = []SecretRequirement{{
		Name: "workspace-db", Secret: "database", Keys: []string{"password", "username"},
	}}
	agreeing.Entities[1].SecretRequirements = []SecretRequirement{{
		Name: "api-db", Secret: "database", Keys: []string{"username", "password"},
	}}
	if errs := Validate(agreeing); len(errs) != 0 {
		t.Fatalf("Entities agreeing on keys were refused: %v", errs)
	}

	// A different namespace is a different Secret, so no agreement is required.
	separate := base()
	separate.Entities[0].Namespace = "workspace"
	separate.Entities[0].SecretRequirements = []SecretRequirement{{
		Name: "workspace-db", Secret: "database", Keys: []string{"username"},
	}}
	separate.Entities[1].SecretRequirements = []SecretRequirement{{
		Name: "api-db", Secret: "database", Keys: []string{"token"},
	}}
	if errs := Validate(separate); len(errs) != 0 {
		t.Fatalf("Entities in different namespaces were refused: %v", errs)
	}
}

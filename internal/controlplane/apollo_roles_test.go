package controlplane

import (
	"encoding/json"
	"github.com/jonathanagustin/conduit/internal/apollo"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The generated Apollo role table is only worth anything if it still says what
// the capture said. These gates fail on drift in either direction: a table
// edited by hand, or a capture replaced without regenerating.

const apolloGraphEvidencePath = "../../docs/evidence/apollo-authorization-graph.json"

type apolloGraphEvidence struct {
	Source struct {
		ResponseSha256 string `json:"responseSha256"`
	} `json:"source"`
	GraphCloses                     bool     `json:"graphCloses"`
	RolesWithNoRetainedOperations   []string `json:"rolesWithNoRetainedOperations"`
	RolesWithNoTransitiveOperations []string `json:"rolesWithNoTransitiveOperations"`
	Counts                          struct {
		Roles      int `json:"roles"`
		Operations int `json:"operations"`
		Workflows  int `json:"workflows"`
		Categories int `json:"categories"`
	} `json:"counts"`
	Graph struct {
		Roles map[string]struct {
			DisplayName string   `json:"displayName"`
			Operations  []string `json:"operations"`
			CanAssigns  []string `json:"canAssigns"`
		} `json:"roles"`
	} `json:"graph"`
}

func loadApolloGraphEvidence(t *testing.T) apolloGraphEvidence {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(apolloGraphEvidencePath))
	if err != nil {
		t.Fatalf("read apollo authorization graph evidence: %v", err)
	}
	var doc apolloGraphEvidence
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode apollo authorization graph evidence: %v", err)
	}
	return doc
}

func TestApolloRoleTableMatchesCapturedEvidence(t *testing.T) {
	evidence := loadApolloGraphEvidence(t)

	if evidence.Source.ResponseSha256 != apollo.AuthorizationGraphDigest {
		t.Fatalf("generated table pins %q but evidence records %q: regenerate with apolloroleg",
			apollo.AuthorizationGraphDigest, evidence.Source.ResponseSha256)
	}
	if got, want := len(apollo.Roles), evidence.Counts.Roles; got != want {
		t.Fatalf("generated %d roles, evidence records %d", got, want)
	}
	if got, want := len(apollo.Operations), evidence.Counts.Operations; got != want {
		t.Fatalf("generated %d operations, evidence records %d", got, want)
	}
	if got, want := len(apollo.Workflows), evidence.Counts.Workflows; got != want {
		t.Fatalf("generated %d workflows, evidence records %d", got, want)
	}
	if got, want := len(apollo.Categories), evidence.Counts.Categories; got != want {
		t.Fatalf("generated %d categories, evidence records %d", got, want)
	}

	for id, want := range evidence.Graph.Roles {
		got, ok := apollo.Roles[id]
		if !ok {
			t.Fatalf("evidence declares role %q that the generated table omits", id)
		}
		if got.DisplayName != want.DisplayName {
			t.Errorf("role %q display name: generated %q, evidence %q", id, got.DisplayName, want.DisplayName)
		}
		// Compared element by element, not by length. Counting alone passes a
		// generated table whose operations were substituted for different ones,
		// or reordered -- and the whole point of this gate is that the table
		// still says what the capture said, which a count cannot establish.
		assertSameStrings(t, "role "+id+" operations", got.Operations, want.Operations)
		assertSameStrings(t, "role "+id+" canAssigns", got.CanAssigns, want.CanAssigns)
	}
}

// The recovered roles form a consistent tier hierarchy: where a family declares
// administrator, editor, and viewer, the administrator transitively grants both
// lower tiers. This held for every family with multiple tiers in the capture, so
// it is a structural regularity of a faithful capture, and the reconstruction
// specs (docs/apollo/reference/notifications.md) lean on it. Gate it: a future
// capture where a higher tier stops covering a lower one is either a capture
// defect or an Apollo change, and either way is worth a loud failure rather than
// a silent one.
func TestApolloRoleTiersFormAConsistentHierarchy(t *testing.T) {
	transitiveGrants := func(start string) map[string]struct{} {
		reached := map[string]struct{}{}
		stack := []string{start}
		for len(stack) > 0 {
			id := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if _, seen := reached[id]; seen {
				continue
			}
			reached[id] = struct{}{}
			for _, granted := range apollo.Roles[id].Operations {
				if _, isRole := apollo.Roles[granted]; isRole {
					stack = append(stack, granted)
				}
			}
		}
		return reached
	}

	families := map[string]struct{}{}
	for id := range apollo.Roles {
		if family, _, ok := strings.Cut(id[len("apollo-"):], ":"); ok {
			families["apollo-"+family] = struct{}{}
		}
	}

	checked := 0
	for family := range families {
		admin := family + ":administrator"
		if _, ok := apollo.Roles[admin]; !ok {
			continue
		}
		reachable := transitiveGrants(admin)
		for _, tier := range []string{":editor", ":viewer"} {
			lower := family + tier
			if _, ok := apollo.Roles[lower]; !ok {
				continue
			}
			checked++
			if _, covered := reachable[lower]; !covered {
				t.Errorf("%s does not transitively grant %s: the tier hierarchy is inconsistent", admin, lower)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no multi-tier families found, so this gate proves nothing")
	}
	t.Logf("%d administrator-covers-lower relations verified", checked)
}

// A dangling grant is the failure mode that matters: a role that grants an
// identifier nothing defines would authorize against a name no check can
// resolve. The capture closes, so the generated table must close too.
func TestApolloRoleTableClosesOverItsOwnGrants(t *testing.T) {
	if !loadApolloGraphEvidence(t).GraphCloses {
		t.Fatal("evidence reports the captured graph does not close; regenerate the capture")
	}

	defined := func(id string) bool {
		if _, ok := apollo.Roles[id]; ok {
			return true
		}
		if _, ok := apollo.Operations[id]; ok {
			return true
		}
		if _, ok := apollo.Workflows[id]; ok {
			return true
		}
		return false
	}

	for id, role := range apollo.Roles {
		for _, target := range role.Operations {
			if !defined(target) {
				t.Errorf("role %q grants undefined operation %q", id, target)
			}
		}
		for _, target := range role.CanAssigns {
			if !defined(target) {
				t.Errorf("role %q can assign undefined role %q", id, target)
			}
		}
	}
}

// Platform's own vocabulary and Apollo's must stay distinguishable. Every
// Apollo identifier carries the prefix; a Platform role must never acquire one
// by accident, because the two use different authority algebras.
func TestApolloAndPlatformRoleVocabulariesStaySeparable(t *testing.T) {
	for id := range apollo.Roles {
		if len(id) < len("apollo-") || id[:len("apollo-")] != "apollo-" {
			t.Errorf("apollo role table contains unprefixed identifier %q", id)
		}
	}
	// canonicalRoleOperations is Platform's algebra. Any overlap means one
	// identifier would resolve through two different expansions.
	for id := range canonicalRoleOperations {
		if _, clash := apollo.Roles[id]; clash {
			t.Errorf("identifier %q is declared in both the Platform and Apollo role tables", id)
		}
		// Apollo's entity authority is spelled as UNPREFIXED operations
		// (entity:update-secret, entity:remove-label) rather than a role
		// family, so the entity: namespace is genuinely shared with Platform's
		// entity roles. Nothing collides today, but nothing was stopping it.
		if _, clash := apollo.Operations[id]; clash {
			t.Errorf("Platform role %q collides with an Apollo operation of the same name", id)
		}
	}
	for _, operation := range []string{
		OperationActOnBehalf, OperationProposeChange, OperationReadResource,
		OperationOperate, OperationImportBundle, OperationBulkChange,
		OperationManagePermissions, OperationApplyLabel, OperationCreateArtifact,
		OperationCreateProduct, OperationCreateRelease,
	} {
		if _, clash := apollo.Operations[operation]; clash {
			t.Errorf("Platform operation %q collides with an Apollo operation of the same name", operation)
		}
		if _, clash := apollo.Roles[operation]; clash {
			t.Errorf("Platform operation %q collides with an Apollo role of the same name", operation)
		}
	}
}

func assertSameStrings(t *testing.T, subject string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: generated %d, evidence records %d", subject, len(got), len(want))
		return
	}
	left, right := append([]string(nil), got...), append([]string(nil), want...)
	sort.Strings(left)
	sort.Strings(right)
	for index := range left {
		if left[index] != right[index] {
			t.Errorf("%s: generated %v, evidence records %v", subject, left, right)
			return
		}
	}
}

// rolesWithNoRetainedOperations is a summary field, and until this test it was
// bound to nothing -- written by the capture script and read by no gate, so it
// could drift from its own rows unnoticed. That is the same defect already
// fixed for byApplication in the Rosetta registry.
//
// It is also the DIRECT count, and it understates what the capture failed to
// establish. A role may present operations that are themselves roles which
// bottom out at nothing: apollo-hubs:administrator declares two and is absent
// from this list while authorizing nothing recoverable. Both counts are
// asserted, because the direct one alone reads as a smaller bound than it is.
func TestTheGraphDisclosesEveryRoleItEstablishesNothingAbout(t *testing.T) {
	t.Parallel()
	evidence := loadApolloGraphEvidence(t)

	direct := []string{}
	for id, role := range evidence.Graph.Roles {
		if len(role.Operations) == 0 {
			direct = append(direct, id)
		}
	}
	assertSameStrings(t, "rolesWithNoRetainedOperations", direct, evidence.RolesWithNoRetainedOperations)

	var closure func(string, map[string]bool) map[string]bool
	closure = func(id string, seen map[string]bool) map[string]bool {
		reached := map[string]bool{}
		if seen[id] {
			return reached
		}
		seen[id] = true
		for _, operation := range evidence.Graph.Roles[id].Operations {
			if _, isRole := evidence.Graph.Roles[operation]; isRole {
				for primitive := range closure(operation, seen) {
					reached[primitive] = true
				}
				continue
			}
			reached[operation] = true
		}
		return reached
	}
	transitive := []string{}
	for id := range evidence.Graph.Roles {
		if len(closure(id, map[string]bool{})) == 0 {
			transitive = append(transitive, id)
		}
	}
	assertSameStrings(t, "rolesWithNoTransitiveOperations", transitive, evidence.RolesWithNoTransitiveOperations)

	if len(transitive) <= len(direct) {
		t.Errorf("the transitive bound (%d) is not wider than the direct one (%d); "+
			"if the capture improved, say so here rather than leaving this test asserting nothing",
			len(transitive), len(direct))
	}
}

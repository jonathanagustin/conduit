package controlplane

import (
	"context"
	"testing"
	"time"
)

var policyFixtureTime = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

// evaluateExpression compiles one policy expression against the published
// predicate set and returns the decision it reaches.
func evaluateExpression(t *testing.T, trust PolicyTrust, expression string, change EntityChange, author Principal, editors []string) policyResult {
	t.Helper()
	policy := ApprovalPolicy{ID: "under-test", DisplayName: "Under test", Enabled: true, Expression: expression}
	result, err := evaluatePolicy(policy, policyInput(change, author, editors, policyFixtureTime), trust)
	if err != nil {
		t.Fatalf("evaluate %q: %v", expression, err)
	}
	return result
}

func TestDiffOnlyModifiesPathsGatesByGlob(t *testing.T) {
	t.Parallel()
	expression := `diff_only_modifies_paths(diff, policyVariable.allowed) ? approve("scoped") : reject("out of scope")`
	for name, testCase := range map[string]struct {
		paths     []string
		allowed   []string
		wantAllow bool
	}{
		"exact top-level key is allowed": {
			paths: []string{"dependency-stack-overrides"}, allowed: []string{"dependency-stack-overrides"}, wantAllow: true,
		},
		"single star matches one key": {
			paths: []string{"incrementalSettings/cadence"}, allowed: []string{"incrementalSettings/*"}, wantAllow: true,
		},
		"single star does not match deeper nesting": {
			paths: []string{"incrementalSettings/cadence/value"}, allowed: []string{"incrementalSettings/*"},
		},
		"double star matches deep nesting": {
			paths: []string{"incrementalSettings/cadence/value"}, allowed: []string{"incrementalSettings/**"}, wantAllow: true,
		},
		// A pattern that permits editing something nested under a key must not
		// silently permit replacing that key wholesale, which is a strictly
		// broader change.
		"double star does not match the parent itself": {
			paths: []string{"incrementalSettings"}, allowed: []string{"incrementalSettings/**"},
		},
		"a top-level pattern does not match a child": {
			paths: []string{"settings/replicas"}, allowed: []string{"settings"},
		},
		"every changed path must match": {
			paths: []string{"settings/replicas", "image/tag"}, allowed: []string{"settings/*"},
		},
		"all changed paths matching is allowed": {
			paths: []string{"settings/replicas", "settings/cpu"}, allowed: []string{"settings/*"}, wantAllow: true,
		},
		"empty pattern list rejects any change": {
			paths: []string{"settings/replicas"}, allowed: nil,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			change := EntityChange{Type: ChangeTypeUpdateEntity, Diff: map[string]ChangeDiffEntry{}}
			for _, path := range testCase.paths {
				change.Diff[path] = ChangeDiffEntry{Before: "before", After: "after"}
			}
			policy := ApprovalPolicy{
				ID: "scoped", DisplayName: "Scoped", Enabled: true, Expression: expression,
				Variables: map[string]PolicyVariable{"allowed": {Type: PolicyStringList, List: testCase.allowed}},
			}
			result, err := evaluatePolicy(policy, policyInput(change, Principal{Username: "svc"}, nil, policyFixtureTime), PolicyTrust{})
			if err != nil {
				t.Fatal(err)
			}
			if allowed := result.Kind == "APPROVE"; allowed != testCase.wantAllow {
				t.Fatalf("kind = %s, wantAllow = %v", result.Kind, testCase.wantAllow)
			}
		})
	}
}

func TestServiceUserPredicatesRequireUneditedAutomation(t *testing.T) {
	t.Parallel()
	trust := PolicyTrust{
		ServiceAccounts: []TrustedServiceAccount{
			{Username: "svc-deploy", ChangeTypes: []string{ChangeTypeUpdateEntity}},
		},
	}
	change := EntityChange{Type: ChangeTypeUpdateEntity}
	expression := `should_auto_approve_service_user(type, author, onBehalfOf, editors) ? approve("automation") : no_op()`

	serviceAuthor := Principal{Username: "svc-deploy", ServiceAccount: true}
	if result := evaluateExpression(t, trust, expression, change, serviceAuthor, nil); result.Kind != "APPROVE" {
		t.Fatalf("trusted unedited automation kind = %s", result.Kind)
	}
	// A human amending an automated proposal withdraws its auto-approval.
	if result := evaluateExpression(t, trust, expression, change, serviceAuthor, []string{"operator@example.test"}); result.Kind != "NO_OP" {
		t.Fatalf("edited automation kind = %s", result.Kind)
	}
	// The same account is not trusted for a Change Type it was not granted.
	other := EntityChange{Type: ChangeTypeDeleteEntity}
	if result := evaluateExpression(t, trust, expression, other, serviceAuthor, nil); result.Kind != "NO_OP" {
		t.Fatalf("ungranted change type kind = %s", result.Kind)
	}
	// A human with the same username as a declared account is not a service account.
	human := Principal{Username: "svc-deploy"}
	if result := evaluateExpression(t, trust, expression, change, human, nil); result.Kind != "NO_OP" {
		t.Fatalf("non-service-account kind = %s", result.Kind)
	}
	// An empty trust configuration auto-approves nothing.
	if result := evaluateExpression(t, PolicyTrust{}, expression, change, serviceAuthor, nil); result.Kind != "NO_OP" {
		t.Fatalf("undeclared trust kind = %s", result.Kind)
	}

	named := `is_author_service_user("svc-deploy", author, onBehalfOf, editors) ? approve("named") : no_op()`
	if result := evaluateExpression(t, trust, named, change, serviceAuthor, nil); result.Kind != "APPROVE" {
		t.Fatalf("named service user kind = %s", result.Kind)
	}
	if result := evaluateExpression(t, trust, named, change, Principal{Username: "svc-other", ServiceAccount: true}, nil); result.Kind != "NO_OP" {
		t.Fatalf("different service user kind = %s", result.Kind)
	}
}

func TestTrustedAutomationRequestsRequireOriginMetadata(t *testing.T) {
	t.Parallel()
	trust := PolicyTrust{ModuleService: "svc-modules", ImportService: "svc-import"}
	moduleExpression := `is_trusted_module_request(author, metadata, editors) ? approve("module") : no_op()`
	importExpression := `is_trusted_import_request(author, metadata, editors) ? approve("import") : no_op()`

	moduleAuthor := Principal{Username: "svc-modules", ServiceAccount: true}
	moduleChange := EntityChange{Type: ChangeTypeUpdateEntity, Metadata: map[string]any{
		MetadataChangeSource: ChangeSourceModuleInstallation, MetadataModuleID: "mod_1",
	}}
	if result := evaluateExpression(t, trust, moduleExpression, moduleChange, moduleAuthor, nil); result.Kind != "APPROVE" {
		t.Fatalf("module request kind = %s", result.Kind)
	}
	if result := evaluateExpression(t, trust, moduleExpression, moduleChange,
		Principal{Username: "svc-modules"}, nil); result.Kind != "NO_OP" {
		t.Fatalf("human using the trusted module username kind = %s", result.Kind)
	}
	// Module installation metadata is required, not just the identity.
	bare := EntityChange{Type: ChangeTypeUpdateEntity, Metadata: map[string]any{
		MetadataChangeSource: ChangeSourceModuleInstallation,
	}}
	if result := evaluateExpression(t, trust, moduleExpression, bare, moduleAuthor, nil); result.Kind != "NO_OP" {
		t.Fatalf("module request without module metadata kind = %s", result.Kind)
	}
	// An import-origin change is not a module change even from a trusted account.
	importChange := EntityChange{Type: importChangeTypePrefix + "ENVIRONMENT", Metadata: map[string]any{
		MetadataChangeSource: ChangeSourceEnvironmentImport, MetadataBundleID: "bnd_1",
	}}
	if result := evaluateExpression(t, trust, moduleExpression, importChange, moduleAuthor, nil); result.Kind != "NO_OP" {
		t.Fatalf("import change under module predicate kind = %s", result.Kind)
	}

	importAuthor := Principal{Username: "svc-import", ServiceAccount: true}
	if result := evaluateExpression(t, trust, importExpression, importChange, importAuthor, nil); result.Kind != "APPROVE" {
		t.Fatalf("import request kind = %s", result.Kind)
	}
	if result := evaluateExpression(t, trust, importExpression, importChange,
		Principal{Username: "svc-import"}, nil); result.Kind != "NO_OP" {
		t.Fatalf("human using the trusted import username kind = %s", result.Kind)
	}
	if result := evaluateExpression(t, trust, importExpression, importChange, importAuthor, []string{"operator@example.test"}); result.Kind != "NO_OP" {
		t.Fatalf("edited import request kind = %s", result.Kind)
	}
	if result := evaluateExpression(t, PolicyTrust{}, importExpression, importChange, importAuthor, nil); result.Kind != "NO_OP" {
		t.Fatalf("undeclared import service kind = %s", result.Kind)
	}
}

// Bundle import changes must carry the origin metadata the published import
// predicate reads, otherwise a policy written upstream can never recognize them.
func TestBundleImportChangesCarryImportOriginMetadata(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewGovernanceService(repository)
	envelope := BundleEnvelope{Manifest: BundleManifest{
		ID: "bnd_import_1",
		Resources: []BundleResource{
			{Type: "ENVIRONMENT", ID: "dev-use2", Digest: digest},
		},
	}}
	author, err := service.ResolvePrincipal(ctx, IdentityClaims{Username: "svc-import", ServiceAccount: true})
	if err != nil {
		t.Fatal(err)
	}
	changes, err := service.bundleImportChanges(ctx, envelope, "dev-use2", author)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("changes = %d, want 1", len(changes))
	}
	metadata := changes[0].Metadata
	if metadata[MetadataChangeSource] != ChangeSourceEnvironmentImport || metadata[MetadataBundleID] != "bnd_import_1" {
		t.Fatalf("metadata = %#v", metadata)
	}
}

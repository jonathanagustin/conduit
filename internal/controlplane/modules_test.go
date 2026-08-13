package controlplane

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompositeModuleEntityOverridesDeepMergeAndKeyInstances(t *testing.T) {
	child := ModuleDefinition{
		Name: "database", Variables: map[string]ModuleVariable{
			"namespace": {Type: "string", String: &ModuleStringVariable{}},
		},
		Entities: []ModuleEntityDefinition{{Type: "helmChart", HelmChart: ModuleHelmChartEntity{
			ProductID: "com.example:database", KubernetesNamespace: "{{ moduleVariable.namespace }}",
			ConfigOverrides:    "1.0.0:\n  overrides:\n    resources:\n      cpu: 1\n      memory: 2Gi\n",
			SecretRequirements: map[string]any{"database": map[string]any{"type": "single"}},
		}}},
	}
	parent := ModuleDefinition{
		Name: "foundation", Variables: map[string]ModuleVariable{
			"blueNamespace":  {Type: "string", String: &ModuleStringVariable{}},
			"greenNamespace": {Type: "string", String: &ModuleStringVariable{}},
			"memory":         {Type: "string", String: &ModuleStringVariable{}},
		},
		Submodules: []ModuleReference{
			{Name: "database", Version: "local", Key: "blue", VariableOverrides: map[string]string{"namespace": "{{ moduleVariable.blueNamespace }}"}},
			{Name: "database", Version: "local", Key: "green", VariableOverrides: map[string]string{"namespace": "{{ moduleVariable.greenNamespace }}"}, EntityOverrides: []ModuleEntityOverride{{
				Type: "helmChart", HelmChart: ModuleHelmChartEntityOverride{ProductID: "com.example:database", Override: ModuleHelmChartEntityPatch{
					ConfigOverrides:    "1.0.0:\n  overrides:\n    resources:\n      memory: '{{ moduleVariable.memory }}'\n    replicas: 3\n",
					SecretRequirements: map[string]any{"database": map[string]any{"type": "multikey"}, "backup": map[string]any{"type": "single"}},
				}},
			}}},
		},
	}
	repository := NewMemoryRepository()
	service := NewModuleService(repository)
	if _, err := service.PublishBatch(context.Background(), []ModuleDefinition{parent, child}); err != nil {
		t.Fatal(err)
	}
	release, err := repository.GetModuleRelease(context.Background(), "foundation", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(release.Lock.Entities) != 2 || release.Lock.Entities[0].InstancePath == release.Lock.Entities[1].InstancePath {
		t.Fatalf("flattened entities = %#v", release.Lock.Entities)
	}
	installation, err := service.Install(context.Background(), "prod", "foundation", "latest", map[string]string{
		"blueNamespace": "blue", "greenNamespace": "green", "memory": "8Gi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(installation.Entities) != 2 || installation.Entities[0].Key == installation.Entities[1].Key {
		t.Fatalf("installed entities = %#v", installation.Entities)
	}
	var green InstalledModuleEntity
	for _, entity := range installation.Entities {
		if strings.Contains(entity.InstancePath, "[green]") {
			green = entity
		}
	}
	for _, expected := range []string{"cpu: 1", "memory: 8Gi", "replicas: 3"} {
		if !strings.Contains(green.ConfigOverrides, expected) {
			t.Fatalf("merged config %q does not contain %q", green.ConfigOverrides, expected)
		}
	}
	if len(green.SecretRequirements) != 2 || green.SecretRequirements["backup"] == nil {
		t.Fatalf("merged secrets = %#v", green.SecretRequirements)
	}
}

func TestCompositeModuleRejectsForbiddenEntityOverridesAndInvalidVariableOverrides(t *testing.T) {
	allowed := false
	child := ModuleDefinition{
		Name: "restricted", Variables: map[string]ModuleVariable{
			"profile": {Type: "options", Options: &ModuleOptionsVariable{Values: []string{"small", "large"}}},
			"memory":  {Type: "stringPreset", StringPreset: &ModuleStringPresetVariable{Options: "profile", Mapping: map[string]string{"small": "1Gi", "large": "4Gi"}}},
		},
		Entities: []ModuleEntityDefinition{{Type: "helmChart", HelmChart: ModuleHelmChartEntity{ProductID: "com.example:restricted", KubernetesNamespace: "apps"}}},
	}
	child.Restrictions.SubmoduleUsage.AllowEntityOverrides = &allowed
	parent := ModuleDefinition{Name: "parent", Submodules: []ModuleReference{{Name: "restricted", Version: "local", EntityOverrides: []ModuleEntityOverride{{
		Type: "helmChart", HelmChart: ModuleHelmChartEntityOverride{ProductID: "com.example:restricted", Override: ModuleHelmChartEntityPatch{ConfigOverrides: "1.0.0: {}"}},
	}}}}}
	service := NewModuleService(NewMemoryRepository())
	if _, err := service.PublishBatch(context.Background(), []ModuleDefinition{child, parent}); err == nil || !strings.Contains(err.Error(), "does not allow Entity overrides") {
		t.Fatalf("forbidden override error = %v", err)
	}

	parent.Submodules[0].EntityOverrides = nil
	parent.Submodules[0].VariableOverrides = map[string]string{"memory": "8Gi"}
	if _, err := service.PublishBatch(context.Background(), []ModuleDefinition{child, parent}); err == nil || !strings.Contains(err.Error(), "stringPreset") {
		t.Fatalf("stringPreset override error = %v", err)
	}
}

func TestRepeatedSubmoduleRequiresKeys(t *testing.T) {
	definition := ModuleDefinition{Name: "parent", Submodules: []ModuleReference{
		{Name: "child", Version: "0.1.0"}, {Name: "child", Version: "0.1.0", Key: "second"},
	}}
	errs := ValidateModuleDefinition(definition)
	if len(errs) == 0 || !strings.Contains(ValidationErrors(errs).Error(), "key: is required") {
		t.Fatalf("validation errors = %#v", errs)
	}
}

func TestModuleInstallationTracksPromotedChannelAndRecallDoesNotRollBack(t *testing.T) {
	definition := ModuleDefinition{Name: "channelled", Entities: []ModuleEntityDefinition{{
		Type: "helmChart", HelmChart: ModuleHelmChartEntity{ProductID: "com.example:app", KubernetesNamespace: "apps", ConfigOverrides: "1.0.0:\n  overrides:\n    value: one\n"},
	}}}
	repository := NewMemoryRepository()
	service := NewModuleService(repository)
	if _, err := service.PublishBatch(context.Background(), []ModuleDefinition{definition}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PromoteRelease(context.Background(), "channelled", "0.1.0", []string{"STABLE"}); err != nil {
		t.Fatal(err)
	}
	installation, err := service.InstallTrackingChannel(context.Background(), "prod", "channelled", "latest", "STABLE", nil)
	if err != nil {
		t.Fatal(err)
	}
	definition.Entities[0].HelmChart.ConfigOverrides = "1.0.0:\n  overrides:\n    value: two\n"
	if _, err := service.PublishBatch(context.Background(), []ModuleDefinition{definition}); err != nil {
		t.Fatal(err)
	}
	proposal, err := service.ProposeUpdate(context.Background(), installation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.TargetVersion != "0.1.0" {
		t.Fatalf("unpromoted release was proposed: %#v", proposal)
	}
	if _, err := service.PromoteRelease(context.Background(), "channelled", "0.2.0", []string{"STABLE"}); err != nil {
		t.Fatal(err)
	}
	proposal, err = service.ProposeUpdate(context.Background(), installation.ID)
	if err != nil || proposal.TargetVersion != "0.2.0" || len(proposal.Changed) != 1 {
		t.Fatalf("promoted proposal = %#v, err = %v", proposal, err)
	}
	installation, err = service.ApplyUpdate(context.Background(), installation.ID)
	if err != nil || installation.Version != "0.2.0" {
		t.Fatalf("applied installation = %#v, err = %v", installation, err)
	}
	if _, err := service.RecallRelease(context.Background(), "channelled", "0.2.0"); err != nil {
		t.Fatal(err)
	}
	proposal, err = service.ProposeUpdate(context.Background(), installation.ID)
	if err != nil || proposal.TargetVersion != "0.2.0" {
		t.Fatalf("recall should cancel the pending update without rolling back: %#v, err = %v", proposal, err)
	}
	if _, err := service.PauseUpdates(context.Background(), installation.ID); err != nil {
		t.Fatal(err)
	}
	proposal, err = service.ProposeUpdate(context.Background(), installation.ID)
	if err != nil || proposal.Status != "PAUSED" {
		t.Fatalf("paused proposal = %#v, err = %v", proposal, err)
	}
}

func TestModuleAutomaticUpdateApprovalAllowsUpdateShapeAndRecordsEvidence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository()
	service := NewModuleService(repository)
	service.now = func() time.Time { return now }
	definition := ModuleDefinition{Name: "auto-module", Entities: []ModuleEntityDefinition{{
		Type: "helmChart", HelmChart: ModuleHelmChartEntity{
			ProductID: "com.example:api", EntityName: "api", KubernetesNamespace: "apps",
			ConfigOverrides: "1.0.0:\n  overrides:\n    value: one\n",
		},
	}}}
	if _, err := service.PublishBatch(ctx, []ModuleDefinition{definition}); err != nil {
		t.Fatal(err)
	}
	installation, err := service.Install(ctx, "prod", definition.Name, "latest", nil)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := service.ConfigureAutoUpdatePolicy(ctx, definition.Name, true, "approved by platform compliance", "change-manager", 0)
	if err != nil || policy.Revision != 1 {
		t.Fatalf("policy = %#v, %v", policy, err)
	}
	if _, _, err := service.ApplyAutomaticUpdate(ctx, installation.ID); err == nil || !strings.Contains(err.Error(), "no valid Module version update") {
		t.Fatalf("initial installation was treated as an automatic update: %v", err)
	}
	definition.Entities = append(definition.Entities, ModuleEntityDefinition{
		Type: "helmChart", HelmChart: ModuleHelmChartEntity{
			ProductID: "com.example:worker", EntityName: "worker", KubernetesNamespace: "apps",
			ConfigOverrides: "1.0.0:\n  overrides:\n    enabled: true\n",
		},
	})
	if _, err := service.PublishBatch(ctx, []ModuleDefinition{definition}); err != nil {
		t.Fatal(err)
	}
	updated, request, err := service.ApplyAutomaticUpdate(ctx, installation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != "0.2.0" || len(updated.Entities) != 2 || updated.LastAutomaticApproval == nil ||
		updated.LastAutomaticApproval.ChangeRequestID != request.ID || updated.LastAutomaticApproval.PolicyRevision != 1 {
		t.Fatalf("automatic update = %#v", updated)
	}
	if request.Status != ChangeApproved || !request.RequirementsSatisfied || len(request.Reviews) != 1 ||
		request.Reviews[0].Rationale != policy.Rationale || request.Changes[0].Type != ModuleUpdateChangeType ||
		request.Changes[0].Request["editsConfigurationOverrides"] != false {
		t.Fatalf("automatic Change Request = %#v", request)
	}
}

func TestModuleAutomaticUpdateApprovalRejectsConfigOverrideChanges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewModuleService(repository)
	definition := ModuleDefinition{Name: "config-module", Entities: []ModuleEntityDefinition{{
		Type: "helmChart", HelmChart: ModuleHelmChartEntity{
			ProductID: "com.example:api", EntityName: "api", KubernetesNamespace: "apps",
			ConfigOverrides: "1.0.0:\n  overrides:\n    value: one\n",
		},
	}}}
	if _, err := service.PublishBatch(ctx, []ModuleDefinition{definition}); err != nil {
		t.Fatal(err)
	}
	installation, err := service.Install(ctx, "prod", definition.Name, "latest", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfigureAutoUpdatePolicy(ctx, definition.Name, true, "safe updates only", "change-manager", 0); err != nil {
		t.Fatal(err)
	}
	definition.Entities[0].HelmChart.ConfigOverrides = "1.0.0:\n  overrides:\n    value: two\n"
	if _, err := service.PublishBatch(ctx, []ModuleDefinition{definition}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ApplyAutomaticUpdate(ctx, installation.ID); err == nil || !strings.Contains(err.Error(), "cannot edit existing Entity config overrides") {
		t.Fatalf("config-changing automatic update error = %v", err)
	}
	stored, err := repository.GetModuleInstallation(ctx, installation.ID)
	if err != nil || stored.Version != "0.1.0" || stored.LastAutomaticApproval != nil {
		t.Fatalf("rejected automatic update mutated installation = %#v, %v", stored, err)
	}
	requests, err := repository.ListChangeRequests(ctx, "prod")
	if err != nil || len(requests) != 0 {
		t.Fatalf("rejected automatic update created approval evidence = %#v, %v", requests, err)
	}
	manual, err := service.ApplyUpdate(ctx, installation.ID)
	if err != nil || manual.Version != "0.2.0" || manual.LastAutomaticApproval != nil {
		t.Fatalf("manual update = %#v, %v", manual, err)
	}
}

func TestFileRepositoryPersistsModuleAutoUpdatePolicy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "state")
	repository, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	service := NewModuleService(repository)
	definition := ModuleDefinition{Name: "persisted-auto", Entities: []ModuleEntityDefinition{{
		Type: "helmChart", HelmChart: ModuleHelmChartEntity{ProductID: "com.example:api", KubernetesNamespace: "apps"},
	}}}
	if _, err := service.PublishBatch(ctx, []ModuleDefinition{definition}); err != nil {
		t.Fatal(err)
	}
	created, err := service.ConfigureAutoUpdatePolicy(ctx, definition.Name, true, "persistent reason", "change-manager", 0)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := reopened.GetModuleAutoUpdatePolicy(ctx, definition.Name)
	if err != nil || stored != created {
		t.Fatalf("persisted policy = %#v, %v", stored, err)
	}
	if _, err := NewModuleService(reopened).ConfigureAutoUpdatePolicy(
		ctx, definition.Name, false, "disabled by compliance", "change-manager", 0,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale policy revision error = %v", err)
	}
}

func TestModuleAutomaticUpdateRepositoryRejectsStaleOrDisabledPolicyAtMutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	for name, newRepository := range map[string]func(*testing.T) ModuleRepository{
		"memory": func(*testing.T) ModuleRepository { return NewMemoryRepository() },
		"file": func(t *testing.T) ModuleRepository {
			repository, err := NewFileRepository(filepath.Join(t.TempDir(), "state"))
			if err != nil {
				t.Fatal(err)
			}
			return repository
		},
	} {
		t.Run(name, func(t *testing.T) {
			repository := newRepository(t)
			installation := ModuleInstallation{
				ID: "modi_atomic", Module: "atomic-module", Version: "0.1.0", Environment: "prod", Revision: 1,
			}
			if _, err := repository.CreateModuleInstallation(ctx, installation); err != nil {
				t.Fatal(err)
			}
			policy := ModuleAutoUpdatePolicy{
				Module: "atomic-module", Enabled: true, Rationale: "approved", ConfiguredBy: "change-manager",
				CreatedAt: now, UpdatedAt: now, Revision: 1,
			}
			if _, err := repository.PutModuleAutoUpdatePolicy(ctx, policy); err != nil {
				t.Fatal(err)
			}
			policy.Enabled, policy.Rationale, policy.Revision = false, "approval withdrawn", 2
			if _, err := repository.PutModuleAutoUpdatePolicy(ctx, policy); err != nil {
				t.Fatal(err)
			}
			called := false
			if _, err := repository.UpdateModuleInstallationAutomatically(
				ctx, installation.ID, installation.Module, 1, func(record *ModuleInstallation) error {
					called = true
					record.Version = "0.2.0"
					return nil
				},
			); !errors.Is(err, ErrConflict) {
				t.Fatalf("stale automatic policy error = %v", err)
			}
			if called {
				t.Fatal("installation update ran after automatic approval was withdrawn")
			}
			stored, err := repository.GetModuleInstallation(ctx, installation.ID)
			if err != nil || stored.Version != installation.Version || stored.Revision != installation.Revision {
				t.Fatalf("rejected mutation changed installation = %#v, %v", stored, err)
			}
		})
	}
}

func TestFileRepositoryRejectsModulePolicyPathTraversal(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "state")
	repository, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	_, err = repository.PutModuleAutoUpdatePolicy(context.Background(), ModuleAutoUpdatePolicy{
		Module: "../escape", Enabled: true, Rationale: "invalid", ConfiguredBy: "change-manager",
		CreatedAt: now, UpdatedAt: now, Revision: 1,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("path traversal policy error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "escape.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path traversal created a policy file: %v", err)
	}
}

func TestPublishAndInstallGitManagedCompositeModule(t *testing.T) {
	definitions := readExampleModuleDefinitions(t)
	repository := NewMemoryRepository()
	service := NewModuleService(repository)
	service.now = func() time.Time { return time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC) }

	releases, err := service.PublishBatch(context.Background(), definitions)
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 3 {
		t.Fatalf("releases = %#v", releases)
	}
	foundation, err := repository.GetModuleRelease(context.Background(), "example-foundation", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(foundation.Lock.Submodules) != 2 || len(foundation.Lock.Entities) != 2 {
		t.Fatalf("lock = %#v", foundation.Lock)
	}
	for _, submodule := range foundation.Lock.Submodules {
		if submodule.Version != "0.1.0" {
			t.Fatalf("unresolved submodule = %#v", submodule)
		}
	}

	installation, err := service.Install(context.Background(), "prod-use1", "example-foundation", "latest", map[string]string{
		"databaseCidr": "10.20.0.0/24",
	})
	if err != nil {
		t.Fatal(err)
	}
	if installation.Version != "0.1.0" || len(installation.Entities) != 2 {
		t.Fatalf("installation = %#v", installation)
	}
	for _, entity := range installation.Entities {
		if entity.KubernetesNamespace != "example" {
			t.Fatalf("namespace was not rendered: %#v", entity)
		}
	}

	idempotent, err := service.PublishBatch(context.Background(), definitions)
	if err != nil {
		t.Fatal(err)
	}
	for _, release := range idempotent {
		if release.Version != "0.1.0" {
			t.Fatalf("idempotent publication created %s:%s", release.Name, release.Version)
		}
	}
}

func TestCompositeModuleUpdatePropagatesLocalSubmoduleAndRelinquishesEntities(t *testing.T) {
	definitions := readExampleModuleDefinitions(t)
	repository := NewMemoryRepository()
	service := NewModuleService(repository)
	if _, err := service.PublishBatch(context.Background(), definitions); err != nil {
		t.Fatal(err)
	}
	installation, err := service.Install(context.Background(), "dev-use1", "example-foundation", "latest", map[string]string{
		"databaseCidr": "10.30.0.0/24",
	})
	if err != nil {
		t.Fatal(err)
	}

	for index := range definitions {
		switch definitions[index].Name {
		case "example-workspace":
			definitions[index].Entities[0].HelmChart.ConfigOverrides += "\n# reviewed update\n"
		case "example-foundation":
			definitions[index].Submodules = definitions[index].Submodules[1:]
		}
	}
	if _, err := service.PublishBatch(context.Background(), definitions); err != nil {
		t.Fatal(err)
	}
	proposal, err := service.ProposeUpdate(context.Background(), installation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Status != "VALID" || proposal.TargetVersion != "0.2.0" || len(proposal.Relinquished) != 1 || len(proposal.Changed) != 0 {
		t.Fatalf("proposal = %#v", proposal)
	}
	updated, err := service.ApplyUpdate(context.Background(), installation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != "0.2.0" || len(updated.Entities) != 1 {
		t.Fatalf("updated = %#v", updated)
	}
}

func TestModuleConfigOwnershipEnabledMergeDisabledUnsetAndInstallOnly(t *testing.T) {
	current := InstalledModuleEntity{ConfigOverrides: `
3.0.0:
  overrides:
    key1: a
    key2: b
    key3: c
`}
	moduleConfig := `
3.0.0:
  overrides:
    key1: x
    key3: "{{ unset }}"
    key4: d
`
	for _, test := range []struct {
		name    string
		mode    string
		want    []string
		notWant []string
	}{
		{name: "enabled", mode: "ENABLED", want: []string{"key1: x", "key4: d"}, notWant: []string{"key2:", "key3:"}},
		{name: "merge", mode: "MERGE", want: []string{"key1: x", "key2: b", "key4: d"}, notWant: []string{"key3:"}},
		{name: "disabled", mode: "DISABLED", want: []string{"key1: a", "key2: b", "key3: c"}, notWant: []string{"key4:"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := InstalledModuleEntity{SyncConfigOverrides: test.mode, ConfigOverrides: moduleConfig}
			resolved, err := reconcileModuleEntityConfig(&current, target, false)
			if err != nil {
				t.Fatal(err)
			}
			for _, expected := range test.want {
				if !strings.Contains(resolved.ConfigOverrides, expected) {
					t.Fatalf("%s config %q missing %q", test.mode, resolved.ConfigOverrides, expected)
				}
			}
			for _, unexpected := range test.notWant {
				if strings.Contains(resolved.ConfigOverrides, unexpected) {
					t.Fatalf("%s config %q retained %q", test.mode, resolved.ConfigOverrides, unexpected)
				}
			}
		})
	}

	first, err := reconcileModuleEntityConfig(nil, InstalledModuleEntity{
		SyncConfigOverrides:    "MERGE",
		ConfigOverrides:        "3.0.0:\n  overrides:\n    controlled: module\n",
		InstallConfigOverrides: "3.0.0:\n  overrides:\n    controlled: default\n    entityOwned: initial\n",
	}, true)
	if err != nil || !strings.Contains(first.ConfigOverrides, "controlled: module") || !strings.Contains(first.ConfigOverrides, "entityOwned: initial") {
		t.Fatalf("first install config=%q err=%v", first.ConfigOverrides, err)
	}
	first.ConfigOverrides = strings.Replace(first.ConfigOverrides, "entityOwned: initial", "entityOwned: customized", 1)
	updated, err := reconcileModuleEntityConfig(&first, InstalledModuleEntity{
		SyncConfigOverrides:    "MERGE",
		ConfigOverrides:        "3.0.0:\n  overrides:\n    controlled: updated\n",
		InstallConfigOverrides: "3.0.0:\n  overrides:\n    entityOwned: new-default-must-not-apply\n",
	}, false)
	if err != nil || !strings.Contains(updated.ConfigOverrides, "controlled: updated") ||
		!strings.Contains(updated.ConfigOverrides, "entityOwned: customized") || strings.Contains(updated.ConfigOverrides, "new-default") {
		t.Fatalf("updated config=%q err=%v", updated.ConfigOverrides, err)
	}
}

func TestModuleUpdateBlocksOnNewRequiredVariable(t *testing.T) {
	definitions := readExampleModuleDefinitions(t)
	repository := NewMemoryRepository()
	service := NewModuleService(repository)
	if _, err := service.PublishBatch(context.Background(), definitions); err != nil {
		t.Fatal(err)
	}
	installation, err := service.Install(context.Background(), "stage-use1", "example-foundation", "latest", map[string]string{
		"databaseCidr": "10.40.0.0/24",
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := range definitions {
		if definitions[index].Name == "example-foundation" {
			definitions[index].Variables["newRequired"] = ModuleVariable{
				Type: "string", String: &ModuleStringVariable{Description: "Must be supplied during a manual update."},
			}
		}
	}
	if _, err := service.PublishBatch(context.Background(), definitions); err != nil {
		t.Fatal(err)
	}
	proposal, err := service.ProposeUpdate(context.Background(), installation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Status != "BLOCKED" || len(proposal.Reasons) != 1 {
		t.Fatalf("proposal = %#v", proposal)
	}
	// The prose reason is not enough on its own: a reader cannot tell which
	// kind of problem it is without matching message text, and cannot tell
	// which variable to fix without splitting the string back apart.
	if len(proposal.Blockers) != 1 {
		t.Fatalf("blockers = %#v, want one", proposal.Blockers)
	}
	blocker := proposal.Blockers[0]
	if blocker.Kind != UpdateBlockerVariablesUnresolved {
		t.Errorf("kind = %q, want %q", blocker.Kind, UpdateBlockerVariablesUnresolved)
	}
	if blocker.Message == "" {
		t.Error("the blocker carries no message to head the group with")
	}
	// The subject is the validation path rather than the bare variable name.
	// Upstream shows "environmentId"; "variables.newRequired" says the same
	// thing and says where it lives, which matters once a Module has variables
	// and entities that can share a name.
	if strings.Join(blocker.Subjects, ",") != "variables.newRequired" {
		t.Errorf("subjects = %v, want the qualified path of the variable that cannot be inferred", blocker.Subjects)
	}
	// Reasons is derived from the blocker, so the two cannot disagree.
	if !strings.HasPrefix(proposal.Reasons[0], blocker.Subjects[0]+":") {
		t.Errorf("reason %q does not correspond to subject %q", proposal.Reasons[0], blocker.Subjects[0])
	}
}

func TestModuleEntityCanBeTakenOverUnlessAnotherModuleOwnsIt(t *testing.T) {
	definitions := readExampleModuleDefinitions(t)
	repository := NewMemoryRepository()
	service := NewModuleService(repository)
	if _, err := service.PublishBatch(context.Background(), definitions); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Install(context.Background(), "takeover-use1", "example-foundation", "latest", map[string]string{
		"databaseCidr": "10.50.0.0/24",
	}); err != nil {
		t.Fatal(err)
	}
	var duplicate ModuleDefinition
	for _, definition := range definitions {
		if definition.Name == "example-workspace" {
			duplicate = definition
			break
		}
	}
	duplicate.Name = "duplicate-owner"
	duplicate.Restrictions.NewInstallations.Allowed = nil
	if _, err := service.PublishBatch(context.Background(), []ModuleDefinition{duplicate}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Install(context.Background(), "takeover-use1", "duplicate-owner", "latest", map[string]string{}); err == nil {
		t.Fatal("second Module was allowed to own an already managed Entity")
	}
}

func TestFileRepositoryPersistsModuleReleasesAndInstallations(t *testing.T) {
	directory := t.TempDir()
	repository, err := NewFileRepository(directory)
	if err != nil {
		t.Fatal(err)
	}
	service := NewModuleService(repository)
	definitions := readExampleModuleDefinitions(t)
	if _, err := service.PublishBatch(context.Background(), definitions); err != nil {
		t.Fatal(err)
	}
	created, err := service.Install(context.Background(), "persist-use1", "example-foundation", "latest", map[string]string{
		"databaseCidr": "10.60.0.0/24",
	})
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := NewFileRepository(directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.GetModuleRelease(context.Background(), "example-foundation", "0.1.0"); err != nil {
		t.Fatal(err)
	}
	loaded, err := reopened.GetModuleInstallation(context.Background(), created.ID)
	if err != nil || loaded.Version != "0.1.0" {
		t.Fatalf("loaded = %#v, err = %v", loaded, err)
	}
}

func readExampleModuleDefinitions(t *testing.T) []ModuleDefinition {
	t.Helper()
	root := "../../examples/modules"
	paths, err := filepath.Glob(filepath.Join(root, "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	definitions := make([]ModuleDefinition, 0, len(paths))
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		definition, decodeErr := DecodeModuleDefinition(file)
		_ = file.Close()
		if decodeErr != nil {
			t.Fatalf("decode %s: %v", path, decodeErr)
		}
		definitions = append(definitions, definition)
	}
	return definitions
}

func TestValidationPathsDeduplicatesInOrder(t *testing.T) {
	t.Parallel()
	// Two problems with one variable is one subject, not two: the popover
	// lists what to fix, and a name repeated twice reads as two variables.
	paths := validationPaths([]ValidationError{
		{Path: "beta", Message: "first"},
		{Path: "alpha", Message: "second"},
		{Path: "beta", Message: "third"},
	})
	if strings.Join(paths, ",") != "beta,alpha" {
		t.Errorf("paths = %v, want beta,alpha in first-seen order", paths)
	}
	if len(validationPaths(nil)) != 0 {
		t.Error("no validation errors should produce no subjects")
	}
}

// Upstream's Module version selector shows "V98": its Module versions are
// opaque, not semantic. The ModuleRelease type here accepts the same, and
// compareVersions falls back to a lexicographic comparison for anything it
// cannot parse, which puts V10 before V9 -- so an update from V9 to V10 would
// be answered "already at the latest version" and do nothing.
//
// This is latent rather than live: PublishBatch is the only producer today and
// always assigns an orderable version. Imported releases are represented here
// by writing through the repository boundary, which constrains both the target
// selection and the update predicate that the interop path will rely on.
func TestModuleUpdatesUseTheOnlyOrderingAnOpaqueVersionHas(t *testing.T) {
	t.Parallel()
	published := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	release := func(version string, offset time.Duration) ModuleRelease {
		return ModuleRelease{Version: version, CreatedAt: published.Add(offset)}
	}

	t.Run("a lexicographically smaller opaque version is still an update", func(t *testing.T) {
		if !moduleUpdateIsNewer(release("V9", 0), release("V10", time.Minute)) {
			t.Error("V9 -> V10 was not treated as an update")
		}
		if compareVersions("V10", "V9") > 0 {
			t.Skip("compareVersions now orders these, so this no longer proves anything")
		}
	})

	t.Run("the same opaque version is not an update", func(t *testing.T) {
		if moduleUpdateIsNewer(release("V98", 0), release("V98", time.Minute)) {
			t.Error("an unchanged version was treated as an update")
		}
	})

	t.Run("orderable versions still refuse a downgrade", func(t *testing.T) {
		// Publication order does not get to override a real version order:
		// republishing 1.0.0 after 2.0.0 must not roll an installation back.
		for _, test := range []struct {
			current, target string
			want            bool
		}{
			{"1.0.0", "2.0.0", true},
			{"2.0.0", "1.0.0", false},
			{"2.0.0", "2.0.0", false},
			{"1.0.0", "1.0.1", true},
		} {
			if got := moduleUpdateIsNewer(release(test.current, 0), release(test.target, time.Minute)); got != test.want {
				t.Errorf("%s -> %s = %v, want %v", test.current, test.target, got, test.want)
			}
		}
	})

	t.Run("a mixed pair falls back to publication order", func(t *testing.T) {
		// One orderable version and one opaque one cannot be compared at all.
		// Claiming an order here is how the original bug read: confident, and
		// wrong. Publication order is what resolution already used.
		if !moduleUpdateIsNewer(release("1.0.0", 0), release("V98", time.Minute)) {
			t.Error("an opaque target after an orderable current was not an update")
		}
		if !moduleUpdateIsNewer(release("V98", 0), release("1.0.0", time.Minute)) {
			t.Error("an orderable target after an opaque current was not an update")
		}
	})

	t.Run("opaque versions do not roll back or guess across equal timestamps", func(t *testing.T) {
		if moduleUpdateIsNewer(release("V10", time.Minute), release("V9", 0)) {
			t.Error("an older opaque release was treated as an update")
		}
		if moduleUpdateIsNewer(release("V9", 0), release("V10", 0)) {
			t.Error("equal publication times were given an invented order")
		}
	})
}

func TestModuleUpdateResolvesOpaqueVersionsByPublicationOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewModuleService(repository)
	created := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	for index, version := range []string{"V9", "V10"} {
		release := ModuleRelease{
			Name: "imported", Version: version,
			ReleaseChannels: []string{"RELEASE"}, CreatedAt: created.Add(time.Duration(index) * time.Minute),
		}
		release.Digest = moduleReleaseDigest(release.Definition, release.Lock)
		if _, err := repository.CreateModuleRelease(ctx, release); err != nil {
			t.Fatal(err)
		}
	}
	installation, err := repository.CreateModuleInstallation(ctx, ModuleInstallation{
		ID: "modi_imported", Environment: "prod", Module: "imported", Version: "V9", ReleaseChannel: "RELEASE",
		Status: "ACTIVE", CreatedAt: created, UpdatedAt: created, Revision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := service.ProposeUpdate(ctx, installation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.TargetVersion != "V10" {
		t.Fatalf("opaque update target = %q, want V10", proposal.TargetVersion)
	}
	installation, err = service.ApplyUpdate(ctx, installation.ID)
	if err != nil || installation.Version != "V10" {
		t.Fatalf("apply opaque update = %#v, %v", installation, err)
	}
	if _, err := service.RecallRelease(ctx, "imported", "V10"); err != nil {
		t.Fatal(err)
	}
	proposal, err = service.ProposeUpdate(ctx, installation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.TargetVersion != "V10" {
		t.Fatalf("recall proposed an opaque rollback: %#v", proposal)
	}
}

func TestFileRepositoryPersistsOpaqueModuleVersions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "state")
	repository, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	for index, version := range []string{"V9", "V10"} {
		release := ModuleRelease{
			Name: "persisted-opaque", Version: version,
			ReleaseChannels: []string{"RELEASE"}, CreatedAt: created.Add(time.Duration(index) * time.Minute),
		}
		release.Digest = moduleReleaseDigest(release.Definition, release.Lock)
		if _, err := repository.CreateModuleRelease(ctx, release); err != nil {
			t.Fatalf("persist %s: %v", version, err)
		}
	}
	observed := ModuleRelease{Name: "observed-opaque", Version: "V98", CreatedAt: created}
	observed.Digest = moduleReleaseDigest(observed.Definition, observed.Lock)
	if _, err := repository.CreateModuleRelease(ctx, observed); err != nil {
		t.Fatalf("persist observed V98 fixture: %v", err)
	}
	installation, err := repository.CreateModuleInstallation(ctx, ModuleInstallation{
		ID: "modi_persisted_opaque", Environment: "prod", Module: "persisted-opaque", Version: "V9",
		ReleaseChannel: "RELEASE", Status: "ACTIVE", CreatedAt: created, UpdatedAt: created, Revision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	repository, err = NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	observed, err = repository.GetModuleRelease(ctx, "observed-opaque", "V98")
	if err != nil || observed.Version != "V98" {
		t.Fatalf("reloaded observed opaque fixture = %#v, %v", observed, err)
	}
	proposal, err := NewModuleService(repository).ProposeUpdate(ctx, installation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.CurrentVersion != "V9" || proposal.TargetVersion != "V10" {
		t.Fatalf("persisted opaque proposal = %#v", proposal)
	}
}

func TestFileRepositoryRejectsUnsafeModuleVersions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository, err := NewFileRepository(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"", ".hidden", "../escape", `V10\\escape`, "V10 latest", "V10/next", strings.Repeat("v", 129)} {
		release := ModuleRelease{Name: "unsafe-version", Version: version, CreatedAt: time.Now().UTC()}
		release.Digest = moduleReleaseDigest(release.Definition, release.Lock)
		if _, err := repository.CreateModuleRelease(ctx, release); !errors.Is(err, ErrConflict) {
			t.Errorf("version %q error = %v, want conflict", version, err)
		}
	}
}

func TestModuleReleasePublicationTimeIsImmutable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tests := []struct {
		name       string
		repository func(*testing.T) ModuleRepository
	}{
		{name: "memory", repository: func(*testing.T) ModuleRepository { return NewMemoryRepository() }},
		{name: "file", repository: func(t *testing.T) ModuleRepository {
			repository, err := NewFileRepository(filepath.Join(t.TempDir(), "state"))
			if err != nil {
				t.Fatal(err)
			}
			return repository
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository := test.repository(t)
			service := NewModuleService(repository)
			published := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
			service.now = func() time.Time { return published }
			definition := ModuleDefinition{Name: "immutable-time", Entities: []ModuleEntityDefinition{{
				Type: "helmChart", HelmChart: ModuleHelmChartEntity{
					ProductID: "com.example:immutable", KubernetesNamespace: "apps",
				},
			}}}
			if _, err := service.PublishBatch(ctx, []ModuleDefinition{definition}); err != nil {
				t.Fatal(err)
			}
			if _, err := repository.UpdateModuleRelease(ctx, "immutable-time", "0.1.0", func(release *ModuleRelease) error {
				release.CreatedAt = release.CreatedAt.Add(time.Minute)
				return nil
			}); !errors.Is(err, ErrConflict) {
				t.Fatalf("publication-time mutation error = %v, want conflict", err)
			}
			release, err := repository.GetModuleRelease(ctx, "immutable-time", "0.1.0")
			if err != nil {
				t.Fatal(err)
			}
			if !release.CreatedAt.Equal(published) {
				t.Fatalf("publication time changed to %s", release.CreatedAt)
			}
		})
	}
}

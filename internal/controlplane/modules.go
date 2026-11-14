package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

type ModuleDefinition struct {
	Name         string                    `yaml:"name" json:"name"`
	DisplayName  string                    `yaml:"displayName,omitempty" json:"displayName,omitempty"`
	Description  string                    `yaml:"description,omitempty" json:"description,omitempty"`
	Variables    map[string]ModuleVariable `yaml:"variables,omitempty" json:"variables,omitempty"`
	Entities     []ModuleEntityDefinition  `yaml:"entities,omitempty" json:"entities,omitempty"`
	Submodules   []ModuleReference         `yaml:"submodules,omitempty" json:"submodules,omitempty"`
	Restrictions ModuleRestrictions        `yaml:"restrictions,omitempty" json:"restrictions,omitempty"`
}

// ModuleVariable is VERIFIED against the published contract, field for field
// (https://www.palantir.com/docs/apollo/managing-modules/create-module/, reviewed 2026-08-07).
// Recorded because most of the Module layer around it is NOT -- no recovered corpus carries Apollo
// Modules at all -- so without saying which part is faithful, a later cycle cannot tell this from
// the invented parts and may "fix" a model that is already right.
//
//	the three types            string, options, stringPreset
//	the reference syntax       {{ moduleVariable.name }}
//	string                     defaultValue, description
//	options                    values, defaultValue, description
//	stringPreset               description, options (naming an options variable), mapping
//	and the rule that "each stringPreset mapping must include all options defined in the
//	referenced options variable" -- enforced in validateModuleVariables below
//
// What is NOT verified is anything about which FIELDS accept a reference. entityName is documented
// as "an identifier for the Entity, defaults to the artifactId from the Entity's ProductId" and is
// never shown templated; this renderer accordingly renders KubernetesNamespace, ConfigOverrides,
// InstallConfigOverrides and SecretRequirements, and not that.
type ModuleVariable struct {
	Type         string                      `yaml:"type" json:"type"`
	String       *ModuleStringVariable       `yaml:"string,omitempty" json:"string,omitempty"`
	Options      *ModuleOptionsVariable      `yaml:"options,omitempty" json:"options,omitempty"`
	StringPreset *ModuleStringPresetVariable `yaml:"stringPreset,omitempty" json:"stringPreset,omitempty"`
}

type ModuleStringVariable struct {
	DefaultValue *string                   `yaml:"defaultValue,omitempty" json:"defaultValue,omitempty"`
	Description  string                    `yaml:"description,omitempty" json:"description,omitempty"`
	Constraint   *ModuleVariableConstraint `yaml:"constraint,omitempty" json:"constraint,omitempty"`
}

type ModuleVariableConstraint struct {
	Type  string   `yaml:"type" json:"type"`
	Regex string   `yaml:"regex,omitempty" json:"regex,omitempty"`
	OneOf []string `yaml:"oneOf,omitempty" json:"oneOf,omitempty"`
}

type ModuleOptionsVariable struct {
	Values       []string `yaml:"values" json:"values"`
	DefaultValue string   `yaml:"defaultValue,omitempty" json:"defaultValue,omitempty"`
	Description  string   `yaml:"description,omitempty" json:"description,omitempty"`
}

type ModuleStringPresetVariable struct {
	Description string            `yaml:"description,omitempty" json:"description,omitempty"`
	Options     string            `yaml:"options" json:"options"`
	Mapping     map[string]string `yaml:"mapping" json:"mapping"`
}

type ModuleEntityDefinition struct {
	Type      string                `yaml:"type" json:"type"`
	HelmChart ModuleHelmChartEntity `yaml:"helmChart" json:"helmChart"`
}

type ModuleHelmChartEntity struct {
	ProductID                string                         `yaml:"productId" json:"productId"`
	EntityName               string                         `yaml:"entityName,omitempty" json:"entityName,omitempty"`
	KubernetesNamespace      string                         `yaml:"k8sNamespace" json:"k8sNamespace"`
	IgnoredDependencies      []string                       `yaml:"ignoredDependencies,omitempty" json:"ignoredDependencies,omitempty"`
	DependencyGroups         []string                       `yaml:"dependencyGroups,omitempty" json:"dependencyGroups,omitempty"`
	DependencyGroupOverrides map[string][]string            `yaml:"dependencyGroupOverrides,omitempty" json:"dependencyGroupOverrides,omitempty"`
	SyncConfigOverrides      string                         `yaml:"syncConfigOverrides,omitempty" json:"syncConfigOverrides,omitempty"`
	ConfigOverrides          string                         `yaml:"configOverrides,omitempty" json:"configOverrides,omitempty"`
	InstallConfigOverrides   string                         `yaml:"installConfigOverrides,omitempty" json:"installConfigOverrides,omitempty"`
	SecretRequirements       map[string]any                 `yaml:"secretRequirements,omitempty" json:"secretRequirements,omitempty"`
	MarkedForUninstallation  *ModuleMarkedForUninstallation `yaml:"markedForUninstallation,omitempty" json:"markedForUninstallation,omitempty"`
}

type ModuleMarkedForUninstallation struct {
	MinVersion             string `yaml:"minVersion,omitempty" json:"minVersion,omitempty"`
	UnmanageAfterUninstall string `yaml:"unmanageAfterUninstall,omitempty" json:"unmanageAfterUninstall,omitempty"`
}

type ModuleReference struct {
	Name              string                 `yaml:"name" json:"name"`
	Version           string                 `yaml:"version" json:"version"`
	Key               string                 `yaml:"key,omitempty" json:"key,omitempty"`
	VariableOverrides map[string]string      `yaml:"variableOverrides,omitempty" json:"variableOverrides,omitempty"`
	EntityOverrides   []ModuleEntityOverride `yaml:"entityOverrides,omitempty" json:"entityOverrides,omitempty"`
}

type ModuleEntityOverride struct {
	Type      string                        `yaml:"type" json:"type"`
	HelmChart ModuleHelmChartEntityOverride `yaml:"helmChart" json:"helmChart"`
}

type ModuleHelmChartEntityOverride struct {
	ProductID  string                     `yaml:"productId" json:"productId"`
	EntityName string                     `yaml:"entityName,omitempty" json:"entityName,omitempty"`
	Override   ModuleHelmChartEntityPatch `yaml:"override" json:"override"`
}

type ModuleHelmChartEntityPatch struct {
	ConfigOverrides    string         `yaml:"configOverrides,omitempty" json:"configOverrides,omitempty"`
	SecretRequirements map[string]any `yaml:"secretRequirements,omitempty" json:"secretRequirements,omitempty"`
}

type ModuleRestrictions struct {
	NewInstallations struct {
		Allowed *bool `yaml:"allowed,omitempty" json:"allowed,omitempty"`
	} `yaml:"newInstallations,omitempty" json:"newInstallations,omitempty"`
	SubmoduleUsage struct {
		AllowEntityOverrides *bool `yaml:"allowEntityOverrides,omitempty" json:"allowEntityOverrides,omitempty"`
	} `yaml:"submoduleUsage,omitempty" json:"submoduleUsage,omitempty"`
}

type ModuleLock struct {
	Module        string                    `json:"module"`
	Submodules    []LockedModuleReference   `json:"submodules,omitempty"`
	Variables     map[string]ModuleVariable `json:"variables,omitempty"`
	Entities      []LockedModuleEntity      `json:"entities"`
	ContentDigest string                    `json:"contentDigest"`
}

type LockedModuleReference struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Key     string `json:"key,omitempty"`
}

type LockedModuleEntity struct {
	SourceModule             string                         `json:"sourceModule"`
	InstancePath             string                         `json:"instancePath,omitempty"`
	Type                     string                         `json:"type"`
	ProductID                string                         `json:"productId"`
	EntityName               string                         `json:"entityName"`
	KubernetesNamespace      string                         `json:"k8sNamespace"`
	IgnoredDependencies      []string                       `json:"ignoredDependencies,omitempty"`
	DependencyGroups         []string                       `json:"dependencyGroups,omitempty"`
	DependencyGroupOverrides map[string][]string            `json:"dependencyGroupOverrides,omitempty"`
	SyncConfigOverrides      string                         `json:"syncConfigOverrides,omitempty"`
	ConfigOverrides          string                         `json:"configOverrides,omitempty"`
	InstallConfigOverrides   string                         `json:"installConfigOverrides,omitempty"`
	SecretRequirements       map[string]any                 `json:"secretRequirements,omitempty"`
	MarkedForUninstallation  *ModuleMarkedForUninstallation `json:"markedForUninstallation,omitempty"`
}

type ModuleRelease struct {
	Name            string           `json:"name"`
	Version         string           `json:"version"`
	Digest          string           `json:"digest"`
	Definition      ModuleDefinition `json:"definition"`
	Lock            ModuleLock       `json:"lock"`
	ReleaseChannels []string         `json:"releaseChannels"`
	Recalled        bool             `json:"recalled,omitempty"`
	CreatedAt       time.Time        `json:"createdAt"`
}

type ModuleInstallation struct {
	ID                    string                   `json:"id"`
	Environment           string                   `json:"environment"`
	Module                string                   `json:"module"`
	Version               string                   `json:"version"`
	ReleaseChannel        string                   `json:"releaseChannel"`
	Variables             map[string]string        `json:"variables"`
	Entities              []InstalledModuleEntity  `json:"entities"`
	Status                string                   `json:"status"`
	CreatedAt             time.Time                `json:"createdAt"`
	UpdatedAt             time.Time                `json:"updatedAt"`
	Revision              uint64                   `json:"revision"`
	LastAutomaticApproval *ModuleAutomaticApproval `json:"lastAutomaticApproval,omitempty"`
}

type ModuleAutoUpdatePolicy struct {
	Module       string    `json:"module"`
	Enabled      bool      `json:"enabled"`
	Rationale    string    `json:"rationale"`
	ConfiguredBy string    `json:"configuredBy"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Revision     uint64    `json:"revision"`
}

type ModuleAutomaticApproval struct {
	ChangeRequestID string    `json:"changeRequestId"`
	FromVersion     string    `json:"fromVersion"`
	ToVersion       string    `json:"toVersion"`
	Rationale       string    `json:"rationale"`
	ApprovedBy      string    `json:"approvedBy"`
	ApprovedAt      time.Time `json:"approvedAt"`
	PolicyRevision  uint64    `json:"policyRevision"`
}

type InstalledModuleEntity struct {
	Key                      string                         `json:"key"`
	InstancePath             string                         `json:"instancePath,omitempty"`
	ProductID                string                         `json:"productId"`
	EntityName               string                         `json:"entityName"`
	KubernetesNamespace      string                         `json:"k8sNamespace"`
	IgnoredDependencies      []string                       `json:"ignoredDependencies,omitempty"`
	DependencyGroups         []string                       `json:"dependencyGroups,omitempty"`
	DependencyGroupOverrides map[string][]string            `json:"dependencyGroupOverrides,omitempty"`
	SyncConfigOverrides      string                         `json:"syncConfigOverrides,omitempty"`
	ConfigOverrides          string                         `json:"configOverrides,omitempty"`
	InstallConfigOverrides   string                         `json:"installConfigOverrides,omitempty"`
	SecretRequirements       map[string]any                 `json:"secretRequirements,omitempty"`
	MarkedForUninstallation  *ModuleMarkedForUninstallation `json:"markedForUninstallation,omitempty"`
}

// UpdateBlockerVariablesUnresolved is the blocker kind for a Module update
// whose variables cannot be inferred from the installation. Kinds are a closed
// set so a reader can group and act on them; a message alone is prose, and
// prose has to be parsed back apart to be useful.
//
// INFERRED. The published docs describe unlinking a Module -- "the Entities
// that were part of the Module will remain in the Environment after the Module
// is unlinked but will no longer be managed by the Module"
// (https://www.palantir.com/docs/apollo/managing-modules/install-module/,
// reviewed 2026-08-07) -- but name no blocking reasons, and no recovered
// artifact carries Apollo Modules at all. The identifier is ours.
const UpdateBlockerVariablesUnresolved = "VARIABLES_UNRESOLVED"

// UpdateBlocker is one reason an update cannot proceed. Subjects names the
// specific things at fault -- the variables, the entities -- because "some
// variables cannot be inferred" sends an operator looking through the whole
// Module for which ones.
type UpdateBlocker struct {
	Kind     string   `json:"kind"`
	Message  string   `json:"message"`
	Subjects []string `json:"subjects,omitempty"`
}

type ModuleUpdateProposal struct {
	InstallationID string `json:"installationId"`
	CurrentVersion string `json:"currentVersion"`
	TargetVersion  string `json:"targetVersion"`
	Status         string `json:"status"`
	// Blockers is the structured form; Reasons is derived from it and kept
	// because existing callers read it. Deriving rather than maintaining both
	// is what stops the two disagreeing.
	Blockers              []UpdateBlocker `json:"blockers,omitempty"`
	Reasons               []string        `json:"reasons,omitempty"`
	Added                 []string        `json:"added,omitempty"`
	Changed               []string        `json:"changed,omitempty"`
	Relinquished          []string        `json:"relinquished,omitempty"`
	ConfigOverrideChanges []string        `json:"configOverrideChanges,omitempty"`
}

type ModuleRepository interface {
	CreateModuleRelease(context.Context, ModuleRelease) (ModuleRelease, error)
	GetModuleRelease(context.Context, string, string) (ModuleRelease, error)
	ListModuleReleases(context.Context, string) ([]ModuleRelease, error)
	UpdateModuleRelease(context.Context, string, string, func(*ModuleRelease) error) (ModuleRelease, error)
	CreateModuleInstallation(context.Context, ModuleInstallation) (ModuleInstallation, error)
	GetModuleInstallation(context.Context, string) (ModuleInstallation, error)
	ListModuleInstallations(context.Context, string) ([]ModuleInstallation, error)
	UpdateModuleInstallation(context.Context, string, func(*ModuleInstallation) error) (ModuleInstallation, error)
	UpdateModuleInstallationAutomatically(context.Context, string, string, uint64, func(*ModuleInstallation) error) (ModuleInstallation, error)
	PutModuleAutoUpdatePolicy(context.Context, ModuleAutoUpdatePolicy) (ModuleAutoUpdatePolicy, error)
	GetModuleAutoUpdatePolicy(context.Context, string) (ModuleAutoUpdatePolicy, error)
	CreateChangeRequest(context.Context, ChangeRequest) (ChangeRequest, error)
}

func validateModuleAutoUpdatePolicy(policy ModuleAutoUpdatePolicy) error {
	if !moduleNamePattern.MatchString(policy.Module) || policy.Revision == 0 ||
		strings.TrimSpace(policy.Rationale) == "" || strings.TrimSpace(policy.ConfiguredBy) == "" ||
		policy.CreatedAt.IsZero() || policy.UpdatedAt.IsZero() || policy.UpdatedAt.Before(policy.CreatedAt) {
		return fmt.Errorf("%w: invalid Module automatic update policy", ErrConflict)
	}
	return nil
}

type ModuleService struct {
	repository ModuleRepository
	now        func() time.Time
}

func NewModuleService(repository ModuleRepository) *ModuleService {
	return &ModuleService{repository: repository, now: time.Now}
}

func DecodeModuleDefinition(reader io.Reader) (ModuleDefinition, error) {
	decoder := yaml.NewDecoder(io.LimitReader(reader, 1<<20))
	decoder.KnownFields(true)
	var definition ModuleDefinition
	if err := decoder.Decode(&definition); err != nil {
		return ModuleDefinition{}, fmt.Errorf("decode Module definition: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return ModuleDefinition{}, errors.New("Module definition must contain exactly one YAML document")
		}
		return ModuleDefinition{}, fmt.Errorf("decode trailing Module data: %w", err)
	}
	if errs := ValidateModuleDefinition(definition); len(errs) > 0 {
		return ModuleDefinition{}, ValidationErrors(errs)
	}
	return definition, nil
}

type ValidationErrors []ValidationError

func (errs ValidationErrors) Error() string {
	parts := make([]string, len(errs))
	for index, err := range errs {
		parts[index] = err.Path + ": " + err.Message
	}
	return strings.Join(parts, "; ")
}

var moduleNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

// moduleReleaseVersionPattern is the persistence grammar for exact Module
// coordinates. Orderable Apollo versions remain valid, while imported opaque
// coordinates such as the observed "V98" are preserved byte-for-byte. The
// bounded path-safe alphabet prevents a coordinate from escaping its state
// directory; ordering is decided separately by sortModuleReleases.
var moduleReleaseVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var moduleTemplatePattern = regexp.MustCompile(`\{\{\s*moduleVariable\.([A-Za-z0-9_.-]+)\s*\}\}`)
var exactModuleTemplatePattern = regexp.MustCompile(`^\{\{\s*moduleVariable\.([A-Za-z0-9_.-]+)\s*\}\}$`)

func ValidateModuleDefinition(definition ModuleDefinition) []ValidationError {
	errs := make([]ValidationError, 0)
	add := func(path, message string) { errs = append(errs, ValidationError{Path: path, Message: message}) }
	if !moduleNamePattern.MatchString(definition.Name) {
		add("name", "must be a lowercase DNS-style Module name")
	}
	if len(definition.Entities) == 0 && len(definition.Submodules) == 0 {
		add("module", "must contain at least one Entity or submodule")
	}
	for name, variable := range definition.Variables {
		path := "variables." + name
		switch variable.Type {
		case "string":
			if variable.String == nil {
				add(path+".string", "is required for a string variable")
			}
		case "options":
			if variable.Options == nil || len(variable.Options.Values) == 0 {
				add(path+".options.values", "must contain at least one option")
			} else if variable.Options.DefaultValue != "" && !containsString(variable.Options.Values, variable.Options.DefaultValue) {
				add(path+".options.defaultValue", "must be one of the configured options")
			}
		case "stringPreset":
			if variable.StringPreset == nil {
				add(path+".stringPreset", "is required")
			} else if options, exists := definition.Variables[variable.StringPreset.Options]; !exists || options.Type != "options" || options.Options == nil {
				add(path+".stringPreset.options", "must reference an options variable")
			} else {
				for _, option := range options.Options.Values {
					if _, exists := variable.StringPreset.Mapping[option]; !exists {
						add(path+".stringPreset.mapping", "must map option "+option)
					}
				}
			}
		default:
			add(path+".type", "must be string, options, or stringPreset")
		}
	}
	entityKeys := map[string]struct{}{}
	for index, entity := range definition.Entities {
		path := fmt.Sprintf("entities[%d]", index)
		if entity.Type != "helmChart" {
			add(path+".type", "must be helmChart")
		}
		if !coordinatePattern.MatchString(entity.HelmChart.ProductID) {
			add(path+".helmChart.productId", "must be a group:name coordinate")
		}
		name := entity.HelmChart.EntityName
		if name == "" {
			pieces := strings.Split(entity.HelmChart.ProductID, ":")
			if len(pieces) == 2 {
				name = pieces[1]
			}
		}
		key := "helmChart:" + entity.HelmChart.ProductID + ":" + name
		if _, exists := entityKeys[key]; exists {
			add(path, "duplicates an Entity identity")
		}
		entityKeys[key] = struct{}{}
		validateModuleTemplates(path, definition.Variables, entity.HelmChart.KubernetesNamespace+"\n"+entity.HelmChart.ConfigOverrides+"\n"+entity.HelmChart.InstallConfigOverrides, &errs)
		secretJSON, _ := json.Marshal(entity.HelmChart.SecretRequirements)
		validateModuleTemplates(path+".helmChart.secretRequirements", definition.Variables, string(secretJSON), &errs)
		if entity.HelmChart.SyncConfigOverrides != "" && entity.HelmChart.SyncConfigOverrides != "ENABLED" && entity.HelmChart.SyncConfigOverrides != "MERGE" && entity.HelmChart.SyncConfigOverrides != "DISABLED" {
			add(path+".helmChart.syncConfigOverrides", "must be ENABLED, MERGE, or DISABLED")
		}
		if entity.HelmChart.InstallConfigOverrides != "" && entity.HelmChart.SyncConfigOverrides != "MERGE" {
			add(path+".helmChart.installConfigOverrides", "requires syncConfigOverrides: MERGE")
		}
		if entity.HelmChart.MarkedForUninstallation != nil {
			marked := entity.HelmChart.MarkedForUninstallation
			if marked.MinVersion != "" && !versionPattern.MatchString(marked.MinVersion) {
				add(path+".helmChart.markedForUninstallation.minVersion", "must be an orderable version")
			}
			if marked.UnmanageAfterUninstall != "" && marked.UnmanageAfterUninstall != "AUTOMATIC" && marked.UnmanageAfterUninstall != "MANUAL" {
				add(path+".helmChart.markedForUninstallation.unmanageAfterUninstall", "must be AUTOMATIC or MANUAL")
			}
		}
	}
	refs := map[string]struct{}{}
	refNameCounts := map[string]int{}
	for _, reference := range definition.Submodules {
		refNameCounts[reference.Name]++
	}
	for index, reference := range definition.Submodules {
		path := fmt.Sprintf("submodules[%d]", index)
		if !moduleNamePattern.MatchString(reference.Name) {
			add(path+".name", "must be a Module name")
		}
		if reference.Version != "local" && !versionPattern.MatchString(reference.Version) {
			add(path+".version", "must be local or an exact orderable version")
		}
		if refNameCounts[reference.Name] > 1 && reference.Key == "" {
			add(path+".key", "is required when the same submodule is referenced more than once")
		}
		key := reference.Name + ":" + reference.Key
		if _, exists := refs[key]; exists {
			add(path, "duplicates a submodule name/key")
		}
		refs[key] = struct{}{}
		for variable, value := range reference.VariableOverrides {
			validateModuleTemplates(path+".variableOverrides."+variable, definition.Variables, value, &errs)
		}
		for overrideIndex, override := range reference.EntityOverrides {
			overridePath := fmt.Sprintf("%s.entityOverrides[%d]", path, overrideIndex)
			if override.Type != "helmChart" {
				add(overridePath+".type", "must be helmChart")
			}
			if !coordinatePattern.MatchString(override.HelmChart.ProductID) {
				add(overridePath+".helmChart.productId", "must be a group:name coordinate")
			}
			if override.HelmChart.Override.ConfigOverrides == "" && len(override.HelmChart.Override.SecretRequirements) == 0 {
				add(overridePath+".helmChart.override", "must contain configOverrides or secretRequirements")
			}
			secretJSON, _ := json.Marshal(override.HelmChart.Override.SecretRequirements)
			validateModuleTemplates(overridePath, definition.Variables, override.HelmChart.Override.ConfigOverrides+"\n"+string(secretJSON), &errs)
		}
	}
	return errs
}

func validateModuleTemplates(path string, variables map[string]ModuleVariable, value string, errs *[]ValidationError) {
	for _, match := range moduleTemplatePattern.FindAllStringSubmatch(value, -1) {
		if _, exists := variables[match[1]]; !exists {
			*errs = append(*errs, ValidationError{Path: path, Message: "references undefined Module variable " + match[1]})
		}
	}
}

func (service *ModuleService) PublishBatch(ctx context.Context, definitions []ModuleDefinition) ([]ModuleRelease, error) {
	byName := make(map[string]ModuleDefinition, len(definitions))
	for _, definition := range definitions {
		if errs := ValidateModuleDefinition(definition); len(errs) > 0 {
			return nil, ValidationErrors(errs)
		}
		if _, exists := byName[definition.Name]; exists {
			return nil, fmt.Errorf("%w: duplicate Module %s", ErrConflict, definition.Name)
		}
		byName[definition.Name] = definition
	}
	order, err := modulePublishOrder(byName)
	if err != nil {
		return nil, err
	}
	published := make(map[string]ModuleRelease, len(order))
	result := make([]ModuleRelease, 0, len(order))
	for _, name := range order {
		definition := byName[name]
		lock, err := service.buildLock(ctx, definition, published)
		if err != nil {
			return nil, fmt.Errorf("lock Module %s: %w", name, err)
		}
		releases, err := service.repository.ListModuleReleases(ctx, name)
		if err != nil {
			return nil, err
		}
		digest := moduleReleaseDigest(definition, lock)
		if len(releases) > 0 && releases[len(releases)-1].Digest == digest {
			published[name] = releases[len(releases)-1]
			result = append(result, releases[len(releases)-1])
			continue
		}
		version := nextModuleVersion(releases)
		release := ModuleRelease{
			Name: name, Version: version, Digest: digest, Definition: definition, Lock: lock,
			ReleaseChannels: defaultModuleReleaseChannels(version), CreatedAt: service.now().UTC(),
		}
		release, err = service.repository.CreateModuleRelease(ctx, release)
		if err != nil {
			return nil, err
		}
		published[name] = release
		result = append(result, release)
	}
	return result, nil
}

// Lock resolves one Module definition against immutable published submodule
// Releases without publishing a new Module Release.
func (service *ModuleService) Lock(ctx context.Context, definition ModuleDefinition) (ModuleLock, error) {
	if errs := ValidateModuleDefinition(definition); len(errs) > 0 {
		return ModuleLock{}, ValidationErrors(errs)
	}
	return service.buildLock(ctx, definition, nil)
}

func modulePublishOrder(definitions map[string]ModuleDefinition) ([]string, error) {
	state := map[string]int{}
	order := make([]string, 0, len(definitions))
	var visit func(string) error
	visit = func(name string) error {
		if state[name] == 1 {
			return fmt.Errorf("Module submodule cycle includes %s", name)
		}
		if state[name] == 2 {
			return nil
		}
		state[name] = 1
		definition := definitions[name]
		dependencies := make([]string, 0)
		for _, reference := range definition.Submodules {
			if reference.Version == "local" {
				if _, exists := definitions[reference.Name]; !exists {
					return fmt.Errorf("local submodule %s is absent from publication batch", reference.Name)
				}
				dependencies = append(dependencies, reference.Name)
			}
		}
		sort.Strings(dependencies)
		for _, dependency := range dependencies {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[name] = 2
		order = append(order, name)
		return nil
	}
	names := make([]string, 0, len(definitions))
	for name := range definitions {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func (service *ModuleService) buildLock(ctx context.Context, definition ModuleDefinition, local map[string]ModuleRelease) (ModuleLock, error) {
	lock := ModuleLock{Module: definition.Name, Variables: cloneModuleVariables(definition.Variables), Entities: []LockedModuleEntity{}}
	for _, entity := range definition.Entities {
		name := entity.HelmChart.EntityName
		if name == "" {
			name = strings.Split(entity.HelmChart.ProductID, ":")[1]
		}
		lock.Entities = append(lock.Entities, LockedModuleEntity{
			SourceModule: definition.Name, Type: entity.Type, ProductID: entity.HelmChart.ProductID, EntityName: name,
			KubernetesNamespace: entity.HelmChart.KubernetesNamespace, IgnoredDependencies: entity.HelmChart.IgnoredDependencies,
			DependencyGroups: entity.HelmChart.DependencyGroups, DependencyGroupOverrides: entity.HelmChart.DependencyGroupOverrides,
			SyncConfigOverrides: entity.HelmChart.SyncConfigOverrides,
			ConfigOverrides:     entity.HelmChart.ConfigOverrides, InstallConfigOverrides: entity.HelmChart.InstallConfigOverrides,
			SecretRequirements: entity.HelmChart.SecretRequirements, MarkedForUninstallation: entity.HelmChart.MarkedForUninstallation,
		})
	}
	for _, reference := range definition.Submodules {
		var release ModuleRelease
		var err error
		if reference.Version == "local" {
			var exists bool
			release, exists = local[reference.Name]
			if !exists {
				return ModuleLock{}, fmt.Errorf("local submodule %s has not been published", reference.Name)
			}
		} else {
			release, err = service.repository.GetModuleRelease(ctx, reference.Name, reference.Version)
			if err != nil {
				return ModuleLock{}, err
			}
		}
		if len(reference.EntityOverrides) > 0 && release.Definition.Restrictions.SubmoduleUsage.AllowEntityOverrides != nil && !*release.Definition.Restrictions.SubmoduleUsage.AllowEntityOverrides {
			return ModuleLock{}, fmt.Errorf("%w: Module %s does not allow Entity overrides", ErrConflict, reference.Name)
		}
		if err := validateSubmoduleVariableOverrides(definition, release, reference); err != nil {
			return ModuleLock{}, err
		}
		lock.Submodules = append(lock.Submodules, LockedModuleReference{Name: reference.Name, Version: release.Version, Key: reference.Key})
		mappings := map[string]string{}
		qualifiedPrefix := reference.Name
		instanceSegment := reference.Name
		if reference.Key != "" {
			qualifiedPrefix += "." + reference.Key
			instanceSegment += "[" + reference.Key + "]"
		}
		for childVariable, sourceDefinition := range release.Lock.Variables {
			childDefinition := cloneJSON(sourceDefinition)
			if override, exists := reference.VariableOverrides[childVariable]; exists {
				mappings[childVariable] = override
				continue
			}
			if childDefinition.Type == "stringPreset" && childDefinition.StringPreset != nil {
				controller := childDefinition.StringPreset.Options
				if override, exists := reference.VariableOverrides[controller]; exists {
					if match := exactModuleTemplatePattern.FindStringSubmatch(override); match != nil {
						qualified := qualifiedPrefix + "." + childVariable
						copied := childDefinition
						copied.StringPreset.Options = match[1]
						lock.Variables[qualified] = copied
						mappings[childVariable] = "{{ moduleVariable." + qualified + " }}"
					} else {
						mappings[childVariable] = childDefinition.StringPreset.Mapping[override]
					}
					continue
				}
			}
			qualified := qualifiedPrefix + "." + childVariable
			mappings[childVariable] = "{{ moduleVariable." + qualified + " }}"
			copied := childDefinition
			if copied.Type == "stringPreset" && copied.StringPreset != nil {
				copied.StringPreset.Options = qualifiedPrefix + "." + copied.StringPreset.Options
			}
			lock.Variables[qualified] = copied
		}
		for _, child := range release.Lock.Entities {
			if child.InstancePath == "" {
				child.InstancePath = instanceSegment
			} else {
				child.InstancePath = instanceSegment + "/" + child.InstancePath
			}
			child.KubernetesNamespace = rewriteModuleTemplates(child.KubernetesNamespace, mappings)
			child.ConfigOverrides = rewriteModuleTemplates(child.ConfigOverrides, mappings)
			child.InstallConfigOverrides = rewriteModuleTemplates(child.InstallConfigOverrides, mappings)
			if rewritten, ok := rewriteModuleTemplatesInValue(child.SecretRequirements, mappings).(map[string]any); ok {
				child.SecretRequirements = rewritten
			}
			var overrideErr error
			child, overrideErr = applyEntityOverrides(child, reference.EntityOverrides)
			if overrideErr != nil {
				return ModuleLock{}, fmt.Errorf("submodule %s Entity override: %w", reference.Name, overrideErr)
			}
			lock.Entities = append(lock.Entities, child)
		}
		if err := ensureEntityOverridesMatched(release.Lock.Entities, reference.EntityOverrides); err != nil {
			return ModuleLock{}, fmt.Errorf("submodule %s Entity override: %w", reference.Name, err)
		}
	}
	sort.Slice(lock.Submodules, func(i, j int) bool {
		return lock.Submodules[i].Name+":"+lock.Submodules[i].Key < lock.Submodules[j].Name+":"+lock.Submodules[j].Key
	})
	sort.Slice(lock.Entities, func(i, j int) bool { return lockedEntityKey(lock.Entities[i]) < lockedEntityKey(lock.Entities[j]) })
	for index := 1; index < len(lock.Entities); index++ {
		if lockedEntityKey(lock.Entities[index-1]) == lockedEntityKey(lock.Entities[index]) {
			return ModuleLock{}, fmt.Errorf("%w: flattened Module duplicates Entity instance %s", ErrConflict, lockedEntityKey(lock.Entities[index]))
		}
	}
	digestCopy := lock
	digestCopy.ContentDigest = ""
	contents, _ := json.Marshal(digestCopy)
	digest := sha256.Sum256(contents)
	lock.ContentDigest = "sha256:" + hex.EncodeToString(digest[:])
	return lock, nil
}

func rewriteModuleTemplates(value string, mappings map[string]string) string {
	return moduleTemplatePattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := moduleTemplatePattern.FindStringSubmatch(match)
		if replacement, exists := mappings[parts[1]]; exists {
			return replacement
		}
		return match
	})
}

func validateSubmoduleVariableOverrides(parent ModuleDefinition, child ModuleRelease, reference ModuleReference) error {
	for name, value := range reference.VariableOverrides {
		definition, exists := child.Definition.Variables[name]
		if !exists {
			return fmt.Errorf("variableOverrides.%s does not name a variable defined directly by Module %s", name, reference.Name)
		}
		if definition.Type == "stringPreset" {
			return fmt.Errorf("variableOverrides.%s cannot override a stringPreset variable", name)
		}
		if definition.Type != "options" || definition.Options == nil {
			continue
		}
		if match := exactModuleTemplatePattern.FindStringSubmatch(value); match != nil {
			parentVariable, exists := parent.Variables[match[1]]
			if !exists || parentVariable.Type != "options" || parentVariable.Options == nil {
				return fmt.Errorf("variableOverrides.%s must reference an options variable", name)
			}
			for _, option := range parentVariable.Options.Values {
				if !containsString(definition.Options.Values, option) {
					return fmt.Errorf("variableOverrides.%s parent option %q is not accepted by the submodule", name, option)
				}
			}
			continue
		}
		if moduleTemplatePattern.MatchString(value) || !containsString(definition.Options.Values, value) {
			return fmt.Errorf("variableOverrides.%s must be an accepted option or an options-variable reference", name)
		}
	}
	return nil
}

func applyEntityOverrides(entity LockedModuleEntity, overrides []ModuleEntityOverride) (LockedModuleEntity, error) {
	for _, candidate := range overrides {
		if !entityOverrideMatches(entity, candidate) {
			continue
		}
		merged, err := mergeModuleConfigYAML(entity.ConfigOverrides, candidate.HelmChart.Override.ConfigOverrides)
		if err != nil {
			return LockedModuleEntity{}, err
		}
		entity.ConfigOverrides = merged
		if entity.SecretRequirements == nil {
			entity.SecretRequirements = map[string]any{}
		} else {
			entity.SecretRequirements = cloneJSON(entity.SecretRequirements)
		}
		for name, requirement := range candidate.HelmChart.Override.SecretRequirements {
			entity.SecretRequirements[name] = cloneJSON(requirement)
		}
	}
	return entity, nil
}

func ensureEntityOverridesMatched(entities []LockedModuleEntity, overrides []ModuleEntityOverride) error {
	for index, override := range overrides {
		matches := 0
		for _, entity := range entities {
			if entityOverrideMatches(entity, override) {
				matches++
			}
		}
		if matches == 0 {
			return fmt.Errorf("entityOverrides[%d] does not match a flattened Entity", index)
		}
		if override.HelmChart.EntityName == "" && matches > 1 {
			return fmt.Errorf("entityOverrides[%d] is ambiguous; entityName is required", index)
		}
	}
	return nil
}

func entityOverrideMatches(entity LockedModuleEntity, override ModuleEntityOverride) bool {
	return override.Type == entity.Type && override.HelmChart.ProductID == entity.ProductID &&
		(override.HelmChart.EntityName == "" || override.HelmChart.EntityName == entity.EntityName)
}

func mergeModuleConfigYAML(base, patch string) (string, error) {
	if strings.TrimSpace(patch) == "" {
		return base, nil
	}
	if strings.TrimSpace(base) == "" {
		base = "{}"
	}
	templates := map[string]string{}
	templateIndex := 0
	protect := func(value string) string {
		return moduleTemplatePattern.ReplaceAllStringFunc(value, func(match string) string {
			key := fmt.Sprintf("__PLATFORM_MODULE_TEMPLATE_%d__", templateIndex)
			templateIndex++
			templates[key] = match
			return key
		})
	}
	var left, right map[string]any
	if err := yaml.Unmarshal([]byte(protect(base)), &left); err != nil {
		return "", fmt.Errorf("parse existing configOverrides: %w", err)
	}
	if err := yaml.Unmarshal([]byte(protect(patch)), &right); err != nil {
		return "", fmt.Errorf("parse overriding configOverrides: %w", err)
	}
	merged := mergeStringMaps(left, right)
	contents, err := yaml.Marshal(merged)
	if err != nil {
		return "", fmt.Errorf("encode merged configOverrides: %w", err)
	}
	result := string(contents)
	for sentinel, template := range templates {
		result = strings.ReplaceAll(result, sentinel, template)
	}
	return result, nil
}

func mergeStringMaps(base, patch map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}
	for key, patchValue := range patch {
		if text, ok := patchValue.(string); ok && strings.TrimSpace(text) == "{{ unset }}" {
			delete(base, key)
			continue
		}
		baseValue, exists := base[key]
		baseMap, baseIsMap := baseValue.(map[string]any)
		patchMap, patchIsMap := patchValue.(map[string]any)
		if patchIsMap {
			if !exists || !baseIsMap {
				baseMap = map[string]any{}
			}
			base[key] = mergeStringMaps(baseMap, patchMap)
		} else {
			base[key] = patchValue
		}
	}
	return base
}

func rewriteModuleTemplatesInValue(value any, mappings map[string]string) any {
	switch typed := value.(type) {
	case string:
		return rewriteModuleTemplates(typed, mappings)
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			result[key] = rewriteModuleTemplatesInValue(child, mappings)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			result[index] = rewriteModuleTemplatesInValue(child, mappings)
		}
		return result
	default:
		return value
	}
}

func cloneModuleVariables(input map[string]ModuleVariable) map[string]ModuleVariable {
	contents, _ := json.Marshal(input)
	var result map[string]ModuleVariable
	_ = json.Unmarshal(contents, &result)
	if result == nil {
		result = map[string]ModuleVariable{}
	}
	return result
}

func moduleReleaseDigest(definition ModuleDefinition, lock ModuleLock) string {
	contents, _ := json.Marshal(struct {
		Definition ModuleDefinition `json:"definition"`
		Lock       ModuleLock       `json:"lock"`
	}{definition, lock})
	digest := sha256.Sum256(contents)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func nextModuleVersion(releases []ModuleRelease) string {
	if len(releases) == 0 {
		return "0.1.0"
	}
	parts := strings.Split(releases[len(releases)-1].Version, ".")
	minor, _ := strconv.Atoi(parts[1])
	return fmt.Sprintf("0.%d.0", minor+1)
}

func defaultModuleReleaseChannels(version string) []string {
	if versionPattern.MatchString(version) {
		return []string{"DEV", "RELEASE_CANDIDATE", "RELEASE"}
	}
	return []string{"DEV"}
}

func (service *ModuleService) PromoteRelease(ctx context.Context, module, version string, channels []string) (ModuleRelease, error) {
	return service.repository.UpdateModuleRelease(ctx, module, version, func(release *ModuleRelease) error {
		set := map[string]struct{}{}
		for _, channel := range release.ReleaseChannels {
			set[channel] = struct{}{}
		}
		for _, channel := range channels {
			channel = strings.TrimSpace(channel)
			if channel == "" {
				return errors.New("Release Channel must not be empty")
			}
			set[channel] = struct{}{}
		}
		release.ReleaseChannels = release.ReleaseChannels[:0]
		for channel := range set {
			release.ReleaseChannels = append(release.ReleaseChannels, channel)
		}
		sort.Strings(release.ReleaseChannels)
		return nil
	})
}

func (service *ModuleService) RecallRelease(ctx context.Context, module, version string) (ModuleRelease, error) {
	return service.repository.UpdateModuleRelease(ctx, module, version, func(release *ModuleRelease) error {
		release.Recalled = true
		return nil
	})
}

func (service *ModuleService) Install(ctx context.Context, environment, module, version string, supplied map[string]string) (ModuleInstallation, error) {
	return service.InstallTrackingChannel(ctx, environment, module, version, "RELEASE", supplied)
}

func (service *ModuleService) InstallTrackingChannel(ctx context.Context, environment, module, version, channel string, supplied map[string]string) (ModuleInstallation, error) {
	if channel == "" {
		channel = "RELEASE"
	}
	release, err := service.resolveReleaseForChannel(ctx, module, version, channel)
	if err != nil {
		return ModuleInstallation{}, err
	}
	installation, err := service.prepareModuleInstallation(ctx, environment, release, channel, supplied)
	if err != nil {
		return ModuleInstallation{}, err
	}
	return service.repository.CreateModuleInstallation(ctx, installation)
}

// prepareModuleInstallation validates a read-only candidate. Selection and any
// recall exception remain the responsibility of the admission path, not this helper.
func (service *ModuleService) prepareModuleInstallation(ctx context.Context, environment string, release ModuleRelease, channel string, supplied map[string]string) (ModuleInstallation, error) {
	module := release.Name
	if release.Definition.Restrictions.NewInstallations.Allowed != nil && !*release.Definition.Restrictions.NewInstallations.Allowed {
		return ModuleInstallation{}, fmt.Errorf("%w: Module %s does not allow new installations", ErrConflict, module)
	}
	values, validation := resolveModuleVariables(release.Lock.Variables, supplied)
	if len(validation) > 0 {
		return ModuleInstallation{}, ValidationErrors(validation)
	}
	entities := renderLockedModuleEntities(release.Lock.Entities, values)
	for index := range entities {
		var err error
		entities[index], err = reconcileModuleEntityConfig(nil, entities[index], true)
		if err != nil {
			return ModuleInstallation{}, err
		}
	}
	existing, err := service.repository.ListModuleInstallations(ctx, environment)
	if err != nil {
		return ModuleInstallation{}, err
	}
	owners := map[string]string{}
	for _, installation := range existing {
		if installation.Status == "UNLINKED" {
			continue
		}
		if installation.Module == module {
			return ModuleInstallation{}, fmt.Errorf("%w: Module %s is already installed in Environment %s", ErrConflict, module, environment)
		}
		for _, entity := range installation.Entities {
			owners[entity.Key] = installation.ID
		}
	}
	for _, entity := range entities {
		if owner := owners[entity.Key]; owner != "" {
			return ModuleInstallation{}, fmt.Errorf("%w: Entity %s is managed by %s", ErrConflict, entity.Key, owner)
		}
	}
	now := service.now().UTC()
	identity := sha256.Sum256([]byte(environment + "\x00" + module))
	installation := ModuleInstallation{
		ID: "modi_" + hex.EncodeToString(identity[:16]), Environment: environment, Module: module, Version: release.Version,
		ReleaseChannel: channel, Variables: values, Entities: entities, Status: "ACTIVE", CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	return installation, nil
}

// moduleInstallChannel resolves the Release Channel a Module installation tracks
// when the request did not name one: an explicit request wins, else it inherits
// the Environment's default from the latest desired state (as Product Entities
// do). "" -- unknown Environment or no default -- leaves InstallTrackingChannel's
// "RELEASE" fallback intact.
func moduleInstallChannel(requested string, plans []PlanRecord, environment string) string {
	if requested != "" {
		return requested
	}
	if state, ok := latestDesiredStates(plans)[environment]; ok {
		return state.Environment.DefaultReleaseChannel
	}
	return ""
}

// moduleUpdateIsNewer decides whether a resolved target is actually an update.
//
// The ModuleRelease type does not require an orderable version, and upstream's
// are opaque -- its version selector shows "V98". compareVersions falls back to
// a lexicographic comparison for anything it cannot parse, which puts V10 before
// V9, so the guard this replaces would answer "already at the latest version" to
// a genuine update request and do nothing. A silent no-op reads exactly like a
// correct one.
//
// Not reachable today: PublishBatch is the only producer of a ModuleRelease and
// always assigns nextModuleVersion, which is orderable. It becomes reachable the
// moment Module releases arrive from a Hub that numbers them its own way, which
// is what the interop work exists for -- and it would arrive as an update that
// silently did not happen, which is the worst way to find out.
//
// Where both versions are orderable the comparison stands, and still refuses a
// downgrade. Where either is not, publication order decides -- the only ordering
// an opaque scheme has, and already the ordering resolveReleaseForChannel used
// to choose this target. Second-guessing it with a string comparison is what
// produced the wrong answer.
func moduleUpdateIsNewer(current, target ModuleRelease) bool {
	if current.Version == target.Version {
		return false
	}
	currentOrderable := IsApolloOrderableVersion(current.Version)
	targetOrderable := IsApolloOrderableVersion(target.Version)
	if currentOrderable && targetOrderable {
		return compareVersions(target.Version, current.Version) > 0
	}
	return target.CreatedAt.After(current.CreatedAt)
}

// sortModuleReleases keeps a Module's releases in semantic-version order only
// when every coordinate in that Module can participate in that order. If one is
// opaque, creation time is the sole ordering shared by the complete sequence.
// Applying this policy to the sequence rather than pairwise avoids a
// non-transitive comparator when semantic and opaque coordinates are mixed.
func sortModuleReleases(releases []ModuleRelease) {
	orderable := make(map[string]bool, len(releases))
	for _, release := range releases {
		if _, seen := orderable[release.Name]; !seen {
			orderable[release.Name] = true
		}
		if !IsApolloOrderableVersion(release.Version) {
			orderable[release.Name] = false
		}
	}
	sort.Slice(releases, func(i, j int) bool {
		left, right := releases[i], releases[j]
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		if orderable[left.Name] {
			return compareVersions(left.Version, right.Version) < 0
		}
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.Before(right.CreatedAt)
		}
		return left.Version < right.Version
	})
}

func (service *ModuleService) resolveReleaseForChannel(ctx context.Context, module, version, channel string) (ModuleRelease, error) {
	if version != "" && version != "latest" {
		release, err := service.repository.GetModuleRelease(ctx, module, version)
		if err != nil {
			return ModuleRelease{}, err
		}
		if release.Recalled || !containsString(release.ReleaseChannels, channel) {
			return ModuleRelease{}, fmt.Errorf("%w: Module Release %s:%s is unavailable on channel %s", ErrConflict, module, version, channel)
		}
		return release, nil
	}
	releases, err := service.repository.ListModuleReleases(ctx, module)
	if err != nil {
		return ModuleRelease{}, err
	}
	for index := len(releases) - 1; index >= 0; index-- {
		if !releases[index].Recalled && containsString(releases[index].ReleaseChannels, channel) {
			return releases[index], nil
		}
	}
	return ModuleRelease{}, ErrNotFound
}

func resolveModuleVariables(definitions map[string]ModuleVariable, supplied map[string]string) (map[string]string, []ValidationError) {
	values := make(map[string]string, len(definitions))
	errs := make([]ValidationError, 0)
	names := make([]string, 0, len(definitions))
	for name := range definitions {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		definition := definitions[name]
		value, exists := supplied[name]
		switch definition.Type {
		case "string":
			if !exists && definition.String != nil && definition.String.DefaultValue != nil {
				value, exists = *definition.String.DefaultValue, true
			}
			if !exists {
				errs = append(errs, ValidationError{Path: "variables." + name, Message: "is required"})
				continue
			}
			if definition.String != nil && definition.String.Constraint != nil && !moduleValueSatisfies(value, *definition.String.Constraint) {
				errs = append(errs, ValidationError{Path: "variables." + name, Message: "does not satisfy its constraint"})
				continue
			}
			values[name] = value
		case "options":
			if !exists && definition.Options != nil && definition.Options.DefaultValue != "" {
				value, exists = definition.Options.DefaultValue, true
			}
			if !exists || definition.Options == nil || !containsString(definition.Options.Values, value) {
				errs = append(errs, ValidationError{Path: "variables." + name, Message: "must be one of the configured options"})
				continue
			}
			values[name] = value
		case "stringPreset":
			// Derived after the controlling options variable is resolved.
		}
	}
	for _, name := range names {
		definition := definitions[name]
		if definition.Type == "stringPreset" && definition.StringPreset != nil {
			values[name] = definition.StringPreset.Mapping[values[definition.StringPreset.Options]]
		}
	}
	return values, errs
}

func moduleValueSatisfies(value string, constraint ModuleVariableConstraint) bool {
	switch constraint.Type {
	case "regex":
		compiled, err := regexp.Compile("^(?:" + constraint.Regex + ")$")
		return err == nil && compiled.MatchString(value)
	case "oneOf":
		return containsString(constraint.OneOf, value)
	default:
		return false
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func renderLockedModuleEntities(entities []LockedModuleEntity, values map[string]string) []InstalledModuleEntity {
	result := make([]InstalledModuleEntity, 0, len(entities))
	for _, entity := range entities {
		render := func(value string) string {
			return moduleTemplatePattern.ReplaceAllStringFunc(value, func(match string) string {
				parts := moduleTemplatePattern.FindStringSubmatch(match)
				return values[parts[1]]
			})
		}
		key := entity.Type + ":" + entity.ProductID + ":" + entity.EntityName
		if strings.Contains(entity.InstancePath, "[") {
			key += "@" + entity.InstancePath
		}
		secretRequirements, _ := rewriteModuleTemplatesInValue(entity.SecretRequirements, values).(map[string]any)
		result = append(result, InstalledModuleEntity{
			Key: key, InstancePath: entity.InstancePath, ProductID: entity.ProductID, EntityName: entity.EntityName,
			KubernetesNamespace: render(entity.KubernetesNamespace), IgnoredDependencies: entity.IgnoredDependencies,
			DependencyGroups: entity.DependencyGroups, DependencyGroupOverrides: cloneJSON(entity.DependencyGroupOverrides),
			SyncConfigOverrides: entity.SyncConfigOverrides,
			ConfigOverrides:     render(entity.ConfigOverrides), InstallConfigOverrides: render(entity.InstallConfigOverrides),
			SecretRequirements: secretRequirements, MarkedForUninstallation: cloneJSON(entity.MarkedForUninstallation),
		})
	}
	return result
}

func lockedEntityKey(entity LockedModuleEntity) string {
	key := entity.Type + ":" + entity.ProductID + ":" + entity.EntityName
	if strings.Contains(entity.InstancePath, "[") {
		key += "@" + entity.InstancePath
	}
	return key
}

func (service *ModuleService) ProposeUpdate(ctx context.Context, installationID string) (ModuleUpdateProposal, error) {
	installation, err := service.repository.GetModuleInstallation(ctx, installationID)
	if err != nil {
		return ModuleUpdateProposal{}, err
	}
	if installation.Status == "PAUSED" {
		return ModuleUpdateProposal{InstallationID: installation.ID, CurrentVersion: installation.Version, TargetVersion: installation.Version, Status: "PAUSED"}, nil
	}
	channel := installation.ReleaseChannel
	if channel == "" {
		channel = "RELEASE"
	}
	target, err := service.resolveReleaseForChannel(ctx, installation.Module, "latest", channel)
	if err != nil {
		return ModuleUpdateProposal{}, err
	}
	current, err := service.repository.GetModuleRelease(ctx, installation.Module, installation.Version)
	if err != nil {
		return ModuleUpdateProposal{}, err
	}
	if !moduleUpdateIsNewer(current, target) {
		return ModuleUpdateProposal{
			InstallationID: installation.ID, CurrentVersion: installation.Version,
			TargetVersion: installation.Version, Status: "VALID",
		}, nil
	}
	proposal := ModuleUpdateProposal{InstallationID: installation.ID, CurrentVersion: installation.Version, TargetVersion: target.Version, Status: "VALID"}
	values, validation := resolveModuleVariables(target.Lock.Variables, installation.Variables)
	if len(validation) > 0 {
		proposal.Status = "BLOCKED"
		proposal.block(UpdateBlocker{
			Kind:     UpdateBlockerVariablesUnresolved,
			Message:  "the Module update has variables that cannot be inferred",
			Subjects: validationPaths(validation),
		}, validation)
		return proposal, nil
	}
	targetEntities := renderLockedModuleEntities(target.Lock.Entities, values)
	currentByKey := map[string]InstalledModuleEntity{}
	for _, entity := range installation.Entities {
		currentByKey[entity.Key] = entity
	}
	targetByKey := map[string]InstalledModuleEntity{}
	for _, entity := range targetEntities {
		if current, exists := currentByKey[entity.Key]; exists {
			var reconcileErr error
			entity, reconcileErr = reconcileModuleEntityConfig(&current, entity, false)
			if reconcileErr != nil {
				return ModuleUpdateProposal{}, reconcileErr
			}
		}
		targetByKey[entity.Key] = entity
		current, exists := currentByKey[entity.Key]
		if !exists {
			proposal.Added = append(proposal.Added, entity.Key)
		} else if !moduleEntitiesEqual(current, entity) {
			proposal.Changed = append(proposal.Changed, entity.Key)
		}
		if exists && current.ConfigOverrides != entity.ConfigOverrides {
			proposal.ConfigOverrideChanges = append(proposal.ConfigOverrideChanges, entity.Key)
		}
	}
	for key := range currentByKey {
		if _, exists := targetByKey[key]; !exists {
			proposal.Relinquished = append(proposal.Relinquished, key)
		}
	}
	sort.Strings(proposal.Added)
	sort.Strings(proposal.Changed)
	sort.Strings(proposal.Relinquished)
	sort.Strings(proposal.ConfigOverrideChanges)
	return proposal, nil
}

func moduleEntitiesEqual(left, right InstalledModuleEntity) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}

func (service *ModuleService) ApplyUpdate(ctx context.Context, installationID string) (ModuleInstallation, error) {
	proposal, err := service.ProposeUpdate(ctx, installationID)
	if err != nil {
		return ModuleInstallation{}, err
	}
	if proposal.Status != "VALID" {
		return ModuleInstallation{}, fmt.Errorf("%w: Module update is blocked: %s", ErrConflict, strings.Join(proposal.Reasons, "; "))
	}
	return service.applyUpdateProposal(ctx, proposal, nil)
}

func (service *ModuleService) ConfigureAutoUpdatePolicy(
	ctx context.Context, module string, enabled bool, rationale, subject string, revision uint64,
) (ModuleAutoUpdatePolicy, error) {
	module, rationale, subject = strings.TrimSpace(module), strings.TrimSpace(rationale), strings.TrimSpace(subject)
	if !moduleNamePattern.MatchString(module) || rationale == "" || subject == "" {
		return ModuleAutoUpdatePolicy{}, fmt.Errorf("%w: Module, rationale, and subject are required", ErrInvalidTransition)
	}
	releases, err := service.repository.ListModuleReleases(ctx, module)
	if err != nil {
		return ModuleAutoUpdatePolicy{}, err
	}
	if len(releases) == 0 {
		return ModuleAutoUpdatePolicy{}, ErrNotFound
	}
	now := service.now().UTC()
	policy := ModuleAutoUpdatePolicy{
		Module: module, Enabled: enabled, Rationale: rationale, ConfiguredBy: subject,
		UpdatedAt: now, Revision: revision,
	}
	existing, err := service.repository.GetModuleAutoUpdatePolicy(ctx, module)
	switch {
	case err == nil:
		if revision != existing.Revision {
			return ModuleAutoUpdatePolicy{}, ErrConflict
		}
		policy.CreatedAt, policy.Revision = existing.CreatedAt, existing.Revision+1
	case errors.Is(err, ErrNotFound):
		if revision != 0 {
			return ModuleAutoUpdatePolicy{}, ErrConflict
		}
		policy.CreatedAt, policy.Revision = now, 1
	case err != nil:
		return ModuleAutoUpdatePolicy{}, err
	}
	return service.repository.PutModuleAutoUpdatePolicy(ctx, policy)
}

func (service *ModuleService) ApplyAutomaticUpdate(ctx context.Context, installationID string) (ModuleInstallation, ChangeRequest, error) {
	installation, err := service.repository.GetModuleInstallation(ctx, installationID)
	if err != nil {
		return ModuleInstallation{}, ChangeRequest{}, err
	}
	policy, err := service.repository.GetModuleAutoUpdatePolicy(ctx, installation.Module)
	if err != nil {
		return ModuleInstallation{}, ChangeRequest{}, err
	}
	if !policy.Enabled {
		return ModuleInstallation{}, ChangeRequest{}, fmt.Errorf("%w: Module automatic update approval is disabled", ErrConflict)
	}
	proposal, err := service.ProposeUpdate(ctx, installationID)
	if err != nil {
		return ModuleInstallation{}, ChangeRequest{}, err
	}
	if proposal.Status != "VALID" || proposal.TargetVersion == proposal.CurrentVersion {
		return ModuleInstallation{}, ChangeRequest{}, fmt.Errorf("%w: no valid Module version update is available", ErrConflict)
	}
	if len(proposal.ConfigOverrideChanges) != 0 {
		return ModuleInstallation{}, ChangeRequest{}, fmt.Errorf(
			"%w: automatic approval cannot edit existing Entity config overrides: %s",
			ErrConflict, strings.Join(proposal.ConfigOverrideChanges, ", "),
		)
	}
	now := service.now().UTC()
	changeRequestID := "crq_" + stableShortID("MODULE_UPDATE\x00"+installation.ID+"\x00"+proposal.TargetVersion)
	change := EntityChange{
		ID: "chg_" + stableShortID(changeRequestID), Type: ModuleUpdateChangeType,
		InstallationID: installation.ID,
		Request: map[string]any{
			"moduleId": installation.Module, "fromVersion": proposal.CurrentVersion, "toVersion": proposal.TargetVersion,
			"added": proposal.Added, "changed": proposal.Changed, "relinquished": proposal.Relinquished,
			"editsConfigurationOverrides": false,
		},
		Metadata: map[string]any{MetadataChangeSource: ChangeSourceModuleInstallation, MetadataModuleID: installation.Module},
		Diff:     map[string]ChangeDiffEntry{"version": {Before: proposal.CurrentVersion, After: proposal.TargetVersion}},
		ReferencedResources: map[string]ReferencedResource{
			"module":       {RID: moduleRID(installation.Module), Name: installation.Module, Labels: map[string]string{}},
			"installation": {RID: installationRID(installation.Environment, installation.ID), Name: installation.ID, Labels: map[string]string{}},
			"environment":  {RID: environmentRID(installation.Environment), Name: installation.Environment, Labels: map[string]string{}},
		},
	}
	if errs := validateChangeType(0, change); len(errs) != 0 {
		return ModuleInstallation{}, ChangeRequest{}, ValidationErrors(errs)
	}
	request := ChangeRequest{
		ID: changeRequestID, TargetKind: ModuleUpdateChangeType,
		TargetID: installation.ID + ":" + proposal.TargetVersion, Environment: installation.Environment,
		Title:       "Automatically update Module " + installation.Module + " to " + proposal.TargetVersion,
		Description: policy.Rationale,
		Author:      Principal{Username: "module-auto-update-controller", ServiceAccount: true},
		Status:      ChangeApproved, RequirementsSatisfied: true, Changes: []EntityChange{change},
		Reviews: []ChangeReview{{
			Subject: "module-auto-update-controller", Decision: "APPROVE", Rationale: policy.Rationale, CreatedAt: now,
		}},
		CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	request, err = service.repository.CreateChangeRequest(ctx, request)
	if err != nil {
		return ModuleInstallation{}, ChangeRequest{}, err
	}
	approval := &ModuleAutomaticApproval{
		ChangeRequestID: request.ID, FromVersion: proposal.CurrentVersion, ToVersion: proposal.TargetVersion,
		Rationale: policy.Rationale, ApprovedBy: "module-auto-update-controller", ApprovedAt: request.CreatedAt,
		PolicyRevision: policy.Revision,
	}
	updated, err := service.applyUpdateProposal(ctx, proposal, approval)
	return updated, request, err
}

func (service *ModuleService) applyUpdateProposal(
	ctx context.Context, proposal ModuleUpdateProposal, automaticApproval *ModuleAutomaticApproval,
) (ModuleInstallation, error) {
	installationID := proposal.InstallationID
	installation, err := service.repository.GetModuleInstallation(ctx, installationID)
	if err != nil {
		return ModuleInstallation{}, err
	}
	channel := installation.ReleaseChannel
	if channel == "" {
		channel = "RELEASE"
	}
	release, err := service.resolveReleaseForChannel(ctx, installation.Module, proposal.TargetVersion, channel)
	if err != nil {
		return ModuleInstallation{}, err
	}
	values, _ := resolveModuleVariables(release.Lock.Variables, installation.Variables)
	targetEntities := renderLockedModuleEntities(release.Lock.Entities, values)
	currentByKey := map[string]InstalledModuleEntity{}
	for _, entity := range installation.Entities {
		currentByKey[entity.Key] = entity
	}
	for index, entity := range targetEntities {
		if current, exists := currentByKey[entity.Key]; exists {
			targetEntities[index], err = reconcileModuleEntityConfig(&current, entity, false)
		} else {
			targetEntities[index], err = reconcileModuleEntityConfig(nil, entity, true)
		}
		if err != nil {
			return ModuleInstallation{}, err
		}
	}
	update := func(record *ModuleInstallation) error {
		if record.Version != proposal.CurrentVersion {
			return ErrConflict
		}
		record.Version = release.Version
		record.Variables = values
		record.Entities = targetEntities
		if automaticApproval != nil {
			record.LastAutomaticApproval = cloneJSON(automaticApproval)
		}
		record.UpdatedAt = service.now().UTC()
		return nil
	}
	if automaticApproval != nil {
		return service.repository.UpdateModuleInstallationAutomatically(
			ctx, installationID, installation.Module, automaticApproval.PolicyRevision, update,
		)
	}
	return service.repository.UpdateModuleInstallation(ctx, installationID, update)
}

func reconcileModuleEntityConfig(current *InstalledModuleEntity, target InstalledModuleEntity, firstInstall bool) (InstalledModuleEntity, error) {
	mode := target.SyncConfigOverrides
	if mode == "" {
		mode = "ENABLED"
		target.SyncConfigOverrides = mode
	}
	if current == nil {
		base := "{}"
		if firstInstall && mode == "MERGE" {
			base = target.InstallConfigOverrides
		}
		resolved, err := mergeModuleConfigYAML(base, target.ConfigOverrides)
		if err != nil {
			return InstalledModuleEntity{}, err
		}
		target.ConfigOverrides = resolved
		return target, nil
	}
	switch mode {
	case "DISABLED":
		target.ConfigOverrides = current.ConfigOverrides
	case "MERGE":
		merged, err := mergeModuleConfigYAML(current.ConfigOverrides, target.ConfigOverrides)
		if err != nil {
			return InstalledModuleEntity{}, err
		}
		target.ConfigOverrides = merged
	case "ENABLED":
		normalized, err := mergeModuleConfigYAML("{}", target.ConfigOverrides)
		if err != nil {
			return InstalledModuleEntity{}, err
		}
		target.ConfigOverrides = normalized
	default:
		return InstalledModuleEntity{}, fmt.Errorf("invalid syncConfigOverrides mode %s", mode)
	}
	return target, nil
}

func (service *ModuleService) Unlink(ctx context.Context, installationID string) (ModuleInstallation, error) {
	return service.repository.UpdateModuleInstallation(ctx, installationID, func(record *ModuleInstallation) error {
		if record.Status == "UNLINKED" {
			return nil
		}
		record.Status = "UNLINKED"
		record.UpdatedAt = service.now().UTC()
		return nil
	})
}

func (service *ModuleService) PauseUpdates(ctx context.Context, installationID string) (ModuleInstallation, error) {
	return service.repository.UpdateModuleInstallation(ctx, installationID, func(record *ModuleInstallation) error {
		if record.Status == "UNLINKED" {
			return fmt.Errorf("%w: cannot pause an unlinked Module installation", ErrConflict)
		}
		record.Status = "PAUSED"
		record.UpdatedAt = service.now().UTC()
		return nil
	})
}

func (service *ModuleService) ResumeUpdates(ctx context.Context, installationID string) (ModuleInstallation, error) {
	return service.repository.UpdateModuleInstallation(ctx, installationID, func(record *ModuleInstallation) error {
		if record.Status == "UNLINKED" {
			return fmt.Errorf("%w: cannot resume an unlinked Module installation", ErrConflict)
		}
		record.Status = "ACTIVE"
		record.UpdatedAt = service.now().UTC()
		return nil
	})
}

// block records a structured blocker and the prose Reasons derived from it, so
// the two cannot drift. The validation errors are carried through verbatim
// because their per-path messages say more than the blocker's summary.
func (proposal *ModuleUpdateProposal) block(blocker UpdateBlocker, validation []ValidationError) {
	proposal.Blockers = append(proposal.Blockers, blocker)
	for _, problem := range validation {
		proposal.Reasons = append(proposal.Reasons, problem.Path+": "+problem.Message)
	}
}

func validationPaths(validation []ValidationError) []string {
	paths := make([]string, 0, len(validation))
	seen := map[string]struct{}{}
	for _, problem := range validation {
		if _, duplicate := seen[problem.Path]; duplicate {
			continue
		}
		seen[problem.Path] = struct{}{}
		paths = append(paths, problem.Path)
	}
	return paths
}

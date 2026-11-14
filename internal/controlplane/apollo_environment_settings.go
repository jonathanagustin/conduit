package controlplane

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Apollo's Environment settings are a single YAML document. It is the surface an
// operator edits directly, and more importantly it is the surface CEL approval
// policies are written against: a policy that permits a service account to touch
// only "dependency-stack-overrides" names a path in *this* document, not in
// Platform's internal desired state.
//
// So the document shape is a contract in its own right. Without it, a policy
// authored upstream either matches nothing here or, worse, matches a differently
// named path and permits more than its author intended. The adapter below exists
// to convert between the two shapes and, critically, to project a change into this
// document's path vocabulary so diff_only_modifies_paths evaluates the same way it
// would on Apollo.
//
// This is a named, tested adapter over a documented shape. It is not a claim of
// compatibility with Apollo's private settings service.
//
// Provenance (reviewed 2026-08-02):
// https://www.palantir.com/docs/apollo/managing-environments/environment-settings/

// Published field names in the Environment settings document. They are constants
// because CEL policies reference them as literal path segments; a typo here
// silently changes which changes a policy permits.
const (
	SettingsFieldReleaseChannel      = "release-channel"
	SettingsFieldManagedHelmCharts   = "managed-helm-charts"
	SettingsFieldEntityLocator       = "entity-locator"
	SettingsFieldKubernetesNamespace = "k8s-namespace"
	SettingsFieldProduct             = "product"
	SettingsFieldMaintenanceWindows  = "maintenance-windows"
	SettingsFieldGlobalMaintenance   = "global-maintenance-windows"
	SettingsFieldOverrideProductWind = "override-product-maintenance-windows"
	SettingsFieldDependencyStacks    = "dependency-stack-overrides"
	SettingsFieldIgnoredDependencies = "ignored-dependencies"
	SettingsFieldHelmReleaseName     = "helm-release-name"
	SettingsFieldStack               = "stack"
	SettingsFieldDowntime            = "downtime"
	SettingsFieldNoDowntime          = "no-downtime"
)

// ApolloEnvironmentSettings is the Environment settings document.
type ApolloEnvironmentSettings struct {
	// ReleaseChannel is the Environment default that each Entity inherits unless
	// it declares its own.
	ReleaseChannel string `yaml:"release-channel,omitempty" json:"releaseChannel,omitempty"`
	// GlobalMaintenanceWindows applies to every Entity in the Environment.
	GlobalMaintenanceWindows *ApolloMaintenanceWindows `yaml:"global-maintenance-windows,omitempty" json:"globalMaintenanceWindows,omitempty"`
	// OverrideProductMaintenanceWindows is a pointer because the document
	// distinguishes "not stated" from an explicit false, and only an explicit
	// value should override a Product's own window.
	OverrideProductMaintenanceWindows *bool                    `yaml:"override-product-maintenance-windows,omitempty" json:"overrideProductMaintenanceWindows,omitempty"`
	ManagedHelmCharts                 []ApolloManagedHelmChart `yaml:"managed-helm-charts,omitempty" json:"managedHelmCharts,omitempty"`
	Other                             map[string]yaml.Node     `yaml:",inline" json:"-"`
}

// ApolloMaintenanceWindows is the downtime/no-downtime pair. Apollo accepts the
// literal "all-time" as well as interval expressions, so the values stay strings
// rather than being parsed into a structure that cannot hold them.
type ApolloMaintenanceWindows struct {
	Downtime   string `yaml:"downtime,omitempty" json:"downtime,omitempty"`
	NoDowntime string `yaml:"no-downtime,omitempty" json:"noDowntime,omitempty"`
}

// ApolloManagedHelmChart is one installation in the settings document.
type ApolloManagedHelmChart struct {
	EntityLocator            ApolloEntityLocator       `yaml:"entity-locator" json:"entityLocator"`
	Product                  string                    `yaml:"product,omitempty" json:"product,omitempty"`
	KubernetesNamespace      string                    `yaml:"k8s-namespace,omitempty" json:"k8sNamespace,omitempty"`
	ReleaseChannel           string                    `yaml:"release-channel,omitempty" json:"releaseChannel,omitempty"`
	HelmReleaseName          string                    `yaml:"helm-release-name,omitempty" json:"helmReleaseName,omitempty"`
	Stack                    string                    `yaml:"stack,omitempty" json:"stack,omitempty"`
	IgnoredDependencies      []string                  `yaml:"ignored-dependencies,omitempty" json:"ignoredDependencies,omitempty"`
	DependencyStackOverrides map[string]string         `yaml:"dependency-stack-overrides,omitempty" json:"dependencyStackOverrides,omitempty"`
	MaintenanceWindows       *ApolloMaintenanceWindows `yaml:"maintenance-windows,omitempty" json:"maintenanceWindows,omitempty"`
	Other                    map[string]yaml.Node      `yaml:",inline" json:"-"`
}

// ApolloEntityLocator names one installation within an Environment.
type ApolloEntityLocator struct {
	Name  string               `yaml:"name" json:"name"`
	Other map[string]yaml.Node `yaml:",inline" json:"-"`
}

// DecodeApolloEnvironmentSettings parses the settings document strictly. Unknown
// keys are preserved rather than rejected, because an operator's document may
// legitimately carry fields this adapter does not model yet, and silently dropping
// them on a round trip would destroy settings.
func DecodeApolloEnvironmentSettings(contents string) (ApolloEnvironmentSettings, error) {
	var settings ApolloEnvironmentSettings
	if strings.TrimSpace(contents) == "" {
		return ApolloEnvironmentSettings{}, ValidationErrors{{
			Path: "settings", Message: "document is empty",
		}}
	}
	if err := yaml.Unmarshal([]byte(contents), &settings); err != nil {
		return ApolloEnvironmentSettings{}, ValidationErrors{{
			Path: "settings", Message: "decode settings document: " + err.Error(),
		}}
	}
	if errs := validateApolloEnvironmentSettings(settings); len(errs) > 0 {
		return ApolloEnvironmentSettings{}, errs
	}
	return settings, nil
}

func validateApolloEnvironmentSettings(settings ApolloEnvironmentSettings) ValidationErrors {
	errs := ValidationErrors{}
	if settings.ReleaseChannel != "" && !releaseChannelPattern.MatchString(settings.ReleaseChannel) {
		errs = append(errs, ValidationError{Path: SettingsFieldReleaseChannel, Message: "must be an uppercase Release Channel identifier"})
	}
	seen := make(map[string]struct{}, len(settings.ManagedHelmCharts))
	for index, chart := range settings.ManagedHelmCharts {
		path := fmt.Sprintf("%s[%d]", SettingsFieldManagedHelmCharts, index)
		name := strings.TrimSpace(chart.EntityLocator.Name)
		if name == "" {
			errs = append(errs, ValidationError{
				Path: path + "." + SettingsFieldEntityLocator + ".name", Message: "must not be empty",
			})
		}
		// Two entries locating the same installation make the document
		// self-contradictory: nothing decides which one wins.
		if _, duplicate := seen[name]; duplicate && name != "" {
			errs = append(errs, ValidationError{Path: path, Message: "duplicate installation " + name})
		}
		seen[name] = struct{}{}
		if chart.Product != "" && !coordinatePattern.MatchString(chart.Product) {
			errs = append(errs, ValidationError{
				Path: path + "." + SettingsFieldProduct, Message: "must be a group:name coordinate",
			})
		}
		if chart.KubernetesNamespace != "" && !dnsLabelPattern.MatchString(chart.KubernetesNamespace) {
			errs = append(errs, ValidationError{Path: path + "." + SettingsFieldKubernetesNamespace, Message: "must be a DNS label"})
		}
		if chart.ReleaseChannel != "" && !releaseChannelPattern.MatchString(chart.ReleaseChannel) {
			errs = append(errs, ValidationError{Path: path + "." + SettingsFieldReleaseChannel, Message: "must be an uppercase Release Channel identifier"})
		}
		if chart.HelmReleaseName != "" &&
			(!dnsLabelPattern.MatchString(chart.HelmReleaseName) || len(chart.HelmReleaseName) > 53) {
			errs = append(errs, ValidationError{Path: path + "." + SettingsFieldHelmReleaseName, Message: "must be a DNS label no longer than 53 characters"})
		}
		if chart.Stack != "" && !dependencyGroupPattern.MatchString(chart.Stack) {
			errs = append(errs, ValidationError{Path: path + "." + SettingsFieldStack, Message: "must be a stack identifier"})
		}
		for product, stack := range chart.DependencyStackOverrides {
			if !coordinatePattern.MatchString(product) || !dependencyGroupPattern.MatchString(stack) {
				errs = append(errs, ValidationError{Path: path + "." + SettingsFieldDependencyStacks, Message: "must map Product coordinates to stack identifiers"})
			}
		}
	}
	return errs
}

// EncodeApolloEnvironmentSettings renders the settings document.
func EncodeApolloEnvironmentSettings(settings ApolloEnvironmentSettings) (string, error) {
	encoded, err := yaml.Marshal(settings)
	if err != nil {
		return "", fmt.Errorf("encode settings document: %w", err)
	}
	return string(encoded), nil
}

// ApolloEnvironmentSettingsFromDesiredState projects Platform's desired state into
// the published document shape.
func ApolloEnvironmentSettingsFromDesiredState(state DesiredState) ApolloEnvironmentSettings {
	settings := ApolloEnvironmentSettings{}
	if window := maintenanceWindowsToApollo(state.Environment.Maintenance.Downtime, state.Environment.Maintenance.NoDowntime); window != nil {
		settings.GlobalMaintenanceWindows = window
	}
	for _, entity := range state.Entities {
		chart := ApolloManagedHelmChart{
			EntityLocator:            ApolloEntityLocator{Name: entity.Name},
			Product:                  entity.Product,
			KubernetesNamespace:      entity.Namespace,
			ReleaseChannel:           entity.ReleaseChannel,
			HelmReleaseName:          entity.HelmReleaseName,
			Stack:                    entity.Stack,
			IgnoredDependencies:      append([]string(nil), entity.IgnoredDependencies...),
			DependencyStackOverrides: cloneStringMap(entity.DependencyStackOverrides),
			MaintenanceWindows:       maintenanceWindowsToApollo(entity.Maintenance.Downtime, entity.Maintenance.NoDowntime),
		}
		if entity.Maintenance.OverrideProductMaintenanceWindow {
			override := true
			settings.OverrideProductMaintenanceWindows = &override
		}
		settings.ManagedHelmCharts = append(settings.ManagedHelmCharts, chart)
	}
	return settings
}

// ApplyApolloEnvironmentSettings merges a settings document onto desired state.
// Only fields the document actually states are applied: an omitted field means
// "unchanged", so importing a partial document cannot silently clear settings the
// operator never mentioned.
func ApplyApolloEnvironmentSettings(state DesiredState, settings ApolloEnvironmentSettings) (DesiredState, error) {
	result := state
	result.Entities = append([]Entity(nil), state.Entities...)
	byName := make(map[string]int, len(result.Entities))
	for index, entity := range result.Entities {
		byName[entity.Name] = index
	}
	for position, chart := range settings.ManagedHelmCharts {
		name := strings.TrimSpace(chart.EntityLocator.Name)
		index, known := byName[name]
		if !known {
			return DesiredState{}, ValidationErrors{{
				Path: fmt.Sprintf("%s[%d].%s.name", SettingsFieldManagedHelmCharts, position, SettingsFieldEntityLocator),
				Message: "settings name installation " + name +
					" which is not declared in the Environment's desired state",
			}}
		}
		entity := &result.Entities[index]
		if chart.KubernetesNamespace != "" {
			entity.Namespace = chart.KubernetesNamespace
		}
		if chart.ReleaseChannel != "" {
			entity.ReleaseChannel = chart.ReleaseChannel
		}
		if chart.HelmReleaseName != "" {
			entity.HelmReleaseName = chart.HelmReleaseName
		}
		if chart.Stack != "" {
			entity.Stack = chart.Stack
		}
		if len(chart.IgnoredDependencies) > 0 {
			entity.IgnoredDependencies = append([]string(nil), chart.IgnoredDependencies...)
		}
		if len(chart.DependencyStackOverrides) > 0 {
			entity.DependencyStackOverrides = cloneStringMap(chart.DependencyStackOverrides)
		}
		if chart.MaintenanceWindows != nil {
			entity.Maintenance.Downtime = apolloWindowToWeekly(chart.MaintenanceWindows.Downtime)
			entity.Maintenance.NoDowntime = apolloWindowToWeekly(chart.MaintenanceWindows.NoDowntime)
		}
		// An Entity-level channel overrides the Environment default; the default
		// only fills in where the installation stated nothing.
		if entity.ReleaseChannel == "" && settings.ReleaseChannel != "" {
			entity.ReleaseChannel = settings.ReleaseChannel
		}
	}
	if settings.OverrideProductMaintenanceWindows != nil {
		for index := range result.Entities {
			result.Entities[index].Maintenance.OverrideProductMaintenanceWindow = *settings.OverrideProductMaintenanceWindows
		}
	}
	if settings.GlobalMaintenanceWindows != nil {
		result.Environment.Maintenance.Downtime = apolloWindowToWeekly(settings.GlobalMaintenanceWindows.Downtime)
		result.Environment.Maintenance.NoDowntime = apolloWindowToWeekly(settings.GlobalMaintenanceWindows.NoDowntime)
	}
	return result, nil
}

// ApolloSettingsDiffPaths projects a change into the settings document's path
// vocabulary, using "/" separators so diff_only_modifies_paths matches the same
// paths a policy author wrote upstream.
//
// Paths are returned sorted so a policy decision does not depend on map ordering.
func ApolloSettingsDiffPaths(before, after ApolloEnvironmentSettings) []string {
	paths := map[string]struct{}{}
	addUnknownDiffPaths(paths, "", before.Other, after.Other)
	if before.ReleaseChannel != after.ReleaseChannel {
		paths[SettingsFieldReleaseChannel] = struct{}{}
	}
	if !sameMaintenanceWindows(before.GlobalMaintenanceWindows, after.GlobalMaintenanceWindows) {
		paths[SettingsFieldGlobalMaintenance] = struct{}{}
	}
	if !sameBoolPointer(before.OverrideProductMaintenanceWindows, after.OverrideProductMaintenanceWindows) {
		paths[SettingsFieldOverrideProductWind] = struct{}{}
	}
	beforeCharts := chartsByLocator(before.ManagedHelmCharts)
	afterCharts := chartsByLocator(after.ManagedHelmCharts)
	for name, updated := range afterCharts {
		previous, existed := beforeCharts[name]
		root := SettingsFieldManagedHelmCharts + "/" + name
		if !existed {
			paths[root] = struct{}{}
			continue
		}
		addUnknownDiffPaths(paths, root, previous.Other, updated.Other)
		addUnknownDiffPaths(paths, root+"/"+SettingsFieldEntityLocator,
			previous.EntityLocator.Other, updated.EntityLocator.Other)
		for field, changed := range map[string]bool{
			SettingsFieldProduct:             previous.Product != updated.Product,
			SettingsFieldKubernetesNamespace: previous.KubernetesNamespace != updated.KubernetesNamespace,
			SettingsFieldReleaseChannel:      previous.ReleaseChannel != updated.ReleaseChannel,
			SettingsFieldHelmReleaseName:     previous.HelmReleaseName != updated.HelmReleaseName,
			SettingsFieldStack:               previous.Stack != updated.Stack,
			SettingsFieldIgnoredDependencies: !sameStrings(previous.IgnoredDependencies, updated.IgnoredDependencies),
			SettingsFieldDependencyStacks:    !sameStringMap(previous.DependencyStackOverrides, updated.DependencyStackOverrides),
			SettingsFieldMaintenanceWindows:  !sameMaintenanceWindows(previous.MaintenanceWindows, updated.MaintenanceWindows),
		} {
			if changed {
				paths[root+"/"+field] = struct{}{}
			}
		}
	}
	for name := range beforeCharts {
		if _, retained := afterCharts[name]; !retained {
			paths[SettingsFieldManagedHelmCharts+"/"+name] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	return ordered
}

// addUnknownDiffPaths makes preserved-but-unmodelled settings visible to CEL
// allowlists. Ignoring these nodes would let a document change a future setting
// while diff_only_modifies_paths saw no change at all.
func addUnknownDiffPaths(paths map[string]struct{}, root string, before, after map[string]yaml.Node) {
	keys := make(map[string]struct{}, len(before)+len(after))
	for key := range before {
		keys[key] = struct{}{}
	}
	for key := range after {
		keys[key] = struct{}{}
	}
	for key := range keys {
		left, leftPresent := before[key]
		right, rightPresent := after[key]
		if leftPresent == rightPresent && yamlNodesEqual(left, right) {
			continue
		}
		path := key
		if root != "" {
			path = root + "/" + key
		}
		paths[path] = struct{}{}
	}
}

func yamlNodesEqual(left, right yaml.Node) bool {
	var leftValue, rightValue any
	if left.Kind != 0 {
		if err := left.Decode(&leftValue); err != nil {
			return false
		}
	}
	if right.Kind != 0 {
		if err := right.Decode(&rightValue); err != nil {
			return false
		}
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func chartsByLocator(charts []ApolloManagedHelmChart) map[string]ApolloManagedHelmChart {
	byName := make(map[string]ApolloManagedHelmChart, len(charts))
	for _, chart := range charts {
		byName[strings.TrimSpace(chart.EntityLocator.Name)] = chart
	}
	return byName
}

// maintenanceWindowsToApollo renders a window pair, returning nil when neither
// side is stated so an absent window is not encoded as an empty one.
func maintenanceWindowsToApollo(downtime, noDowntime *WeeklyMaintenanceWindow) *ApolloMaintenanceWindows {
	rendered := ApolloMaintenanceWindows{
		Downtime: weeklyWindowToApollo(downtime), NoDowntime: weeklyWindowToApollo(noDowntime),
	}
	if rendered.Downtime == "" && rendered.NoDowntime == "" {
		return nil
	}
	return &rendered
}

// apolloAllTime is the literal Apollo uses for an unrestricted window.
const apolloAllTime = "all-time"

func weeklyWindowToApollo(window *WeeklyMaintenanceWindow) string {
	if window == nil {
		return ""
	}
	if len(window.Intervals) == 0 {
		return apolloAllTime
	}
	return strings.Join(window.Intervals, ",")
}

func apolloWindowToWeekly(value string) *WeeklyMaintenanceWindow {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if trimmed == apolloAllTime {
		return &WeeklyMaintenanceWindow{TimeZone: "UTC"}
	}
	intervals := make([]string, 0)
	for _, interval := range strings.Split(trimmed, ",") {
		if candidate := strings.TrimSpace(interval); candidate != "" {
			intervals = append(intervals, candidate)
		}
	}
	return &WeeklyMaintenanceWindow{TimeZone: "UTC", Intervals: intervals}
}

func sameMaintenanceWindows(left, right *ApolloMaintenanceWindows) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func sameBoolPointer(left, right *bool) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sameStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

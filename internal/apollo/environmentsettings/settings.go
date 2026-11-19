// Package environmentsettings owns the published compatibility document shape
// and its policy diff-path vocabulary. Native state conversion remains at the edge.
package environmentsettings

import (
	"go.yaml.in/yaml/v4"
	"reflect"
	"sort"
	"strings"
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

// Settings is the Environment settings document.
type Settings struct {
	// ReleaseChannel is the Environment default that each Entity inherits unless
	// it declares its own.
	ReleaseChannel string `yaml:"release-channel,omitempty" json:"releaseChannel,omitempty"`
	// GlobalMaintenanceWindows applies to every Entity in the Environment.
	GlobalMaintenanceWindows *MaintenanceWindows `yaml:"global-maintenance-windows,omitempty" json:"globalMaintenanceWindows,omitempty"`
	// OverrideProductMaintenanceWindows is a pointer because the document
	// distinguishes "not stated" from an explicit false, and only an explicit
	// value should override a Product's own window.
	OverrideProductMaintenanceWindows *bool                `yaml:"override-product-maintenance-windows,omitempty" json:"overrideProductMaintenanceWindows,omitempty"`
	ManagedHelmCharts                 []ManagedHelmChart   `yaml:"managed-helm-charts,omitempty" json:"managedHelmCharts,omitempty"`
	Other                             map[string]yaml.Node `yaml:",inline" json:"-"`
}

// MaintenanceWindows is the downtime/no-downtime pair. Apollo accepts the
// literal "all-time" as well as interval expressions, so the values stay strings
// rather than being parsed into a structure that cannot hold them.
type MaintenanceWindows struct {
	Downtime   string `yaml:"downtime,omitempty" json:"downtime,omitempty"`
	NoDowntime string `yaml:"no-downtime,omitempty" json:"noDowntime,omitempty"`
}

// ManagedHelmChart is one installation in the settings document.
type ManagedHelmChart struct {
	EntityLocator            EntityLocator        `yaml:"entity-locator" json:"entityLocator"`
	Product                  string               `yaml:"product,omitempty" json:"product,omitempty"`
	KubernetesNamespace      string               `yaml:"k8s-namespace,omitempty" json:"k8sNamespace,omitempty"`
	ReleaseChannel           string               `yaml:"release-channel,omitempty" json:"releaseChannel,omitempty"`
	HelmReleaseName          string               `yaml:"helm-release-name,omitempty" json:"helmReleaseName,omitempty"`
	Stack                    string               `yaml:"stack,omitempty" json:"stack,omitempty"`
	IgnoredDependencies      []string             `yaml:"ignored-dependencies,omitempty" json:"ignoredDependencies,omitempty"`
	DependencyStackOverrides map[string]string    `yaml:"dependency-stack-overrides,omitempty" json:"dependencyStackOverrides,omitempty"`
	MaintenanceWindows       *MaintenanceWindows  `yaml:"maintenance-windows,omitempty" json:"maintenanceWindows,omitempty"`
	Other                    map[string]yaml.Node `yaml:",inline" json:"-"`
}

// EntityLocator names one installation within an Environment.
type EntityLocator struct {
	Name  string               `yaml:"name" json:"name"`
	Other map[string]yaml.Node `yaml:",inline" json:"-"`
}

// DiffPaths projects a change into the settings document's path
// vocabulary, using "/" separators so diff_only_modifies_paths matches the same
// paths a policy author wrote upstream.
//
// Paths are returned sorted so a policy decision does not depend on map ordering.
func DiffPaths(before, after Settings) []string {
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

func chartsByLocator(charts []ManagedHelmChart) map[string]ManagedHelmChart {
	byName := make(map[string]ManagedHelmChart, len(charts))
	for _, chart := range charts {
		byName[strings.TrimSpace(chart.EntityLocator.Name)] = chart
	}
	return byName
}

func sameMaintenanceWindows(left, right *MaintenanceWindows) bool {
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

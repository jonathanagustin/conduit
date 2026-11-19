package controlplane

import (
	"fmt"
	"strings"

	settingsshape "github.com/jonathanagustin/conduit/internal/apollo/environmentsettings"
)

// Source-compatible aliases retain the existing settings transport names while
// the compatibility leaf owns document shape and policy diff-path semantics.
const (
	SettingsFieldReleaseChannel      = settingsshape.SettingsFieldReleaseChannel
	SettingsFieldManagedHelmCharts   = settingsshape.SettingsFieldManagedHelmCharts
	SettingsFieldEntityLocator       = settingsshape.SettingsFieldEntityLocator
	SettingsFieldKubernetesNamespace = settingsshape.SettingsFieldKubernetesNamespace
	SettingsFieldProduct             = settingsshape.SettingsFieldProduct
	SettingsFieldMaintenanceWindows  = settingsshape.SettingsFieldMaintenanceWindows
	SettingsFieldGlobalMaintenance   = settingsshape.SettingsFieldGlobalMaintenance
	SettingsFieldOverrideProductWind = settingsshape.SettingsFieldOverrideProductWind
	SettingsFieldDependencyStacks    = settingsshape.SettingsFieldDependencyStacks
	SettingsFieldIgnoredDependencies = settingsshape.SettingsFieldIgnoredDependencies
	SettingsFieldHelmReleaseName     = settingsshape.SettingsFieldHelmReleaseName
	SettingsFieldStack               = settingsshape.SettingsFieldStack
	SettingsFieldDowntime            = settingsshape.SettingsFieldDowntime
	SettingsFieldNoDowntime          = settingsshape.SettingsFieldNoDowntime
)

type ApolloEnvironmentSettings = settingsshape.Settings
type ApolloMaintenanceWindows = settingsshape.MaintenanceWindows
type ApolloManagedHelmChart = settingsshape.ManagedHelmChart
type ApolloEntityLocator = settingsshape.EntityLocator

// DecodeApolloEnvironmentSettings retains the legacy entry point for the codec.
func DecodeApolloEnvironmentSettings(contents string) (ApolloEnvironmentSettings, error) {
	return settingsshape.Decode(contents)
}

// EncodeApolloEnvironmentSettings retains the legacy entry point for the codec.
func EncodeApolloEnvironmentSettings(settings ApolloEnvironmentSettings) (string, error) {
	return settingsshape.Encode(settings)
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

// ApolloSettingsDiffPaths delegates the published document vocabulary to its adapter.
func ApolloSettingsDiffPaths(before, after ApolloEnvironmentSettings) []string {
	return settingsshape.DiffPaths(before, after)
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

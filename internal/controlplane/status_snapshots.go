package controlplane

import "time"

// LatestDesiredSnapshots exposes the existing read projection, with copied
// state, so administrator reporting does not invent a second latest-plan rule.
func LatestDesiredSnapshots(plans []PlanRecord) map[string]DesiredState {
	return latestDesiredStates(plans)
}

// ReportedSpokeStatus uses the same freshness rule as the environment catalog.
func ReportedSpokeStatus(report *EnvironmentReport, now time.Time) SpokeLiveness {
	return spokeLiveness(report, now)
}

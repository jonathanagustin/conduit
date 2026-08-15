package controlplane

import "time"

// Promotion evidence fixtures. The rules they exercise live in Catalog and are
// tested there; these remain because several tests in this package build a
// Promotion end to end through the repository and need the same evidence shapes.

func promotionFixtureTime() time.Time { return time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC) }

// healthyFor builds evidence for an Entity that has held healthy for a duration.
func healthyFor(environment, installation string, held time.Duration) PromotionHealthEvidence {
	observed := promotionFixtureTime()
	return PromotionHealthEvidence{
		Environment: environment, InstallationID: installation,
		Health: HealthHealthy, HealthySince: observed.Add(-held), ObservedAt: observed,
	}
}

func unhealthy(environment, installation string) PromotionHealthEvidence {
	return PromotionHealthEvidence{
		Environment: environment, InstallationID: installation,
		Health: HealthUnhealthy, ObservedAt: promotionFixtureTime(),
	}
}

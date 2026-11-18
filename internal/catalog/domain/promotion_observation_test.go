package domain

import (
	"testing"
	"time"
)

func TestPromotionObservationInstantCreditsNoSoak(t *testing.T) {
	observed := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name        string
		minimum     int
		now         time.Time
		wantBlocked bool
	}{
		{"zero duration", 0, observed, false},
		{"positive duration", 3600, observed, true},
		{"later evaluation does not age observation into soak", 3600, observed.Add(24 * time.Hour), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := PromotionEvaluationRequest{Policy: PromotionPolicy{MinimumHealthyEntities: 1, MinimumHealthyDurationSeconds: tc.minimum}, Evidence: []PromotionHealthEvidence{{Environment: "edge-a", InstallationID: "api", Health: HealthHealthy, ObservedAt: observed, HealthySince: observed}}}
			blockers := EvaluatePromotion(request, tc.now)
			if !tc.wantBlocked {
				if len(blockers) != 0 {
					t.Fatalf("instantaneous health refused zero-duration policy: %+v", blockers)
				}
				return
			}
			if len(blockers) != 1 || blockers[0].Kind != PromotionBlockerHealthyBelowMinimum {
				t.Fatalf("observation credited unproven soak: %+v", blockers)
			}
		})
	}
}

package optimization

import (
	"testing"
	"time"
)

func TestAssessDORAComputesFiveExactServiceScopedMeasures(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	observations := []DeliveryObservation{
		{DeploymentID: "d1", ServiceID: "hub", CommitAt: "2026-08-01T00:00:00Z", DeployedAt: "2026-08-01T01:00:00Z", Source: "release-ledger", SourceRevision: "1"},
		{DeploymentID: "d2", ServiceID: "hub", CommitAt: "2026-08-02T00:00:00Z", DeployedAt: "2026-08-02T02:00:00Z", Failed: true, RecoveredAt: "2026-08-02T02:10:00Z", Rework: true, Source: "release-ledger", SourceRevision: "2"},
	}
	got, err := AssessDORA("hub", start, start.Add(7*24*time.Hour), observations)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Complete || got.DeploymentCount != 2 || got.WindowSeconds != "604800" || got.DeploymentFrequencyPerSecond != "1/302400" || got.ChangeFailRate != "1/2" || got.DeploymentReworkRate != "1/2" || got.ChangeLeadTimeSeconds[0] != "3600" || got.ChangeLeadTimeSeconds[1] != "7200" || got.FailedDeploymentRecoverySeconds[0] != "600" {
		t.Fatalf("assessment = %#v", got)
	}
}

func TestAssessDORAReportsMissingRecoveryWithoutInventingMTTR(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	got, err := AssessDORA("spoke", start, start.Add(time.Hour), []DeliveryObservation{{DeploymentID: "d1", ServiceID: "spoke", CommitAt: "2026-08-01T00:00:00Z", DeployedAt: "2026-08-01T00:10:00Z", Failed: true, Source: "release-ledger", SourceRevision: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Complete || len(got.Gaps) != 1 || got.ChangeFailRate != "1" || len(got.FailedDeploymentRecoverySeconds) != 0 {
		t.Fatalf("assessment = %#v", got)
	}
}

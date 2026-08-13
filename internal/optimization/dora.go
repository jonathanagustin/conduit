package optimization

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"
)

// DeliveryObservation is one application/service deployment joined to its
// originating commit and any immediate remediation. It is intentionally not a
// repository-wide productivity score.
type DeliveryObservation struct {
	DeploymentID   string `json:"deploymentId"`
	ServiceID      string `json:"serviceId"`
	CommitAt       string `json:"commitAt"`
	DeployedAt     string `json:"deployedAt"`
	Failed         bool   `json:"failed"`
	RecoveredAt    string `json:"recoveredAt,omitempty"`
	Rework         bool   `json:"rework"`
	Source         string `json:"source"`
	SourceRevision string `json:"sourceRevision"`
}

type DORAAssessment struct {
	ServiceID                       string   `json:"serviceId"`
	WindowStart                     string   `json:"windowStart"`
	WindowEnd                       string   `json:"windowEnd"`
	Complete                        bool     `json:"complete"`
	DeploymentCount                 int64    `json:"deploymentCount"`
	WindowSeconds                   string   `json:"windowSeconds"`
	DeploymentFrequencyPerSecond    string   `json:"deploymentFrequencyPerSecond"`
	ChangeLeadTimeSeconds           []string `json:"changeLeadTimeSeconds"`
	FailedDeploymentRecoverySeconds []string `json:"failedDeploymentRecoverySeconds"`
	ChangeFailRate                  string   `json:"changeFailRate"`
	DeploymentReworkRate            string   `json:"deploymentReworkRate"`
	Gaps                            []string `json:"gaps,omitempty"`
}

// AssessDORA computes the current five software-delivery measures for one
// application/service and one explicit window. It emits exact event counts,
// durations, and rational rates; policy may summarize distributions later.
func AssessDORA(serviceID string, windowStart, windowEnd time.Time, observations []DeliveryObservation) (DORAAssessment, error) {
	if serviceID == "" || windowStart.IsZero() || !windowEnd.After(windowStart) {
		return DORAAssessment{}, errors.New("service and a positive measurement window are required")
	}
	if len(observations) > 1_000_000 {
		return DORAAssessment{}, errors.New("DORA assessment exceeds one million deployments")
	}
	windowDuration := windowEnd.Sub(windowStart)
	assessment := DORAAssessment{ServiceID: serviceID, WindowStart: windowStart.UTC().Format(time.RFC3339Nano), WindowEnd: windowEnd.UTC().Format(time.RFC3339Nano), WindowSeconds: exactDurationSeconds(windowDuration), Complete: true}
	seen := map[string]bool{}
	failed, rework := int64(0), int64(0)
	leadTimes, recoveryTimes := []time.Duration{}, []time.Duration{}
	for _, observation := range observations {
		if observation.DeploymentID == "" || observation.ServiceID != serviceID || observation.Source == "" || observation.SourceRevision == "" || seen[observation.DeploymentID] {
			return DORAAssessment{}, fmt.Errorf("deployment %q has invalid identity, service, provenance, or duplication", observation.DeploymentID)
		}
		seen[observation.DeploymentID] = true
		commitAt, err := time.Parse(time.RFC3339Nano, observation.CommitAt)
		if err != nil {
			return DORAAssessment{}, fmt.Errorf("deployment %q commitAt: %w", observation.DeploymentID, err)
		}
		deployedAt, err := time.Parse(time.RFC3339Nano, observation.DeployedAt)
		if err != nil {
			return DORAAssessment{}, fmt.Errorf("deployment %q deployedAt: %w", observation.DeploymentID, err)
		}
		if deployedAt.Before(windowStart) || !deployedAt.Before(windowEnd) {
			return DORAAssessment{}, fmt.Errorf("deployment %q lies outside half-open measurement window", observation.DeploymentID)
		}
		if deployedAt.Before(commitAt) {
			return DORAAssessment{}, fmt.Errorf("deployment %q predates commit", observation.DeploymentID)
		}
		leadTimes = append(leadTimes, deployedAt.Sub(commitAt))
		assessment.DeploymentCount++
		if observation.Rework {
			rework++
		}
		if observation.Failed {
			failed++
			if observation.RecoveredAt == "" {
				assessment.Complete = false
				assessment.Gaps = append(assessment.Gaps, observation.DeploymentID+": missing failed-deployment recovery time")
				continue
			}
			recoveredAt, err := time.Parse(time.RFC3339Nano, observation.RecoveredAt)
			if err != nil || recoveredAt.Before(deployedAt) {
				return DORAAssessment{}, fmt.Errorf("deployment %q has invalid recovery time", observation.DeploymentID)
			}
			recoveryTimes = append(recoveryTimes, recoveredAt.Sub(deployedAt))
		} else if observation.RecoveredAt != "" {
			return DORAAssessment{}, fmt.Errorf("successful deployment %q cannot have failed-deployment recovery time", observation.DeploymentID)
		}
	}
	sort.Slice(leadTimes, func(i, j int) bool { return leadTimes[i] < leadTimes[j] })
	sort.Slice(recoveryTimes, func(i, j int) bool { return recoveryTimes[i] < recoveryTimes[j] })
	for _, duration := range leadTimes {
		assessment.ChangeLeadTimeSeconds = append(assessment.ChangeLeadTimeSeconds, exactDurationSeconds(duration))
	}
	for _, duration := range recoveryTimes {
		assessment.FailedDeploymentRecoverySeconds = append(assessment.FailedDeploymentRecoverySeconds, exactDurationSeconds(duration))
	}
	assessment.DeploymentFrequencyPerSecond = new(big.Rat).SetFrac(big.NewInt(assessment.DeploymentCount*int64(time.Second)), big.NewInt(windowDuration.Nanoseconds())).RatString()
	assessment.ChangeFailRate = exactRate(failed, assessment.DeploymentCount)
	assessment.DeploymentReworkRate = exactRate(rework, assessment.DeploymentCount)
	sort.Strings(assessment.Gaps)
	return assessment, nil
}

func exactDurationSeconds(duration time.Duration) string {
	return new(big.Rat).SetFrac(big.NewInt(duration.Nanoseconds()), big.NewInt(int64(time.Second))).RatString()
}

func exactRate(numerator, denominator int64) string {
	if denominator == 0 {
		return "0"
	}
	return new(big.Rat).SetFrac(big.NewInt(numerator), big.NewInt(denominator)).RatString()
}

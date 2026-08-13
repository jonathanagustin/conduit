package optimization

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"time"
)

type ExperimentMetricRole string

const (
	ExperimentPrimary   ExperimentMetricRole = "PRIMARY"
	ExperimentGuardrail ExperimentMetricRole = "GUARDRAIL"
)

type ExperimentMetric struct {
	ID        string               `json:"id"`
	Role      ExperimentMetricRole `json:"role"`
	Direction ValueDirection       `json:"direction"`
	Unit      string               `json:"unit"`
	// HarmLimit is an exact, non-negative non-inferiority margin for a
	// guardrail. Primary metrics must omit it and prove strict improvement.
	HarmLimit string `json:"harmLimit,omitempty"`
}

// ExperimentPlan is a preregistered causal measurement contract. AnalysisID
// identifies the pinned statistical procedure; this package validates its
// reported bounds rather than pretending every experiment shares one test.
type ExperimentPlan struct {
	ID                  string             `json:"id"`
	Hypothesis          string             `json:"hypothesis"`
	Control             string             `json:"control"`
	Treatment           string             `json:"treatment"`
	AssignmentUnit      string             `json:"assignmentUnit"`
	RandomizationUnit   string             `json:"randomizationUnit"`
	AllocationControl   string             `json:"allocationControl"`
	AllocationTreatment string             `json:"allocationTreatment"`
	AnalysisID          string             `json:"analysisId"`
	StoppingRule        string             `json:"stoppingRule"`
	MinimumSampleSize   int64              `json:"minimumSampleSize"`
	MaximumDurationSec  int64              `json:"maximumDurationSeconds"`
	Metrics             []ExperimentMetric `json:"metrics"`
}

type ExperimentEffect struct {
	MetricID       string `json:"metricId"`
	Estimate       string `json:"estimate"`
	LowerBound     string `json:"lowerBound"`
	UpperBound     string `json:"upperBound"`
	ControlCount   int64  `json:"controlCount"`
	TreatmentCount int64  `json:"treatmentCount"`
}

type ExperimentOutcome struct {
	PlanID           string             `json:"planId"`
	AnalysisRevision string             `json:"analysisRevision"`
	StartedAt        string             `json:"startedAt"`
	EndedAt          string             `json:"endedAt"`
	ExposureCost     string             `json:"exposureCost"`
	CostUnit         string             `json:"costUnit"`
	Effects          []ExperimentEffect `json:"effects"`
}

type ExperimentDecision struct {
	Eligible         bool     `json:"eligible"`
	PrimaryPassed    bool     `json:"primaryPassed"`
	GuardrailsPassed bool     `json:"guardrailsPassed"`
	Reasons          []string `json:"reasons,omitempty"`
}

// AssessExperiment applies only preregistered decision semantics. It does not
// infer causality, peek at partial data, or authorize rollout.
func AssessExperiment(plan ExperimentPlan, outcome ExperimentOutcome) (ExperimentDecision, error) {
	if err := validateExperimentPlan(plan); err != nil {
		return ExperimentDecision{}, err
	}
	if outcome.PlanID != plan.ID || outcome.AnalysisRevision == "" || outcome.CostUnit == "" {
		return ExperimentDecision{}, errors.New("outcome requires matching plan, analysis revision, and cost unit")
	}
	start, err := time.Parse(time.RFC3339, outcome.StartedAt)
	if err != nil {
		return ExperimentDecision{}, fmt.Errorf("startedAt: %w", err)
	}
	end, err := time.Parse(time.RFC3339, outcome.EndedAt)
	if err != nil || !end.After(start) {
		return ExperimentDecision{}, errors.New("endedAt must be RFC3339 and after startedAt")
	}
	if end.Sub(start) > time.Duration(plan.MaximumDurationSec)*time.Second {
		return ExperimentDecision{}, errors.New("outcome exceeds preregistered maximum duration")
	}
	if cost, ok := new(big.Rat).SetString(outcome.ExposureCost); !ok || cost.Sign() < 0 {
		return ExperimentDecision{}, errors.New("exposure cost must be a non-negative exact rational")
	}

	metrics := make(map[string]ExperimentMetric, len(plan.Metrics))
	for _, metric := range plan.Metrics {
		metrics[metric.ID] = metric
	}
	seen := map[string]bool{}
	decision := ExperimentDecision{PrimaryPassed: true, GuardrailsPassed: true}
	for _, effect := range outcome.Effects {
		metric, ok := metrics[effect.MetricID]
		if !ok || seen[effect.MetricID] {
			return ExperimentDecision{}, fmt.Errorf("unknown or duplicate effect %q", effect.MetricID)
		}
		seen[effect.MetricID] = true
		lower, lok := new(big.Rat).SetString(effect.LowerBound)
		upper, uok := new(big.Rat).SetString(effect.UpperBound)
		estimate, eok := new(big.Rat).SetString(effect.Estimate)
		if !lok || !uok || !eok || lower.Cmp(estimate) > 0 || estimate.Cmp(upper) > 0 {
			return ExperimentDecision{}, fmt.Errorf("effect %q has invalid exact bounds", effect.MetricID)
		}
		if effect.ControlCount < plan.MinimumSampleSize || effect.TreatmentCount < plan.MinimumSampleSize {
			decision.Reasons = append(decision.Reasons, effect.MetricID+": minimum sample not reached")
		}
		passed := lower.Sign() > 0
		if metric.Direction == ValueMinimize {
			passed = upper.Sign() < 0
		}
		if metric.Role == ExperimentPrimary && !passed {
			decision.PrimaryPassed = false
			decision.Reasons = append(decision.Reasons, effect.MetricID+": primary effect is inconclusive or adverse")
		}
		if metric.Role == ExperimentGuardrail {
			harmLimit, _ := new(big.Rat).SetString(metric.HarmLimit)
			passed = lower.Cmp(new(big.Rat).Neg(harmLimit)) >= 0
			if metric.Direction == ValueMinimize {
				passed = upper.Cmp(harmLimit) <= 0
			}
		}
		if metric.Role == ExperimentGuardrail && !passed {
			decision.GuardrailsPassed = false
			decision.Reasons = append(decision.Reasons, effect.MetricID+": guardrail is inconclusive or adverse")
		}
	}
	for id := range metrics {
		if !seen[id] {
			decision.Reasons = append(decision.Reasons, id+": missing effect")
		}
	}
	sort.Strings(decision.Reasons)
	decision.Eligible = decision.PrimaryPassed && decision.GuardrailsPassed && len(decision.Reasons) == 0
	return decision, nil
}

func validateExperimentPlan(plan ExperimentPlan) error {
	if plan.ID == "" || plan.Hypothesis == "" || plan.Control == "" || plan.Treatment == "" || plan.Control == plan.Treatment || plan.AssignmentUnit == "" || plan.RandomizationUnit == "" || plan.AnalysisID == "" || plan.StoppingRule == "" {
		return errors.New("experiment identity, hypothesis, distinct variants, assignment/randomization units, analysis, and stopping rule are required")
	}
	if plan.MinimumSampleSize <= 0 || plan.MaximumDurationSec <= 0 || plan.MaximumDurationSec > math.MaxInt64/int64(time.Second) {
		return errors.New("positive sample size and duration are required")
	}
	control, cok := new(big.Rat).SetString(plan.AllocationControl)
	treatment, tok := new(big.Rat).SetString(plan.AllocationTreatment)
	if !cok || !tok || control.Sign() <= 0 || treatment.Sign() <= 0 || new(big.Rat).Add(control, treatment).Cmp(big.NewRat(1, 1)) != 0 {
		return errors.New("allocation fractions must be positive exact rationals summing to one")
	}
	primary := 0
	seen := map[string]bool{}
	for _, metric := range plan.Metrics {
		if metric.ID == "" || metric.Unit == "" || seen[metric.ID] {
			return errors.New("metrics require unique ids and units")
		}
		seen[metric.ID] = true
		if metric.Direction != ValueMaximize && metric.Direction != ValueMinimize {
			return fmt.Errorf("metric %q has invalid direction", metric.ID)
		}
		if metric.Role == ExperimentPrimary {
			if metric.HarmLimit != "" {
				return fmt.Errorf("primary metric %q cannot declare a harm limit", metric.ID)
			}
			primary++
		} else if metric.Role == ExperimentGuardrail {
			harmLimit, ok := new(big.Rat).SetString(metric.HarmLimit)
			if !ok || harmLimit.Sign() < 0 {
				return fmt.Errorf("guardrail %q requires a non-negative exact harm limit", metric.ID)
			}
		} else {
			return fmt.Errorf("metric %q has invalid role", metric.ID)
		}
	}
	if primary != 1 {
		return errors.New("exactly one primary metric is required")
	}
	return nil
}

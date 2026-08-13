package optimization

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"
)

type ValueDirection string

const (
	ValueMaximize ValueDirection = "MAXIMIZE"
	ValueMinimize ValueDirection = "MINIMIZE"
)

type ValueCriterion struct {
	ID          string         `json:"id"`
	Stakeholder string         `json:"stakeholder"`
	UserSegment string         `json:"userSegment"`
	Function    string         `json:"function"`
	Criticality string         `json:"criticality"`
	Measure     string         `json:"measure"`
	Unit        string         `json:"unit"`
	Scale       string         `json:"scale"`
	Baseline    string         `json:"baseline"`
	Target      string         `json:"target"`
	Direction   ValueDirection `json:"direction"`
	HardMinimum string         `json:"hardMinimum,omitempty"`
	HardMaximum string         `json:"hardMaximum,omitempty"`
}

type ValueEstimate struct {
	Value          string `json:"value"`
	LowerBound     string `json:"lowerBound,omitempty"`
	UpperBound     string `json:"upperBound,omitempty"`
	Source         string `json:"source"`
	SourceRevision string `json:"sourceRevision"`
	AsOf           string `json:"asOf"`
	HorizonSeconds int64  `json:"horizonSeconds"`
	Distribution   string `json:"distribution"`
}

type LifecycleCost struct {
	Currency             string `json:"currency"`
	PriceSource          string `json:"priceSource"`
	PriceSourceRevision  string `json:"priceSourceRevision"`
	AsOf                 string `json:"asOf"`
	HorizonSeconds       int64  `json:"horizonSeconds"`
	DevelopmentMicros    int64  `json:"developmentMicros"`
	InfrastructureMicros int64  `json:"infrastructureMicros"`
	OperationsMicros     int64  `json:"operationsMicros"`
	SupportMicros        int64  `json:"supportMicros"`
	RiskMicros           int64  `json:"riskMicros"`
	MigrationMicros      int64  `json:"migrationMicros"`
	RetirementMicros     int64  `json:"retirementMicros"`
}

type ValueAlternative struct {
	ID                 string                   `json:"id"`
	Estimates          map[string]ValueEstimate `json:"estimates"`
	LifecycleCost      *LifecycleCost           `json:"lifecycleCost"`
	TimeToValueSeconds int64                    `json:"timeToValueSeconds"`
	DiscountRate       string                   `json:"discountRate"`
	DiscountBasis      string                   `json:"discountBasis"`
	Assumptions        []string                 `json:"assumptions"`
}

type ValueAssessment struct {
	ParetoEfficient      []string            `json:"paretoEfficient"`
	Dominated            map[string][]string `json:"dominated"`
	Infeasible           map[string][]string `json:"infeasible"`
	InsufficientEvidence map[string][]string `json:"insufficientEvidence"`
}

// AssessValue returns the conservative Pareto frontier. An alternative
// dominates another only when its worst-case bound is no worse on every
// criterion and strictly better on at least one. Missing evidence is never
// imputed, and hard functional thresholds are checked before comparison.
func AssessValue(criteria []ValueCriterion, alternatives []ValueAlternative) (ValueAssessment, error) {
	if len(criteria) == 0 {
		return ValueAssessment{}, errors.New("at least one value criterion is required")
	}
	if len(criteria) > 64 || len(alternatives) > 1_000 {
		return ValueAssessment{}, errors.New("value assessment exceeds 64 criteria or 1000 alternatives")
	}
	criteria = append([]ValueCriterion(nil), criteria...)
	alternatives = append([]ValueAlternative(nil), alternatives...)
	sort.Slice(criteria, func(i, j int) bool { return criteria[i].ID < criteria[j].ID })
	sort.Slice(alternatives, func(i, j int) bool { return alternatives[i].ID < alternatives[j].ID })
	criterionIndex := map[string]ValueCriterion{}
	for _, criterion := range criteria {
		if err := validateValueCriterion(criterion); err != nil {
			return ValueAssessment{}, err
		}
		if _, exists := criterionIndex[criterion.ID]; exists {
			return ValueAssessment{}, fmt.Errorf("duplicate criterion %q", criterion.ID)
		}
		criterionIndex[criterion.ID] = criterion
	}
	assessment := ValueAssessment{Dominated: map[string][]string{}, Infeasible: map[string][]string{}, InsufficientEvidence: map[string][]string{}}
	type candidate struct {
		id     string
		bounds map[string][2]*big.Rat
	}
	candidates := []candidate{}
	seen := map[string]struct{}{}
	for _, alternative := range alternatives {
		if alternative.ID == "" {
			return ValueAssessment{}, errors.New("alternative id is required")
		}
		if _, exists := seen[alternative.ID]; exists {
			return ValueAssessment{}, fmt.Errorf("duplicate alternative %q", alternative.ID)
		}
		seen[alternative.ID] = struct{}{}
		if err := validateValueAlternative(alternative); err != nil {
			return ValueAssessment{}, fmt.Errorf("alternative %q: %w", alternative.ID, err)
		}
		bounds := map[string][2]*big.Rat{}
		for criterionID := range alternative.Estimates {
			if _, ok := criterionIndex[criterionID]; !ok {
				return ValueAssessment{}, fmt.Errorf("alternative %q estimates unknown criterion %q", alternative.ID, criterionID)
			}
		}
		for _, criterion := range criteria {
			estimate, ok := alternative.Estimates[criterion.ID]
			if !ok {
				assessment.InsufficientEvidence[alternative.ID] = append(assessment.InsufficientEvidence[alternative.ID], criterion.ID)
				continue
			}
			lower, upper, err := validateValueEstimate(estimate)
			if err != nil {
				return ValueAssessment{}, fmt.Errorf("alternative %q criterion %q: %w", alternative.ID, criterion.ID, err)
			}
			if criterion.Scale == "ORDINAL" && (!lower.IsInt() || !upper.IsInt()) {
				return ValueAssessment{}, fmt.Errorf("alternative %q criterion %q has non-integer ordinal evidence", alternative.ID, criterion.ID)
			}
			bounds[criterion.ID] = [2]*big.Rat{lower, upper}
			if reason := thresholdViolation(criterion, lower, upper); reason != "" {
				assessment.Infeasible[alternative.ID] = append(assessment.Infeasible[alternative.ID], reason)
			}
		}
		if len(assessment.InsufficientEvidence[alternative.ID]) == 0 && len(assessment.Infeasible[alternative.ID]) == 0 {
			candidates = append(candidates, candidate{alternative.ID, bounds})
		}
	}
	for _, candidate := range candidates {
		for _, other := range candidates {
			if candidate.id == other.id {
				continue
			}
			if valueDominates(criteria, other.bounds, candidate.bounds) {
				assessment.Dominated[candidate.id] = append(assessment.Dominated[candidate.id], other.id)
			}
		}
		if len(assessment.Dominated[candidate.id]) == 0 {
			assessment.ParetoEfficient = append(assessment.ParetoEfficient, candidate.id)
		} else {
			sort.Strings(assessment.Dominated[candidate.id])
		}
	}
	return assessment, nil
}

func validateValueCriterion(criterion ValueCriterion) error {
	if criterion.ID == "" || criterion.Stakeholder == "" || criterion.UserSegment == "" || criterion.Function == "" || criterion.Criticality == "" || criterion.Measure == "" || criterion.Unit == "" || criterion.Scale == "" {
		return fmt.Errorf("criterion %q requires stakeholder, user segment, function, criticality, measure, unit, and scale", criterion.ID)
	}
	if criterion.Direction != ValueMaximize && criterion.Direction != ValueMinimize {
		return fmt.Errorf("criterion %q has invalid direction", criterion.ID)
	}
	if criterion.Scale != "CARDINAL" && criterion.Scale != "ORDINAL" {
		return fmt.Errorf("criterion %q has invalid scale", criterion.ID)
	}
	baseline, baselineOK := new(big.Rat).SetString(criterion.Baseline)
	target, targetOK := new(big.Rat).SetString(criterion.Target)
	if !baselineOK || !targetOK {
		return fmt.Errorf("criterion %q requires exact baseline and target", criterion.ID)
	}
	if criterion.Scale == "ORDINAL" && (!baseline.IsInt() || !target.IsInt()) {
		return fmt.Errorf("ordinal criterion %q requires integer baseline and target", criterion.ID)
	}
	if criterion.HardMinimum != "" {
		if _, ok := new(big.Rat).SetString(criterion.HardMinimum); !ok {
			return fmt.Errorf("criterion %q has invalid hard minimum", criterion.ID)
		}
	}
	if criterion.HardMaximum != "" {
		if _, ok := new(big.Rat).SetString(criterion.HardMaximum); !ok {
			return fmt.Errorf("criterion %q has invalid hard maximum", criterion.ID)
		}
	}
	return nil
}
func validateValueEstimate(estimate ValueEstimate) (*big.Rat, *big.Rat, error) {
	if estimate.Source == "" || estimate.SourceRevision == "" || estimate.HorizonSeconds <= 0 || estimate.Distribution == "" {
		return nil, nil, errors.New("source, revision, distribution, and positive horizon are required")
	}
	if _, err := time.Parse(time.RFC3339, estimate.AsOf); err != nil {
		return nil, nil, fmt.Errorf("asOf must be RFC3339: %w", err)
	}
	value, ok := new(big.Rat).SetString(estimate.Value)
	if !ok {
		return nil, nil, errors.New("value is not an exact rational")
	}
	lower := new(big.Rat).Set(value)
	upper := new(big.Rat).Set(value)
	if estimate.LowerBound != "" {
		if lower, ok = new(big.Rat).SetString(estimate.LowerBound); !ok {
			return nil, nil, errors.New("lower bound is not an exact rational")
		}
	}
	if estimate.UpperBound != "" {
		if upper, ok = new(big.Rat).SetString(estimate.UpperBound); !ok {
			return nil, nil, errors.New("upper bound is not an exact rational")
		}
	}
	if lower.Cmp(value) > 0 || upper.Cmp(value) < 0 {
		return nil, nil, errors.New("value falls outside bounds")
	}
	return lower, upper, nil
}

func validateValueAlternative(alternative ValueAlternative) error {
	if alternative.LifecycleCost == nil || alternative.TimeToValueSeconds < 0 || alternative.DiscountRate == "" || alternative.DiscountBasis == "" || len(alternative.Assumptions) == 0 {
		return errors.New("lifecycle cost, non-negative time-to-value, discount contract, and assumptions are required")
	}
	cost := alternative.LifecycleCost
	if !validCurrency(cost.Currency) || cost.PriceSource == "" || cost.PriceSourceRevision == "" || cost.HorizonSeconds <= 0 {
		return errors.New("lifecycle cost requires currency, pinned price source, and positive horizon")
	}
	if _, err := time.Parse(time.RFC3339, cost.AsOf); err != nil {
		return fmt.Errorf("lifecycle cost asOf: %w", err)
	}
	parts := []int64{cost.DevelopmentMicros, cost.InfrastructureMicros, cost.OperationsMicros, cost.SupportMicros, cost.RiskMicros, cost.MigrationMicros, cost.RetirementMicros}
	total := int64(0)
	for _, part := range parts {
		if part < 0 {
			return errors.New("lifecycle cost categories cannot be negative")
		}
		if part > int64(^uint64(0)>>1)-total {
			return errors.New("lifecycle cost total overflows micro-units")
		}
		total += part
	}
	discount, ok := new(big.Rat).SetString(alternative.DiscountRate)
	if !ok || discount.Sign() < 0 {
		return errors.New("discount rate must be a non-negative exact rational")
	}
	for _, assumption := range alternative.Assumptions {
		if assumption == "" {
			return errors.New("assumptions cannot be empty")
		}
	}
	return nil
}
func thresholdViolation(criterion ValueCriterion, lower, upper *big.Rat) string {
	if criterion.HardMinimum != "" {
		minimum, _ := new(big.Rat).SetString(criterion.HardMinimum)
		if lower.Cmp(minimum) < 0 {
			return criterion.ID + ": lower bound violates hard minimum"
		}
	}
	if criterion.HardMaximum != "" {
		maximum, _ := new(big.Rat).SetString(criterion.HardMaximum)
		if upper.Cmp(maximum) > 0 {
			return criterion.ID + ": upper bound violates hard maximum"
		}
	}
	return ""
}
func valueDominates(criteria []ValueCriterion, left, right map[string][2]*big.Rat) bool {
	strict := false
	for _, criterion := range criteria {
		leftBounds, rightBounds := left[criterion.ID], right[criterion.ID]
		comparison := 0
		if criterion.Direction == ValueMaximize {
			comparison = leftBounds[0].Cmp(rightBounds[1])
		} else {
			comparison = rightBounds[0].Cmp(leftBounds[1])
		}
		if comparison < 0 {
			return false
		}
		if comparison > 0 {
			strict = true
		}
	}
	return strict
}

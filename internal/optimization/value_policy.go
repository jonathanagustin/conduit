package optimization

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
)

type ValuePolicy struct {
	ID             string            `json:"id"`
	Source         string            `json:"source"`
	SourceRevision string            `json:"sourceRevision"`
	Weights        map[string]string `json:"weights"`
}

type ValuePolicyResult struct {
	PolicyID string            `json:"policyId"`
	Scores   map[string]string `json:"scores"`
	Selected []string          `json:"selected"`
}

type ValuePolicySensitivity struct {
	ParetoAssessment ValueAssessment     `json:"paretoAssessment"`
	Policies         []ValuePolicyResult `json:"policies"`
	SelectionChanges bool                `json:"selectionChanges"`
}

// AssessValuePolicies performs explicit weight sensitivity only after the
// conservative Pareto assessment. Baseline-to-target improvement normalizes
// unlike units; no implicit weights or monetary conversion are introduced.
func AssessValuePolicies(criteria []ValueCriterion, alternatives []ValueAlternative, policies []ValuePolicy) (ValuePolicySensitivity, error) {
	assessment, err := AssessValue(criteria, alternatives)
	if err != nil {
		return ValuePolicySensitivity{}, err
	}
	if len(policies) == 0 {
		return ValuePolicySensitivity{}, errors.New("at least one explicit value policy is required")
	}
	criterionByID := map[string]ValueCriterion{}
	for _, criterion := range criteria {
		criterionByID[criterion.ID] = criterion
	}
	alternativeByID := map[string]ValueAlternative{}
	for _, alternative := range alternatives {
		alternativeByID[alternative.ID] = alternative
	}
	result := ValuePolicySensitivity{ParetoAssessment: assessment}
	var firstSelection string
	seenPolicies := map[string]bool{}
	for _, policy := range policies {
		if policy.ID == "" || policy.Source == "" || policy.SourceRevision == "" || seenPolicies[policy.ID] {
			return ValuePolicySensitivity{}, fmt.Errorf("policy %q requires unique identity and provenance", policy.ID)
		}
		seenPolicies[policy.ID] = true
		weights := map[string]*big.Rat{}
		total := new(big.Rat)
		for criterionID, encoded := range policy.Weights {
			if _, ok := criterionByID[criterionID]; !ok {
				return ValuePolicySensitivity{}, fmt.Errorf("policy %q weights unknown criterion %q", policy.ID, criterionID)
			}
			weight, ok := new(big.Rat).SetString(encoded)
			if !ok || weight.Sign() < 0 {
				return ValuePolicySensitivity{}, fmt.Errorf("policy %q has invalid weight", policy.ID)
			}
			weights[criterionID] = weight
			total.Add(total, weight)
		}
		if len(weights) != len(criteria) || total.Cmp(big.NewRat(1, 1)) != 0 {
			return ValuePolicySensitivity{}, fmt.Errorf("policy %q must weight every criterion with exact total one", policy.ID)
		}
		policyResult := ValuePolicyResult{PolicyID: policy.ID, Scores: map[string]string{}}
		var best *big.Rat
		for _, alternativeID := range assessment.ParetoEfficient {
			alternative := alternativeByID[alternativeID]
			score := new(big.Rat)
			for _, criterion := range criteria {
				baseline, _ := new(big.Rat).SetString(criterion.Baseline)
				target, _ := new(big.Rat).SetString(criterion.Target)
				value, _ := new(big.Rat).SetString(alternative.Estimates[criterion.ID].Value)
				denominator, numerator := new(big.Rat), new(big.Rat)
				if criterion.Direction == ValueMaximize {
					denominator.Sub(target, baseline)
					numerator.Sub(value, baseline)
				} else {
					denominator.Sub(baseline, target)
					numerator.Sub(baseline, value)
				}
				if denominator.Sign() <= 0 {
					return ValuePolicySensitivity{}, fmt.Errorf("criterion %q target does not improve on baseline", criterion.ID)
				}
				normalized := new(big.Rat).Quo(numerator, denominator)
				score.Add(score, new(big.Rat).Mul(weights[criterion.ID], normalized))
			}
			policyResult.Scores[alternativeID] = score.RatString()
			if best == nil || score.Cmp(best) > 0 {
				best = new(big.Rat).Set(score)
				policyResult.Selected = []string{alternativeID}
			} else if score.Cmp(best) == 0 {
				policyResult.Selected = append(policyResult.Selected, alternativeID)
			}
		}
		sort.Strings(policyResult.Selected)
		selection := fmt.Sprint(policyResult.Selected)
		if firstSelection == "" {
			firstSelection = selection
		} else if selection != firstSelection {
			result.SelectionChanges = true
		}
		result.Policies = append(result.Policies, policyResult)
	}
	sort.Slice(result.Policies, func(i, j int) bool { return result.Policies[i].PolicyID < result.Policies[j].PolicyID })
	return result, nil
}

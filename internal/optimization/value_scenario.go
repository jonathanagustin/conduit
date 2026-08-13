package optimization

import (
	"errors"
	"fmt"
	"sort"
)

// ValueScenario is a complete, provenance-pinned set of estimates under one
// explicit set of assumptions. Alternatives must have stable identities across
// scenarios so frontier changes cannot be confused with inventory changes.
type ValueScenario struct {
	ID             string             `json:"id"`
	Source         string             `json:"source"`
	SourceRevision string             `json:"sourceRevision"`
	Alternatives   []ValueAlternative `json:"alternatives"`
}

type ValueScenarioResult struct {
	ScenarioID string          `json:"scenarioId"`
	Assessment ValueAssessment `json:"assessment"`
}

type ValueScenarioSensitivity struct {
	Scenarios       []ValueScenarioResult `json:"scenarios"`
	FrontierChanges bool                  `json:"frontierChanges"`
}

// AssessValueScenarios exposes assumption sensitivity without averaging or
// assigning probabilities to scenarios. Each scenario gets an independent
// conservative Pareto assessment.
func AssessValueScenarios(criteria []ValueCriterion, scenarios []ValueScenario) (ValueScenarioSensitivity, error) {
	if len(scenarios) < 2 {
		return ValueScenarioSensitivity{}, errors.New("at least two value scenarios are required")
	}
	result := ValueScenarioSensitivity{}
	seen := map[string]bool{}
	var alternativeIDs, firstFrontier string
	for _, scenario := range scenarios {
		if scenario.ID == "" || scenario.Source == "" || scenario.SourceRevision == "" || seen[scenario.ID] {
			return ValueScenarioSensitivity{}, fmt.Errorf("scenario %q requires unique identity and provenance", scenario.ID)
		}
		seen[scenario.ID] = true
		ids := make([]string, len(scenario.Alternatives))
		for i, alternative := range scenario.Alternatives {
			ids[i] = alternative.ID
		}
		sort.Strings(ids)
		encodedIDs := fmt.Sprint(ids)
		if alternativeIDs == "" {
			alternativeIDs = encodedIDs
		} else if alternativeIDs != encodedIDs {
			return ValueScenarioSensitivity{}, fmt.Errorf("scenario %q changes alternative identities", scenario.ID)
		}
		assessment, err := AssessValue(criteria, scenario.Alternatives)
		if err != nil {
			return ValueScenarioSensitivity{}, fmt.Errorf("scenario %q: %w", scenario.ID, err)
		}
		frontier := fmt.Sprint(assessment.ParetoEfficient)
		if firstFrontier == "" {
			firstFrontier = frontier
		} else if frontier != firstFrontier {
			result.FrontierChanges = true
		}
		result.Scenarios = append(result.Scenarios, ValueScenarioResult{ScenarioID: scenario.ID, Assessment: assessment})
	}
	sort.Slice(result.Scenarios, func(i, j int) bool { return result.Scenarios[i].ScenarioID < result.Scenarios[j].ScenarioID })
	return result, nil
}

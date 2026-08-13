package optimization

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"
)

type RealizedValue struct {
	CriterionID string        `json:"criterionId"`
	Estimate    ValueEstimate `json:"estimate"`
}

type ValueOutcomeEvidence struct {
	AssessmentID  string          `json:"assessmentId"`
	AlternativeID string          `json:"alternativeId"`
	ObservedAt    string          `json:"observedAt"`
	Values        []RealizedValue `json:"values"`
}

type ValueAccuracyEntry struct {
	CriterionID       string `json:"criterionId"`
	Unit              string `json:"unit"`
	Predicted         string `json:"predicted"`
	Realized          string `json:"realized"`
	SignedError       string `json:"signedError"`
	AbsoluteError     string `json:"absoluteError"`
	PredictionSource  string `json:"predictionSource"`
	RealizationSource string `json:"realizationSource"`
}

type ValueAccuracy struct {
	AssessmentID  string               `json:"assessmentId"`
	AlternativeID string               `json:"alternativeId"`
	ObservedAt    string               `json:"observedAt"`
	Entries       []ValueAccuracyEntry `json:"entries"`
}

// MeasureValueAccuracy joins complete realized stakeholder/function outcomes
// to the exact persisted prediction. It never rewrites the Pareto assessment.
func MeasureValueAccuracy(record ValueAssessmentRecord, evidence ValueOutcomeEvidence) (ValueAccuracy, error) {
	if err := VerifyValueAssessmentRecord(record); err != nil {
		return ValueAccuracy{}, err
	}
	if evidence.AssessmentID != record.ID || evidence.AlternativeID == "" {
		return ValueAccuracy{}, errors.New("value outcome assessment/alternative identity does not match")
	}
	observedAt, err := time.Parse(time.RFC3339Nano, evidence.ObservedAt)
	if err != nil {
		return ValueAccuracy{}, fmt.Errorf("value outcome observedAt: %w", err)
	}
	createdAt, _ := time.Parse(time.RFC3339Nano, record.CreatedAt)
	if observedAt.Before(createdAt) {
		return ValueAccuracy{}, errors.New("value outcome predates assessment")
	}
	criteria := map[string]ValueCriterion{}
	for _, criterion := range record.Criteria {
		criteria[criterion.ID] = criterion
	}
	var alternative *ValueAlternative
	for i := range record.Alternatives {
		if record.Alternatives[i].ID == evidence.AlternativeID {
			alternative = &record.Alternatives[i]
			break
		}
	}
	if alternative == nil {
		return ValueAccuracy{}, errors.New("value outcome names unknown alternative")
	}
	realized := map[string]ValueEstimate{}
	for _, value := range evidence.Values {
		if _, exists := realized[value.CriterionID]; exists {
			return ValueAccuracy{}, fmt.Errorf("duplicate realized criterion %q", value.CriterionID)
		}
		realized[value.CriterionID] = value.Estimate
	}
	accuracy := ValueAccuracy{AssessmentID: record.ID, AlternativeID: alternative.ID, ObservedAt: observedAt.UTC().Format(time.RFC3339Nano)}
	for criterionID, prediction := range alternative.Estimates {
		criterion, ok := criteria[criterionID]
		if !ok {
			return ValueAccuracy{}, fmt.Errorf("prediction references unknown criterion %q", criterionID)
		}
		outcome, ok := realized[criterionID]
		if !ok {
			return ValueAccuracy{}, fmt.Errorf("missing realized criterion %q", criterionID)
		}
		if outcome.HorizonSeconds != prediction.HorizonSeconds {
			return ValueAccuracy{}, fmt.Errorf("criterion %q horizon does not match prediction", criterionID)
		}
		_, _, err := validateValueEstimate(outcome)
		if err != nil {
			return ValueAccuracy{}, fmt.Errorf("criterion %q realized value: %w", criterionID, err)
		}
		predictionValue, _ := new(big.Rat).SetString(prediction.Value)
		realizedValue, _ := new(big.Rat).SetString(outcome.Value)
		signed := new(big.Rat).Sub(realizedValue, predictionValue)
		absolute := new(big.Rat).Abs(new(big.Rat).Set(signed))
		accuracy.Entries = append(accuracy.Entries, ValueAccuracyEntry{CriterionID: criterionID, Unit: criterion.Unit, Predicted: predictionValue.RatString(), Realized: realizedValue.RatString(), SignedError: signed.RatString(), AbsoluteError: absolute.RatString(), PredictionSource: prediction.Source + "@" + prediction.SourceRevision, RealizationSource: outcome.Source + "@" + outcome.SourceRevision})
		delete(realized, criterionID)
	}
	if len(realized) != 0 {
		return ValueAccuracy{}, errors.New("value outcome contains unpredicted criteria")
	}
	sort.Slice(accuracy.Entries, func(i, j int) bool { return accuracy.Entries[i].CriterionID < accuracy.Entries[j].CriterionID })
	return accuracy, nil
}

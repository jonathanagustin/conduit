package optimization

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"
)

// RealizedCost is authoritative post-execution evidence for one recommended
// assignment. CostMicros is exact in OutcomeCostEvidence.Currency.
type RealizedCost struct {
	Item       string `json:"item"`
	Resource   string `json:"resource"`
	CostMicros int64  `json:"costMicros"`
}

type OutcomeCostEvidence struct {
	RecommendationID string         `json:"recommendationId"`
	Currency         string         `json:"currency"`
	Source           string         `json:"source"`
	Revision         string         `json:"revision"`
	ObservedAt       string         `json:"observedAt"`
	Costs            []RealizedCost `json:"costs"`
}

type CostAccuracy struct {
	RecommendationID             string `json:"recommendationId"`
	Currency                     string `json:"currency"`
	EstimateSource               string `json:"estimateSource"`
	RealizedSource               string `json:"realizedSource"`
	EstimatedTotalMicros         int64  `json:"estimatedTotalMicros"`
	RealizedTotalMicros          int64  `json:"realizedTotalMicros"`
	SignedErrorMicros            int64  `json:"signedErrorMicros"`
	AbsoluteErrorMicros          int64  `json:"absoluteErrorMicros"`
	AbsoluteErrorPartsPerMillion string `json:"absoluteErrorPartsPerMillion,omitempty"`
}

// MeasureCostAccuracy compares a complete, exact recommendation estimate with
// complete authoritative outcome evidence. Partial or mismatched evidence is
// rejected rather than producing an apparently precise accuracy metric.
func MeasureCostAccuracy(recommendation Recommendation, evidence OutcomeCostEvidence) (CostAccuracy, error) {
	if err := VerifyRecommendation(recommendation); err != nil {
		return CostAccuracy{}, err
	}
	if evidence.RecommendationID != recommendation.ID {
		return CostAccuracy{}, errors.New("cost outcome recommendation id does not match")
	}
	result := recommendation.Result
	if result.CostModel == nil || result.RoundedEstimatedCostMicros == nil {
		return CostAccuracy{}, errors.New("recommendation has no complete estimated cost")
	}
	if evidence.Currency != result.CostModel.Currency || !validCurrency(evidence.Currency) {
		return CostAccuracy{}, errors.New("realized cost currency does not match estimate")
	}
	if evidence.Source == "" || evidence.Revision == "" {
		return CostAccuracy{}, errors.New("realized cost source and revision are required")
	}
	observedAt, err := time.Parse(time.RFC3339, evidence.ObservedAt)
	if err != nil {
		return CostAccuracy{}, fmt.Errorf("realized cost observedAt must be RFC3339: %w", err)
	}
	createdAt, _ := time.Parse(time.RFC3339Nano, recommendation.CreatedAt)
	modelAsOf, _ := time.Parse(time.RFC3339, result.CostModel.AsOf)
	if observedAt.Before(createdAt) || observedAt.Before(modelAsOf) {
		return CostAccuracy{}, errors.New("realized cost predates recommendation or cost model")
	}
	want := make(map[string]string, len(result.Assignments))
	for _, assignment := range result.Assignments {
		want[assignment.Item] = assignment.Resource
	}
	seen := make(map[string]struct{}, len(evidence.Costs))
	realized := int64(0)
	for _, cost := range evidence.Costs {
		resource, ok := want[cost.Item]
		if !ok || resource != cost.Resource {
			return CostAccuracy{}, fmt.Errorf("realized cost does not match assignment %q/%q", cost.Item, cost.Resource)
		}
		if cost.CostMicros < 0 {
			return CostAccuracy{}, fmt.Errorf("realized cost for %q is negative", cost.Item)
		}
		if _, duplicate := seen[cost.Item]; duplicate {
			return CostAccuracy{}, fmt.Errorf("duplicate realized cost for %q", cost.Item)
		}
		seen[cost.Item] = struct{}{}
		if cost.CostMicros > math.MaxInt64-realized {
			return CostAccuracy{}, errors.New("realized cost total overflows micro-units")
		}
		realized += cost.CostMicros
	}
	if len(seen) != len(want) {
		return CostAccuracy{}, fmt.Errorf("realized cost covers %d of %d assignments", len(seen), len(want))
	}
	estimated := *result.RoundedEstimatedCostMicros
	errorMicros := realized - estimated
	absolute := errorMicros
	if absolute < 0 {
		absolute = -absolute
	}
	accuracy := CostAccuracy{RecommendationID: recommendation.ID, Currency: evidence.Currency, EstimateSource: result.CostModel.Source, RealizedSource: evidence.Source + "@" + evidence.Revision, EstimatedTotalMicros: estimated, RealizedTotalMicros: realized, SignedErrorMicros: errorMicros, AbsoluteErrorMicros: absolute}
	if realized > 0 {
		numerator := new(big.Int).Mul(big.NewInt(absolute), big.NewInt(1_000_000))
		accuracy.AbsoluteErrorPartsPerMillion = new(big.Int).Quo(numerator, big.NewInt(realized)).String()
	}
	return accuracy, nil
}

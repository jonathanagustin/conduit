// Package providerprometheus reduces Prometheus HTTP API query results to
// configured scalar facts without retaining series labels or raw responses.
package providerprometheus

import (
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

type ScalarProjection struct {
	CallID        string
	RequirementID string
	EntityID      string
	Attribute     string
	Unit          string
	Fidelity      optimization.Fidelity
}

// ScalarExtractor requires exactly one scalar or one vector sample. Queries
// that fan out to multiple series are ambiguous and fail closed rather than
// silently aggregating labels with tenant semantics.
type ScalarExtractor struct {
	Projections []ScalarProjection
}

func (extractor ScalarExtractor) Extract(request optimization.AcquisitionReadRequest, response optimization.AcquisitionReadResponse) ([]optimization.ExtractedAcquisitionFact, error) {
	if request.Source != "prometheus" || !strings.HasPrefix(request.Endpoint, "/api/v1/query") {
		return nil, errors.New("Prometheus extractor received a foreign request")
	}
	var projection *ScalarProjection
	for index := range extractor.Projections {
		candidate := &extractor.Projections[index]
		if candidate.CallID == request.ID {
			if projection != nil {
				return nil, errors.New("Prometheus extractor has duplicate call projections")
			}
			projection = candidate
		}
	}
	if projection == nil || projection.RequirementID == "" || projection.EntityID == "" || projection.Attribute == "" || projection.Unit == "" {
		return nil, errors.New("Prometheus extractor lacks an exact fact projection")
	}
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string          `json:"resultType"`
			Result     json.RawMessage `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &envelope); err != nil || envelope.Status != "success" {
		return nil, errors.New("Prometheus response is not a successful query result")
	}
	sample, err := exactSample(envelope.Data.ResultType, envelope.Data.Result)
	if err != nil {
		return nil, err
	}
	fidelity := projection.Fidelity
	if fidelity == "" {
		fidelity = optimization.FidelitySampled
	}
	value, _ := json.Marshal(sample.value)
	return []optimization.ExtractedAcquisitionFact{{
		RequirementID: projection.RequirementID, EntityID: projection.EntityID, Attribute: projection.Attribute,
		Value: value, Unit: projection.Unit, Plane: optimization.StateObserved, Fidelity: fidelity,
		ValidFrom: sample.observedAt.Format(time.RFC3339Nano), Uncertainty: "Prometheus query sample; series labels and scrape internals are not retained",
	}}, nil
}

type prometheusSample struct {
	observedAt time.Time
	value      string
}

func exactSample(resultType string, raw json.RawMessage) (prometheusSample, error) {
	var tuple []json.RawMessage
	switch resultType {
	case "scalar":
		if err := json.Unmarshal(raw, &tuple); err != nil {
			return prometheusSample{}, errors.New("Prometheus scalar result is malformed")
		}
	case "vector":
		var vector []struct {
			Value []json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal(raw, &vector); err != nil || len(vector) != 1 {
			return prometheusSample{}, errors.New("Prometheus vector result must contain exactly one series")
		}
		tuple = vector[0].Value
	default:
		return prometheusSample{}, errors.New("Prometheus result must be scalar or single-series vector")
	}
	if len(tuple) != 2 {
		return prometheusSample{}, errors.New("Prometheus sample tuple is malformed")
	}
	var timestampNumber json.Number
	if err := json.Unmarshal(tuple[0], &timestampNumber); err != nil {
		return prometheusSample{}, errors.New("Prometheus sample timestamp is malformed")
	}
	var value string
	if err := json.Unmarshal(tuple[1], &value); err != nil || !canonicalFiniteDecimal(value) {
		return prometheusSample{}, errors.New("Prometheus sample value is not a finite decimal")
	}
	timestamp, err := decimalUnixTime(timestampNumber.String())
	if err != nil {
		return prometheusSample{}, err
	}
	return prometheusSample{observedAt: timestamp, value: value}, nil
}

func decimalUnixTime(value string) (time.Time, error) {
	if !canonicalFiniteDecimal(value) {
		return time.Time{}, errors.New("Prometheus timestamp is not a finite decimal")
	}
	rational, ok := new(big.Rat).SetString(value)
	if !ok || rational.Sign() < 0 {
		return time.Time{}, errors.New("Prometheus timestamp is outside the supported domain")
	}
	nanoseconds := new(big.Rat).Mul(rational, big.NewRat(1_000_000_000, 1))
	if !nanoseconds.IsInt() || !nanoseconds.Num().IsInt64() {
		return time.Time{}, errors.New("Prometheus timestamp exceeds nanosecond precision")
	}
	total := nanoseconds.Num().Int64()
	return time.Unix(total/1_000_000_000, total%1_000_000_000).UTC(), nil
}

func canonicalFiniteDecimal(value string) bool {
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "eE+ ") || value == "NaN" || strings.Contains(value, "Inf") {
		return false
	}
	value = strings.TrimPrefix(value, "-")
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" || (len(parts) == 2 && parts[1] == "") {
		return false
	}
	for _, part := range parts {
		for _, character := range part {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

var _ optimization.AcquisitionFactExtractor = ScalarExtractor{}

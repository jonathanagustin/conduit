// Package provideropencost contains read-only OpenCost anti-corruption
// adapters. OpenCost projections are modeled cost observations; they are not
// authoritative provider invoices and therefore never claim the BILLED plane.
package provideropencost

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

const maximumCloudCostEntries = 10_000

type CloudCostProjection struct {
	CallID        string
	RequirementID string
	EntityID      string
	Currency      string
	// Metric is deliberately closed to a cost concept documented by the
	// OpenCost Cloud Costs API. It defaults to amortizedNetCost.
	Metric string
}

type CloudCostExtractor struct {
	Projection CloudCostProjection
}

type costWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type costValue struct {
	Cost json.Number `json:"cost"`
}

type cloudCostEntry struct {
	Window           costWindow `json:"window"`
	AmortizedNetCost costValue  `json:"amortizedNetCost"`
}

// Extract sums a bounded set of non-overlapping Cloud Cost windows using exact
// rational arithmetic. Provider/account/resource labels are intentionally
// discarded. Currency is an exact environment-owned projection setting because
// the documented response does not carry a currency field.
func (extractor CloudCostExtractor) Extract(request optimization.AcquisitionReadRequest, response optimization.AcquisitionReadResponse) ([]optimization.ExtractedAcquisitionFact, error) {
	projection := extractor.Projection
	if request.ID != projection.CallID || request.Source != "opencost" || !strings.HasPrefix(request.Endpoint, "/cloudCost?") {
		return nil, errors.New("OpenCost extractor received a foreign request")
	}
	if projection.RequirementID == "" || projection.EntityID == "" || !validCurrency(projection.Currency) {
		return nil, errors.New("OpenCost extractor lacks an exact requirement, entity, or currency projection")
	}
	metric := projection.Metric
	if metric == "" {
		metric = "amortizedNetCost"
	}
	if metric != "amortizedNetCost" {
		return nil, errors.New("OpenCost extractor metric is not admitted")
	}
	decoder := json.NewDecoder(strings.NewReader(string(response.Body)))
	decoder.UseNumber()
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Sets []struct {
				CloudCosts map[string]cloudCostEntry `json:"cloudCosts"`
			} `json:"sets"`
		} `json:"data"`
	}
	if err := decoder.Decode(&envelope); err != nil || envelope.Code != 200 || len(envelope.Data.Sets) == 0 || len(envelope.Data.Sets) > 366 {
		return nil, errors.New("OpenCost response violates the bounded Cloud Cost contract")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("OpenCost response contains trailing input")
	}
	total := new(big.Rat)
	type interval struct{ start, end time.Time }
	intervals := make([]interval, 0, len(envelope.Data.Sets))
	entries := 0
	for _, set := range envelope.Data.Sets {
		if len(set.CloudCosts) == 0 || entries > maximumCloudCostEntries-len(set.CloudCosts) {
			return nil, errors.New("OpenCost response has no costs or exceeds the entry bound")
		}
		entries += len(set.CloudCosts)
		var setInterval interval
		for _, item := range set.CloudCosts {
			start, err := time.Parse(time.RFC3339Nano, item.Window.Start)
			if err != nil {
				return nil, errors.New("OpenCost item has an invalid window start")
			}
			end, err := time.Parse(time.RFC3339Nano, item.Window.End)
			if err != nil || !end.After(start) {
				return nil, errors.New("OpenCost item has an invalid window end")
			}
			if setInterval.start.IsZero() {
				setInterval = interval{start.UTC(), end.UTC()}
			} else if !setInterval.start.Equal(start) || !setInterval.end.Equal(end) {
				return nil, errors.New("OpenCost set mixes cost windows")
			}
			amount, ok := new(big.Rat).SetString(item.AmortizedNetCost.Cost.String())
			if !ok || amount.Sign() < 0 {
				return nil, errors.New("OpenCost item cost is not a non-negative exact number")
			}
			total.Add(total, amount)
		}
		intervals = append(intervals, setInterval)
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].start.Before(intervals[j].start) })
	for index := 1; index < len(intervals); index++ {
		if intervals[index].start.Before(intervals[index-1].end) {
			return nil, errors.New("OpenCost response contains overlapping accumulation windows")
		}
		if !intervals[index].start.Equal(intervals[index-1].end) {
			return nil, errors.New("OpenCost response contains a gap between accumulation windows")
		}
	}
	value, err := json.Marshal(struct {
		Amount      string `json:"amountRational"`
		Currency    string `json:"currency"`
		Metric      string `json:"metric"`
		WindowStart string `json:"windowStart"`
		WindowEnd   string `json:"windowEnd"`
		EntryCount  int    `json:"entryCount"`
	}{total.RatString(), projection.Currency, metric, intervals[0].start.Format(time.RFC3339Nano), intervals[len(intervals)-1].end.Format(time.RFC3339Nano), entries})
	if err != nil {
		return nil, fmt.Errorf("encode OpenCost projection: %w", err)
	}
	return []optimization.ExtractedAcquisitionFact{{
		RequirementID: projection.RequirementID,
		EntityID:      projection.EntityID,
		Attribute:     "opencost.cloud-cost.amortized-net",
		Value:         value,
		Unit:          projection.Currency,
		Plane:         optimization.StateObserved,
		Fidelity:      optimization.FidelityEstimated,
		ValidFrom:     intervals[len(intervals)-1].end.Format(time.RFC3339Nano),
		Uncertainty:   "OpenCost allocation projection; configured currency is not response-authenticated and this is not an authoritative invoice",
	}}, nil
}

func validCurrency(currency string) bool {
	if len(currency) != 3 {
		return false
	}
	for _, character := range currency {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}

var _ optimization.AcquisitionFactExtractor = CloudCostExtractor{}

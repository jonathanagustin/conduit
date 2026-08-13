package provideropencost

import (
	"testing"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

func TestCloudCostProjectionIsExactObservedEstimateNotBilled(t *testing.T) {
	extractor := CloudCostExtractor{Projection: CloudCostProjection{CallID: "cost", RequirementID: "cost.allocated", EntityID: "environment:test", Currency: "USD"}}
	request := optimization.AcquisitionReadRequest{ID: "cost", Source: "opencost", Endpoint: "/cloudCost?accumulate=all&aggregate=provider&window=2026-08-01T00%3A00%3A00Z%2C2026-08-03T00%3A00%3A00Z"}
	response := optimization.AcquisitionReadResponse{Body: []byte(`{"code":200,"data":{"sets":[{"cloudCosts":{"AWS":{"window":{"start":"2026-08-01T00:00:00Z","end":"2026-08-02T00:00:00Z"},"amortizedNetCost":{"cost":1.20}}}},{"cloudCosts":{"AWS":{"window":{"start":"2026-08-02T00:00:00Z","end":"2026-08-03T00:00:00Z"},"amortizedNetCost":{"cost":2.35}}}}]}}`)}
	facts, err := extractor.Extract(request, response)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].Plane != optimization.StateObserved || facts[0].Fidelity != optimization.FidelityEstimated || facts[0].Unit != "USD" {
		t.Fatalf("unexpected fact: %#v", facts)
	}
	if got := string(facts[0].Value); got != `{"amountRational":"71/20","currency":"USD","metric":"amortizedNetCost","windowStart":"2026-08-01T00:00:00Z","windowEnd":"2026-08-03T00:00:00Z","entryCount":2}` {
		t.Fatalf("unexpected exact projection: %s", got)
	}
}

func TestCloudCostFailsClosedOnOverlapNegativeOrForeignProjection(t *testing.T) {
	extractor := CloudCostExtractor{Projection: CloudCostProjection{CallID: "cost", RequirementID: "cost.allocated", EntityID: "environment:test", Currency: "USD"}}
	request := optimization.AcquisitionReadRequest{ID: "cost", Source: "opencost", Endpoint: "/cloudCost?aggregate=provider&window=7d"}
	for name, body := range map[string]string{
		"overlap":  `{"code":200,"data":{"sets":[{"cloudCosts":{"a":{"window":{"start":"2026-08-01T00:00:00Z","end":"2026-08-03T00:00:00Z"},"amortizedNetCost":{"cost":1}}}},{"cloudCosts":{"a":{"window":{"start":"2026-08-02T00:00:00Z","end":"2026-08-04T00:00:00Z"},"amortizedNetCost":{"cost":1}}}}]}}`,
		"gap":      `{"code":200,"data":{"sets":[{"cloudCosts":{"a":{"window":{"start":"2026-08-01T00:00:00Z","end":"2026-08-02T00:00:00Z"},"amortizedNetCost":{"cost":1}}}},{"cloudCosts":{"a":{"window":{"start":"2026-08-03T00:00:00Z","end":"2026-08-04T00:00:00Z"},"amortizedNetCost":{"cost":1}}}}]}}`,
		"negative": `{"code":200,"data":{"sets":[{"cloudCosts":{"a":{"window":{"start":"2026-08-01T00:00:00Z","end":"2026-08-02T00:00:00Z"},"amortizedNetCost":{"cost":-1}}}}]}}`,
		"trailing": `{"code":200,"data":{"sets":[{"cloudCosts":{"a":{"window":{"start":"2026-08-01T00:00:00Z","end":"2026-08-02T00:00:00Z"},"amortizedNetCost":{"cost":1}}}}]}} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := extractor.Extract(request, optimization.AcquisitionReadResponse{Body: []byte(body)}); err == nil {
				t.Fatal("expected malformed cost response to fail closed")
			}
		})
	}
	foreign := request
	foreign.Source = "provider-billing"
	if _, err := extractor.Extract(foreign, optimization.AcquisitionReadResponse{}); err == nil {
		t.Fatal("expected foreign source rejection")
	}
}

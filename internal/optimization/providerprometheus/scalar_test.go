package providerprometheus

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

func TestScalarExtractorProjectsOneSampleWithoutLabels(t *testing.T) {
	extractor := ScalarExtractor{Projections: []ScalarProjection{{
		CallID: "prometheus.latency", RequirementID: "observed.latency", EntityID: "environment/dev-use2",
		Attribute: "slo.p95-latency", Unit: "seconds", Fidelity: optimization.FidelitySampled,
	}}}
	body := []byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"tenant":"must-not-leave","pod":"secret-pod"},"value":[1786536000.125,"0.250"]}]}}`)
	facts, err := extractor.Extract(
		optimization.AcquisitionReadRequest{ID: "prometheus.latency", Source: "prometheus", Endpoint: "/api/v1/query?query=histogram_quantile"},
		optimization.AcquisitionReadResponse{Body: body},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].ValidFrom != "2026-08-12T12:00:00.125Z" || string(facts[0].Value) != `"0.250"` {
		t.Fatalf("Prometheus fact = %#v", facts)
	}
	encoded, _ := json.Marshal(facts)
	if strings.Contains(string(encoded), "must-not-leave") || strings.Contains(string(encoded), "secret-pod") {
		t.Fatalf("series labels crossed projection: %s", encoded)
	}
}

func TestScalarExtractorRejectsAmbiguousOrNonFiniteResults(t *testing.T) {
	extractor := ScalarExtractor{Projections: []ScalarProjection{{CallID: "query", RequirementID: "r", EntityID: "e", Attribute: "a", Unit: "count"}}}
	request := optimization.AcquisitionReadRequest{ID: "query", Source: "prometheus", Endpoint: "/api/v1/query?query=x"}
	for _, body := range []string{
		`{"status":"success","data":{"resultType":"vector","result":[]}}`,
		`{"status":"success","data":{"resultType":"vector","result":[{"value":[1,"1"]},{"value":[1,"2"]}]}}`,
		`{"status":"success","data":{"resultType":"scalar","result":[1,"NaN"]}}`,
		`{"status":"success","data":{"resultType":"matrix","result":[]}}`,
	} {
		if _, err := extractor.Extract(request, optimization.AcquisitionReadResponse{Body: []byte(body)}); err == nil {
			t.Fatalf("accepted ambiguous Prometheus response %s", body)
		}
	}
}

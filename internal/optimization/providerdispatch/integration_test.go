package providerdispatch_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/optimization"
	"github.com/jonathanagustin/conduit/internal/optimization/providerdispatch"
	"github.com/jonathanagustin/conduit/internal/optimization/providerhttp"
	"github.com/jonathanagustin/conduit/internal/optimization/providerkubernetes"
)

type integrationRoundTrip func(*http.Request) (*http.Response, error)

func (function integrationRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestExactDispatchHTTPAndKubernetesProjectionProduceOnlyNormalizedFact(t *testing.T) {
	observedAt := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	endpoint := "/api/v1/namespaces/dev-use2/pods?limit=500"
	httpAdapter, err := providerhttp.New(providerhttp.Options{
		BaseURL: "https://kubernetes.example.test", Now: func() time.Time { return observedAt },
		Routes: []providerhttp.Route{{Source: "kubernetes", Endpoint: endpoint, Consistency: "resource-version"}},
		Transport: integrationRoundTrip(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodGet || request.URL.RequestURI() != endpoint {
				t.Fatalf("unexpected provider request: %s %s", request.Method, request.URL)
			}
			body := `{"kind":"PodList","metadata":{"resourceVersion":"17"},"items":[{"metadata":{"name":"must-not-persist"},"spec":{"containers":[{"env":[{"name":"TOKEN","value":"must-not-persist"}]}]},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}]}`
			return &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"X-Resource-Version": []string{"17"}},
				Body: io.NopCloser(strings.NewReader(body)),
			}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := providerdispatch.NewReader([]providerdispatch.ReadBinding{{Source: "kubernetes", Port: httpAdapter}})
	if err != nil {
		t.Fatal(err)
	}
	callID := "kubernetes.pods"
	requirementID := "observed.pods.total"
	extractor, err := providerdispatch.NewExtractor([]providerdispatch.ExtractBinding{{
		CallID: callID, Source: "kubernetes", Endpoint: endpoint,
		Extractor: providerkubernetes.PodListExtractor{Requirements: providerkubernetes.PodListRequirements{
			EntityID: "environment/dev-use2", Total: requirementID,
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	store := optimization.FileTwinStore{Directory: t.TempDir()}
	executor := optimization.AcquisitionExecutor{
		Reader: reader, Extractor: extractor, Facts: store,
		CollectorIdentity: "collector:kubernetes-test", Now: func() time.Time { return observedAt },
	}
	receipt, err := executor.Execute(context.Background(), []optimization.StateRequirement{{
		ID: requirementID, Plane: optimization.StateObserved, MinimumFidelity: optimization.FidelityExact, MaximumStalenessSeconds: 30,
	}}, []optimization.AcquisitionCall{{
		ID: callID, Source: "kubernetes", Method: http.MethodGet, Endpoint: endpoint, Consistency: "resource-version", CostUnits: 1,
		Observations: []optimization.FieldObservation{{Requirement: requirementID, Plane: optimization.StateObserved, Fidelity: optimization.FidelityExact, MaximumStalenessSeconds: 30}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Complete || len(receipt.FactIDs) != 1 || len(receipt.Calls) != 1 || receipt.Calls[0].Status != optimization.AcquisitionCallComplete {
		t.Fatalf("acquisition receipt = %#v", receipt)
	}
	fact, err := store.GetFact(receipt.FactIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if fact.EntityID != "environment/dev-use2" || fact.Attribute != "kubernetes.pods.total" || string(fact.Value) != "1" {
		t.Fatalf("normalized fact = %#v", fact)
	}
	evidence, _ := json.Marshal(struct {
		Receipt optimization.AcquisitionExecutionReceipt `json:"receipt"`
		Fact    optimization.TemporalFact                `json:"fact"`
	}{receipt, fact})
	if strings.Contains(string(evidence), "must-not-persist") || strings.Contains(string(evidence), "TOKEN") {
		t.Fatalf("raw provider payload crossed the adapter boundary: %s", evidence)
	}
}

package providerkubernetes

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

func TestPodListExtractorProjectsCountsWithoutTenantPayload(t *testing.T) {
	body := []byte(`{
  "kind":"PodList","metadata":{"resourceVersion":"123"},
  "items":[
    {"metadata":{"name":"secret-pod","labels":{"tenant":"must-not-leave"}},"spec":{"containers":[{"env":[{"name":"TOKEN","value":"must-not-leave"}]}]},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}],"containerStatuses":[{"restartCount":2}]}},
    {"metadata":{"name":"pending"},"status":{"phase":"Pending","conditions":[{"type":"Ready","status":"False"}],"containerStatuses":[{"restartCount":1}]}},
    {"metadata":{"name":"failed"},"status":{"phase":"Failed","containerStatuses":[]}}
  ]
}`)
	extractor := PodListExtractor{Requirements: PodListRequirements{
		EntityID: "environment/dev-use2", Total: "pods.total", Ready: "pods.ready", Pending: "pods.pending", Failed: "pods.failed", RestartCount: "pods.restarts",
	}}
	facts, err := extractor.Extract(
		optimization.AcquisitionReadRequest{Source: "kubernetes", Endpoint: "/api/v1/namespaces/dev-use2/pods?limit=500"},
		optimization.AcquisitionReadResponse{ObservedAt: "2026-08-12T12:00:00Z", Body: body},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"pods.total": "3", "pods.ready": "1", "pods.pending": "1", "pods.failed": "1", "pods.restarts": "3"}
	for _, fact := range facts {
		if string(fact.Value) != want[fact.RequirementID] || fact.Plane != optimization.StateObserved || fact.Unit != "count" {
			t.Fatalf("projected fact = %#v", fact)
		}
		encoded, _ := json.Marshal(fact)
		if strings.Contains(string(encoded), "must-not-leave") || strings.Contains(string(encoded), "secret-pod") {
			t.Fatalf("tenant payload crossed projection: %s", encoded)
		}
	}
	if len(facts) != len(want) {
		t.Fatalf("facts = %#v", facts)
	}
}

func TestPodListExtractorRejectsForeignUnversionedAndUnboundedResponses(t *testing.T) {
	extractor := PodListExtractor{Requirements: PodListRequirements{EntityID: "environment/dev", Total: "pods.total"}}
	request := optimization.AcquisitionReadRequest{Source: "kubernetes", Endpoint: "/api/v1/namespaces/dev/pods"}
	response := optimization.AcquisitionReadResponse{ObservedAt: "2026-08-12T12:00:00Z", Body: []byte(`{"kind":"PodList","metadata":{},"items":[]}`)}
	if _, err := extractor.Extract(request, response); err == nil {
		t.Fatal("accepted PodList without resourceVersion")
	}
	request.Source = "foreign"
	if _, err := extractor.Extract(request, response); err == nil {
		t.Fatal("accepted foreign source")
	}
}

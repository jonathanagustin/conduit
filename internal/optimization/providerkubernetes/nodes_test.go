package providerkubernetes

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

func TestNodeListExtractorProjectsAggregateCapacityWithoutNodePayload(t *testing.T) {
	body := []byte(`{"kind":"NodeList","metadata":{"resourceVersion":"17"},"items":[{"metadata":{"name":"node-secret","labels":{"tenant":"must-not-leave"}},"spec":{"unschedulable":false},"status":{"allocatable":{"cpu":"4"},"conditions":[{"type":"Ready","status":"True"}]}},{"metadata":{"name":"cordoned"},"spec":{"unschedulable":true},"status":{"allocatable":{"cpu":"4"},"conditions":[{"type":"Ready","status":"True"}]}}]}`)
	extractor := NodeListExtractor{Requirements: NodeListRequirements{EntityID: "environment/dev", Total: "nodes.total", Ready: "nodes.ready", Schedulable: "nodes.schedulable"}}
	facts, err := extractor.Extract(optimization.AcquisitionReadRequest{Source: "kubernetes", Method: "GET", Endpoint: "/api/v1/nodes?limit=5000"}, optimization.AcquisitionReadResponse{ObservedAt: "2026-08-12T12:00:00Z", SourceRevision: "rv:17", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"nodes.total": "2", "nodes.ready": "2", "nodes.schedulable": "1"}
	for _, fact := range facts {
		if string(fact.Value) != want[fact.RequirementID] || fact.Plane != optimization.StateObserved || fact.Unit != "count" {
			t.Fatalf("node fact = %#v", fact)
		}
		encoded, _ := json.Marshal(fact)
		if strings.Contains(string(encoded), "node-secret") || strings.Contains(string(encoded), "must-not-leave") {
			t.Fatalf("node payload crossed projection: %s", encoded)
		}
	}
}

func TestNodeListExtractorRetainsObservedZeroAsARealFact(t *testing.T) {
	extractor := NodeListExtractor{Requirements: NodeListRequirements{EntityID: "environment/dev", Total: "nodes.total", Ready: "nodes.ready", Schedulable: "nodes.schedulable"}}
	facts, err := extractor.Extract(optimization.AcquisitionReadRequest{Source: "kubernetes", Method: "GET", Endpoint: "/api/v1/nodes?limit=5000"}, optimization.AcquisitionReadResponse{ObservedAt: "2026-08-12T12:00:00Z", SourceRevision: "rv:empty", Body: []byte(`{"kind":"NodeList","metadata":{"resourceVersion":"empty"},"items":[]}`)})
	if err != nil || len(facts) != 3 {
		t.Fatalf("empty NodeList did not produce aggregate facts: %#v, %v", facts, err)
	}
	for _, fact := range facts {
		if string(fact.Value) != "0" {
			t.Fatalf("zero aggregate was not retained: %#v", fact)
		}
	}
}

func TestNodeListExtractorRejectsDuplicateUnversionedOrUnboundedNodes(t *testing.T) {
	extractor := NodeListExtractor{Requirements: NodeListRequirements{EntityID: "environment/dev", Total: "nodes.total"}}
	request := optimization.AcquisitionReadRequest{Source: "kubernetes", Method: "GET", Endpoint: "/api/v1/nodes?limit=5000"}
	for name, body := range map[string]string{
		"missing resource version": `{"kind":"NodeList","metadata":{},"items":[]}`,
		"duplicate":                `{"kind":"NodeList","metadata":{"resourceVersion":"1"},"items":[{"metadata":{"name":"n"},"status":{"allocatable":{}}},{"metadata":{"name":"n"},"status":{"allocatable":{}}}]}`,
		"missing allocatable":      `{"kind":"NodeList","metadata":{"resourceVersion":"1"},"items":[{"metadata":{"name":"n"},"status":{}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := extractor.Extract(request, optimization.AcquisitionReadResponse{ObservedAt: "2026-08-12T12:00:00Z", SourceRevision: "rv:1", Body: []byte(body)}); err == nil {
				t.Fatal("accepted malformed NodeList")
			}
		})
	}
}

func TestNodeListExtractorRejectsMissingObservationProvenance(t *testing.T) {
	extractor := NodeListExtractor{Requirements: NodeListRequirements{EntityID: "environment/dev", Total: "nodes.total"}}
	request := optimization.AcquisitionReadRequest{Source: "kubernetes", Method: "GET", Endpoint: "/api/v1/nodes?limit=5000"}
	body := []byte(`{"kind":"NodeList","metadata":{"resourceVersion":"1"},"items":[]}`)
	for _, response := range []optimization.AcquisitionReadResponse{
		{SourceRevision: "rv:1", Body: body},
		{ObservedAt: "2026-08-12T12:00:00Z", Body: body},
		{ObservedAt: "not-a-time", SourceRevision: "rv:1", Body: body},
	} {
		if _, err := extractor.Extract(request, response); err == nil {
			t.Fatalf("accepted missing observation provenance %#v", response)
		}
	}
}

func TestNodeListExtractorRejectsForeignMethodOrPath(t *testing.T) {
	extractor := NodeListExtractor{Requirements: NodeListRequirements{EntityID: "environment/dev", Total: "nodes.total"}}
	body := []byte(`{"kind":"NodeList","metadata":{"resourceVersion":"1"},"items":[]}`)
	for _, request := range []optimization.AcquisitionReadRequest{
		{Source: "kubernetes", Method: "POST", Endpoint: "/api/v1/nodes?limit=5000"},
		{Source: "kubernetes", Method: "GET", Endpoint: "/api/v1/pods?limit=5000"},
	} {
		if _, err := extractor.Extract(request, optimization.AcquisitionReadResponse{Body: body}); err == nil {
			t.Fatalf("accepted foreign Kubernetes request %#v", request)
		}
	}
}

package providerdispatch

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

type stubReader struct{ source string }

func (reader stubReader) Read(_ context.Context, request optimization.AcquisitionReadRequest) (optimization.AcquisitionReadResponse, error) {
	if request.Source != reader.source {
		return optimization.AcquisitionReadResponse{}, errors.New("foreign source reached reader")
	}
	return optimization.AcquisitionReadResponse{StatusCode: 200, Body: []byte(`{}`)}, nil
}

type stubExtractor struct{ requirement string }

func (extractor stubExtractor) Extract(request optimization.AcquisitionReadRequest, _ optimization.AcquisitionReadResponse) ([]optimization.ExtractedAcquisitionFact, error) {
	return []optimization.ExtractedAcquisitionFact{{RequirementID: extractor.requirement, EntityID: request.ID, Value: json.RawMessage(`0`)}}, nil
}

func TestDispatchesOnlyExactReadAndExtractionBindings(t *testing.T) {
	reader, err := NewReader([]ReadBinding{{Source: "kubernetes", Port: stubReader{source: "kubernetes"}}, {Source: "prometheus", Port: stubReader{source: "prometheus"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Read(context.Background(), optimization.AcquisitionReadRequest{Source: "prometheus"}); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Read(context.Background(), optimization.AcquisitionReadRequest{Source: "unknown"}); err == nil {
		t.Fatal("expected unregistered source to fail closed")
	}

	extractor, err := NewExtractor([]ExtractBinding{{CallID: "pods", Source: "kubernetes", Endpoint: "/api/v1/pods", Extractor: stubExtractor{requirement: "pods.total"}}})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := extractor.Extract(optimization.AcquisitionReadRequest{ID: "pods", Source: "kubernetes", Endpoint: "/api/v1/pods"}, optimization.AcquisitionReadResponse{})
	if err != nil || len(facts) != 1 || facts[0].RequirementID != "pods.total" {
		t.Fatalf("unexpected extraction: %#v, %v", facts, err)
	}
	for _, request := range []optimization.AcquisitionReadRequest{
		{ID: "foreign", Source: "kubernetes", Endpoint: "/api/v1/pods"},
		{ID: "pods", Source: "prometheus", Endpoint: "/api/v1/pods"},
		{ID: "pods", Source: "kubernetes", Endpoint: "/api/v1/nodes"},
	} {
		if _, err := extractor.Extract(request, optimization.AcquisitionReadResponse{}); err == nil {
			t.Fatalf("expected foreign request %#v to fail closed", request)
		}
	}
}

func TestRejectsAmbiguousOrIncompleteBindings(t *testing.T) {
	if _, err := NewReader([]ReadBinding{{Source: "kubernetes", Port: stubReader{source: "kubernetes"}}, {Source: "kubernetes", Port: stubReader{source: "kubernetes"}}}); err == nil {
		t.Fatal("expected duplicate reader source rejection")
	}
	if _, err := NewExtractor([]ExtractBinding{{CallID: "pods", Source: "kubernetes", Endpoint: "/pods", Extractor: stubExtractor{}}, {CallID: "pods", Source: "kubernetes", Endpoint: "/pods", Extractor: stubExtractor{}}}); err == nil {
		t.Fatal("expected duplicate extractor call rejection")
	}
	if _, err := NewReader(nil); err == nil {
		t.Fatal("expected empty reader rejection")
	}
	if _, err := NewExtractor(nil); err == nil {
		t.Fatal("expected empty extractor rejection")
	}
}

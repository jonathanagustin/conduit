package providernetwork

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

func TestFlowExtractorProjectsAggregateTelemetryWithoutNetworkIdentity(t *testing.T) {
	extractor := FlowExtractor{Projection: FlowProjection{CallID: "flows", RequirementID: "network.forwarded", EntityID: "environment/dev", Forwarded: "network.forwarded", Dropped: "network.dropped", Bytes: "network.bytes"}}
	request := optimization.AcquisitionReadRequest{ID: "flows", Source: "hubble", Method: "GET", Endpoint: "/flows?window=1m"}
	response := optimization.AcquisitionReadResponse{SourceRevision: "hubble-sha256:17", ObservedAt: "2026-08-12T12:00:00Z", Body: []byte(`{"schema":"network.flow.v1","revision":"flow-17","window":{"start":"2026-08-12T11:59:00Z","end":"2026-08-12T12:00:00Z"},"flows":[{"flowId":"f1","verdict":"FORWARDED","direction":"INGRESS","bytes":100},{"flowId":"f2","verdict":"DROPPED","direction":"EGRESS","bytes":25}]}`)}
	facts, err := extractor.Extract(request, response)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"network.forwarded": "1", "network.dropped": "1", "network.bytes": "125"}
	for _, fact := range facts {
		if string(fact.Value) != want[fact.RequirementID] || fact.Plane != optimization.StateObserved || fact.Fidelity != optimization.FidelityAggregated {
			t.Fatalf("flow fact = %#v", fact)
		}
		encoded, _ := json.Marshal(fact)
		if strings.Contains(string(encoded), "f1") || strings.Contains(string(encoded), "sourceIp") || strings.Contains(string(encoded), "10.0.0.1") {
			t.Fatalf("network identity crossed projection: %s", encoded)
		}
	}
}

func TestFlowExtractorRejectsIdentityAndMalformedFlows(t *testing.T) {
	extractor := FlowExtractor{Projection: FlowProjection{CallID: "flows", RequirementID: "network.forwarded", EntityID: "environment/dev", Forwarded: "network.forwarded"}}
	request := optimization.AcquisitionReadRequest{ID: "flows", Source: "hubble", Method: "GET", Endpoint: "/flows?window=1m"}
	base := `{"schema":"network.flow.v1","revision":"flow-17","window":{"start":"2026-08-12T11:59:00Z","end":"2026-08-12T12:00:00Z"},"flows":[{"flowId":"f1","verdict":"FORWARDED","direction":"INGRESS","bytes":100}]}`
	for name, body := range map[string]string{
		"duplicate":        strings.Replace(base, `}]}`, `,{"flowId":"f1","verdict":"FORWARDED","direction":"INGRESS","bytes":1}]}`, 1),
		"unknown endpoint": strings.Replace(base, `"bytes":100`, `"bytes":100,"sourceIp":"10.0.0.1"`, 1),
		"bad verdict":      strings.Replace(base, `"FORWARDED"`, `"UNKNOWN"`, 1),
		"bad direction":    strings.Replace(base, `"INGRESS"`, `"SIDEWAYS"`, 1),
		"bad window":       strings.Replace(base, `"end":"2026-08-12T12:00:00Z"`, `"end":"2026-08-12T11:59:00Z"`, 1),
		"trailing":         base + ` {}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := extractor.Extract(request, optimization.AcquisitionReadResponse{SourceRevision: "hubble-sha256:17", Body: []byte(body)}); err == nil {
				t.Fatal("accepted unsafe flow export")
			}
		})
	}
}

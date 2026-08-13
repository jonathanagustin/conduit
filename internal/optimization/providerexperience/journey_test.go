package providerexperience

import (
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

func TestJourneyExtractorProjectsPrivacySafeAggregate(t *testing.T) {
	extractor := JourneyExtractor{Projection: JourneyProjection{CallID: "journeys", RequirementID: "experience.checkout", EntityID: "cohort:pilot", Attribute: "journey.checkout", Journey: "checkout", Cohort: "cohort:pilot"}}
	request := optimization.AcquisitionReadRequest{ID: "journeys", Source: "experience-telemetry", Method: "GET", Endpoint: "/journeys?window=1h"}
	response := optimization.AcquisitionReadResponse{SourceRevision: "telemetry-sha256:17", Body: []byte(`{"schema":"experience.journey.v1","revision":"telemetry-17","window":{"start":"2026-08-12T11:00:00Z","end":"2026-08-12T12:00:00Z"},"events":[{"eventId":"e1","journey":"checkout","cohort":"cohort:pilot","outcome":"SUCCESS","latencyMillis":100},{"eventId":"e2","journey":"checkout","cohort":"cohort:pilot","outcome":"FAILURE","latencyMillis":250},{"eventId":"e3","journey":"checkout","cohort":"cohort:pilot","outcome":"ABANDONED","latencyMillis":50}]}`)}
	facts, err := extractor.Extract(request, response)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].Plane != optimization.StateExperienced || facts[0].Fidelity != optimization.FidelityAggregated || !strings.Contains(string(facts[0].Value), `"eventCount":3`) || !strings.Contains(string(facts[0].Value), `"totalLatencyMillis":400`) {
		t.Fatalf("experience fact = %#v", facts)
	}
}

func TestJourneyExtractorRejectsIdentityAndMalformedTelemetry(t *testing.T) {
	extractor := JourneyExtractor{Projection: JourneyProjection{CallID: "journeys", RequirementID: "experience.checkout", EntityID: "cohort:pilot", Attribute: "journey.checkout", Journey: "checkout", Cohort: "cohort:pilot"}}
	request := optimization.AcquisitionReadRequest{ID: "journeys", Source: "experience-telemetry", Method: "GET", Endpoint: "/journeys?window=1h"}
	base := `{"schema":"experience.journey.v1","revision":"telemetry-17","window":{"start":"2026-08-12T11:00:00Z","end":"2026-08-12T12:00:00Z"},"events":[{"eventId":"e1","journey":"checkout","cohort":"cohort:pilot","outcome":"SUCCESS","latencyMillis":100}]}`
	for name, body := range map[string]string{
		"user identifier": strings.Replace(base, `"latencyMillis":100`, `"latencyMillis":100,"userId":"secret-user"`, 1),
		"duplicate":       strings.Replace(base, `}]}`, `},{"eventId":"e1","journey":"checkout","cohort":"cohort:pilot","outcome":"SUCCESS","latencyMillis":100}]}`, 1),
		"foreign cohort":  strings.Replace(base, `cohort:pilot`, `user:raw`, 1),
		"unknown outcome": strings.Replace(base, `"SUCCESS"`, `"REVENUE"`, 1),
		"trailing":        base + ` {}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := extractor.Extract(request, optimization.AcquisitionReadResponse{SourceRevision: "telemetry-sha256:17", Body: []byte(body)}); err == nil {
				t.Fatal("accepted unsafe experience telemetry")
			}
		})
	}
}

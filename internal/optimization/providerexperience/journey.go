// Package providerexperience projects privacy-safe aggregate journey telemetry
// into the EXPERIENCED plane. It intentionally does not model revenue, profit,
// identity, or raw user events; those require separate consented sources.
package providerexperience

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

const maximumJourneyEvents = 10000

type JourneyProjection struct {
	CallID        string
	RequirementID string
	EntityID      string
	Attribute     string
	Journey       string
	Cohort        string
}

type JourneyExtractor struct{ Projection JourneyProjection }

type journeyEnvelope struct {
	Schema   string         `json:"schema"`
	Revision string         `json:"revision"`
	Window   journeyWindow  `json:"window"`
	Events   []journeyEvent `json:"events"`
}

type journeyWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type journeyEvent struct {
	EventID   string `json:"eventId"`
	Journey   string `json:"journey"`
	Cohort    string `json:"cohort"`
	Outcome   string `json:"outcome"`
	LatencyMS int64  `json:"latencyMillis"`
}

func (extractor JourneyExtractor) Extract(request optimization.AcquisitionReadRequest, response optimization.AcquisitionReadResponse) ([]optimization.ExtractedAcquisitionFact, error) {
	p := extractor.Projection
	if request.ID != p.CallID || request.Source != "experience-telemetry" || request.Method != "GET" || !strings.HasPrefix(request.Endpoint, "/journeys?") {
		return nil, errors.New("experience extractor received a foreign request")
	}
	if p.RequirementID == "" || p.EntityID == "" || p.Attribute == "" || !safeToken(p.Journey) || !safeCohort(p.Cohort) || response.SourceRevision == "" {
		return nil, errors.New("experience projection is incomplete or not privacy-safe")
	}
	decoder := json.NewDecoder(bytes.NewReader(response.Body))
	decoder.DisallowUnknownFields()
	var envelope journeyEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return nil, errors.New("experience response is not a strict journey export")
	}
	if err := decoder.Decode(&struct{}{}); err == nil || !errors.Is(err, io.EOF) {
		return nil, errors.New("experience response contains trailing input")
	}
	if envelope.Schema != "experience.journey.v1" || envelope.Revision == "" || len(envelope.Revision) > 256 || len(envelope.Events) == 0 || len(envelope.Events) > maximumJourneyEvents {
		return nil, errors.New("experience export violates its bounded schema")
	}
	start, err := time.Parse(time.RFC3339Nano, envelope.Window.Start)
	if err != nil {
		return nil, errors.New("experience window start is invalid")
	}
	end, err := time.Parse(time.RFC3339Nano, envelope.Window.End)
	if err != nil || !end.After(start) {
		return nil, errors.New("experience window end is invalid")
	}
	seen := make(map[string]struct{}, len(envelope.Events))
	counts := map[string]int{"SUCCESS": 0, "FAILURE": 0, "ABANDONED": 0}
	var totalLatency int64
	for _, event := range envelope.Events {
		if event.EventID == "" || !safeToken(event.EventID) || event.Journey != p.Journey || event.Cohort != p.Cohort || event.LatencyMS < 0 {
			return nil, errors.New("experience event is outside the declared privacy-safe projection")
		}
		if _, exists := seen[event.EventID]; exists {
			return nil, errors.New("experience export contains duplicate event ids")
		}
		seen[event.EventID] = struct{}{}
		if _, exists := counts[event.Outcome]; !exists {
			return nil, errors.New("experience event outcome is not admitted")
		}
		if totalLatency > int64(^uint64(0)>>1)-event.LatencyMS {
			return nil, errors.New("experience latency total overflows")
		}
		totalLatency += event.LatencyMS
		counts[event.Outcome]++
	}
	value, err := json.Marshal(struct {
		Journey            string `json:"journey"`
		Cohort             string `json:"cohort"`
		WindowStart        string `json:"windowStart"`
		WindowEnd          string `json:"windowEnd"`
		EventCount         int    `json:"eventCount"`
		SuccessCount       int    `json:"successCount"`
		FailureCount       int    `json:"failureCount"`
		AbandonedCount     int    `json:"abandonedCount"`
		TotalLatencyMillis int64  `json:"totalLatencyMillis"`
	}{p.Journey, p.Cohort, start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano), len(envelope.Events), counts["SUCCESS"], counts["FAILURE"], counts["ABANDONED"], totalLatency})
	if err != nil {
		return nil, err
	}
	return []optimization.ExtractedAcquisitionFact{{
		RequirementID: p.RequirementID, EntityID: p.EntityID, Attribute: p.Attribute, Value: value,
		Unit: "journeys", Plane: optimization.StateExperienced, Fidelity: optimization.FidelityAggregated,
		ValidFrom: end.UTC().Format(time.RFC3339Nano), Uncertainty: "privacy-safe aggregate journey telemetry; not a business-value or revenue measure",
	}}, nil
}

func safeToken(value string) bool {
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "\r\n\x00 ,/\\") {
		return false
	}
	return true
}

func safeCohort(value string) bool { return strings.HasPrefix(value, "cohort:") && safeToken(value) }

var _ optimization.AcquisitionFactExtractor = JourneyExtractor{}

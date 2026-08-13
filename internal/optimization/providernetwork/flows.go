// Package providernetwork projects bounded Hubble/Cilium flow telemetry into
// aggregate observed facts. It never owns policy, endpoint identity, or flow
// transport and never persists raw network metadata.
package providernetwork

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

const maximumFlowsPerResponse = 20000

type FlowProjection struct {
	CallID        string
	RequirementID string
	EntityID      string
	Forwarded     string
	Dropped       string
	Bytes         string
}

type FlowExtractor struct{ Projection FlowProjection }

type flowEnvelope struct {
	Schema   string      `json:"schema"`
	Revision string      `json:"revision"`
	Window   flowWindow  `json:"window"`
	Flows    []flowEvent `json:"flows"`
}

type flowWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type flowEvent struct {
	FlowID    string `json:"flowId"`
	Verdict   string `json:"verdict"`
	Direction string `json:"direction"`
	Bytes     int64  `json:"bytes"`
}

func (extractor FlowExtractor) Extract(request optimization.AcquisitionReadRequest, response optimization.AcquisitionReadResponse) ([]optimization.ExtractedAcquisitionFact, error) {
	p := extractor.Projection
	if request.ID != p.CallID || request.Source != "hubble" || request.Method != "GET" || !strings.HasPrefix(request.Endpoint, "/flows?") {
		return nil, errors.New("network flow extractor received a foreign request")
	}
	if p.RequirementID == "" || p.EntityID == "" || response.SourceRevision == "" {
		return nil, errors.New("network flow projection is incomplete")
	}
	decoder := json.NewDecoder(bytes.NewReader(response.Body))
	decoder.DisallowUnknownFields()
	var envelope flowEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return nil, errors.New("network flow response is not a strict bounded export")
	}
	if err := decoder.Decode(&struct{}{}); err == nil || !errors.Is(err, io.EOF) {
		return nil, errors.New("network flow response contains trailing input")
	}
	if envelope.Schema != "network.flow.v1" || envelope.Revision == "" || len(envelope.Revision) > 256 || len(envelope.Flows) == 0 || len(envelope.Flows) > maximumFlowsPerResponse || envelope.Window.Start == "" || envelope.Window.End == "" {
		return nil, errors.New("network flow export violates its bounded schema")
	}
	windowStart, err := time.Parse(time.RFC3339Nano, envelope.Window.Start)
	if err != nil {
		return nil, errors.New("network flow window start is invalid")
	}
	windowEnd, err := time.Parse(time.RFC3339Nano, envelope.Window.End)
	if err != nil || !windowEnd.After(windowStart) {
		return nil, errors.New("network flow window end is invalid")
	}
	seen := make(map[string]struct{}, len(envelope.Flows))
	counts := map[string]int64{"FORWARDED": 0, "DROPPED": 0, "bytes": 0}
	for _, flow := range envelope.Flows {
		if flow.FlowID == "" || !safeToken(flow.FlowID) || flow.Bytes < 0 {
			return nil, errors.New("network flow event is invalid")
		}
		if _, exists := seen[flow.FlowID]; exists {
			return nil, errors.New("network flow export contains duplicate flow ids")
		}
		seen[flow.FlowID] = struct{}{}
		if flow.Direction != "INGRESS" && flow.Direction != "EGRESS" {
			return nil, errors.New("network flow direction is not admitted")
		}
		if flow.Verdict != "FORWARDED" && flow.Verdict != "DROPPED" {
			return nil, errors.New("network flow verdict is not admitted")
		}
		if counts["bytes"] > int64(^uint64(0)>>1)-flow.Bytes {
			return nil, errors.New("network flow bytes overflow")
		}
		counts[flow.Verdict]++
		counts["bytes"] += flow.Bytes
	}
	projections := []struct {
		requirement string
		attribute   string
		value       int64
	}{
		{p.Forwarded, "network.flows.forwarded", counts["FORWARDED"]},
		{p.Dropped, "network.flows.dropped", counts["DROPPED"]},
		{p.Bytes, "network.flows.bytes", counts["bytes"]},
	}
	result := make([]optimization.ExtractedAcquisitionFact, 0, len(projections))
	seenRequirements := map[string]bool{}
	for _, projection := range projections {
		if projection.requirement == "" {
			continue
		}
		if seenRequirements[projection.requirement] {
			return nil, errors.New("network flow projection repeats a requirement")
		}
		seenRequirements[projection.requirement] = true
		value, _ := json.Marshal(projection.value)
		unit := "count"
		if projection.attribute == "network.flows.bytes" {
			unit = "bytes"
		}
		result = append(result, optimization.ExtractedAcquisitionFact{RequirementID: projection.requirement, EntityID: p.EntityID, Attribute: projection.attribute, Value: value, Unit: unit, Plane: optimization.StateObserved, Fidelity: optimization.FidelityAggregated, ValidFrom: windowEnd.UTC().Format(time.RFC3339Nano), Uncertainty: "aggregate flow telemetry; endpoint identities and policy authority are excluded"})
	}
	if len(result) == 0 {
		return nil, errors.New("network flow extractor has no fact projections")
	}
	return result, nil
}

func safeToken(value string) bool {
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "\r\n\x00 ,/\\") {
		return false
	}
	return true
}

var _ optimization.AcquisitionFactExtractor = FlowExtractor{}

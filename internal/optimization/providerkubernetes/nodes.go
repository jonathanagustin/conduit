package providerkubernetes

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

const maximumNodesPerResponse = 5000

type NodeListRequirements struct {
	EntityID    string
	Total       string
	Ready       string
	Schedulable string
	Fidelity    optimization.Fidelity
}

// NodeListExtractor projects aggregate capacity availability only. Node names,
// labels, taints, provider IDs, images, addresses, and allocatable quantities
// never cross the anti-corruption boundary.
type NodeListExtractor struct{ Requirements NodeListRequirements }

func (extractor NodeListExtractor) Extract(request optimization.AcquisitionReadRequest, response optimization.AcquisitionReadResponse) ([]optimization.ExtractedAcquisitionFact, error) {
	c := extractor.Requirements
	if request.Source != "kubernetes" || request.Method != "GET" || !strings.HasPrefix(request.Endpoint, "/api/v1/nodes?") || c.EntityID == "" {
		return nil, errors.New("Kubernetes Node extractor received a foreign request")
	}
	observedAt, err := time.Parse(time.RFC3339Nano, response.ObservedAt)
	if err != nil || response.SourceRevision == "" {
		return nil, errors.New("Kubernetes Node response requires a valid observation time and source revision")
	}
	fidelity := c.Fidelity
	if fidelity == "" {
		fidelity = optimization.FidelityExact
	}
	var payload struct {
		Kind     string `json:"kind"`
		Metadata struct {
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Spec struct {
				Unschedulable bool `json:"unschedulable"`
			} `json:"spec"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
				Allocatable map[string]string `json:"allocatable"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body, &payload); err != nil || payload.Kind != "NodeList" || payload.Metadata.ResourceVersion == "" || len(payload.Items) > maximumNodesPerResponse {
		return nil, errors.New("Kubernetes Node response violates the bounded NodeList contract")
	}
	counts := map[string]int64{"total": int64(len(payload.Items))}
	seen := make(map[string]struct{}, len(payload.Items))
	for _, node := range payload.Items {
		if node.Metadata.Name == "" {
			return nil, errors.New("Kubernetes Node response contains an unnamed node")
		}
		if _, exists := seen[node.Metadata.Name]; exists {
			return nil, errors.New("Kubernetes Node response contains duplicate node names")
		}
		seen[node.Metadata.Name] = struct{}{}
		if node.Status.Allocatable == nil {
			return nil, errors.New("Kubernetes Node response omits allocatable capacity")
		}
		ready := false
		for _, condition := range node.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				ready = true
				break
			}
		}
		if ready {
			counts["ready"]++
		}
		if !node.Spec.Unschedulable {
			counts["schedulable"]++
		}
	}
	projections := []struct {
		requirement string
		attribute   string
		value       int64
	}{
		{c.Total, "kubernetes.nodes.total", counts["total"]},
		{c.Ready, "kubernetes.nodes.ready", counts["ready"]},
		{c.Schedulable, "kubernetes.nodes.schedulable", counts["schedulable"]},
	}
	result := make([]optimization.ExtractedAcquisitionFact, 0, len(projections))
	seenRequirements := map[string]bool{}
	for _, projection := range projections {
		if projection.requirement == "" {
			continue
		}
		if seenRequirements[projection.requirement] {
			return nil, fmt.Errorf("Kubernetes Node projection repeats requirement %q", projection.requirement)
		}
		seenRequirements[projection.requirement] = true
		value, _ := json.Marshal(projection.value)
		result = append(result, optimization.ExtractedAcquisitionFact{RequirementID: projection.requirement, EntityID: c.EntityID, Attribute: projection.attribute, Value: value, Unit: "count", Plane: optimization.StateObserved, Fidelity: fidelity, ValidFrom: observedAt.UTC().Format(time.RFC3339Nano)})
	}
	if len(result) == 0 {
		return nil, errors.New("Kubernetes Node extractor has no fact projections")
	}
	return result, nil
}

var _ optimization.AcquisitionFactExtractor = NodeListExtractor{}

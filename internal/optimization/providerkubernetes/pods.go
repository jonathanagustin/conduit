// Package providerkubernetes contains Kubernetes-specific anti-corruption
// adapters that reduce bounded API responses to normalized optimizer facts.
package providerkubernetes

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

const maximumPodsPerResponse = 10_000

type PodListRequirements struct {
	EntityID     string
	Total        string
	Ready        string
	Pending      string
	Failed       string
	RestartCount string
	Fidelity     optimization.Fidelity
}

// PodListExtractor projects only bounded counts. Pod names, labels,
// annotations, environment values, and container specifications never cross
// the anti-corruption boundary.
type PodListExtractor struct {
	Requirements PodListRequirements
}

func (extractor PodListExtractor) Extract(request optimization.AcquisitionReadRequest, response optimization.AcquisitionReadResponse) ([]optimization.ExtractedAcquisitionFact, error) {
	configuration := extractor.Requirements
	if request.Source != "kubernetes" || !strings.Contains(request.Endpoint, "/pods") || configuration.EntityID == "" {
		return nil, errors.New("Kubernetes Pod extractor received a foreign request")
	}
	fidelity := configuration.Fidelity
	if fidelity == "" {
		fidelity = optimization.FidelityExact
	}
	type condition struct {
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	type containerStatus struct {
		RestartCount int64 `json:"restartCount"`
	}
	var payload struct {
		Kind     string `json:"kind"`
		Metadata struct {
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
		Items []struct {
			Status struct {
				Phase             string            `json:"phase"`
				Conditions        []condition       `json:"conditions"`
				ContainerStatuses []containerStatus `json:"containerStatuses"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body, &payload); err != nil || payload.Kind != "PodList" || payload.Metadata.ResourceVersion == "" || len(payload.Items) > maximumPodsPerResponse {
		return nil, errors.New("Kubernetes Pod response violates the bounded PodList contract")
	}
	counts := map[string]int64{"total": int64(len(payload.Items))}
	for _, item := range payload.Items {
		switch item.Status.Phase {
		case "Pending":
			counts["pending"]++
		case "Failed":
			counts["failed"]++
		}
		for _, condition := range item.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				counts["ready"]++
				break
			}
		}
		for _, status := range item.Status.ContainerStatuses {
			if status.RestartCount < 0 || counts["restarts"] > (1<<63-1)-status.RestartCount {
				return nil, errors.New("Kubernetes Pod restart count exceeds int64")
			}
			counts["restarts"] += status.RestartCount
		}
	}
	projections := []struct {
		requirement string
		attribute   string
		value       int64
	}{
		{configuration.Total, "kubernetes.pods.total", counts["total"]},
		{configuration.Ready, "kubernetes.pods.ready", counts["ready"]},
		{configuration.Pending, "kubernetes.pods.pending", counts["pending"]},
		{configuration.Failed, "kubernetes.pods.failed", counts["failed"]},
		{configuration.RestartCount, "kubernetes.pods.restart-count", counts["restarts"]},
	}
	result := make([]optimization.ExtractedAcquisitionFact, 0, len(projections))
	seen := map[string]bool{}
	for _, projection := range projections {
		if projection.requirement == "" {
			continue
		}
		if seen[projection.requirement] {
			return nil, fmt.Errorf("Kubernetes Pod projection repeats requirement %q", projection.requirement)
		}
		seen[projection.requirement] = true
		value, _ := json.Marshal(projection.value)
		result = append(result, optimization.ExtractedAcquisitionFact{
			RequirementID: projection.requirement, EntityID: configuration.EntityID, Attribute: projection.attribute,
			Value: value, Unit: "count", Plane: optimization.StateObserved, Fidelity: fidelity, ValidFrom: response.ObservedAt,
		})
	}
	if len(result) == 0 {
		return nil, errors.New("Kubernetes Pod extractor has no fact projections")
	}
	return result, nil
}

var _ optimization.AcquisitionFactExtractor = PodListExtractor{}

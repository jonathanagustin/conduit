package domain

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// A health check identifier, restated rather than imported for the same reason
// the rest of this package restates what it needs.
var healthCheckIDPattern = regexp.MustCompile(`^[A-Z][A-Z_]*$`)

// NormalizeEntityHealth resolves a reported Entity's health bottom-up and
// reports the first thing wrong with the report, or nil.
//
// It mutates the details it is given: every container's result is derived from
// its liveness, readiness and checks, every node's from its containers, and the
// nodes are ordered so two reports of the same fleet compare equal. A node with
// no containers is UNKNOWN rather than healthy — nothing was observed, and
// calling that healthy is how an unreported Entity comes to vouch for a Release.
//
// It returns an Issue rather than an error: which error class a malformed
// report belongs to is the caller's vocabulary.
func NormalizeEntityHealth(path string, details *EntityHealthDetails) *Issue {
	seenNodes := map[string]struct{}{}
	for nodeIndex := range details.Nodes {
		node := &details.Nodes[nodeIndex]
		node.Result = HealthHealthy
		nodePath := fmt.Sprintf("%s.nodes[%d]", path, nodeIndex)
		if strings.TrimSpace(node.NodeID) == "" || strings.TrimSpace(node.ProductVersion) == "" {
			return &Issue{Path: nodePath, Message: "nodeId and productVersion are required"}
		}
		if _, duplicate := seenNodes[node.NodeID]; duplicate {
			return &Issue{Path: nodePath + ".nodeId", Message: "must be unique"}
		}
		seenNodes[node.NodeID] = struct{}{}
		seenContainers := map[string]struct{}{}
		for containerIndex := range node.Containers {
			container := &node.Containers[containerIndex]
			containerPath := fmt.Sprintf("%s.containers[%d]", nodePath, containerIndex)
			if strings.TrimSpace(container.ContainerID) == "" {
				return &Issue{Path: containerPath + ".containerId", Message: "is required"}
			}
			if _, duplicate := seenContainers[container.ContainerID]; duplicate {
				return &Issue{Path: containerPath + ".containerId", Message: "must be unique within the node"}
			}
			seenContainers[container.ContainerID] = struct{}{}
			result := HealthHealthy
			if !container.Liveness || !container.Readiness {
				result = HealthError
			}
			seenChecks := map[string]struct{}{}
			for checkIndex := range container.Checks {
				check := &container.Checks[checkIndex]
				checkPath := fmt.Sprintf("%s.checks[%d]", containerPath, checkIndex)
				if !healthCheckIDPattern.MatchString(check.ID) ||
					!contains([]string{HealthHealthy, HealthRepairing, HealthWarning, HealthError, HealthTerminal}, check.Result) {
					return &Issue{Path: checkPath, Message: "id and result must use the health contract"}
				}
				if _, duplicate := seenChecks[check.ID]; duplicate {
					return &Issue{Path: checkPath + ".id", Message: "must be unique within the container"}
				}
				seenChecks[check.ID] = struct{}{}
				if check.Params == nil {
					check.Params = map[string]any{}
				}
				result = WorseHealth(result, check.Result)
			}
			container.Result = result
			node.Result = WorseHealth(node.Result, result)
		}
		if len(node.Containers) == 0 {
			node.Result = HealthUnknown
		}
	}
	sort.Slice(details.Nodes, func(i, j int) bool { return details.Nodes[i].NodeID < details.Nodes[j].NodeID })
	return nil
}

// EntityHealthResult reduces a normalized Entity to one health value, under the
// quorum its details declare.
//
// An Entity with no nodes is UNKNOWN, and a declared-but-unrecognized quorum is
// INDETERMINATE rather than defaulting to all-nodes: a quorum nobody can
// evaluate has not been met, and silently choosing the strictest reading would
// report a fleet as unhealthy on a configuration error.
func EntityHealthResult(details EntityHealthDetails) string {
	if len(details.Nodes) == 0 {
		return HealthUnknown
	}
	healthy, worst := 0, HealthHealthy
	for _, node := range details.Nodes {
		if node.Result == HealthHealthy {
			healthy++
		}
		worst = WorseHealth(worst, node.Result)
	}
	required := len(details.Nodes)
	switch details.Quorum {
	case "ANY", "NO_GUARANTEE":
		required = 1
	case "ALL_BUT_ONE":
		required = len(details.Nodes) - 1
		if required < 1 {
			required = 1
		}
	case "SIMPLE_MAJORITY":
		required = len(details.Nodes)/2 + 1
	case "UNKNOWN":
		return HealthIndeterminate
	}
	if healthy >= required {
		return HealthHealthy
	}
	return worst
}

package domain

import (
	"fmt"
	"strings"
)

// Prompt tells a model which tools exist and which mode it is in.
// The reply must be one JSON object.
func Prompt(mode, message string, chips []Chip) string {
	lines := make([]string, 0, len(chips))
	for _, chip := range chips {
		lines = append(lines, fmt.Sprintf("- %s %s (%s)", chip.ObjectType, chip.ID, one(chip.Label)))
	}
	if len(lines) == 0 {
		lines = append(lines, "- none")
	}
	return strings.TrimSpace(fmt.Sprintf(`Reply with one JSON object and no other text.
The object has text and toolCalls.
toolCalls is an array of at most 3 objects. Each object has name and arguments.
Allowed tools in mode %s: %s.
query_ontology_objects arguments: {"objectType":"Environment","filter":{"field":"id","value":"dev"},"links":[]}
propose_governed_action arguments: {"actionId":"cancel_packaging_run","targetEntityId":"sample-app-1","targetObjectType":"PackagingRun","environment":"dev","installationId":"sample-app","productId":"app.example:sample-app"}
review_change_diff arguments: {"changeRequestId":"crq_example"}
A propose_governed_action call does not apply the action. It asks the server to evaluate the action rules and, when they allow it, to open a change request that still needs a person.
Context:
%s
Operator: %s`, mode, strings.Join(toolsForMode(mode), ", "), strings.Join(lines, "\n"), one(message)))
}

func one(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "not recorded"
	}
	return strings.ReplaceAll(value, "\n", " ")
}

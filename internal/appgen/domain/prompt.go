package domain

import (
	"fmt"
	"strings"
)

// Prompt is the instruction sent to a generator. The response must be one JSON object.
func Prompt(request Request) string {
	return strings.TrimSpace(base(request) + planContext(request) + attachmentContext(request.Attachments))
}

// planContext binds generation to the accepted plan and the conversation that
// reviewed it. Requirements that arrived after the original problem statement
// are in one of those two places.
func planContext(request Request) string {
	if strings.TrimSpace(request.AcceptedPlan) == "" && len(request.Conversation) == 0 {
		return ""
	}
	var out strings.Builder
	fmt.Fprintf(&out, "\nAccepted plan revision %s digest %s. Implement this plan, including requirements added during review.", oneLine(request.PlanRevisionID), oneLine(request.PlanDigest))
	if strings.TrimSpace(request.AcceptedPlan) != "" {
		fmt.Fprintf(&out, "\n--- accepted plan ---\n%s\n--- end accepted plan ---", request.AcceptedPlan)
	}
	if len(request.Conversation) > 0 {
		out.WriteString("\nPlanning conversation:")
		for _, turn := range request.Conversation {
			fmt.Fprintf(&out, "\n%s: %s", oneLine(turn.Role), turn.Text)
		}
	}
	return out.String()
}

// attachmentContext lists the bound files as read-only reference text.
func attachmentContext(items []Attachment) string {
	if len(items) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("\nAttached files are read-only context. Use them to choose the spec. Do not copy them into a package document and do not return a Dockerfile or chart.")
	for _, item := range items {
		fmt.Fprintf(&out, "\n--- attachment %s sha256 %s ---\n%s\n--- end attachment ---", oneLine(item.Name), item.SHA256, item.Content)
	}
	return out.String()
}

func base(request Request) string {
	return strings.TrimSpace(fmt.Sprintf(`Reply with one JSON object and no other text.
The object has name, port, routes, page, resources, and files.
name is %s. port is an integer from 1024 to 65535.
routes is an array of 1 to 8 objects with method, path, status, and body.
One route must be GET /health with status 200.
page has title and body.
resources has memory, such as 64Mi, and cpu, such as 50m.
memory must not exceed 1Gi. cpu must not exceed 1.
files is an array of exactly one object with path, language, and content.
Allowed source shapes, exactly one file: main.go language go, main.py language python, or index.html language html.
Do not send more than one of these files. Each file is at most 4096 bytes.
Do not include a Dockerfile, chart values, a package document, or any other path.
Description: %s`, oneLine(request.Name), oneLine(request.Description)))
}

func oneLine(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "not recorded"
	}
	return strings.ReplaceAll(value, "\n", " ")
}

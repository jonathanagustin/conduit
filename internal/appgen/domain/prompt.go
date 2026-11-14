package domain

import (
	"fmt"
	"strings"
)

// Prompt is the instruction sent to a generator. The response must be one JSON object.
func Prompt(request Request) string {
	return strings.TrimSpace(base(request) + attachmentContext(request.Attachments))
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
files is an array of at most 3 objects with path, language, and content.
Allowed files: main.go language go, main.py language python, index.html language html.
main.go is required. Each file is at most 4096 bytes.
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

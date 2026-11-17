package domain

import (
	"bytes"
	"strings"
)

// replyCandidates returns the JSON objects a model reply may hold: the whole
// reply first when it is one object, then fenced and embedded objects, latest
// first. It is the planner's extractor (internal/planning/adapters/planner),
// which lives in an adapter package this domain cannot import.
func replyCandidates(raw []byte) [][]byte {
	text := string(bytes.TrimSpace(raw))
	var out [][]byte
	seen := map[string]bool{}
	if strings.HasPrefix(text, "{") {
		seen[text] = true
		out = append(out, []byte(text))
	}
	var found [][]byte
	add := func(body string) {
		body = strings.TrimSpace(body)
		if body == "" || seen[body] || !strings.HasPrefix(body, "{") {
			return
		}
		seen[body] = true
		found = append(found, []byte(body))
	}
	fenced := strings.ReplaceAll(text, "```json", "```")
	for _, part := range strings.Split(fenced, "```") {
		add(part)
	}
	for _, object := range balancedObjects([]byte(fenced)) {
		add(string(object))
	}
	// Prefer later objects: models often print a final JSON after logs.
	for i := len(found) - 1; i >= 0; i-- {
		out = append(out, found[i])
	}
	return out
}

// balancedObjects returns each top-level {...} run, skipping braces inside
// JSON strings.
func balancedObjects(raw []byte) [][]byte {
	var out [][]byte
	for i := 0; i < len(raw); i++ {
		if raw[i] != '{' {
			continue
		}
		depth := 0
		inString := false
		escape := false
		for j := i; j < len(raw); j++ {
			c := raw[j]
			if inString {
				if escape {
					escape = false
					continue
				}
				if c == '\\' {
					escape = true
					continue
				}
				if c == '"' {
					inString = false
				}
				continue
			}
			switch c {
			case '"':
				inString = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					out = append(out, raw[i:j+1])
					i = j
					j = len(raw)
				}
			}
		}
	}
	return out
}

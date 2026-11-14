package domain

import "regexp"

var (
	secretAssign = regexp.MustCompile(`(?i)\b(authorization|password|passwd|token|secret|api[_-]?key)\s*[:=]\s*\S+`)
	secretBearer = regexp.MustCompile(`(?i)\b(bearer)\s+\S+`)
)

// RedactLine bounds one log or file line and replaces secret-shaped text.
func RedactLine(line string) (string, bool) {
	redacted := false
	if secretAssign.MatchString(line) || secretBearer.MatchString(line) {
		line = secretAssign.ReplaceAllString(line, "$1=[redacted]")
		line = secretBearer.ReplaceAllString(line, "$1 [redacted]")
		redacted = true
	}
	if len(line) > MaxLogLine {
		line = line[:MaxLogLine] + "…"
		redacted = true
	}
	return line, redacted
}

// RedactText applies RedactLine to each line of a file.
func RedactText(content string) (string, bool) {
	if content == "" {
		return "", false
	}
	redacted := false
	out := make([]byte, 0, len(content))
	start := 0
	for index := 0; index <= len(content); index++ {
		if index != len(content) && content[index] != '\n' {
			continue
		}
		line, changed := RedactLine(content[start:index])
		if changed {
			redacted = true
		}
		out = append(out, line...)
		if index != len(content) {
			out = append(out, '\n')
		}
		start = index + 1
	}
	return string(out), redacted
}

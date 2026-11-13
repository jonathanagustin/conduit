package domain

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type SourceFact struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Path  string `json:"path"`
	Line  int    `json:"line"`
	Stage int    `json:"stage"`
}
type Discovery struct {
	Status    string       `json:"status"`
	Extractor string       `json:"extractor"`
	Facts     []SourceFact `json:"facts"`
	Warnings  []string     `json:"warnings"`
}

var escapeDirective = regexp.MustCompile(`(?im)^\s*#\s*escape\s*=`)
var literalImage = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/:@-]*$`)

// DiscoverDockerfile extracts only literal, single-line facts. It is not a
// Dockerfile interpreter and never converts an observation into readiness.
func DiscoverDockerfile(name, text string) Discovery {
	d := Discovery{Status: "needs-assessment", Extractor: "dockerfile-literals/v1", Facts: []SourceFact{}, Warnings: []string{"Image digests, runtime behavior, identity, licensing and disconnected operation require independent validation."}}
	stage := 0
	continued := false
	if escapeDirective.MatchString(text) || strings.Contains(text, "<<") {
		d.Warnings = append(d.Warnings, "Escape directives or heredocs require a full Dockerfile parser.")
		return d
	}
	add := func(kind, value string, line int) {
		d.Facts = append(d.Facts, SourceFact{Kind: kind, Value: value, Path: name, Line: line, Stage: stage})
	}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if continued {
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			continued = strings.HasSuffix(line, "\\")
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		op := strings.ToUpper(fields[0])
		args := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
		if op == "FROM" {
			stage++
		}
		if strings.HasSuffix(line, "\\") {
			continued = true
			d.Warnings = append(d.Warnings, fmt.Sprintf("%s:%d: continuation requires full parser", name, i+1))
			continue
		}
		switch op {
		case "FROM":
			if len(fields) >= 2 && literalImage.MatchString(fields[1]) {
				add("base-image", fields[1], i+1)
			} else {
				d.Warnings = append(d.Warnings, fmt.Sprintf("%s:%d: unresolved base image", name, i+1))
			}
		case "EXPOSE":
			for _, p := range fields[1:] {
				parts := strings.Split(p, "/")
				n, err := strconv.Atoi(parts[0])
				if err != nil || n < 1 || n > 65535 || len(parts) > 2 || (len(parts) == 2 && parts[1] != "tcp" && parts[1] != "udp") {
					d.Warnings = append(d.Warnings, fmt.Sprintf("%s:%d: unresolved port", name, i+1))
					continue
				}
				add("declared-port", p, i+1)
			}
		case "VOLUME":
			var paths []string
			if err := json.Unmarshal([]byte(args), &paths); err != nil {
				d.Warnings = append(d.Warnings, fmt.Sprintf("%s:%d: volume requires JSON-array literal", name, i+1))
				continue
			}
			for _, p := range paths {
				if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "$\r\n\x00") {
					d.Warnings = append(d.Warnings, fmt.Sprintf("%s:%d: unresolved volume", name, i+1))
					continue
				}
				add("declared-volume", p, i+1)
			}
		case "USER":
			if len(fields) == 2 && !strings.ContainsAny(args, "$\r\n\x00") {
				add("declared-user", args, i+1)
			}
		}
	}
	if stage == 0 {
		d.Warnings = append(d.Warnings, "No literal FROM instruction found.")
	}
	if stage > 1 {
		d.Warnings = append(d.Warnings, "Multiple stages: facts are stage-scoped, not final runtime requirements.")
	}
	return d
}

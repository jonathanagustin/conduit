// Package runfacts reads a packaging run's source lines for the proposal port.
// It does not start a process and it does not score the proposal.
package runfacts

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

// Reader loads sources/<runID>.txt, then source.txt, from a fixture directory.
type Reader struct {
	Dir string
}

func (reader Reader) Load(_ context.Context, runID string) (domain.PackagingContext, error) {
	body, err := read(reader.Dir, runID)
	if err != nil {
		return domain.PackagingContext{}, err
	}
	return Parse(runID, body), nil
}

// Parse copies chart, resource, memory, and image lines into the port input.
// A source that only records a memory limit still supplies a resources excerpt.
func Parse(runID string, body []byte) domain.PackagingContext {
	values := map[string]string{}
	for _, line := range bytes.Split(body, []byte("\n")) {
		text := strings.TrimSpace(string(line))
		key, value, ok := strings.Cut(text, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		if _, seen := values[key]; !seen {
			values[key] = value
		}
	}
	parsed := domain.PackagingContext{
		RunID:         runID,
		ChartValues:   values["chart-values"],
		Resources:     values["resources"],
		MemoryRequest: values["memory-request"],
		MemoryLimit:   values["memory"],
		ImageRef:      values["image-ref"],
		ImageDigest:   values["image-digest"],
	}
	if ref, digest, ok := splitImage(values["image"]); ok {
		if parsed.ImageRef == "" {
			parsed.ImageRef = ref
		}
		if parsed.ImageDigest == "" {
			parsed.ImageDigest = digest
		}
	}
	if parsed.Resources == "" {
		parsed.Resources = resourcesExcerpt(parsed.MemoryRequest, parsed.MemoryLimit)
	}
	if parsed.ChartValues == "" {
		parsed.ChartValues = parsed.Resources
	}
	return parsed
}

func resourcesExcerpt(request, limit string) string {
	var parts []string
	if request != "" {
		parts = append(parts, "requests.memory="+request)
	}
	if limit != "" {
		parts = append(parts, "limits.memory="+limit)
	}
	return strings.Join(parts, " ")
}

func splitImage(value string) (string, string, bool) {
	ref, digest, ok := strings.Cut(value, "@")
	if !ok || ref == "" || !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
		return "", "", false
	}
	for _, r := range digest[len("sha256:"):] {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return "", "", false
		}
	}
	return ref, digest, true
}

func read(dir, runID string) ([]byte, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("packaging fixture directory is not set")
	}
	if runID == "" || runID != filepath.Base(runID) || strings.Contains(runID, "..") || strings.ContainsAny(runID, `/\`) {
		return nil, fmt.Errorf("packaging run id is not a file name")
	}
	named := filepath.Join(dir, "sources", runID+".txt")
	body, err := os.ReadFile(named)
	if err == nil {
		return body, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read packaging source: %w", err)
	}
	body, err = os.ReadFile(filepath.Join(dir, "source.txt"))
	if err != nil {
		return nil, fmt.Errorf("read packaging source: %w", err)
	}
	return body, nil
}

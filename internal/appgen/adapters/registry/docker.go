package registry

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// DockerPublisher tags a local image and pushes it, then uploads the chart.
type DockerPublisher struct {
	Registry string
}

func (publisher DockerPublisher) Push(ctx context.Context, imageID, name string, chart []byte) (string, string, string, error) {
	registry := strings.TrimSpace(publisher.Registry)
	if registry == "" {
		registry = "127.0.0.1:5000"
	}
	ref := registry + "/" + name + ":0.1.0"
	if output, err := exec.CommandContext(ctx, "docker", "tag", imageID, ref).CombinedOutput(); err != nil {
		return "", "", "", fmt.Errorf("tag image: %s", first(output))
	}
	if output, err := exec.CommandContext(ctx, "docker", "push", ref).CombinedOutput(); err != nil {
		return "", "", "", fmt.Errorf("push image: %s", first(output))
	}
	output, err := exec.CommandContext(ctx, "docker", "image", "inspect", ref, "--format", "{{range .RepoDigests}}{{println .}}{{end}}").Output()
	if err != nil {
		return "", "", "", fmt.Errorf("image digest was not recorded")
	}
	digest := ""
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, ref+"@sha256:") || strings.Contains(line, "@sha256:") {
			_, after, ok := strings.Cut(strings.TrimSpace(line), "@")
			if ok && strings.HasPrefix(after, "sha256:") {
				digest = after
				break
			}
		}
	}
	if !strings.HasPrefix(digest, "sha256:") {
		return "", "", "", fmt.Errorf("image digest was not a sha256")
	}
	chartDigest, err := PushChart(ctx, registry, name+"-chart", chart)
	if err != nil {
		return "", "", "", err
	}
	return ref, digest, chartDigest, nil
}

func first(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	line, _, _ := strings.Cut(text, "\n")
	if len(line) > 240 {
		line = line[:240]
	}
	if line == "" {
		return "no output"
	}
	return line
}

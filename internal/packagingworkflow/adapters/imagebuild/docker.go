// Package imagebuild builds and inspects an image with the local container
// engine. The digest is the engine's image id after the build. A caller-supplied
// digest is not accepted.
package imagebuild

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

// Docker builds from a workspace directory and reads the resulting image id.
type Docker struct {
	Bin     string
	Network string
	Tag     string
}

func (builder Docker) binary() string {
	if strings.TrimSpace(builder.Bin) != "" {
		return builder.Bin
	}
	return "docker"
}

// BuildImage runs docker build and returns the tag. The image id is checked
// against the engine before the tag is returned.
func (builder Docker) BuildImage(ctx context.Context, dir string, logOut io.Writer) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("image build directory is required")
	}
	tag := strings.TrimSpace(builder.Tag)
	if tag == "" {
		return "", fmt.Errorf("image build tag is required")
	}
	iid, err := os.CreateTemp("", "conduit-image-id-*")
	if err != nil {
		return "", err
	}
	iidPath := iid.Name()
	_ = iid.Close()
	defer os.Remove(iidPath)
	network := strings.TrimSpace(builder.Network)
	if network == "" {
		network = "none"
	}
	cmd := exec.CommandContext(ctx, builder.binary(), "build", "--network", network, "--tag", tag, "--iidfile", iidPath, dir)
	var output bytes.Buffer
	if logOut != nil {
		cmd.Stdout = io.MultiWriter(&output, logOut)
		cmd.Stderr = io.MultiWriter(&output, logOut)
	} else {
		cmd.Stdout = &output
		cmd.Stderr = &output
	}
	if err := cmd.Run(); err != nil {
		text := strings.TrimSpace(output.String())
		if text == "" {
			text = err.Error()
		}
		return "", fmt.Errorf("image build failed: %s", text)
	}
	raw, err := os.ReadFile(iidPath)
	if err != nil {
		return "", fmt.Errorf("image id was not recorded: %w", err)
	}
	recorded := strings.TrimSpace(string(raw))
	if !digestPattern(recorded) {
		return "", fmt.Errorf("image id was not recorded")
	}
	observed, err := builder.imageID(ctx, tag)
	if err != nil {
		return "", err
	}
	if observed != recorded {
		return "", fmt.Errorf("built image id %s does not match the engine id %s", recorded, observed)
	}
	return tag, nil
}

// InspectImage reads the local image. Digest is the engine image id.
func (builder Docker) InspectImage(ctx context.Context, ref string) (domain.ImageInspect, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return domain.ImageInspect{}, fmt.Errorf("image reference is required")
	}
	cmd := exec.CommandContext(ctx, builder.binary(), "image", "inspect", ref)
	out, err := cmd.Output()
	if err != nil {
		return domain.ImageInspect{}, fmt.Errorf("inspect image: %w", err)
	}
	var rows []struct {
		ID     string `json:"Id"`
		Config struct {
			User         string              `json:"User"`
			ExposedPorts map[string]struct{} `json:"ExposedPorts"`
			Entrypoint   []string            `json:"Entrypoint"`
			Cmd          []string            `json:"Cmd"`
			Env          []string            `json:"Env"`
			Labels       map[string]string   `json:"Labels"`
		} `json:"Config"`
	}
	if err := json.Unmarshal(out, &rows); err != nil || len(rows) != 1 {
		return domain.ImageInspect{}, fmt.Errorf("inspect image: engine returned no image config")
	}
	digest := strings.TrimSpace(rows[0].ID)
	if !digestPattern(digest) {
		return domain.ImageInspect{}, fmt.Errorf("inspect image: engine returned no digest")
	}
	ports := make([]string, 0, len(rows[0].Config.ExposedPorts))
	for port := range rows[0].Config.ExposedPorts {
		ports = append(ports, port)
	}
	return domain.ImageInspect{
		Ref: ref, Digest: digest, User: rows[0].Config.User, ExposedPorts: ports,
		Entrypoint: rows[0].Config.Entrypoint, Cmd: rows[0].Config.Cmd,
		Env: rows[0].Config.Env, Labels: rows[0].Config.Labels,
	}, nil
}

func (builder Docker) imageID(ctx context.Context, ref string) (string, error) {
	cmd := exec.CommandContext(ctx, builder.binary(), "image", "inspect", "--format", "{{.Id}}", ref)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("inspect image: %w", err)
	}
	id := strings.TrimSpace(string(out))
	if !digestPattern(id) {
		return "", fmt.Errorf("inspect image: engine returned no digest")
	}
	return id, nil
}

// ResolvePinned pulls ref and returns the repo digest the engine recorded.
func ResolvePinned(ctx context.Context, bin, ref string) (string, error) {
	if strings.TrimSpace(bin) == "" {
		bin = "docker"
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("base image ref is required")
	}
	pull := exec.CommandContext(ctx, bin, "pull", ref)
	pullOut, err := pull.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("pull base image: %s", strings.TrimSpace(string(pullOut)))
	}
	inspect := exec.CommandContext(ctx, bin, "image", "inspect", "--format", "{{json .RepoDigests}}", ref)
	out, err := inspect.Output()
	if err != nil {
		return "", fmt.Errorf("base image has no repo digest: %w", err)
	}
	var digests []string
	if err := json.Unmarshal(out, &digests); err != nil {
		return "", fmt.Errorf("base image has no repo digest")
	}
	for _, item := range digests {
		name, digest, ok := strings.Cut(item, "@")
		if ok && digestPattern(digest) && name != "" {
			return name + "@" + digest, nil
		}
	}
	return "", fmt.Errorf("base image has no repo digest")
}

func digestPattern(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	for _, r := range value[len("sha256:"):] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

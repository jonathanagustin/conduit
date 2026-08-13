package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

var (
	ErrArtifactCorrupt = errors.New("artifact content is corrupt")
)

// BlobDescriptor is the content identity used at every artifact boundary. It
// intentionally mirrors the required portion of an OCI content descriptor.
// Digest and size describe the raw bytes; MediaType is contextual metadata.
type BlobDescriptor struct {
	MediaType string    `json:"mediaType"`
	Digest    string    `json:"digest"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
}

// ArtifactRepository stores immutable bytes by digest. expectedDigest may be
// empty for server-computed uploads. Implementations must verify it before
// making content visible and must treat a repeated upload as idempotent.
type ArtifactRepository interface {
	PutBlob(context.Context, string, string, io.Reader) (BlobDescriptor, bool, error)
	OpenBlob(context.Context, string) (BlobDescriptor, io.ReadCloser, error)
}

func normalizeMediaType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "application/octet-stream"
	}
	return value
}

// releaseContentDigest commits a Plan to the immutable part of a Product
// Release. Channel membership, scan results, and registry availability are
// mutable control-plane evidence and are deliberately excluded.
func releaseContentDigest(release Release) (string, error) {
	intrinsic := struct {
		Product           string                   `json:"product"`
		Version           string                   `json:"version"`
		Chart             Artifact                 `json:"chart"`
		Images            []Artifact               `json:"images"`
		Dependencies      []Dependency             `json:"dependencies,omitempty"`
		Incompatibilities []Incompatibility        `json:"incompatibilities,omitempty"`
		MaintenanceWindow *WeeklyMaintenanceWindow `json:"maintenanceWindow,omitempty"`
		Labels            map[string]string        `json:"labels,omitempty"`
	}{
		Product: release.Product, Version: release.Version, Chart: release.Chart,
		Images: release.Images, Dependencies: release.Dependencies, Incompatibilities: release.Incompatibilities,
		MaintenanceWindow: release.MaintenanceWindow, Labels: release.Labels,
	}
	contents, err := json.Marshal(intrinsic)
	if err != nil {
		return "", fmt.Errorf("encode Product Release: %w", err)
	}
	sum := sha256.Sum256(contents)
	return sha256Digest(sum), nil
}

func validateExpectedDigest(digest string) error {
	if digest == "" || digestPattern.MatchString(digest) {
		return nil
	}
	return fmt.Errorf("%w: expected digest must be lowercase sha256", ErrDigestMismatch)
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-reader.ctx.Done():
		return 0, reader.ctx.Err()
	default:
		return reader.reader.Read(buffer)
	}
}

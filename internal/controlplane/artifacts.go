package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"

	artifacts "github.com/jonathanagustin/conduit/internal/artifacts/domain"
	artifactports "github.com/jonathanagustin/conduit/internal/artifacts/ports"
)

// Content identity — what a blob is called, how it is labelled, and what a
// caller may claim about it — is Artifacts'. The store that keeps the bytes is
// still implemented here, against Artifacts' port.

// BlobDescriptor is Artifacts' content descriptor, aliased so the several
// hundred call sites here are spelled as they were.
type BlobDescriptor = artifacts.BlobDescriptor

// ArtifactRepository is the legacy spelling of Artifacts' BlobStore port. It is
// an alias, so the repository implementations satisfy the port without knowing
// it exists.
type ArtifactRepository = artifactports.BlobStore

var ErrArtifactCorrupt = artifacts.ErrCorrupt

func normalizeMediaType(value string) string { return artifacts.NormalizeMediaType(value) }

func validateExpectedDigest(digest string) error { return artifacts.ValidateExpectedDigest(digest) }

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

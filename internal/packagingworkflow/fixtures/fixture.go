// Package fixtures is the pinned sample application the localhost proof
// assesses, renders, and lints. It is not a product catalog.
package fixtures

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	_ "embed"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

//go:embed sample-app/source.txt
var Source []byte

const (
	TemplateVersion = "fixture-1"
	ToolVersions    = "lint=fixture-1,render=fixture-1"
	TargetPlanRef   = "env-dev/sample-app"
)

// Materialize writes the pinned fixture into dir. The embedded bytes are the
// repository; dir is the activity workspace.
func Materialize(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "source.txt"), Source, 0o644)
}

func SourceDigest() string {
	return domain.DigestBytes(Source)
}

func Identity(runID string) (domain.RunIdentity, error) {
	return domain.RunIdentity{
		RunID:           runID,
		SourceDigest:    SourceDigest(),
		TemplateVersion: TemplateVersion,
		ToolVersions:    ToolVersions,
		TargetPlanRef:   TargetPlanRef,
	}, nil
}

type Suite struct{}

func (Suite) Assess(_ context.Context, id domain.RunIdentity, dir string) (domain.Receipt, error) {
	body, err := readSource(dir)
	if err != nil {
		return domain.Receipt{}, err
	}
	if err := rejectCredential(body); err != nil {
		return domain.Receipt{}, err
	}
	digest := domain.DigestBytes(body)
	receipt := domain.Receipt{
		Step:         "assess",
		RunID:        id.RunID,
		SourceDigest: digest,
		Outcome:      domain.OutcomePass,
		Reason:       "source hashed",
		Artifact:     domain.ArtifactRef{Digest: digest, ByteSize: len(body), Media: "source"},
	}
	return receipt, receipt.Bound()
}

func (Suite) Propose(_ context.Context, id domain.RunIdentity, dir string) (domain.Receipt, error) {
	body, err := readSource(dir)
	if err != nil {
		return domain.Receipt{}, err
	}
	if err := rejectCredential(body); err != nil {
		return domain.Receipt{}, err
	}
	sourceDigest := domain.DigestBytes(body)
	manifest := Render(sourceDigest, id.TemplateVersion, id.ToolVersions)
	if err := os.MkdirAll(filepath.Join(dir, "out"), 0o755); err != nil {
		return domain.Receipt{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "out", "manifest.txt"), manifest, 0o644); err != nil {
		return domain.Receipt{}, err
	}
	digest := domain.DigestBytes(manifest)
	revision := Revision(sourceDigest, id.TemplateVersion, id.ToolVersions, digest)
	receipt := domain.Receipt{
		Step:             "propose",
		RunID:            id.RunID,
		ProposalRevision: revision,
		SourceDigest:     sourceDigest,
		TargetPlanRef:    id.TargetPlanRef,
		Outcome:          domain.OutcomePass,
		Reason:           "manifest rendered",
		Artifact:         domain.ArtifactRef{Digest: digest, ByteSize: len(manifest), Media: "generated-file"},
	}
	return receipt, receipt.Bound()
}

func (Suite) Validate(_ context.Context, id domain.RunIdentity, dir string, proposal domain.Receipt) (domain.Validation, error) {
	body, err := readSource(dir)
	if err != nil {
		return domain.Validation{}, err
	}
	if err := rejectCredential(body); err != nil {
		return domain.Validation{}, err
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "out", "manifest.txt"))
	if err != nil {
		return domain.Validation{}, err
	}
	validation := Gate(body, manifest, domain.DigestBytes(body), id.TemplateVersion, id.ToolVersions)
	if validation.ArtifactDigest != proposal.Artifact.Digest || validation.ProposalRevision != proposal.ProposalRevision {
		validation.Outcome = domain.OutcomeFailRequired
		validation.Reason = "validation digest does not match the proposal"
	}
	if err := domain.RejectSensitive(validation.Reason); err != nil {
		return domain.Validation{}, err
	}
	return validation, nil
}

// Render is the deterministic generated file. Callers persist it outside
// workflow history and return only its digest.
func Render(sourceDigest, templateVersion, toolVersions string) []byte {
	return []byte(fmt.Sprintf(
		"apiVersion: packaging.conduit.dev/v0\nname: sample-app\nsourceDigest: %s\ntemplateVersion: %s\ntoolVersions: %s\n",
		sourceDigest, templateVersion, toolVersions,
	))
}

func Revision(sourceDigest, templateVersion, toolVersions, manifestDigest string) string {
	sum := sha256.Sum256([]byte(sourceDigest + "\x00" + templateVersion + "\x00" + toolVersions + "\x00" + manifestDigest))
	return "rev-" + hex.EncodeToString(sum[:6])
}

// Gate is the deterministic lint and render check. Unknown security evidence
// and unsupported requirements are terminal failures, not passes and not
// retries.
func Gate(source, manifest []byte, sourceDigest, templateVersion, toolVersions string) domain.Validation {
	want := Render(sourceDigest, templateVersion, toolVersions)
	digest := domain.DigestBytes(manifest)
	validation := domain.Validation{
		ArtifactDigest:   digest,
		ProposalRevision: Revision(sourceDigest, templateVersion, toolVersions, digest),
	}
	switch {
	case bytes.Contains(source, []byte("requirement: unsupported")):
		validation.Outcome = domain.OutcomeUnsupported
		validation.Reason = "application requirement is unsupported"
	case bytes.Contains(source, []byte("security-evidence: unknown")):
		validation.Outcome = domain.OutcomeUnknownSecurity
		validation.Reason = "security evidence is unknown"
	case bytes.Contains(source, []byte("lint: fail")):
		validation.Outcome = domain.OutcomeFailRequired
		validation.Reason = "required lint failed"
	case !bytes.Equal(manifest, want):
		validation.Outcome = domain.OutcomeFailRequired
		validation.Reason = "rendered manifest failed lint"
	default:
		validation.Outcome = domain.OutcomePass
		validation.Reason = "lint and render passed"
	}
	return validation
}

func readSource(dir string) ([]byte, error) {
	if dir == "" {
		return append([]byte(nil), Source...), nil
	}
	body, err := os.ReadFile(filepath.Join(dir, "source.txt"))
	if err != nil {
		return nil, fmt.Errorf("read fixture source: %w", err)
	}
	return body, nil
}

func rejectCredential(body []byte) error {
	if bytes.Contains(body, []byte("BEGIN PRIVATE KEY")) || bytes.Contains(body, []byte("BEGIN CERTIFICATE")) {
		return domain.ErrSensitive
	}
	return nil
}

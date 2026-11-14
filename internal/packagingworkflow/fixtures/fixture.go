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
	"strings"

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
	return identity(runID, SourceDigest()), nil
}

// IdentityFromSource binds a run to the digest of an explicit source. The
// worker reads that source from sources/<runID>.txt when the file is present.
func IdentityFromSource(runID string, source []byte) (domain.RunIdentity, error) {
	if len(source) == 0 {
		return domain.RunIdentity{}, fmt.Errorf("%w: source is empty", domain.ErrIdentity)
	}
	return identity(runID, domain.DigestBytes(source)), nil
}

func identity(runID, sourceDigest string) domain.RunIdentity {
	return domain.RunIdentity{
		RunID:           runID,
		SourceDigest:    sourceDigest,
		TemplateVersion: TemplateVersion,
		ToolVersions:    ToolVersions,
		TargetPlanRef:   TargetPlanRef,
	}
}

func proposalReason(body []byte) string {
	proposal := lineValue(body, "proposal:")
	rationale := lineValue(body, "rationale:")
	if proposal == "" {
		return "uds package rendered"
	}
	if rationale == "" {
		return "proposed " + proposal
	}
	return "proposed " + proposal + ": " + rationale
}

func lineValue(body []byte, prefix string) string {
	for _, line := range bytes.Split(body, []byte("\n")) {
		text := strings.TrimSpace(string(line))
		if strings.HasPrefix(text, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(text, prefix))
		}
	}
	return ""
}

type Suite struct{}

func (Suite) Assess(_ context.Context, id domain.RunIdentity, dir string) (domain.Receipt, error) {
	body, err := readSource(dir, id.RunID)
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
	body, err := readSource(dir, id.RunID)
	if err != nil {
		return domain.Receipt{}, err
	}
	if err := rejectCredential(body); err != nil {
		return domain.Receipt{}, err
	}
	sourceDigest := domain.DigestBytes(body)
	pkg, err := Render(body, sourceDigest, id.TemplateVersion, id.ToolVersions)
	if err != nil {
		return domain.Receipt{}, err
	}
	out, err := renderedDir(dir, id.RunID)
	if err != nil {
		return domain.Receipt{}, err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return domain.Receipt{}, err
	}
	if err := os.WriteFile(filepath.Join(out, "zarf.yaml"), pkg.Zarf, 0o644); err != nil {
		return domain.Receipt{}, err
	}
	if err := os.WriteFile(filepath.Join(out, "uds-package.yaml"), pkg.CR, 0o644); err != nil {
		return domain.Receipt{}, err
	}
	digest := pkg.Digest()
	revision := Revision(sourceDigest, id.TemplateVersion, id.ToolVersions, digest)
	receipt := domain.Receipt{
		Step:             "propose",
		RunID:            id.RunID,
		ProposalRevision: revision,
		SourceDigest:     sourceDigest,
		TargetPlanRef:    id.TargetPlanRef,
		Outcome:          domain.OutcomePass,
		Reason:           proposalReason(body),
		Artifact:         domain.ArtifactRef{Digest: digest, ByteSize: pkg.Size(), Media: "uds-package"},
	}
	return receipt, receipt.Bound()
}

func (Suite) Validate(_ context.Context, id domain.RunIdentity, dir string, proposal domain.Receipt) (domain.Validation, error) {
	body, err := readSource(dir, id.RunID)
	if err != nil {
		return domain.Validation{}, err
	}
	if err := rejectCredential(body); err != nil {
		return domain.Validation{}, err
	}
	out, err := renderedDir(dir, id.RunID)
	if err != nil {
		return domain.Validation{}, err
	}
	zarf, err := os.ReadFile(filepath.Join(out, "zarf.yaml"))
	if err != nil {
		return domain.Validation{}, err
	}
	cr, err := os.ReadFile(filepath.Join(out, "uds-package.yaml"))
	if err != nil {
		return domain.Validation{}, err
	}
	validation := Gate(body, zarf, cr, domain.DigestBytes(body), id.TemplateVersion, id.ToolVersions)
	if validation.ArtifactDigest != proposal.Artifact.Digest || validation.ProposalRevision != proposal.ProposalRevision {
		validation.Outcome = domain.OutcomeFailRequired
		validation.Reason = "validation digest does not match the proposal"
	}
	if err := domain.RejectSensitive(validation.Reason); err != nil {
		return domain.Validation{}, err
	}
	return validation, nil
}

func Revision(sourceDigest, templateVersion, toolVersions, manifestDigest string) string {
	sum := sha256.Sum256([]byte(sourceDigest + "\x00" + templateVersion + "\x00" + toolVersions + "\x00" + manifestDigest))
	return "rev-" + hex.EncodeToString(sum[:6])
}

// Gate is the deterministic lint and render check. Unknown security evidence
// and unsupported requirements are terminal failures, not passes and not
// retries.
func Gate(source, zarf, cr []byte, sourceDigest, templateVersion, toolVersions string) domain.Validation {
	digest := domain.DigestBytes(PackageFiles{Zarf: zarf, CR: cr}.canonical())
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
	case memoryOverCeiling(source, &validation):
	default:
		want, err := Render(source, sourceDigest, templateVersion, toolVersions)
		switch {
		case err != nil:
			validation.Outcome = domain.OutcomeFailRequired
			validation.Reason = "declared needs are invalid"
		case !bytes.Equal(zarf, want.Zarf) || !bytes.Equal(cr, want.CR):
			validation.Outcome = domain.OutcomeFailRequired
			validation.Reason = "rendered package failed check"
		default:
			validation.Outcome = domain.OutcomePass
			validation.Reason = "lint and render passed"
		}
	}
	return validation
}

func memoryOverCeiling(source []byte, validation *domain.Validation) bool {
	value, ok := memoryValue(source)
	if !ok {
		return false
	}
	outcome, reason, blocked := domain.AssessMemory(value)
	if !blocked {
		return false
	}
	validation.Outcome = outcome
	validation.Reason = reason
	return true
}

func memoryValue(source []byte) (string, bool) {
	for _, line := range bytes.Split(source, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, []byte("memory:")) {
			return strings.TrimSpace(string(bytes.TrimPrefix(line, []byte("memory:")))), true
		}
	}
	return "", false
}

func renderedDir(dir, runID string) (string, error) {
	if runID == "" || runID != filepath.Base(runID) || strings.Contains(runID, "..") || strings.ContainsAny(runID, `/\`) {
		return "", fmt.Errorf("run id is not a file name")
	}
	return filepath.Join(dir, "out", runID), nil
}

func readSource(dir, runID string) ([]byte, error) {
	if dir != "" && runID != "" {
		named := filepath.Join(dir, "sources", runID+".txt")
		body, err := os.ReadFile(named)
		if err == nil {
			return body, nil
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read fixture source: %w", err)
		}
	}
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

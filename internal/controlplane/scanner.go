package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maximumScannerOutput = 1 << 20

// ArtifactScanRequest contains only immutable release identity and artifact
// coordinates. Scanner credentials and provider-specific configuration remain
// outside the control plane process.
type ArtifactScanRequest struct {
	APIVersion string     `json:"apiVersion"`
	Kind       string     `json:"kind"`
	Product    string     `json:"product"`
	Version    string     `json:"version"`
	Chart      Artifact   `json:"chart"`
	Images     []Artifact `json:"images"`
}

// Validate checks the provider-neutral scanner boundary without granting the
// scanner access to mutable desired state, credentials, or policy decisions.
func (request ArtifactScanRequest) Validate() error {
	if request.APIVersion != "platform.delivery/v1alpha2" {
		return errors.New("artifact scan request apiVersion must be platform.delivery/v1alpha2")
	}
	if request.Kind != "ArtifactScan" {
		return errors.New("artifact scan request kind must be ArtifactScan")
	}
	return validateArtifactScanRelease(Release{
		Product: request.Product, Version: request.Version,
		Chart: request.Chart, Images: request.Images,
	})
}

type ArtifactScanResult struct {
	VulnerabilityAssessment *VulnerabilityAssessment `json:"vulnerabilityAssessment,omitempty"`
	MalwareAssessment       *MalwareAssessment       `json:"malwareAssessment,omitempty"`
}

type ArtifactScanner interface {
	Scan(context.Context, Release) (ArtifactScanResult, error)
}

// ExternalArtifactScanner delegates scanning to a separately configured,
// least-privileged executable. The executable reads one JSON request on stdin
// and returns one bounded JSON result on stdout. Stderr is discarded to keep
// provider diagnostics and credentials out of Hub logs.
type ExternalArtifactScanner struct {
	Command string
	Timeout time.Duration
}

func NewExternalArtifactScanner(command string, timeout time.Duration) (*ExternalArtifactScanner, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, errors.New("external artifact scanner command is required")
	}
	if !filepath.IsAbs(command) || filepath.Clean(command) != command || strings.ContainsRune(command, '\x00') {
		return nil, errors.New("external artifact scanner command must be a clean absolute path")
	}
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	if timeout < time.Second || timeout > 30*time.Minute {
		return nil, errors.New("external artifact scanner timeout must be between 1 second and 30 minutes")
	}
	return &ExternalArtifactScanner{Command: command, Timeout: timeout}, nil
}

func (scanner *ExternalArtifactScanner) Scan(ctx context.Context, release Release) (ArtifactScanResult, error) {
	if err := validateArtifactScanRelease(release); err != nil {
		return ArtifactScanResult{}, err
	}
	request := ArtifactScanRequest{
		APIVersion: "platform.delivery/v1alpha2", Kind: "ArtifactScan",
		Product: release.Product, Version: release.Version,
		Chart: release.Chart, Images: append([]Artifact(nil), release.Images...),
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return ArtifactScanResult{}, fmt.Errorf("encode scanner request: %w", err)
	}
	commandCtx, cancel := context.WithTimeout(ctx, scanner.Timeout)
	defer cancel()
	process := exec.CommandContext(commandCtx, scanner.Command)
	process.Stdin = bytes.NewReader(payload)
	process.Stderr = io.Discard
	stdout, err := process.StdoutPipe()
	if err != nil {
		return ArtifactScanResult{}, fmt.Errorf("open scanner output: %w", err)
	}
	if err := process.Start(); err != nil {
		return ArtifactScanResult{}, fmt.Errorf("start external artifact scanner: %w", err)
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, maximumScannerOutput+1))
	if len(output) > maximumScannerOutput {
		_ = process.Process.Kill()
		_ = process.Wait()
		return ArtifactScanResult{}, errors.New("external artifact scanner output exceeds 1 MiB")
	}
	waitErr := process.Wait()
	if commandCtx.Err() != nil {
		return ArtifactScanResult{}, fmt.Errorf("external artifact scanner timed out: %w", commandCtx.Err())
	}
	if readErr != nil {
		return ArtifactScanResult{}, fmt.Errorf("read scanner output: %w", readErr)
	}
	if waitErr != nil {
		return ArtifactScanResult{}, fmt.Errorf("external artifact scanner failed: %w", waitErr)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.DisallowUnknownFields()
	var result ArtifactScanResult
	if err := decoder.Decode(&result); err != nil {
		return ArtifactScanResult{}, fmt.Errorf("decode scanner result: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return ArtifactScanResult{}, fmt.Errorf("decode scanner result: %w", err)
	}
	if err := validateArtifactScanResult(release, result); err != nil {
		return ArtifactScanResult{}, err
	}
	return result, nil
}

func validateArtifactScanRelease(release Release) error {
	if !coordinatePattern.MatchString(release.Product) || !versionPattern.MatchString(release.Version) {
		return errors.New("artifact scan requires a valid Product coordinate and orderable version")
	}
	if len(release.Images) == 0 {
		return errors.New("artifact scan requires at least one image")
	}
	var errs []ValidationError
	validateArtifact("release.chart", release.Chart, &errs)
	for index, artifact := range release.Images {
		validateImageArtifact(fmt.Sprintf("release.images[%d]", index), artifact, &errs)
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid artifact scan release: %s: %s", errs[0].Path, errs[0].Message)
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func validateArtifactScanResult(release Release, result ArtifactScanResult) error {
	if result.VulnerabilityAssessment == nil && result.MalwareAssessment == nil {
		return errors.New("scanner result must contain vulnerabilityAssessment or malwareAssessment")
	}
	if assessment := result.VulnerabilityAssessment; assessment != nil {
		var errs []ValidationError
		validateVulnerabilityAssessment("release", Release{
			Chart: release.Chart, Images: release.Images, VulnerabilityAssessment: assessment,
		}, &errs)
		if len(errs) > 0 {
			return fmt.Errorf("invalid vulnerability assessment: %s: %s", errs[0].Path, errs[0].Message)
		}
		if !sameDigestSet(assessment.ArtifactDigests, release.Images) {
			return errors.New("invalid vulnerability assessment: artifactDigests must exactly cover the release images")
		}
	}
	if assessment := result.MalwareAssessment; assessment != nil {
		if strings.TrimSpace(assessment.Provider) == "" || strings.TrimSpace(assessment.RunID) == "" {
			return errors.New("invalid malware assessment: provider and runId are required")
		}
		if assessment.Status != "COMPLETE" || (assessment.Outcome != "PASS" && assessment.Outcome != "FAIL") {
			return errors.New("invalid malware assessment: status must be COMPLETE and outcome must be PASS or FAIL")
		}
		if assessment.Findings < 0 || (assessment.Outcome == "PASS" && assessment.Findings != 0) || (assessment.Outcome == "FAIL" && assessment.Findings == 0) {
			return errors.New("invalid malware assessment: findings must agree with outcome")
		}
		if assessment.AssessedAt.IsZero() {
			return errors.New("invalid malware assessment: assessedAt is required")
		}
		if !sameDigestSet(assessment.ArtifactDigests, releaseArtifacts(release)) {
			return errors.New("invalid malware assessment: artifactDigests must exactly cover the release")
		}
	}
	return nil
}

func sameDigestSet(got []string, artifacts []Artifact) bool {
	wantSet := make(map[string]struct{}, len(artifacts))
	for _, artifact := range artifacts {
		wantSet[artifact.Digest] = struct{}{}
	}
	gotSet := make(map[string]struct{}, len(got))
	for _, digest := range got {
		if !digestPattern.MatchString(digest) {
			return false
		}
		gotSet[digest] = struct{}{}
	}
	if len(gotSet) != len(got) || len(gotSet) != len(wantSet) {
		return false
	}
	want, actual := make([]string, 0, len(wantSet)), make([]string, 0, len(gotSet))
	for digest := range wantSet {
		want = append(want, digest)
	}
	for digest := range gotSet {
		actual = append(actual, digest)
	}
	sort.Strings(want)
	sort.Strings(actual)
	return strings.Join(want, "\x00") == strings.Join(actual, "\x00")
}

func releaseArtifacts(release Release) []Artifact {
	return append([]Artifact{release.Chart}, release.Images...)
}

func enrichRequiredArtifactAssessments(ctx context.Context, scanner ArtifactScanner, state *DesiredState, now time.Time) error {
	policy := state.Environment.ArtifactSecurityPolicy
	if policy == nil {
		return nil
	}
	for index := range state.Releases {
		// A present assessment is left alone unless it has aged past the policy's
		// freshness window, which every policy now has: an unstated bound resolves
		// to the domain default rather than to "unchecked" (#54). Rescanning here
		// -- at proposal, before the plan is built -- is how freshness is enforced
		// without a clock in the pure desired-state validation: the plan carries a
		// fresh assessment, and the generation stays immutable.
		//
		// Bounded gap, recorded rather than hidden: freshness is checked when the
		// upgrade is proposed, not again when a leased plan finally executes, so a
		// plan approved just inside the window can execute later with older
		// evidence. Closing that would mean re-validating at lease time, a larger
		// change than this gate.
		if assessment := state.Releases[index].VulnerabilityAssessment; assessment != nil &&
			!artifactAssessmentStale(assessment, *policy, now) {
			continue
		}
		if scanner == nil {
			return errors.New("artifact security policy requires vulnerability evidence but no scanner is configured")
		}
		result, err := scanner.Scan(ctx, state.Releases[index])
		if err != nil {
			return fmt.Errorf("scan %s:%s: %w", state.Releases[index].Product, state.Releases[index].Version, err)
		}
		if result.VulnerabilityAssessment == nil {
			return fmt.Errorf("scan %s:%s returned no vulnerability assessment", state.Releases[index].Product, state.Releases[index].Version)
		}
		state.Releases[index].VulnerabilityAssessment = result.VulnerabilityAssessment
		if state.Releases[index].MalwareAssessment == nil {
			state.Releases[index].MalwareAssessment = result.MalwareAssessment
		}
	}
	return nil
}

// artifactAssessmentStale reports whether a present assessment is too old for a
// policy's freshness window, which EffectiveMaximumScanAge always supplies. A
// missing or future-dated AssessedAt counts as stale: neither can be trusted as
// recent, and because this triggers a rescan rather than a hard refusal, a clock
// skew that back-dates or forward-dates a scan only costs a fresh scan -- never
// a wrongly blocked upgrade.
func artifactAssessmentStale(assessment *VulnerabilityAssessment, policy ArtifactSecurityPolicy, now time.Time) bool {
	if assessment.AssessedAt.IsZero() {
		return true
	}
	age := now.Sub(assessment.AssessedAt)
	return age < 0 || age > policy.EffectiveMaximumScanAge()
}

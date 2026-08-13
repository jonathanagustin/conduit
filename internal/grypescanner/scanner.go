// Package grypescanner adapts Grype to the Hub's artifact-scanning contract.
// It invokes a pinned Grype binary, bounds what it will read back, and
// normalizes the report into the evidence the control plane records. Deciding
// whether that evidence passes is the control plane's job, not this package's.
package grypescanner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

const maximumReportSize = 32 << 20

type ReportRunner interface {
	Run(context.Context, string) ([]byte, error)
}

type ExecRunner struct {
	Binary     string
	ConfigFile string
}

func NewExecRunner(binary, configFile string) (*ExecRunner, error) {
	binary = strings.TrimSpace(binary)
	if binary == "" || !filepath.IsAbs(binary) || filepath.Clean(binary) != binary || strings.ContainsRune(binary, '\x00') {
		return nil, errors.New("GRYPE_BIN must be a clean absolute path")
	}
	configFile = strings.TrimSpace(configFile)
	if configFile == "" || !filepath.IsAbs(configFile) || filepath.Clean(configFile) != configFile ||
		(strings.ToLower(filepath.Ext(configFile)) != ".yaml" && strings.ToLower(filepath.Ext(configFile)) != ".yml") {
		return nil, errors.New("GRYPE_CONFIG_FILE must be a clean absolute YAML path")
	}
	return &ExecRunner{Binary: binary, ConfigFile: configFile}, nil
}

func (runner *ExecRunner) Run(ctx context.Context, reference string) ([]byte, error) {
	command := exec.CommandContext(ctx, runner.Binary,
		"registry:"+reference, "--config", runner.ConfigFile, "--quiet", "--by-cve", "--output", "json")
	command.Stderr = io.Discard
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open Grype output: %w", err)
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start Grype: %w", err)
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, maximumReportSize+1))
	if len(output) > maximumReportSize {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, errors.New("Grype report exceeds 32 MiB")
	}
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("Grype scan interrupted: %w", ctx.Err())
	}
	if readErr != nil {
		return nil, fmt.Errorf("read Grype output: %w", readErr)
	}
	if waitErr != nil {
		return nil, fmt.Errorf("Grype scan failed: %w", waitErr)
	}
	return output, nil
}

type Scanner struct {
	Runner ReportRunner
	Now    func() time.Time
}

func (scanner Scanner) Scan(ctx context.Context, request controlplane.ArtifactScanRequest) (controlplane.ArtifactScanResult, error) {
	if err := request.Validate(); err != nil {
		return controlplane.ArtifactScanResult{}, err
	}
	if scanner.Runner == nil {
		return controlplane.ArtifactScanResult{}, errors.New("Grype runner is required")
	}
	now := scanner.Now
	if now == nil {
		now = time.Now
	}
	images := append([]controlplane.Artifact(nil), request.Images...)
	sort.Slice(images, func(i, j int) bool {
		left := images[i].Repository + "@" + images[i].Digest
		right := images[j].Repository + "@" + images[j].Digest
		return left < right
	})
	hasher := sha256.New()
	digests := make([]string, 0, len(images))
	findings := make(map[string]controlplane.VulnerabilityFinding)
	provider := ""
	for _, image := range images {
		reference := image.Repository + "@" + image.Digest
		reportBytes, err := scanner.Runner.Run(ctx, reference)
		if err != nil {
			return controlplane.ArtifactScanResult{}, fmt.Errorf("scan %s: %w", reference, err)
		}
		report, err := decodeReport(reportBytes)
		if err != nil {
			return controlplane.ArtifactScanResult{}, fmt.Errorf("decode Grype report for %s: %w", reference, err)
		}
		if len(report.IgnoredMatches) != 0 {
			return controlplane.ArtifactScanResult{}, fmt.Errorf("Grype report for %s contains ignored findings without Platform suppression provenance", reference)
		}
		currentProvider := report.Descriptor.Name + "/" + report.Descriptor.Version
		if provider == "" {
			provider = currentProvider
		} else if provider != currentProvider {
			return controlplane.ArtifactScanResult{}, errors.New("Grype version changed within one artifact scan")
		}
		_, _ = hasher.Write([]byte(reference))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write(reportBytes)
		if !contains(digests, image.Digest) {
			digests = append(digests, image.Digest)
		}
		for _, match := range report.Matches {
			finding := normalizeFinding(match.Vulnerability)
			if finding.ID == "" {
				return controlplane.ArtifactScanResult{}, fmt.Errorf("Grype report for %s contains a finding without an ID", reference)
			}
			if previous, exists := findings[finding.ID]; !exists || findingRank(finding) > findingRank(previous) {
				findings[finding.ID] = finding
			}
		}
	}
	active := make([]controlplane.VulnerabilityFinding, 0, len(findings))
	for _, finding := range findings {
		active = append(active, finding)
	}
	sort.Slice(active, func(i, j int) bool { return active[i].ID < active[j].ID })
	outcome := "PASS"
	if len(active) > 0 {
		outcome = "FAIL"
	}
	return controlplane.ArtifactScanResult{VulnerabilityAssessment: &controlplane.VulnerabilityAssessment{
		Provider: provider, RunID: "sha256:" + hex.EncodeToString(hasher.Sum(nil)),
		Status: "COMPLETE", Outcome: outcome, ArtifactDigests: digests,
		ActiveVulnerabilities: len(active), ActiveFindings: active, AssessedAt: now().UTC(),
	}}, nil
}

type report struct {
	Matches        []match           `json:"matches"`
	IgnoredMatches []json.RawMessage `json:"ignoredMatches,omitempty"`
	Descriptor     struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"descriptor"`
}

type match struct {
	Vulnerability vulnerability `json:"vulnerability"`
}

type vulnerability struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	CVSS     []struct {
		Metrics struct {
			BaseScore float64 `json:"baseScore"`
		} `json:"metrics"`
	} `json:"cvss"`
}

func decodeReport(payload []byte) (report, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	var decoded report
	if err := decoder.Decode(&decoded); err != nil {
		return report{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return report{}, errors.New("multiple JSON values are not allowed")
		}
		return report{}, err
	}
	if decoded.Matches == nil {
		return report{}, errors.New("matches must be an array")
	}
	version := strings.TrimSpace(decoded.Descriptor.Version)
	if decoded.Descriptor.Name != "grype" || version == "" || version != decoded.Descriptor.Version {
		return report{}, errors.New("descriptor must identify an exact Grype version")
	}
	return decoded, nil
}

func normalizeFinding(vulnerability vulnerability) controlplane.VulnerabilityFinding {
	finding := controlplane.VulnerabilityFinding{ID: strings.TrimSpace(vulnerability.ID), Severity: strings.ToUpper(strings.TrimSpace(vulnerability.Severity))}
	for _, score := range vulnerability.CVSS {
		if score.Metrics.BaseScore > finding.CVSS {
			finding.CVSS = score.Metrics.BaseScore
		}
	}
	return finding
}

func findingRank(finding controlplane.VulnerabilityFinding) float64 {
	if finding.CVSS > 0 {
		return finding.CVSS
	}
	switch finding.Severity {
	case "CRITICAL":
		return 9
	case "HIGH":
		return 7
	case "MEDIUM", "MODERATE":
		return 4
	case "LOW":
		return 0.1
	default:
		return 0
	}
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

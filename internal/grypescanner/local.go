package grypescanner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// LocalResult is intentionally not an ArtifactScanResult: an operator's local
// report does not carry the Hub's immutable-artifact assessment authority.
type LocalResult struct {
	Report   map[string]any
	ExitCode int
}

// ScanLocal runs the installed tool without contacting a Hub. A valid report
// survives Grype's policy-failure exit code so callers can print it and fail.
func ScanLocal(ctx context.Context, source string, flags []string, input io.Reader) (LocalResult, error) {
	if strings.TrimSpace(source) == "" || strings.HasPrefix(source, "-") && source != "-" {
		return LocalResult{}, errors.New("a non-option Grype source is required")
	}
	for _, flag := range flags {
		if flag == "--output" || strings.HasPrefix(flag, "--output=") || flag == "--file" || strings.HasPrefix(flag, "--file=") || (strings.HasPrefix(flag, "-o") || strings.HasPrefix(flag, "-f")) && !strings.HasPrefix(flag, "--") {
			return LocalResult{}, errors.New("Grype output flags cannot be passed through")
		}
	}
	binary, err := exec.LookPath("grype")
	if err != nil {
		return LocalResult{}, errors.New("Grype must be installed on PATH")
	}
	arguments := append([]string{source, "--output", "json"}, flags...)
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Stdin = input
	command.Stderr = io.Discard
	stdout, err := command.StdoutPipe()
	if err != nil {
		return LocalResult{}, fmt.Errorf("open Grype report: %w", err)
	}
	if err := command.Start(); err != nil {
		return LocalResult{}, fmt.Errorf("start Grype: %w", err)
	}
	contents, readErr := io.ReadAll(io.LimitReader(stdout, maximumReportSize+1))
	if len(contents) > maximumReportSize {
		_ = command.Process.Kill()
		_ = command.Wait()
		return LocalResult{}, errors.New("Grype report exceeds 32 MiB")
	}
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return LocalResult{}, ctx.Err()
	}
	if readErr != nil {
		return LocalResult{}, fmt.Errorf("read Grype report: %w", readErr)
	}
	exitCode := 0
	if waitErr != nil {
		var failure *exec.ExitError
		if !errors.As(waitErr, &failure) {
			return LocalResult{}, fmt.Errorf("wait for Grype: %w", waitErr)
		}
		exitCode = failure.ExitCode()
	}
	report, err := NormalizeLocalReport(contents)
	if err != nil {
		return LocalResult{}, fmt.Errorf("Grype did not produce a valid report: %w", err)
	}
	return LocalResult{Report: report, ExitCode: exitCode}, nil
}

var localCVE = regexp.MustCompile(`^CVE-[0-9]{4}-[0-9]{4,}$`)

type localScore struct {
	entry map[string]any
	value float64
	major int
	nvd   bool
	id    string
}

// NormalizeLocalReport keeps the Grype envelope and package/fix evidence,
// selecting NVD identifiers and scores without weakening the Hub scanner.
// CONDUIT-INVENTED: grype.LocalNormalizedReport
// Displaced-by: a sanitized Apollo CLI Grype JSON report establishing output schema, normalization tie-breaks and zero-score severity vocabulary.
func NormalizeLocalReport(contents []byte) (map[string]any, error) {
	if len(contents) > maximumReportSize {
		return nil, errors.New("Grype report exceeds 32 MiB")
	}
	if _, err := decodeReport(contents); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var report map[string]any
	if err := decoder.Decode(&report); err != nil {
		return nil, err
	}
	for _, item := range report["matches"].([]any) {
		match, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("Grype match must be an object")
		}
		if err := normalizeLocalMatch(match); err != nil {
			return nil, err
		}
	}
	if ignored, exists := report["ignoredMatches"]; exists && ignored != nil {
		rows, ok := ignored.([]any)
		if !ok {
			return nil, errors.New("ignoredMatches must be an array")
		}
		for _, row := range rows {
			entry, ok := row.(map[string]any)
			if !ok {
				return nil, errors.New("ignored match must be an object")
			}
			match, ok := entry["match"].(map[string]any)
			if !ok {
				return nil, errors.New("ignored match is missing its match")
			}
			if err := normalizeLocalMatch(match); err != nil {
				return nil, err
			}
		}
	}
	report["conduitNativeNormalization"] = map[string]any{"schemaVersion": 1, "valueTier": "CONDUIT_NATIVE", "authoritativeArtifactAssessment": false}
	return report, nil
}

func normalizeLocalMatch(match map[string]any) error {
	primary, ok := match["vulnerability"].(map[string]any)
	if !ok {
		return errors.New("match is missing a vulnerability")
	}
	originalID, _ := primary["id"].(string)
	if strings.TrimSpace(originalID) == "" {
		return errors.New("vulnerability ID is required")
	}
	candidates := []map[string]any{primary}
	if related, exists := match["relatedVulnerabilities"]; exists && related != nil {
		rows, ok := related.([]any)
		if !ok {
			return errors.New("relatedVulnerabilities must be an array")
		}
		for _, row := range rows {
			candidate, ok := row.(map[string]any)
			if !ok {
				return errors.New("related vulnerability must be an object")
			}
			candidates = append(candidates, candidate)
		}
	}
	var nvdIDs []string
	var scores []localScore
	for _, candidate := range candidates {
		id, _ := candidate["id"].(string)
		namespace, _ := candidate["namespace"].(string)
		source, _ := candidate["dataSource"].(string)
		parsed, _ := url.Parse(source)
		nvd := localCVE.MatchString(id) && (strings.HasPrefix(namespace, "nvd:") || parsed != nil && parsed.Scheme == "https" && parsed.Hostname() == "nvd.nist.gov")
		if nvd {
			nvdIDs = append(nvdIDs, id)
		}
		entries, valid := candidate["cvss"].([]any)
		if candidate["cvss"] != nil && !valid {
			return errors.New("CVSS entries must be an array")
		}
		for _, value := range entries {
			entry, ok := value.(map[string]any)
			if !ok {
				return errors.New("CVSS entry must be an object")
			}
			version, _ := entry["version"].(string)
			vector, _ := entry["vector"].(string)
			major := 0
			v3 := strings.HasPrefix(version, "3.") || strings.HasPrefix(vector, "CVSS:3.")
			v2 := strings.HasPrefix(version, "2.") || strings.HasPrefix(vector, "CVSS:2.")
			if v3 && v2 {
				return errors.New("CVSS version conflicts with vector")
			}
			if v3 {
				major = 3
			}
			if v2 {
				major = 2
			}
			if major == 0 {
				continue
			}
			metrics, _ := entry["metrics"].(map[string]any)
			number, ok := metrics["baseScore"].(json.Number)
			if !ok {
				return errors.New("CVSS baseScore must be numeric")
			}
			score, err := number.Float64()
			if err != nil || score < 0 || score > 10 {
				return errors.New("CVSS baseScore must be between zero and ten")
			}
			scores = append(scores, localScore{entry: entry, value: score, major: major, nvd: nvd, id: id})
		}
	}
	sort.Strings(nvdIDs)
	if len(nvdIDs) > 0 {
		primary["id"] = nvdIDs[0]
	}
	sort.SliceStable(scores, func(i, j int) bool {
		left, right := scores[i], scores[j]
		if left.nvd != right.nvd {
			return left.nvd
		}
		if left.major != right.major {
			return left.major > right.major
		}
		if left.value != right.value {
			return left.value > right.value
		}
		return left.id < right.id
	})
	primary["cvss"] = []any{}
	if len(scores) > 0 {
		selected := scores[0]
		if selected.nvd {
			primary["id"] = selected.id
		}
		primary["severity"] = localSeverity(selected.value, selected.major)
		if selected.major == 3 {
			primary["cvss"] = []any{selected.entry}
		}
	}
	match["conduitNativeNormalization"] = map[string]any{"originalVulnerabilityId": originalID}
	return nil
}

func localSeverity(score float64, major int) string {
	switch {
	case major == 3 && score >= 9:
		return "Critical"
	case score >= 7:
		return "High"
	case score >= 4:
		return "Medium"
	case major == 3 && score == 0:
		return "Negligible"
	default:
		return "Low"
	}
}

package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	environments "github.com/jonathanagustin/conduit/internal/environments/domain"
)

type recordingScanner struct {
	result   ArtifactScanResult
	releases []Release
	err      error
}

func (scanner *recordingScanner) Scan(_ context.Context, release Release) (ArtifactScanResult, error) {
	scanner.releases = append(scanner.releases, release)
	return scanner.result, scanner.err
}

func passingScanResult(release Release) ArtifactScanResult {
	digests := []string{}
	for _, image := range release.Images {
		if !contains(digests, image.Digest) {
			digests = append(digests, image.Digest)
		}
	}
	return ArtifactScanResult{
		VulnerabilityAssessment: &VulnerabilityAssessment{
			Provider: "fixture-scanner", RunID: "vulnerability-1", Status: "COMPLETE", Outcome: "PASS",
			ArtifactDigests: digests,
		},
		MalwareAssessment: &MalwareAssessment{
			Provider: "fixture-scanner", RunID: "malware-1", Status: "COMPLETE", Outcome: "PASS",
			ArtifactDigests: artifactDigests(releaseArtifacts(release)), AssessedAt: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC),
		},
	}
}

func artifactDigests(artifacts []Artifact) []string {
	digests := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		if !contains(digests, artifact.Digest) {
			digests = append(digests, artifact.Digest)
		}
	}
	return digests
}

func TestRequiredArtifactEvidenceIsEnrichedBeforePlanning(t *testing.T) {
	state := foundation()
	state.Environment.ArtifactSecurityPolicy = &ArtifactSecurityPolicy{RequiredOutcome: "PASS"}
	scanner := &recordingScanner{result: passingScanResult(state.Releases[0])}
	if err := enrichRequiredArtifactAssessments(context.Background(), scanner, &state, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(scanner.releases) != len(state.Releases) {
		t.Fatalf("scanner calls = %d, want %d", len(scanner.releases), len(state.Releases))
	}
	for _, release := range state.Releases {
		if release.VulnerabilityAssessment == nil || release.MalwareAssessment == nil {
			t.Fatalf("release was not enriched: %#v", release)
		}
	}
	if _, err := BuildPlan(state); err != nil {
		t.Fatalf("enriched state did not satisfy plan policy: %v", err)
	}
}

func TestRequiredArtifactEvidenceFailsClosedWithoutScanner(t *testing.T) {
	state := foundation()
	state.Environment.ArtifactSecurityPolicy = &ArtifactSecurityPolicy{RequiredOutcome: "PASS"}
	if err := enrichRequiredArtifactAssessments(context.Background(), nil, &state, time.Now()); err == nil || !strings.Contains(err.Error(), "no scanner") {
		t.Fatalf("missing scanner error = %v", err)
	}
}

// assessAllReleasesAt gives every Release in the state a complete, passing
// assessment stamped at the given time, so a freshness test isolates the one
// Release it then mutates rather than counting scans of siblings that started
// with no evidence at all.
func assessAllReleasesAt(state *DesiredState, at time.Time) {
	for index := range state.Releases {
		result := passingScanResult(state.Releases[index])
		result.VulnerabilityAssessment.AssessedAt = at
		result.MalwareAssessment.AssessedAt = at
		state.Releases[index].VulnerabilityAssessment = result.VulnerabilityAssessment
		state.Releases[index].MalwareAssessment = result.MalwareAssessment
	}
}

func TestStaleAssessmentIsRescannedBeforePlanning(t *testing.T) {
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	state := foundation()
	// The policy wants evidence no older than a day. Every Release is fresh
	// except the first, which is a week old, complete and passing. Only it must
	// be rescanned.
	state.Environment.ArtifactSecurityPolicy = &ArtifactSecurityPolicy{RequiredOutcome: "PASS", MaximumScanAgeSeconds: 86400}
	assessAllReleasesAt(&state, now.Add(-1*time.Hour))
	state.Releases[0].VulnerabilityAssessment.AssessedAt = now.Add(-7 * 24 * time.Hour)

	scanner := &recordingScanner{result: passingScanResult(state.Releases[0])}
	if err := enrichRequiredArtifactAssessments(context.Background(), scanner, &state, now); err != nil {
		t.Fatal(err)
	}
	if len(scanner.releases) != 1 || scanner.releases[0].Product != state.Releases[0].Product {
		t.Fatalf("stale assessment was not the one rescanned: scanned %d releases", len(scanner.releases))
	}
}

func TestFreshAssessmentIsNotRescanned(t *testing.T) {
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	state := foundation()
	state.Environment.ArtifactSecurityPolicy = &ArtifactSecurityPolicy{RequiredOutcome: "PASS", MaximumScanAgeSeconds: 86400}
	assessAllReleasesAt(&state, now.Add(-1*time.Hour))

	scanner := &recordingScanner{result: passingScanResult(state.Releases[0])}
	if err := enrichRequiredArtifactAssessments(context.Background(), scanner, &state, now); err != nil {
		t.Fatal(err)
	}
	if len(scanner.releases) != 0 {
		t.Fatalf("a fresh assessment was rescanned: scanner calls = %d, want 0", len(scanner.releases))
	}
}

// A policy that names no scan age used to accept evidence of any age at all,
// which is what made freshness an option rather than a floor (#54). It now
// resolves to the domain default, so the same policy rescans.
func TestStaleAssessmentWithoutFreshnessBoundIsRescanned(t *testing.T) {
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	state := foundation()
	state.Environment.ArtifactSecurityPolicy = &ArtifactSecurityPolicy{RequiredOutcome: "PASS"}
	assessAllReleasesAt(&state, now.Add(-365*24*time.Hour))

	scanner := &recordingScanner{result: passingScanResult(state.Releases[0])}
	if err := enrichRequiredArtifactAssessments(context.Background(), scanner, &state, now); err != nil {
		t.Fatal(err)
	}
	if len(scanner.releases) != len(state.Releases) {
		t.Fatalf("a year-old assessment survived the default freshness floor: scanner calls = %d, want %d",
			len(scanner.releases), len(state.Releases))
	}
}

// The floor is a floor and not a schedule: evidence inside the default window
// is still honoured, so the fix does not turn every proposal into a rescan.
func TestFreshAssessmentWithoutFreshnessBoundIsKept(t *testing.T) {
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	state := foundation()
	state.Environment.ArtifactSecurityPolicy = &ArtifactSecurityPolicy{RequiredOutcome: "PASS"}
	assessAllReleasesAt(&state, now.Add(-(environments.DefaultMaximumScanAgeSeconds-3600)*time.Second))

	scanner := &recordingScanner{result: passingScanResult(state.Releases[0])}
	if err := enrichRequiredArtifactAssessments(context.Background(), scanner, &state, now); err != nil {
		t.Fatal(err)
	}
	if len(scanner.releases) != 0 {
		t.Fatalf("evidence inside the default window was rescanned: scanner calls = %d, want 0", len(scanner.releases))
	}
}

// A stated bound wider than the default is still honoured. The defect was a
// policy that said nothing, not a policy that said something permissive: an
// explicit number is visible to whoever reviews the desired state.
func TestStatedFreshnessBoundWiderThanTheDefaultIsHonoured(t *testing.T) {
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	state := foundation()
	state.Environment.ArtifactSecurityPolicy = &ArtifactSecurityPolicy{
		RequiredOutcome: "PASS", MaximumScanAgeSeconds: 30 * 24 * 60 * 60,
	}
	assessAllReleasesAt(&state, now.Add(-20*24*time.Hour))

	scanner := &recordingScanner{result: passingScanResult(state.Releases[0])}
	if err := enrichRequiredArtifactAssessments(context.Background(), scanner, &state, now); err != nil {
		t.Fatal(err)
	}
	if len(scanner.releases) != 0 {
		t.Fatalf("a stated 30-day bound was overridden by the 7-day default: scanner calls = %d, want 0", len(scanner.releases))
	}
}

// The assembled Hub, on the path the issue names. A Hub with no scanner
// configured cannot derive evidence, so old evidence under a bound-less policy
// is refused rather than accepted -- which is the freshness floor being load
// bearing at the HTTP boundary and not only in the helper.
func TestPlanProposalRefusesOldEvidenceUnderAPolicyWithNoStatedScanAge(t *testing.T) {
	state := assessedFoundation()
	state.Environment.ArtifactSecurityPolicy = &ArtifactSecurityPolicy{RequiredOutcome: "PASS"}
	assessAllReleasesAt(&state, time.Now().Add(-365*24*time.Hour))
	payload, _ := json.Marshal(state)

	handler := NewHandler(NewMemoryRepository(), HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/plans", bytes.NewReader(payload)))
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "no scanner") {
		t.Fatalf("year-old evidence accepted: status=%d body=%s", response.Code, response.Body.String())
	}

	// Same request, same policy, with a scanner: the Hub derives the evidence
	// instead of refusing, so the floor costs a scan rather than an outage.
	scanner := &recordingScanner{result: passingScanResult(state.Releases[0])}
	withScanner := NewHandler(NewMemoryRepository(), HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, ArtifactScanner: scanner,
	})
	response = httptest.NewRecorder()
	withScanner.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/plans", bytes.NewReader(payload)))
	if response.Code != http.StatusCreated || len(scanner.releases) != len(state.Releases) {
		t.Fatalf("plan status=%d body=%s scanner calls=%d", response.Code, response.Body.String(), len(scanner.releases))
	}
}

func TestStaleAssessmentFailsClosedWithoutScanner(t *testing.T) {
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	state := foundation()
	state.Environment.ArtifactSecurityPolicy = &ArtifactSecurityPolicy{RequiredOutcome: "PASS", MaximumScanAgeSeconds: 86400}
	assessAllReleasesAt(&state, now.Add(-7*24*time.Hour))

	if err := enrichRequiredArtifactAssessments(context.Background(), nil, &state, now); err == nil || !strings.Contains(err.Error(), "no scanner") {
		t.Fatalf("stale evidence with no scanner should fail closed, got %v", err)
	}
}

func TestArtifactScanEndpointIsAuthenticatedAndReturnsNormalizedEvidence(t *testing.T) {
	release := foundation().Releases[0]
	scanner := &recordingScanner{result: passingScanResult(release)}
	handler := NewHandler(NewMemoryRepository(), HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, ArtifactScanner: scanner,
	})
	payload, _ := json.Marshal(release)
	request := httptest.NewRequest(http.MethodPost, "/v1/artifact-scans", bytes.NewReader(payload))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || len(scanner.releases) != 1 {
		t.Fatalf("scan status=%d body=%s calls=%d", response.Code, response.Body.String(), len(scanner.releases))
	}

	secureHandler := NewHandler(NewMemoryRepository(), HandlerOptions{ArtifactScanner: scanner})
	response = httptest.NewRecorder()
	secureHandler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/artifact-scans", bytes.NewReader(payload)))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated scan status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPlanProposalUsesConfiguredScannerForMissingRequiredEvidence(t *testing.T) {
	state := foundation()
	state.Environment.ArtifactSecurityPolicy = &ArtifactSecurityPolicy{RequiredOutcome: "PASS"}
	scanner := &recordingScanner{result: passingScanResult(state.Releases[0])}
	handler := NewHandler(NewMemoryRepository(), HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, ArtifactScanner: scanner,
	})
	payload, _ := json.Marshal(state)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/plans", bytes.NewReader(payload)))
	if response.Code != http.StatusCreated || len(scanner.releases) != 2 {
		t.Fatalf("plan status=%d body=%s scanner calls=%d", response.Code, response.Body.String(), len(scanner.releases))
	}

	withoutScanner := NewHandler(NewMemoryRepository(), HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	response = httptest.NewRecorder()
	withoutScanner.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/plans", bytes.NewReader(payload)))
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "no scanner") {
		t.Fatalf("missing scanner status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestExternalArtifactScannerRejectsMalformedAndMismatchedResults(t *testing.T) {
	release := foundation().Releases[0]
	for name, output := range map[string]string{
		"malformed":  `{not-json}`,
		"mismatched": `{"vulnerabilityAssessment":{"provider":"scanner","runId":"1","status":"COMPLETE","outcome":"PASS","artifactDigests":["sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"],"activeVulnerabilities":0,"suppressedFindings":0,"gracePeriodFindings":0}}`,
		"unknown":    `{"unexpected":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			command := scannerFixtureCommand(t, output)
			scanner, err := NewExternalArtifactScanner(command, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := scanner.Scan(context.Background(), release); err == nil {
				t.Fatal("invalid scanner output was accepted")
			}
		})
	}
}

func TestVulnerabilityAssessmentCoversImagesButNotChart(t *testing.T) {
	release := foundation().Releases[0]
	release.Chart.Digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	release.Images[0].Digest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	assessment := passingScanResult(release).VulnerabilityAssessment
	if err := validateArtifactScanResult(release, ArtifactScanResult{VulnerabilityAssessment: assessment}); err != nil {
		t.Fatalf("image-only vulnerability evidence rejected: %v", err)
	}
	assessment.ArtifactDigests = append(assessment.ArtifactDigests, release.Chart.Digest)
	if err := validateArtifactScanResult(release, ArtifactScanResult{VulnerabilityAssessment: assessment}); err == nil {
		t.Fatal("chart digest was accepted as container vulnerability evidence")
	}
}

func TestExternalArtifactScannerBoundsOutputWithoutWaitingForTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scanner")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nhead -c 1048577 /dev/zero\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	scanner, err := NewExternalArtifactScanner(path, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := scanner.Scan(context.Background(), foundation().Releases[0]); err == nil || !strings.Contains(err.Error(), "exceeds 1 MiB") {
		t.Fatalf("oversized scanner output error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("oversized output was not terminated promptly: %s", elapsed)
	}
}

func TestSuppressedFindingsRequireActiveApprovedProvenance(t *testing.T) {
	release := foundation().Releases[0]
	assessment := passingScanResult(release).VulnerabilityAssessment
	assessedAt := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	assessment.AssessedAt = assessedAt
	assessment.SuppressedFindings = 1
	assessment.SuppressedFindingDetails = []SuppressedVulnerabilityFinding{{
		FindingID: "scanner-finding-1", VulnerabilityID: "CVE-2026-0001", ArtifactDigest: release.Images[0].Digest,
		Suppression: VulnerabilitySuppressionProvenance{
			ProviderID: "suppression-1", Scope: VulnerabilitySuppressionImage, ScopeValue: "registry.example/example-api",
			Category: VulnerabilitySuppressionPlannedFix, Rationale: "Upgrade tracked in SEC-1234",
			RequestedBy: "product-owner", ApprovedBy: "security-reviewer", ChangeRequestID: "change-1",
			CreatedAt: assessedAt.Add(-2 * time.Hour), ApprovedAt: assessedAt.Add(-time.Hour), ValidUntil: assessedAt.Add(30 * 24 * time.Hour),
		},
	}}
	if err := validateArtifactScanResult(release, ArtifactScanResult{VulnerabilityAssessment: assessment}); err != nil {
		t.Fatalf("valid suppression provenance rejected: %v", err)
	}
	payload, err := json.Marshal(ArtifactScanResult{VulnerabilityAssessment: assessment})
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := NewExternalArtifactScanner(scannerFixtureCommand(t, string(payload)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := scanner.Scan(context.Background(), release)
	if err != nil {
		t.Fatalf("external scanner rejected valid suppression provenance: %v", err)
	}
	if !reflect.DeepEqual(decoded.VulnerabilityAssessment.SuppressedFindingDetails, assessment.SuppressedFindingDetails) {
		t.Fatalf("suppression provenance changed across scanner boundary: %#v", decoded.VulnerabilityAssessment.SuppressedFindingDetails)
	}

	for name, mutate := range map[string]func(*VulnerabilityAssessment){
		"missing details": func(candidate *VulnerabilityAssessment) { candidate.SuppressedFindingDetails = nil },
		"unknown scope": func(candidate *VulnerabilityAssessment) {
			candidate.SuppressedFindingDetails[0].Suppression.Scope = "TENANT"
		},
		"missing scoped value": func(candidate *VulnerabilityAssessment) {
			candidate.SuppressedFindingDetails[0].Suppression.ScopeValue = ""
		},
		"missing approver": func(candidate *VulnerabilityAssessment) {
			candidate.SuppressedFindingDetails[0].Suppression.ApprovedBy = ""
		},
		"expired": func(candidate *VulnerabilityAssessment) {
			candidate.SuppressedFindingDetails[0].Suppression.ValidUntil = assessedAt
		},
		"outside artifact set": func(candidate *VulnerabilityAssessment) {
			candidate.SuppressedFindingDetails[0].ArtifactDigest = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
		},
	} {
		t.Run(name, func(t *testing.T) {
			copyAssessment := *assessment
			copyAssessment.SuppressedFindingDetails = append([]SuppressedVulnerabilityFinding(nil), assessment.SuppressedFindingDetails...)
			mutate(&copyAssessment)
			if err := validateArtifactScanResult(release, ArtifactScanResult{VulnerabilityAssessment: &copyAssessment}); err == nil {
				t.Fatal("invalid suppression provenance was accepted")
			}
		})
	}
}

func scannerFixtureCommand(t *testing.T, output string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scanner")
	quoted := strings.ReplaceAll(output, "'", `'\''`)
	contents := "#!/bin/sh\ncat >/dev/null\nprintf '%s' '" + quoted + "'\n"
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

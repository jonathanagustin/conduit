package grypescanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

type fixtureRunner struct {
	reports map[string][]byte
	refs    []string
	err     error
}

func (runner *fixtureRunner) Run(_ context.Context, reference string) ([]byte, error) {
	runner.refs = append(runner.refs, reference)
	if runner.err != nil {
		return nil, runner.err
	}
	return runner.reports[reference], nil
}

func scanRequest() controlplane.ArtifactScanRequest {
	return controlplane.ArtifactScanRequest{
		APIVersion: "platform.delivery/v1alpha2", Kind: "ArtifactScan",
		Product: "com.example:api", Version: "1.2.3",
		Chart: controlplane.Artifact{
			Repository: "registry.example/charts/api", Tag: "1.2.3",
			Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		Images: []controlplane.Artifact{
			{Repository: "registry.example/images/worker", Tag: "1.2.3", Digest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},
			{Repository: "registry.example/images/api", Tag: "1.2.3", Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		},
	}
}

func TestScannerNormalizesExactImageEvidence(t *testing.T) {
	request := scanRequest()
	apiRef := request.Images[1].Repository + "@" + request.Images[1].Digest
	workerRef := request.Images[0].Repository + "@" + request.Images[0].Digest
	runner := &fixtureRunner{reports: map[string][]byte{
		apiRef:    []byte(`{"matches":[{"vulnerability":{"id":"CVE-2026-0001","severity":"High","cvss":[{"metrics":{"baseScore":7.5}}]}},{"vulnerability":{"id":"CVE-2026-0001","severity":"Critical","cvss":[{"metrics":{"baseScore":9.1}}]}}],"ignoredMatches":[],"descriptor":{"name":"grype","version":"0.116.1"}}`),
		workerRef: []byte(`{"matches":[{"vulnerability":{"id":"CVE-2026-0002","severity":"Medium","cvss":[]}}],"ignoredMatches":[],"descriptor":{"name":"grype","version":"0.116.1"}}`),
	}}
	assessedAt := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	result, err := (Scanner{Runner: runner, Now: func() time.Time { return assessedAt }}).Scan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	wantRefs := []string{apiRef, workerRef}
	if !reflect.DeepEqual(runner.refs, wantRefs) {
		t.Fatalf("references = %#v, want %#v", runner.refs, wantRefs)
	}
	assessment := result.VulnerabilityAssessment
	if assessment == nil || assessment.Provider != "grype/0.116.1" || assessment.Status != "COMPLETE" || assessment.Outcome != "FAIL" ||
		assessment.ActiveVulnerabilities != 2 || assessment.AssessedAt != assessedAt || !strings.HasPrefix(assessment.RunID, "sha256:") {
		t.Fatalf("assessment = %#v", assessment)
	}
	if len(assessment.ArtifactDigests) != 2 || contains(assessment.ArtifactDigests, request.Chart.Digest) {
		t.Fatalf("vulnerability digest coverage = %#v", assessment.ArtifactDigests)
	}
	if got := assessment.ActiveFindings; len(got) != 2 || got[0].ID != "CVE-2026-0001" || got[0].CVSS != 9.1 || got[1].ID != "CVE-2026-0002" {
		t.Fatalf("findings = %#v", got)
	}
	if err := requestResultValid(request, result); err != nil {
		t.Fatalf("normalized result violates control-plane contract: %v", err)
	}
}

func TestScannerPassesEmptyMatchSet(t *testing.T) {
	request := scanRequest()
	reports := map[string][]byte{}
	for _, image := range request.Images {
		reports[image.Repository+"@"+image.Digest] = []byte(`{"matches":[],"ignoredMatches":[],"descriptor":{"name":"grype","version":"0.116.1"}}`)
	}
	result, err := (Scanner{Runner: &fixtureRunner{reports: reports}}).Scan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.VulnerabilityAssessment.Outcome != "PASS" || result.VulnerabilityAssessment.ActiveVulnerabilities != 0 {
		t.Fatalf("assessment = %#v", result.VulnerabilityAssessment)
	}
}

func TestScannerRejectsIgnoredMalformedAndFailedReports(t *testing.T) {
	request := scanRequest()
	firstRef := request.Images[1].Repository + "@" + request.Images[1].Digest
	// Several of these fail for more than one reason once the report is
	// rejected -- an empty report has no descriptor version either -- so the
	// cases that distinguish one refusal from another assert on the message.
	// "did it error" cannot tell a rejected second value from an empty report
	// that failed a later check.
	type rejection struct {
		runner *fixtureRunner
		want   string
	}
	for name, test := range map[string]rejection{
		"ignored":         {runner: &fixtureRunner{reports: map[string][]byte{firstRef: []byte(`{"matches":[],"ignoredMatches":[{"reason":"configured"}],"descriptor":{"name":"grype","version":"0.116.1"}}`)}}},
		"unclean version": {runner: &fixtureRunner{reports: map[string][]byte{firstRef: []byte(`{"matches":[],"ignoredMatches":[],"descriptor":{"name":"grype","version":" 0.116.1"}}`)}}},
		"malformed":       {runner: &fixtureRunner{reports: map[string][]byte{firstRef: []byte(`{"matches":`)}}},
		// Two reports concatenated is a Grype invocation whose output was
		// captured twice, or a wrapper that appended to the file. Acting on
		// the first and discarding the second would scan against evidence
		// nobody can see.
		"two JSON values": {want: "multiple JSON values", runner: &fixtureRunner{reports: map[string][]byte{firstRef: []byte(
			`{"matches":[],"ignoredMatches":[],"descriptor":{"name":"grype","version":"0.116.1"}}` +
				`{"matches":[],"ignoredMatches":[],"descriptor":{"name":"grype","version":"0.116.1"}}`)}}},
		// Trailing bytes that are not a JSON value at all take the other arm of
		// the same check: a decode error rather than a second value.
		"trailing garbage": {runner: &fixtureRunner{reports: map[string][]byte{firstRef: []byte(
			`{"matches":[],"ignoredMatches":[],"descriptor":{"name":"grype","version":"0.116.1"}} tail`)}}},
		"runner": {runner: &fixtureRunner{err: errors.New("database unavailable")}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := (Scanner{Runner: test.runner}).Scan(context.Background(), request)
			if err == nil {
				t.Fatal("invalid report was accepted")
			}
			if test.want != "" && !strings.Contains(err.Error(), test.want) {
				t.Errorf("error %q does not name the reason %q", err, test.want)
			}
		})
	}
}

func TestExecRunnerRequiresExplicitAbsoluteBinaryAndYAMLConfig(t *testing.T) {
	tests := []struct{ name, binary, config string }{
		{"relative binary", "grype", "/scanner/grype.yaml"},
		{"missing config", "/scanner/grype", ""},
		{"non-yaml config", "/scanner/grype", "/scanner/config.json"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := NewExecRunner(testCase.binary, testCase.config); err == nil {
				t.Fatal("unsafe runner configuration was accepted")
			}
		})
	}
	if runner, err := NewExecRunner("/scanner/grype", "/scanner/grype-empty.yaml"); err != nil ||
		runner.Binary != "/scanner/grype" || runner.ConfigFile != "/scanner/grype-empty.yaml" {
		t.Fatalf("valid runner = %#v, %v", runner, err)
	}
}

func requestResultValid(request controlplane.ArtifactScanRequest, result controlplane.ArtifactScanResult) error {
	// Exercise the public request validation here; the external scanner boundary
	// independently validates the returned evidence before accepting it.
	if err := request.Validate(); err != nil {
		return err
	}
	if result.VulnerabilityAssessment == nil || len(result.VulnerabilityAssessment.ArtifactDigests) != len(request.Images) {
		return errors.New("result does not cover request images")
	}
	return nil
}

// findingRank decides which instance of a duplicated CVE survives across image
// reports, so its severity ladder is what the High/Critical gate ultimately
// acts on. Every other test here supplies a CVSS score, which means the ladder
// itself was never exercised: mutation testing kept the branch alive because
// nothing distinguished CVSS ranking from severity ranking.
func TestFindingRankOrdersSeverityWhenNoCVSSScoreIsPublished(t *testing.T) {
	t.Parallel()
	rank := func(severity string, cvss float64) float64 {
		return findingRank(controlplane.VulnerabilityFinding{Severity: severity, CVSS: cvss})
	}

	// A published CVSS score outranks the ladder entirely.
	if rank("LOW", 9.8) != 9.8 {
		t.Fatalf("a published CVSS score must be the rank, got %v", rank("LOW", 9.8))
	}
	// Without one, severity orders strictly, and an unknown severity ranks last.
	ordered := []string{"CRITICAL", "HIGH", "MODERATE", "LOW", "UNRECOGNISED"}
	for index := 1; index < len(ordered); index++ {
		higher, lower := rank(ordered[index-1], 0), rank(ordered[index], 0)
		if higher <= lower {
			t.Errorf("%s ranked %v, not above %s at %v", ordered[index-1], higher, ordered[index], lower)
		}
	}
	// MEDIUM and MODERATE are the same severity spelled two ways.
	if rank("MEDIUM", 0) != rank("MODERATE", 0) {
		t.Errorf("MEDIUM ranked %v but MODERATE ranked %v", rank("MEDIUM", 0), rank("MODERATE", 0))
	}
}

// The rank is only worth anything if the worse duplicate is the one retained.
func TestScannerKeepsTheWorstInstanceOfADuplicatedFinding(t *testing.T) {
	t.Parallel()
	// Neither instance carries a CVSS score, so only the severity ladder can
	// decide which one survives the collapse.
	descriptor := `,"ignoredMatches":[],"descriptor":{"name":"grype","version":"0.116.1"}}`
	low := `{"matches":[{"vulnerability":{"id":"CVE-2026-9999","severity":"Low","cvss":[]}}]` + descriptor
	critical := `{"matches":[{"vulnerability":{"id":"CVE-2026-9999","severity":"Critical","cvss":[]}}]` + descriptor
	request := scanRequest()
	runner := &fixtureRunner{reports: map[string][]byte{
		request.Images[0].Repository + "@" + request.Images[0].Digest: []byte(low),
		request.Images[1].Repository + "@" + request.Images[1].Digest: []byte(critical),
	}}
	result, err := Scanner{Runner: runner, Now: func() time.Time { return time.Unix(0, 0).UTC() }}.
		Scan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	assessment := result.VulnerabilityAssessment
	if assessment == nil || len(assessment.ActiveFindings) != 1 {
		t.Fatalf("duplicate CVE was not collapsed: %#v", assessment)
	}
	if assessment.ActiveFindings[0].Severity != "CRITICAL" {
		t.Fatalf("kept the %s instance of a duplicated CVE, not CRITICAL", assessment.ActiveFindings[0].Severity)
	}
}

// stubGrype writes a script that stands in for the Grype binary. ExecRunner
// requires a clean absolute path, which a temp file satisfies.
func stubGrype(t *testing.T, body string) *ExecRunner {
	t.Helper()
	directory := t.TempDir()
	binary := filepath.Join(directory, "grype")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(directory, "grype.yaml")
	if err := os.WriteFile(config, []byte("db: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner, err := NewExecRunner(binary, config)
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

// ExecRunner is how a scan actually happens: it invokes Grype, bounds what it
// will read back, and distinguishes the ways that can fail. Every one of those
// paths was unreached -- the existing test covers only the constructor's path
// validation.
func TestExecRunnerInvokesGrypeAndBoundsItsOutput(t *testing.T) {
	t.Parallel()

	// Every stub is written here, before the subtests below reach t.Parallel()
	// and start running concurrently.
	//
	// os.WriteFile holds a write descriptor on the stub, and a sibling subtest
	// forking inside that window gives the child a copy of it. exec then fails
	// with ETXTBSY -- "text file busy" -- because the binary is still open for
	// writing somewhere (golang/go#22315). Writing every stub before any fork
	// can happen leaves nothing to race with.
	//
	// This was a 10% flake: 3 failures in 30 runs.
	echoArguments := stubGrype(t, `echo "$@"`)
	writeToStderr := stubGrype(t, `echo "secret-bearing diagnostic" >&2; echo '{"matches":[]}'`)
	exitNonZero := stubGrype(t, `echo '{}'; exit 3`)
	// exec replaces the shell, so cancelling the context kills the process
	// holding stdout. Without exec the shell dies and its child keeps the pipe
	// open, and Wait blocks until the child exits on its own.
	sleepForever := stubGrype(t, `exec sleep 30`)
	// One byte past the bound, which is what the +1 on the LimitReader is
	// there to notice.
	oversizedReport := stubGrype(t, `head -c 33554433 /dev/zero | tr '\0' 'x'`)
	// Exactly the bound, so the refusal is pinned on both sides. Testing only
	// the byte past it leaves > and >= indistinguishable, and a report of
	// exactly 32 MiB would then be rejected for being the right size.
	reportAtTheBound := stubGrype(t, `head -c 33554432 /dev/zero | tr '\0' 'x'`)

	t.Run("passes the pinned arguments and returns stdout", func(t *testing.T) {
		t.Parallel()
		// Echo the arguments back so the invocation itself is asserted rather
		// than assumed: the reference is prefixed registry:, and the output is
		// requested as machine-readable JSON keyed by CVE.
		output, err := echoArguments.Run(context.Background(), "registry.example/app@sha256:abc")
		if err != nil {
			t.Fatal(err)
		}
		arguments := string(output)
		for _, want := range []string{
			"registry:registry.example/app@sha256:abc", "--config", "--quiet", "--by-cve", "--output json",
		} {
			if !strings.Contains(arguments, want) {
				t.Errorf("invocation %q is missing %q", strings.TrimSpace(arguments), want)
			}
		}
	})

	t.Run("stderr does not reach the caller", func(t *testing.T) {
		t.Parallel()
		// Grype is handed registry credentials; anything it writes to stderr is
		// discarded rather than folded into the report or an error.
		output, err := writeToStderr.Run(context.Background(), "registry.example/app")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(output), "secret-bearing") {
			t.Fatalf("stderr leaked into the report: %s", output)
		}
	})

	t.Run("a failing scan is attributed", func(t *testing.T) {
		t.Parallel()
		if _, err := exitNonZero.Run(context.Background(), "registry.example/app"); err == nil ||
			!strings.Contains(err.Error(), "scan failed") {
			t.Fatalf("error = %v, want one naming a failed scan", err)
		}
	})

	t.Run("cancellation is not reported as a scan failure", func(t *testing.T) {
		t.Parallel()
		// A killed process exits non-zero, so without the context check a
		// cancelled scan would be indistinguishable from a Grype error.
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(50 * time.Millisecond); cancel() }()
		_, err := sleepForever.Run(ctx, "registry.example/app")
		if err == nil || !strings.Contains(err.Error(), "interrupted") {
			t.Fatalf("error = %v, want one naming the interruption", err)
		}
	})

	t.Run("an oversized report is refused", func(t *testing.T) {
		t.Parallel()
		_, err := oversizedReport.Run(context.Background(), "registry.example/app")
		if err == nil || !strings.Contains(err.Error(), "32 MiB") {
			t.Fatalf("error = %v, want one naming the size bound", err)
		}
	})

	t.Run("a report of exactly the bound is read", func(t *testing.T) {
		t.Parallel()
		output, err := reportAtTheBound.Run(context.Background(), "registry.example/app")
		if err != nil {
			t.Fatalf("a report of exactly 32 MiB was refused: %v", err)
		}
		if len(output) != maximumReportSize {
			t.Errorf("read %d bytes, want %d", len(output), maximumReportSize)
		}
	})
}

// When one CVE appears twice with the same rank, the first instance is the one
// retained. MEDIUM and MODERATE rank identically, so a report carrying both
// spellings of one finding is the case that distinguishes "strictly better
// replaces" from "equal replaces". Without this the two are interchangeable
// and the recorded severity can flip between runs for no reason anybody could
// see in the input.
func TestScannerKeepsTheFirstOfTwoEquallyRankedDuplicates(t *testing.T) {
	t.Parallel()
	request := scanRequest()
	reports := map[string][]byte{}
	for _, image := range request.Images {
		reports[image.Repository+"@"+image.Digest] = []byte(
			`{"matches":[` +
				`{"vulnerability":{"id":"CVE-2026-0009","severity":"MEDIUM","cvss":[]}},` +
				`{"vulnerability":{"id":"CVE-2026-0009","severity":"MODERATE","cvss":[]}}` +
				`],"ignoredMatches":[],"descriptor":{"name":"grype","version":"0.116.1"}}`)
	}
	result, err := (Scanner{Runner: &fixtureRunner{reports: reports}}).Scan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	findings := result.VulnerabilityAssessment.ActiveFindings
	if len(findings) != 1 {
		t.Fatalf("findings = %#v, want one deduplicated entry", findings)
	}
	if findings[0].Severity != "MEDIUM" {
		t.Errorf("retained severity = %q, want MEDIUM: an equal rank must not displace the first instance",
			findings[0].Severity)
	}
}

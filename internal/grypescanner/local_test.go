package grypescanner

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func localFixture(t *testing.T, primary map[string]any, related ...map[string]any) []byte {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"descriptor": map[string]any{"name": "grype", "version": "0.116.1"}, "matches": []any{map[string]any{"vulnerability": primary, "relatedVulnerabilities": related, "artifact": map[string]any{"name": "example", "version": "1.0.0"}}}})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func localScoreFixture(version string, score float64) map[string]any {
	return map[string]any{"version": version, "metrics": map[string]any{"baseScore": score}}
}

func localPrimary(t *testing.T, contents []byte) (map[string]any, map[string]any) {
	t.Helper()
	report, err := NormalizeLocalReport(contents)
	if err != nil {
		t.Fatal(err)
	}
	return report, report["matches"].([]any)[0].(map[string]any)["vulnerability"].(map[string]any)
}

func TestLocalNormalizationPrefersNVDWithoutDiscardingPackageOrFixEvidence(t *testing.T) {
	t.Parallel()
	primary := map[string]any{"id": "GHSA-example", "severity": "Critical", "cvss": []any{localScoreFixture("3.1", 9.9)}, "fix": map[string]any{"state": "fixed", "versions": []string{"2.0.0"}}}
	nvd := map[string]any{"id": "CVE-2026-1234", "namespace": "nvd:cpe", "cvss": []any{localScoreFixture("2.0", 10), localScoreFixture("3.1", 4)}}
	report, result := localPrimary(t, localFixture(t, primary, nvd))
	if result["id"] != "CVE-2026-1234" || result["severity"] != "Medium" {
		t.Fatalf("NVD selection = %#v", result)
	}
	scores := result["cvss"].([]any)
	if len(scores) != 1 || scores[0].(map[string]any)["version"] != "3.1" {
		t.Fatalf("retained scores = %#v", scores)
	}
	if result["fix"].(map[string]any)["state"] != "fixed" {
		t.Fatal("fix evidence discarded")
	}
	match := report["matches"].([]any)[0].(map[string]any)
	if match["artifact"].(map[string]any)["name"] != "example" || match["conduitNativeNormalization"].(map[string]any)["originalVulnerabilityId"] != "GHSA-example" {
		t.Fatal("package or original identity discarded")
	}
	provenance := report["conduitNativeNormalization"].(map[string]any)
	if provenance["valueTier"] != "CONDUIT_NATIVE" || provenance["authoritativeArtifactAssessment"] != false {
		t.Fatal("local report lacks authority/provenance boundary")
	}
}

func TestLocalNormalizationCVSSFallbackAndExactSeverityBoundaries(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		version  string
		score    float64
		severity string
	}{
		{"3.1", 0, "Negligible"}, {"3.1", 0.1, "Low"}, {"3.1", 3.9, "Low"}, {"3.1", 4, "Medium"}, {"3.1", 6.9, "Medium"}, {"3.1", 7, "High"}, {"3.1", 8.9, "High"}, {"3.1", 9, "Critical"}, {"3.1", 10, "Critical"},
		{"2.0", 0, "Low"}, {"2.0", 3.9, "Low"}, {"2.0", 4, "Medium"}, {"2.0", 7, "High"}, {"2.0", 10, "High"},
	} {
		_, result := localPrimary(t, localFixture(t, map[string]any{"id": "CVE-2026-1234", "severity": "Unknown", "cvss": []any{localScoreFixture(test.version, test.score)}}))
		if result["severity"] != test.severity {
			t.Errorf("CVSS %s/%v = %v, want %s", test.version, test.score, result["severity"], test.severity)
		}
		if test.version == "2.0" && len(result["cvss"].([]any)) != 0 {
			t.Fatal("v2 score was retained")
		}
	}
	_, fallback := localPrimary(t, localFixture(t, map[string]any{"id": "GHSA-fallback", "severity": "Low", "cvss": []any{}}))
	if fallback["severity"] != "Low" || fallback["id"] != "GHSA-fallback" {
		t.Fatal("Grype fallback changed")
	}
}

func TestLocalNormalizationStableTiesAndNVDURL(t *testing.T) {
	t.Parallel()
	primary := map[string]any{"id": "GHSA-tie", "severity": "Unknown"}
	first := map[string]any{"id": "CVE-2026-1234", "dataSource": "https://nvd.nist.gov/vuln/detail/CVE-2026-1234", "cvss": []any{localScoreFixture("3.1", 8)}}
	second := map[string]any{"id": "CVE-2026-5678", "namespace": "nvd:cpe", "cvss": []any{localScoreFixture("3.1", 8)}}
	for _, related := range [][]map[string]any{{first, second}, {second, first}} {
		_, result := localPrimary(t, localFixture(t, primary, related...))
		if result["id"] != "CVE-2026-1234" {
			t.Fatalf("tie depends on report ordering: %#v", result)
		}
	}
}

func TestLocalNormalizationRejectsInvalidReportsAndScores(t *testing.T) {
	t.Parallel()
	for _, contents := range []string{
		`{}`, `null`, `{"matches":null,"descriptor":{"name":"grype","version":"1"}}`,
		`{"matches":[],"descriptor":{"name":"other","version":"1"}}`,
		`{"matches":[],"descriptor":{"name":"grype","version":"1"}} {}`,
		string(localFixture(t, map[string]any{"id": "", "severity": "High"})),
		string(localFixture(t, map[string]any{"id": "CVE-2026-1234", "severity": "High", "cvss": []any{localScoreFixture("3.1", 11)}})),
		string(localFixture(t, map[string]any{"id": "CVE-2026-1234", "severity": "High", "cvss": []any{localScoreFixture("3.1", -1)}})),
		string(localFixture(t, map[string]any{"id": "GHSA-related", "severity": "High"}, map[string]any{"id": "CVE-2026-1234", "cvss": "invalid"})),
	} {
		if _, err := NormalizeLocalReport([]byte(contents)); err == nil {
			t.Errorf("accepted invalid report %s", contents)
		}
	}
	if _, err := NormalizeLocalReport(bytes.Repeat([]byte("x"), maximumReportSize+1)); err == nil || !strings.Contains(err.Error(), "32 MiB") {
		t.Fatal("oversize report accepted")
	}
}

func TestLocalNormalizationIgnoredMatchesPreserveReasonAndUnknownNumbers(t *testing.T) {
	t.Parallel()
	report, err := NormalizeLocalReport([]byte(`{"matches":[],"descriptor":{"name":"grype","version":"1"},"serial":9007199254740993,"ignoredMatches":[{"appliedIgnoreRules":[{"reason":"accepted risk"}],"match":{"vulnerability":{"id":"GHSA-ignored","severity":"High"},"relatedVulnerabilities":[{"id":"CVE-2026-1234","namespace":"nvd:cpe","cvss":[{"version":"3.1","metrics":{"baseScore":9}}]}]}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if report["serial"].(json.Number).String() != "9007199254740993" {
		t.Fatal("unknown number lost precision")
	}
	ignored := report["ignoredMatches"].([]any)[0].(map[string]any)
	if ignored["appliedIgnoreRules"].([]any)[0].(map[string]any)["reason"] != "accepted risk" {
		t.Fatal("ignore reason lost")
	}
	finding := ignored["match"].(map[string]any)["vulnerability"].(map[string]any)
	if finding["id"] != "CVE-2026-1234" || finding["severity"] != "Critical" {
		t.Fatalf("ignored finding not normalized: %#v", finding)
	}
}

func TestLocalNormalizationRejectsConflictingCVSSVersionAndVector(t *testing.T) {
	t.Parallel()
	score := localScoreFixture("3.1", 8)
	score["vector"] = "CVSS:2.0/AV:N"
	if _, err := NormalizeLocalReport(localFixture(t, map[string]any{"id": "GHSA-conflict", "cvss": []any{score}})); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("conflicting score accepted: %v", err)
	}
}

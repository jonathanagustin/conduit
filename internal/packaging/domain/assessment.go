// Package domain defines Conduit-native packaging assessment, not deployment authority.
package domain

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
var topics = []string{"build", "images", "network", "persistence", "identity", "privileges", "offline", "licensing"}

// Evidence identifies a source location; it is not proof that a validation ran.
type Evidence struct {
	Path string `json:"path"`
	Line int    `json:"line"`
}
type Finding struct {
	Topic    string     `json:"topic"`
	Summary  string     `json:"summary"`
	Basis    string     `json:"basis"`
	Evidence []Evidence `json:"evidence"`
}
type Assessment struct {
	Revision   string    `json:"revision"`
	UDSVersion string    `json:"udsVersion"`
	Findings   []Finding `json:"findings"`
}
type Review struct {
	ValueTier  string   `json:"valueTier"`
	Status     string   `json:"status"`
	Revision   string   `json:"revision"`
	UDSVersion string   `json:"udsVersion"`
	Blockers   []string `json:"blockers"`
	NextSteps  []string `json:"nextSteps"`
}

// ReviewAssessment checks the completeness of a supplied assessment. It does
// not inspect source, verify citations, invoke a model, or certify a package.
// CONDUIT-INVENTED: packaging.ReviewAssessment
// Displaced-by: a reviewed replacement packaging-assessment contract.
func ReviewAssessment(a Assessment) (Review, error) {
	r := Review{ValueTier: "CONDUIT_NATIVE", Status: "needs-evidence", Revision: a.Revision, UDSVersion: a.UDSVersion, Blockers: []string{}, NextSteps: []string{}}
	if !commitPattern.MatchString(a.Revision) {
		return r, fmt.Errorf("revision must be a full lowercase Git object ID")
	}
	if strings.TrimSpace(a.UDSVersion) == "" || len(a.UDSVersion) > 80 {
		return r, fmt.Errorf("udsVersion must identify a bounded target version")
	}
	if len(a.Findings) > len(topics) {
		return r, fmt.Errorf("too many findings")
	}
	seen := map[string]Finding{}
	for _, f := range a.Findings {
		valid := false
		for _, topic := range topics {
			if f.Topic == topic {
				valid = true
			}
		}
		if !valid {
			return r, fmt.Errorf("unknown topic %q", f.Topic)
		}
		if _, ok := seen[f.Topic]; ok {
			return r, fmt.Errorf("duplicate topic %q", f.Topic)
		}
		if strings.TrimSpace(f.Summary) == "" || len(f.Summary) > 4096 {
			return r, fmt.Errorf("topic %s needs a bounded summary", f.Topic)
		}
		switch f.Basis {
		case "source-observed", "reference-derived", "inferred", "unknown":
		default:
			return r, fmt.Errorf("invalid evidence basis for %s", f.Topic)
		}
		if len(f.Evidence) > 32 {
			return r, fmt.Errorf("too many citations for %s", f.Topic)
		}
		for _, e := range f.Evidence {
			if e.Line < 1 || len(e.Path) > 512 || e.Path == "" || e.Path == "." || path.IsAbs(e.Path) || path.Clean(e.Path) != e.Path || e.Path == ".." || strings.HasPrefix(e.Path, "../") || strings.ContainsAny(e.Path, "\\:\x00\r\n") {
				return r, fmt.Errorf("invalid source citation for %s", f.Topic)
			}
		}
		if f.Basis == "source-observed" && len(f.Evidence) == 0 {
			return r, fmt.Errorf("source-observed topic %s needs a citation", f.Topic)
		}
		seen[f.Topic] = f
	}
	for _, topic := range topics {
		f, ok := seen[topic]
		if !ok {
			r.Blockers = append(r.Blockers, topic+": missing assessment")
			continue
		}
		if f.Basis != "source-observed" {
			r.Blockers = append(r.Blockers, topic+": requires application-specific source evidence")
		}
	}
	if len(r.Blockers) == 0 {
		r.Status = "reviewable-not-validated"
	}
	r.NextSteps = []string{"Verify every citation against the pinned source revision.", "Assess suitable upstream Helm chart reuse before adding custom adaptation.", "Render a version-pinned UDS Package integration chart and Zarf package in an isolated output workspace.", "Validate schemas, package contents, and workload behavior separately; no deployment is authorized by this review."}
	return r, nil
}

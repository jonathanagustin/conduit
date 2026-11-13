package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const ReviewedPlanContract = "reviewed-packaging/v1"

// SourceDocument is an in-memory read result. The caller owns trusted acquisition.
type SourceDocument struct {
	Path string
	Blob string
	Text string
}
type SourceRecord struct {
	Path   string `json:"path"`
	Blob   string `json:"blob"`
	SHA256 string `json:"sha256"`
}
type RequirementDecision struct {
	Topic       string `json:"topic"`
	Disposition string `json:"disposition"`
	Rationale   string `json:"rationale"`
}
type EvidenceReview struct {
	EvidenceSHA256 string                `json:"evidenceSHA256"`
	Decisions      []RequirementDecision `json:"decisions"`
}
type PlanInput struct {
	Scaffold ScaffoldInput  `json:"scaffold"`
	Review   EvidenceReview `json:"review"`
}
type PackagingPlan struct {
	Contract       string         `json:"contract"`
	ValueTier      string         `json:"valueTier"`
	Status         string         `json:"status"`
	Revision       string         `json:"revision"`
	EvidenceSHA256 string         `json:"evidenceSHA256"`
	Sources        []SourceRecord `json:"sources"`
	Discovery      Discovery      `json:"discovery"`
	Blockers       []string       `json:"blockers"`
	PlanSHA256     string         `json:"planSHA256,omitempty"`
	Artifacts      *Scaffold      `json:"artifacts,omitempty"`
}

// PlanPackaging binds explicit review decisions to the exact selected source set.
// A proposal digest is not a human approval or a grant of execution authority.
// CONDUIT-INVENTED: packaging.PlanPackaging
// Displaced-by: a reviewed production evidence-to-packaging workflow contract.
func PlanPackaging(revision, primary string, docs []SourceDocument, in PlanInput) (PackagingPlan, error) {
	out := PackagingPlan{Contract: ReviewedPlanContract, ValueTier: "CONDUIT_NATIVE", Status: "needs-review", Revision: revision, Sources: []SourceRecord{}, Blockers: []string{}}
	if !commitPattern.MatchString(revision) || len(docs) < 1 || len(docs) > 16 {
		return out, fmt.Errorf("bounded pinned source snapshot required")
	}
	var primaryText, primaryHash string
	seen := map[string]bool{}
	for _, d := range docs {
		if seen[d.Path] || len(d.Text) > 64*1024 || !commitPattern.MatchString(d.Blob) {
			return out, fmt.Errorf("invalid source document")
		}
		seen[d.Path] = true
		h := sha256.Sum256([]byte(d.Text))
		digest := hex.EncodeToString(h[:])
		out.Sources = append(out.Sources, SourceRecord{d.Path, d.Blob, digest})
		if d.Path == primary {
			primaryText = d.Text
			primaryHash = digest
		}
	}
	if primaryHash == "" {
		return out, fmt.Errorf("primary source is missing")
	}
	sort.Slice(out.Sources, func(i, j int) bool { return out.Sources[i].Path < out.Sources[j].Path })
	evidence, _ := json.Marshal(struct {
		Revision string         `json:"revision"`
		Primary  string         `json:"primary"`
		Sources  []SourceRecord `json:"sources"`
	}{revision, primary, out.Sources})
	h := sha256.Sum256(evidence)
	out.EvidenceSHA256 = hex.EncodeToString(h[:])
	out.Discovery = DiscoverDockerfile(primary, primaryText)
	if in.Review.EvidenceSHA256 != out.EvidenceSHA256 {
		out.Blockers = append(out.Blockers, "review: missing or stale evidence digest")
	}
	required := []string{"persistence", "secrets", "startup-downloads", "identity"}
	decisions := map[string]RequirementDecision{}
	for _, d := range in.Review.Decisions {
		known := false
		for _, topic := range required {
			if topic == d.Topic {
				known = true
			}
		}
		if !known || decisions[d.Topic].Topic != "" || len(d.Rationale) > 2048 || strings.TrimSpace(d.Rationale) == "" {
			return out, fmt.Errorf("invalid or duplicate requirement decision")
		}
		if d.Disposition != "none" && d.Disposition != "required" && d.Disposition != "unknown" {
			return out, fmt.Errorf("invalid requirement disposition")
		}
		decisions[d.Topic] = d
	}
	for _, topic := range required {
		d, ok := decisions[topic]
		if !ok {
			out.Blockers = append(out.Blockers, topic+": explicit source review required")
		} else if d.Disposition != "none" {
			out.Blockers = append(out.Blockers, topic+": unsupported or unresolved in internal-stateless/v1")
		}
	}
	if in.Scaffold.SourceRevision != revision || in.Scaffold.SourceSHA256 != primaryHash {
		out.Blockers = append(out.Blockers, "source: scaffold commit/content does not match acquired evidence")
	}
	// Extra parser warnings mean this narrow pathway cannot interpret the source.
	// The first warning always states the validation limitations of discovery.
	if len(out.Discovery.Warnings) > 1 || out.Discovery.FinalStage < 1 {
		out.Blockers = append(out.Blockers, "discovery: unsupported or unresolved Dockerfile syntax")
	}
	portFound := false
	lastUser := ""
	for _, f := range out.Discovery.Facts {
		if f.Stage != out.Discovery.FinalStage {
			continue
		}
		switch f.Kind {
		case "declared-port":
			if f.Value == strconv.Itoa(in.Scaffold.Port) || f.Value == strconv.Itoa(in.Scaffold.Port)+"/tcp" {
				portFound = true
			}
		case "declared-volume":
			out.Blockers = append(out.Blockers, "persistence: runtime VOLUME is outside stateless profile")
		case "declared-user":
			lastUser = f.Value
		}
	}
	if lastUser != "" && lastUser != strconv.Itoa(in.Scaffold.UID) {
		out.Blockers = append(out.Blockers, "privileges: final declared USER does not match proposed numeric UID")
	}
	if lastUser == "" {
		out.Blockers = append(out.Blockers, "privileges: final-stage literal numeric USER evidence required")
	}
	if !portFound {
		out.Blockers = append(out.Blockers, "network: proposed HTTP port lacks final-stage literal TCP evidence")
	}
	if len(out.Blockers) > 0 {
		return out, nil
	}
	artifacts, err := GenerateScaffold(in.Scaffold)
	if err != nil {
		return out, err
	}
	// Canonicalize decision ordering so input order cannot alter proposal identity.
	review := in.Review
	review.Decisions = append([]RequirementDecision(nil), in.Review.Decisions...)
	sort.Slice(review.Decisions, func(i, j int) bool { return review.Decisions[i].Topic < review.Decisions[j].Topic })
	receipt, _ := json.Marshal(struct {
		Contract  string         `json:"contract"`
		Evidence  string         `json:"evidence"`
		Review    EvidenceReview `json:"review"`
		Artifacts string         `json:"artifacts"`
	}{ReviewedPlanContract, out.EvidenceSHA256, review, artifacts.Digest})
	sum := sha256.Sum256(receipt)
	out.PlanSHA256 = hex.EncodeToString(sum[:])
	out.Artifacts = &artifacts
	out.Status = "scaffold-needs-validation"
	return out, nil
}

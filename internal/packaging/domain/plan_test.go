package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func planFixture(t *testing.T) (string, []SourceDocument, PlanInput) {
	t.Helper()
	revision := strings.Repeat("a", 40)
	docs := []SourceDocument{{Path: "Dockerfile", Blob: strings.Repeat("b", 40), Text: "FROM registry.example.invalid/base@sha256:" + strings.Repeat("c", 64) + "\nEXPOSE 8080\nUSER 1000\n"}}
	packet, err := PlanPackaging(revision, "Dockerfile", docs, PlanInput{})
	if err != nil {
		t.Fatal(err)
	}
	scaffold := scaffoldFixture()
	scaffold.SourceRevision = revision
	h := sha256.Sum256([]byte(docs[0].Text))
	scaffold.SourceSHA256 = hex.EncodeToString(h[:])
	in := PlanInput{Scaffold: scaffold, Review: EvidenceReview{EvidenceSHA256: packet.EvidenceSHA256}}
	for _, topic := range []string{"persistence", "secrets", "startup-downloads", "identity"} {
		in.Review.Decisions = append(in.Review.Decisions, RequirementDecision{Topic: topic, Disposition: "none", Rationale: "Synthetic reviewed fixture, not a production assertion."})
	}
	return revision, docs, in
}
func TestPlanConnectsEvidenceReviewAndArtifacts(t *testing.T) {
	revision, docs, in := planFixture(t)
	out, err := PlanPackaging(revision, "Dockerfile", docs, in)
	if err != nil || out.Status != "scaffold-needs-validation" || out.Artifacts == nil || len(out.PlanSHA256) != 64 || len(out.Blockers) != 0 {
		t.Fatalf("unexpected plan %#v %v", out, err)
	}
	in.Review.Decisions[0], in.Review.Decisions[3] = in.Review.Decisions[3], in.Review.Decisions[0]
	same, err := PlanPackaging(revision, "Dockerfile", docs, in)
	if err != nil || same.PlanSHA256 != out.PlanSHA256 {
		t.Fatal("decision order changed identity")
	}
	in.Review.Decisions[0].Rationale = "Changed scope of review."
	changed, err := PlanPackaging(revision, "Dockerfile", docs, in)
	if err != nil || changed.PlanSHA256 == out.PlanSHA256 {
		t.Fatal("review change did not invalidate plan identity")
	}
}
func TestPlanRejectsStaleEvidenceAndUnsafeRequirements(t *testing.T) {
	revision, docs, in := planFixture(t)
	for _, change := range []func(*PlanInput){func(i *PlanInput) { i.Review.EvidenceSHA256 = "stale" }, func(i *PlanInput) { i.Scaffold.SourceSHA256 = strings.Repeat("f", 64) }, func(i *PlanInput) { i.Scaffold.Port = 9090 }, func(i *PlanInput) { i.Review.Decisions = nil }} {
		copy := in
		change(&copy)
		out, err := PlanPackaging(revision, "Dockerfile", docs, copy)
		if err != nil || out.Artifacts != nil || out.PlanSHA256 != "" || len(out.Blockers) == 0 {
			t.Fatal("unsafe plan generated")
		}
	}
	for _, topic := range []string{"persistence", "secrets", "startup-downloads", "identity"} {
		copy := in
		copy.Review.Decisions = append([]RequirementDecision(nil), in.Review.Decisions...)
		for n := range copy.Review.Decisions {
			if copy.Review.Decisions[n].Topic == topic {
				copy.Review.Decisions[n].Disposition = "required"
			}
		}
		out, err := PlanPackaging(revision, "Dockerfile", docs, copy)
		if err != nil || out.Artifacts != nil {
			t.Fatal("unsupported requirement generated artifacts")
		}
	}
	docs = append(docs, SourceDocument{Path: "README.md", Blob: strings.Repeat("d", 40), Text: "new reviewed material"})
	out, err := PlanPackaging(revision, "Dockerfile", docs, in)
	if err != nil || out.Artifacts != nil {
		t.Fatal("evidence scope change accepted stale review")
	}
}
func TestPlanRuntimeVolumeBlocksStatelessEvenWhenReviewSaysNone(t *testing.T) {
	revision, docs, in := planFixture(t)
	docs[0].Text += "VOLUME [\"/opt/geoserver_data\"]\n"
	packet, _ := PlanPackaging(revision, "Dockerfile", docs, PlanInput{})
	in.Review.EvidenceSHA256 = packet.EvidenceSHA256
	in.Scaffold.SourceSHA256 = packet.Sources[0].SHA256
	out, err := PlanPackaging(revision, "Dockerfile", docs, in)
	if err != nil || out.Artifacts != nil || !strings.Contains(strings.Join(out.Blockers, " "), "runtime VOLUME") {
		t.Fatal("runtime persistence ignored")
	}
}

func TestPlanUnresolvedOrMissingUserCannotPassReview(t *testing.T) {
	for _, user := range []string{"USER ${APP_UID}\n", "", "USER root\n"} {
		revision, docs, in := planFixture(t)
		docs[0].Text = "FROM base\nEXPOSE 8080\n" + user
		packet, _ := PlanPackaging(revision, "Dockerfile", docs, PlanInput{})
		in.Review.EvidenceSHA256 = packet.EvidenceSHA256
		in.Scaffold.SourceSHA256 = packet.Sources[0].SHA256
		out, err := PlanPackaging(revision, "Dockerfile", docs, in)
		if err != nil || out.Artifacts != nil {
			t.Fatal("unsupported USER generated artifacts")
		}
	}
}

func TestPlanFinalUserWins(t *testing.T) {
	revision, docs, in := planFixture(t)
	docs[0].Text = "FROM base\nUSER root\nEXPOSE 8080\nUSER 1000\n"
	packet, _ := PlanPackaging(revision, "Dockerfile", docs, PlanInput{})
	in.Review.EvidenceSHA256 = packet.EvidenceSHA256
	in.Scaffold.SourceSHA256 = packet.Sources[0].SHA256
	out, err := PlanPackaging(revision, "Dockerfile", docs, in)
	if err != nil || out.Artifacts == nil {
		t.Fatalf("final USER ignored: %#v %v", out, err)
	}
}

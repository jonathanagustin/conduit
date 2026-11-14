package domain

import (
	"reflect"
	"strings"
	"testing"
)

func complete() Assessment {
	a := Assessment{Revision: strings.Repeat("a", 40), UDSVersion: "1.15.0"}
	for _, topic := range topics {
		a.Findings = append(a.Findings, Finding{Topic: topic, Summary: "review me", Basis: "source-observed", Evidence: []Evidence{{Path: "README.md", Line: 1}}})
	}
	return a
}
func TestCompleteIsNotValidated(t *testing.T) {
	r, err := ReviewAssessment(complete())
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "reviewable-not-validated" || r.ValueTier != "CONDUIT_NATIVE" || len(r.Blockers) != 0 {
		t.Fatalf("%+v", r)
	}
}
func TestMissingAndInferredRemainBlocked(t *testing.T) {
	a := complete()
	a.Findings = a.Findings[:2]
	a.Findings[0].Basis = "inferred"
	r, err := ReviewAssessment(a)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Blockers) != 7 || r.Status != "needs-evidence" {
		t.Fatalf("%+v", r)
	}
	b := a
	b.Findings = []Finding{a.Findings[1], a.Findings[0]}
	s, _ := ReviewAssessment(b)
	if !reflect.DeepEqual(r, s) {
		t.Fatal("output depends on input order")
	}
}
func TestRejectInvalidAssessment(t *testing.T) {
	tests := map[string]func(*Assessment){"revision": func(a *Assessment) { a.Revision = "main" }, "target": func(a *Assessment) { a.UDSVersion = "" }, "duplicate": func(a *Assessment) { a.Findings[1] = a.Findings[0] }, "unknown topic": func(a *Assessment) { a.Findings[0].Topic = "deploy" }, "fake validation": func(a *Assessment) { a.Findings[0].Basis = "validated" }, "empty summary": func(a *Assessment) { a.Findings[0].Summary = " " }, "no citation": func(a *Assessment) { a.Findings[0].Evidence = nil }, "bad line": func(a *Assessment) { a.Findings[0].Evidence[0].Line = 0 }}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			a := complete()
			change(&a)
			if _, err := ReviewAssessment(a); err == nil {
				t.Fatal("accepted invalid assessment")
			}
		})
	}
}
func TestRejectUnsafeCitationPaths(t *testing.T) {
	for _, p := range []string{"../secret", "/etc/passwd", "C:/secret", "a/../b", "a\\b", "", "x\nsecret"} {
		t.Run(p, func(t *testing.T) {
			a := complete()
			a.Findings[0].Evidence[0].Path = p
			if _, err := ReviewAssessment(a); err == nil {
				t.Fatal("accepted unsafe citation")
			}
		})
	}
}

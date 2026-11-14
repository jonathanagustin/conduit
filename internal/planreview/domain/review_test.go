package domain

import (
	"strings"
	"testing"
)

func TestReviewContextIsBoundedAndExplicit(t *testing.T) {
	valid := Review{Title: "Membership review", Rationale: "Reason\nTicket CAB-17", ValueTier: Native, BaseDesiredStateDigest: "sha256:" + strings.Repeat("a", 64)}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Review){
		func(r *Review) { r.ValueTier = "PUBLIC_CONTRACT" },
		func(r *Review) { r.BaseDesiredStateDigest = "" },
		func(r *Review) { r.BaseDesiredStateDigest = "sha256:" + strings.Repeat("g", 64) },
		func(r *Review) { r.Title = strings.Repeat("a", 1025) },
		func(r *Review) { r.Rationale = strings.Repeat("a", 16385) },
		func(r *Review) { r.Rationale = "bad\x00control" },
		func(r *Review) { r.Rationale = string([]byte{0xff}) },
	} {
		invalid := valid
		mutate(&invalid)
		if invalid.Validate() == nil {
			t.Fatal("invalid review accepted")
		}
	}
	valid.Rationale = ""
	if err := valid.Validate(); err != nil {
		t.Fatal("blank editable rationale rejected", err)
	}
}

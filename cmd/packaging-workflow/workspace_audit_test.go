package main

import (
	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	packagingapp "github.com/jonathanagustin/conduit/internal/packagingworkflow/application"
	packagingdomain "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"testing"
)

func TestWorkspaceComputeAuditAllowsOnlyPassingOutcome(t *testing.T) {
	for _, outcome := range []string{string(packagingdomain.OutcomePass), string(packagingdomain.OutcomeFailRequired), ""} {
		t.Run(outcome, func(t *testing.T) {
			repository := controlplane.NewMemoryRepository()
			auditor := fileWorkspaceComputeAuditor{repository: repository, actor: "alice", sessionID: "test"}
			if err := auditor.RecordWorkspacePackage(t.Context(), packagingapp.WorkspaceAudit{RepoURL: "https://git.example/app", Outcome: outcome}); err != nil {
				t.Fatal(err)
			}
			rows, err := repository.ListAuthorizationAudit(t.Context(), "planning-session/test")
			if err != nil || len(rows) != 1 {
				t.Fatalf("rows=%+v err=%v", rows, err)
			}
			want := governance.DecisionDeny
			if outcome == string(packagingdomain.OutcomePass) {
				want = governance.DecisionAllow
			}
			if rows[0].Decision != want || rows[0].Context["outcome"] != outcome {
				t.Fatalf("audit=%+v", rows[0])
			}
		})
	}
}

package controlplane

import (
	"context"
	"errors"
	"testing"
)

// A Change Request id is derived from its target alone, so whatever record a
// target carries first, it carries for good: every later proposal collides and
// receives the stored one back. That makes ERROR the worst status to be stuck
// in. It is not a decision anybody took -- evaluateChangeRequest writes it when
// a policy fails to evaluate, when every policy returns no_op, or when no
// policy is enabled at all -- and until this change Reopen refused it, so a
// deployment that proposed anything before configuring its approval policies
// burnt that target permanently, with no recourse open to any principal.

// erroredRequest proposes against a Hub with no approval policies, which is
// exactly the state a deployment is in before it configures any.
func erroredRequest(t *testing.T, service *GovernanceService, author Principal) ChangeRequest {
	t.Helper()
	request, _, err := service.ProposeChanges(context.Background(),
		"EXPORT_PIPELINE_LIFECYCLE_"+ChangeTypeArchivePipeline, "nightly-export", "platform",
		[]EntityChange{{
			ID: "chg-1", Type: ChangeTypeArchivePipeline,
			Request: map[string]any{"pipelineRid": "nightly-export"},
			ReferencedResources: map[string]ReferencedResource{
				"bundlingpipeline": {RID: "export-pipeline/nightly-export", Name: "nightly-export"},
			},
		}},
		author, "Archive the nightly export", "no longer needed")
	if err != nil {
		t.Fatal(err)
	}
	if request.Status != ChangeError {
		t.Fatalf("a proposal with no enabled policies should error, got %s", request.Status)
	}
	return request
}

func TestAnErroredChangeRequestCanBeReopenedOncePolicyExists(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewGovernanceService(repository)
	author, err := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author", Groups: []string{"ops"}})
	if err != nil {
		t.Fatal(err)
	}

	request := erroredRequest(t, service, author)

	// The target is now occupied: re-proposing the same action returns the
	// errored record rather than a fresh evaluation, which is the wedge.
	again, _, err := service.ProposeChanges(ctx,
		"EXPORT_PIPELINE_LIFECYCLE_"+ChangeTypeArchivePipeline, "nightly-export", "platform",
		[]EntityChange{{
			ID: "chg-1", Type: ChangeTypeArchivePipeline,
			Request:             map[string]any{"pipelineRid": "nightly-export"},
			ReferencedResources: map[string]ReferencedResource{"bundlingpipeline": {RID: "export-pipeline/nightly-export", Name: "nightly-export"}},
		}},
		author, "Archive the nightly export", "no longer needed")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != request.ID || again.Status != ChangeError {
		t.Fatalf("re-proposal did not return the stored errored record: %s %s", again.ID, again.Status)
	}

	// The operator configures the policy that was missing.
	if _, err = service.PutPolicy(ctx, ApprovalPolicy{
		ID: "lifecycle", DisplayName: "Pipeline lifecycle", Enabled: true,
		Expression: `approve("archiving is unrestricted")`,
	}); err != nil {
		t.Fatal(err)
	}

	reopened, _, err := service.Reopen(ctx, request.ID, author)
	if err != nil {
		t.Fatalf("the author could not reopen a request that errored for want of a policy: %v", err)
	}
	// Re-evaluated against the policy in force now, which is the whole point:
	// reopening is not a way to launder a verdict, it is a way to be judged.
	if reopened.Status != ChangeApproved {
		t.Fatalf("the reopened request was not re-evaluated under the new policy: %s (%s)",
			reopened.Status, reopened.Error)
	}
}

// Reopening while the configuration is still broken costs nothing and says the
// same true thing again. Without this the fix could be "admit ERROR and hope",
// and a reader could not tell whether re-evaluation actually happens.
func TestReopeningAStillMisconfiguredRequestErrorsAgain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service := NewGovernanceService(NewMemoryRepository())
	author, err := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author", Groups: []string{"ops"}})
	if err != nil {
		t.Fatal(err)
	}
	request := erroredRequest(t, service, author)

	reopened, _, err := service.Reopen(ctx, request.ID, author)
	if err != nil {
		t.Fatalf("reopening a still-misconfigured request should be permitted: %v", err)
	}
	if reopened.Status != ChangeError {
		t.Fatalf("reopening under the same broken configuration should error again, got %s", reopened.Status)
	}
}

// The principal gate is untouched. Widening which statuses may be reopened must
// not widen who may reopen them -- that is a separate decision, and it needs a
// TargetKind-to-resource authorization mapping that does not exist yet.
func TestReopeningAnErroredRequestIsStillAuthorOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service := NewGovernanceService(NewMemoryRepository())
	author, err := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author", Groups: []string{"ops"}})
	if err != nil {
		t.Fatal(err)
	}
	stranger, err := service.ResolvePrincipal(ctx, IdentityClaims{Username: "stranger", Groups: []string{"ops"}})
	if err != nil {
		t.Fatal(err)
	}
	request := erroredRequest(t, service, author)

	if _, _, err = service.Reopen(ctx, request.ID, stranger); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a non-author reopened an errored request: %v", err)
	}
}

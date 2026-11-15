package composition

import (
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"testing"
)

func TestWorkspaceCallerRetainsCredentialAndRepresentedIdentity(t *testing.T) {
	principal := controlplane.Principal{Username: "agent", ServiceAccount: true, Groups: []string{"operators"}, OnBehalfOf: &controlplane.Principal{Username: "human", Teams: []string{"reviewers"}}}
	caller := devWorkspaceCaller(principal)
	if caller.Username != "agent" || !caller.ServiceAccount || len(caller.Groups) != 1 || caller.OnBehalfOf == nil || caller.OnBehalfOf.Username != "human" || len(caller.OnBehalfOf.Teams) != 1 {
		t.Fatalf("principal context lost: %#v", caller)
	}
}

// Package changerequest submits a workspace through the existing change
// request command. It does not approve, deploy, or publish.
package changerequest

import (
	"context"
	"strings"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
)

// Submitter is the change-request port. A later adapter can replace it.
type Submitter struct {
	governance *controlplane.GovernanceService
}

// New returns a submitter over the Hub's governance service.
func New(governance *controlplane.GovernanceService) *Submitter {
	return &Submitter{governance: governance}
}

// Submit opens one change request for the workspace revision. The author is
// the caller. This method has no approve parameter and does not call Approve.
func (submitter *Submitter) Submit(ctx context.Context, caller domain.Caller, draft domain.Submission) (domain.Receipt, error) {
	if submitter.governance == nil || strings.TrimSpace(caller.Username) == "" {
		return domain.Receipt{}, domain.ErrDisabled
	}
	if strings.TrimSpace(draft.WorkspaceID) == "" || strings.TrimSpace(draft.EnvironmentID) == "" || strings.TrimSpace(draft.Revision) == "" {
		return domain.Receipt{}, domain.ErrInvalid
	}
	change := controlplane.EntityChange{
		ID:   "chg_" + draft.WorkspaceID,
		Type: controlplane.ChangeTypeUpdateEnvironmentConfig,
		Request: map[string]any{
			"config": map[string]any{
				"workspaceId":  draft.WorkspaceID,
				"revision":     draft.Revision,
				"sourceDigest": draft.SourceDigest,
			},
		},
		ReferencedResources: map[string]controlplane.ReferencedResource{
			"environment": {RID: "environment/" + draft.EnvironmentID, Name: draft.EnvironmentID},
		},
	}
	author := controlplane.Principal{Username: caller.Username, Teams: caller.Teams, Groups: caller.Groups}
	title := draft.Title
	if title == "" {
		title = "Workspace " + draft.WorkspaceID
	}
	request, _, err := submitter.governance.ProposeChanges(ctx,
		"WORKSPACE", draft.WorkspaceID+"@"+draft.Revision, draft.EnvironmentID,
		[]controlplane.EntityChange{change}, author, title, draft.Description)
	if err != nil {
		return domain.Receipt{}, err
	}
	return domain.Receipt{ChangeRequestID: request.ID, Status: request.Status}, nil
}

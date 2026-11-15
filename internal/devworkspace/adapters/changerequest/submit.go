// Package changerequest submits a workspace through the existing change
// request command. It does not approve, deploy, or publish.
package changerequest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	author := controlplane.Principal{Username: caller.Username, Teams: caller.Teams, Groups: caller.Groups, ServiceAccount: caller.ServiceAccount}
	if represented := caller.OnBehalfOf; represented != nil {
		author.OnBehalfOf = &controlplane.Principal{Username: represented.Username, Teams: represented.Teams, Groups: represented.Groups, ServiceAccount: represented.ServiceAccount}
	}
	targetID, err := submissionIdentity(author, draft)
	if err != nil {
		return domain.Receipt{}, err
	}
	title := draft.Title
	if title == "" {
		title = "Workspace " + draft.WorkspaceID
	}
	request, err := submitter.governance.ProposeHumanChange(ctx,
		"WORKSPACE", targetID, draft.EnvironmentID,
		[]controlplane.EntityChange{change}, author, title, draft.Description)
	if err != nil {
		return domain.Receipt{}, err
	}
	return domain.Receipt{ChangeRequestID: request.ID, Status: request.Status}, nil
}

// CONDUIT-INVENTED: workspace.ScopedHumanSubmission
// Displaced-by: a published or authorized sanitized workspace submission identity, delegated-author and mandatory-human-review contract.
func submissionIdentity(author controlplane.Principal, draft domain.Submission) (string, error) {
	encoded, err := json.Marshal(struct {
		Author                                   controlplane.Principal
		Workspace, Environment, Revision, Source string
	}{author, draft.WorkspaceID, draft.EnvironmentID, draft.Revision, draft.SourceDigest})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return draft.WorkspaceID + "@" + draft.Revision + ":" + hex.EncodeToString(digest[:16]), nil
}

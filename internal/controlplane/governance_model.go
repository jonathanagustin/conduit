package controlplane

import (
	"context"
	"strings"
)

// ProposeHumanChange stores a change request that still needs a person.
// It does not approve the request and it does not apply the change.
func (service *GovernanceService) ProposeHumanChange(
	ctx context.Context, targetKind, targetID, environment string, changes []EntityChange,
	author Principal, title, description string,
) (ChangeRequest, error) {
	if needsNotificationTransaction(service.repository) {
		var request ChangeRequest
		err := service.withNotificationTransaction(ctx, func(working *GovernanceService) error {
			var err error
			request, err = working.ProposeHumanChange(ctx, targetKind, targetID, environment, changes, author, title, description)
			return err
		})
		if err != nil {
			return ChangeRequest{}, err
		}
		_ = service.announceAwaitingApproval(ctx, request)
		return request, nil
	}
	if strings.TrimSpace(targetKind) == "" || strings.TrimSpace(targetID) == "" ||
		strings.TrimSpace(environment) == "" || len(changes) == 0 || strings.TrimSpace(author.Username) == "" {
		return ChangeRequest{}, ValidationErrors{{Path: "changeRequest", Message: "target, Environment, changes, and author are required"}}
	}
	taxonomyErrors := ValidationErrors{}
	for index, change := range changes {
		taxonomyErrors = append(taxonomyErrors, validateChangeType(index, change)...)
	}
	if len(taxonomyErrors) > 0 {
		return ChangeRequest{}, taxonomyErrors
	}
	request, err := service.evaluateChangeRequest(ctx, ChangeRequest{
		TargetKind: targetKind, TargetID: targetID, Environment: environment,
		Title: title, Description: description, Author: author, Changes: cloneJSON(changes),
	})
	if err != nil {
		return ChangeRequest{}, err
	}
	if request.Status != ChangeRejected {
		request = holdForAPerson(request)
	}
	request, err = service.repository.CreateChangeRequest(ctx, request)
	if err == nil {
		service.auditRequestCreated(ctx, request)
		err = service.announceAwaitingApproval(ctx, request)
	}
	return request, err
}

func holdForAPerson(request ChangeRequest) ChangeRequest {
	if request.Status == ChangePendingApprovals && len(request.Requirements) > 0 {
		request.RequirementsSatisfied = false
		return request
	}
	request.Status = ChangePendingApprovals
	request.Error = ""
	request.RequirementsSatisfied = false
	if len(request.Requirements) == 0 {
		request.Requirements = []ReviewerRequirement{{
			ID: "human-approval:" + request.TargetID, Kind: "TEAM", Team: "release-reviewers",
			Reason: "a person must approve this proposal",
		}}
	}
	return request
}

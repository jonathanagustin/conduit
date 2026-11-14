// Package repository projects Conduit plans, reports, profiles, and change
// requests into the ontology catalog, and proposes a change request through
// governance without approving it.
package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/ontology/domain"
	"github.com/jonathanagustin/conduit/internal/ontology/ports"
)

// Source reads a control-plane repository.
type Source struct {
	repository controlplane.Repository
	governance *controlplane.GovernanceService
}

// New returns a source. A nil governance service refuses Apply.
func New(repository controlplane.Repository, governance *controlplane.GovernanceService) *Source {
	return &Source{repository: repository, governance: governance}
}

// Catalog projects the rows allow accepts. A nil allow shows every environment.
func (source *Source) Catalog(ctx context.Context, allow func(environmentID string) bool) (domain.Catalog, error) {
	if allow == nil {
		allow = func(string) bool { return true }
	}
	plans, err := source.repository.ListPlans(ctx, "")
	if err != nil {
		return domain.Catalog{}, err
	}
	changes, err := source.repository.ListChangeRequests(ctx, "")
	if err != nil {
		return domain.Catalog{}, err
	}
	reports, err := source.repository.ListEnvironmentReports(ctx)
	if err != nil {
		return domain.Catalog{}, err
	}
	builder := newBuilder(allow)
	for _, plan := range plans {
		builder.addPlan(plan)
	}
	for _, change := range changes {
		builder.addChange(change)
	}
	for _, report := range reports {
		builder.addReport(report)
	}
	for _, environmentID := range builder.environmentIDs() {
		profile, err := source.repository.GetEnvironmentProfile(ctx, environmentID)
		if errors.Is(err, controlplane.ErrNotFound) {
			continue
		}
		if err != nil {
			return domain.Catalog{}, err
		}
		builder.addProfile(profile)
	}
	return builder.catalog(), nil
}

// Apply validates a createChangeRequest action. VALIDATE_ONLY evaluates the
// change and does not store it. VALIDATE_AND_EXECUTE calls ProposeChanges, so
// the stored request keeps whatever status policy evaluation produced.
func (source *Source) Apply(ctx context.Context, caller ports.Caller, request domain.ApplyRequest) (domain.ApplyResult, error) {
	if request.Action != domain.ActionCreateChange {
		return domain.ApplyResult{}, domain.ErrUnknownAction
	}
	if source.governance == nil {
		return domain.ApplyResult{}, errors.New("governance is not configured")
	}
	mode := strings.TrimSpace(request.Mode)
	if mode == "" {
		mode = domain.ModeValidateAndExecute
	}
	result := domain.ApplyResult{ParameterMessages: map[string]string{}}
	if mode != domain.ModeValidateOnly && mode != domain.ModeValidateAndExecute {
		result.SubmissionMessage = "mode must be VALIDATE_ONLY or VALIDATE_AND_EXECUTE"
		return result, nil
	}
	parameters := request.Parameters
	if parameters == nil {
		parameters = map[string]string{}
	}
	for _, name := range domain.RequiredActionParameters() {
		if strings.TrimSpace(parameters[name]) == "" {
			result.ParameterMessages[name] = name + " is required"
		}
	}
	if len(result.ParameterMessages) > 0 {
		return result, nil
	}
	change, err := entityChange(parameters)
	if err != nil {
		result.SubmissionMessage = err.Error()
		return result, nil
	}
	author := controlplane.Principal{Username: caller.Username, Teams: caller.Teams, Groups: caller.Groups}
	preview, err := source.governance.PreviewChanges(ctx,
		parameters["targetKind"], parameters["targetId"], parameters["environment"],
		[]controlplane.EntityChange{change}, author, parameters["title"], parameters["description"])
	if err != nil {
		var validation controlplane.ValidationErrors
		if errors.As(err, &validation) {
			for _, item := range validation {
				field := parameterForPath(item.Path)
				if field == "" {
					result.SubmissionMessage = item.Message
					continue
				}
				result.ParameterMessages[field] = item.Message
			}
			return result, nil
		}
		return domain.ApplyResult{}, err
	}
	stored := preview
	if mode == domain.ModeValidateAndExecute {
		stored, _, err = source.governance.ProposeChanges(ctx,
			parameters["targetKind"], parameters["targetId"], parameters["environment"],
			[]controlplane.EntityChange{change}, author, parameters["title"], parameters["description"])
		if err != nil {
			var validation controlplane.ValidationErrors
			if errors.As(err, &validation) {
				result.SubmissionMessage = validation.Error()
				return result, nil
			}
			return domain.ApplyResult{}, err
		}
		result.Stored = true
	}
	result.ChangeRequestID = stored.ID
	result.ChangeRequestState = stored.Status
	switch stored.Status {
	case controlplane.ChangePendingApprovals, controlplane.ChangeApproved:
		result.Valid = true
	default:
		result.SubmissionMessage = stored.Error
		if result.SubmissionMessage == "" {
			result.SubmissionMessage = stored.Status
		}
	}
	return result, nil
}

func entityChange(parameters map[string]string) (controlplane.EntityChange, error) {
	environmentID := strings.TrimSpace(parameters["environment"])
	resources := map[string]controlplane.ReferencedResource{}
	if environmentID != "" {
		resources["environment"] = controlplane.ReferencedResource{
			RID: "environment/" + environmentID, Name: environmentID, Labels: map[string]string{},
		}
	}
	if installation := strings.TrimSpace(parameters["installationId"]); installation != "" {
		resources["installation"] = controlplane.ReferencedResource{
			RID: "installation/" + environmentID + "/" + installation, Name: installation, Labels: map[string]string{},
		}
	}
	if product := strings.TrimSpace(parameters["productId"]); product != "" {
		resources["product"] = controlplane.ReferencedResource{
			RID: "product/" + product, Name: product, Labels: map[string]string{},
		}
	}
	changeID := strings.TrimSpace(parameters["changeId"])
	if changeID == "" {
		changeID = "change"
	}
	request := map[string]any{
		"deploymentId": environmentID,
		"settings":     map[string]any{},
		"config":       map[string]any{},
	}
	for key, value := range parameters {
		request[key] = value
	}
	return controlplane.EntityChange{
		ID: changeID, Type: strings.TrimSpace(parameters["changeType"]),
		InstallationID: strings.TrimSpace(parameters["installationId"]),
		Request:        request, ReferencedResources: resources,
	}, nil
}

func parameterForPath(path string) string {
	switch {
	case strings.Contains(path, "referencedResources.environment"):
		return "environment"
	case strings.Contains(path, "referencedResources.installation"):
		return "installationId"
	case strings.Contains(path, "referencedResources.product"):
		return "productId"
	case strings.Contains(path, ".type"):
		return "changeType"
	default:
		return ""
	}
}

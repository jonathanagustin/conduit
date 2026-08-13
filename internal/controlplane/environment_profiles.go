package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jonathanagustin/conduit/internal/environments"
	"time"
)

const EnvironmentProfileChangeTarget = "ENVIRONMENT_PROFILE"

type EnvironmentProfileRepository interface {
	PutEnvironmentProfile(context.Context, EnvironmentProfile) (EnvironmentProfile, error)
	GetEnvironmentProfile(context.Context, string) (EnvironmentProfile, error)
	ListEnvironmentProfileRevisions(context.Context, string) ([]EnvironmentProfile, error)
}

type EnvironmentProfileService struct {
	repository Repository
	now        func() time.Time
}

func NewEnvironmentProfileService(repository Repository) *EnvironmentProfileService {
	return &EnvironmentProfileService{repository: repository, now: time.Now}
}

func (service *EnvironmentProfileService) Put(
	ctx context.Context, profile EnvironmentProfile, subject, rationale string,
) (EnvironmentProfile, error) {
	profile, err := service.Prepare(ctx, profile, subject, rationale)
	if err != nil {
		return EnvironmentProfile{}, err
	}
	return service.repository.PutEnvironmentProfile(ctx, profile)
}

func (service *EnvironmentProfileService) Prepare(
	ctx context.Context, profile EnvironmentProfile, subject, rationale string,
) (EnvironmentProfile, error) {
	profile.EnvironmentID = strings.TrimSpace(profile.EnvironmentID)
	profile.DisplayName = strings.TrimSpace(profile.DisplayName)
	profile.Description = strings.TrimSpace(profile.Description)
	profile.Accreditation = strings.TrimSpace(profile.Accreditation)
	profile.OwnerTeam = strings.TrimSpace(profile.OwnerTeam)
	profile.SupportOverride = strings.TrimSpace(profile.SupportOverride)
	profile.CoOwnerTeams = normalizedStrings(profile.CoOwnerTeams)
	profile.RegisteredNamespaces = normalizedStrings(profile.RegisteredNamespaces)
	profile.UpdatedBy, profile.Rationale = strings.TrimSpace(subject), strings.TrimSpace(rationale)
	profile.Source = EnvironmentProfileSourceOperatorAPI
	if err := service.validateReferences(ctx, profile); err != nil {
		return EnvironmentProfile{}, err
	}
	now := service.now().UTC()
	existing, err := service.repository.GetEnvironmentProfile(ctx, profile.EnvironmentID)
	switch {
	case err == nil:
		if profile.Revision != existing.Revision {
			return EnvironmentProfile{}, ErrConflict
		}
		profile.CreatedAt, profile.Revision = existing.CreatedAt, existing.Revision+1
	case errors.Is(err, ErrNotFound):
		if profile.Revision != 0 {
			return EnvironmentProfile{}, ErrConflict
		}
		profile.CreatedAt, profile.Revision = now, 1
	case err != nil:
		return EnvironmentProfile{}, err
	}
	profile.UpdatedAt = now
	if err := validateEnvironmentProfile(profile); err != nil {
		return EnvironmentProfile{}, err
	}
	return profile, nil
}

func (service *EnvironmentProfileService) ApplyApprovedChangeRequest(
	ctx context.Context, request ChangeRequest,
) (EnvironmentProfile, ChangeRequest, error) {
	if request.TargetKind != EnvironmentProfileChangeTarget || request.Status != ChangeApproved || len(request.Changes) != 1 ||
		request.Changes[0].Type != ChangeTypeUpdateEnvironmentProperties || request.Changes[0].Request["profile"] == nil {
		return EnvironmentProfile{}, request, ErrInvalidTransition
	}
	if request.Execution != nil && request.Execution.Status == ExecutionSucceeded {
		profile, err := service.repository.GetEnvironmentProfile(ctx, request.Environment)
		return profile, request, err
	}
	encoded, err := json.Marshal(request.Changes[0].Request["profile"])
	if err != nil {
		return EnvironmentProfile{}, request, ErrConflict
	}
	var profile EnvironmentProfile
	if err := json.Unmarshal(encoded, &profile); err != nil || profile.EnvironmentID != request.Environment {
		return EnvironmentProfile{}, request, ErrConflict
	}
	stored, err := service.repository.PutEnvironmentProfile(ctx, profile)
	if errors.Is(err, ErrConflict) {
		if existing, getErr := service.repository.GetEnvironmentProfile(ctx, profile.EnvironmentID); getErr == nil &&
			existing.Revision == profile.Revision && environmentProfilesEqual(existing, profile) {
			stored, err = existing, nil
		}
	}
	if err != nil {
		now := service.now().UTC()
		failed, updateErr := service.repository.UpdateChangeRequest(ctx, request.ID, func(record *ChangeRequest) error {
			if record.Execution != nil && record.Execution.Status == ExecutionSucceeded {
				return ErrConflict
			}
			record.Execution = &ChangeExecution{
				Status: ExecutionFailed, StartedAt: now, CompletedAt: now,
				Items: []ChangeExecutionItem{{
					ChangeID: record.Changes[0].ID, ResourceRID: environmentRID(profile.EnvironmentID),
					Status: ExecutionFailed, Error: errorReason(err),
				}},
			}
			record.UpdatedAt = now
			return nil
		})
		if updateErr == nil {
			request = failed
		}
		return EnvironmentProfile{}, request, err
	}
	now := service.now().UTC()
	request, err = service.repository.UpdateChangeRequest(ctx, request.ID, func(record *ChangeRequest) error {
		if record.Status != ChangeApproved {
			return ErrInvalidTransition
		}
		if record.Execution != nil && record.Execution.Status == ExecutionSucceeded {
			return nil
		}
		record.Execution = &ChangeExecution{
			Status: ExecutionSucceeded, StartedAt: now, CompletedAt: now,
			Items: []ChangeExecutionItem{{
				ChangeID: record.Changes[0].ID, ResourceRID: environmentRID(profile.EnvironmentID),
				Status: ExecutionSucceeded, ResultID: fmt.Sprintf("environment-profile/%s/%d", profile.EnvironmentID, profile.Revision),
			}},
		}
		record.UpdatedAt = now
		return nil
	})
	return stored, request, err
}

func (service *EnvironmentProfileService) validateReferences(ctx context.Context, profile EnvironmentProfile) error {
	for _, team := range environments.ContactTeams(profile) {
		if !environments.ValidContactTeam(team) {
			return fmt.Errorf("%w: invalid Environment contact team %q", ErrConflict, team)
		}
		if _, err := service.repository.GetTeam(ctx, team); err != nil {
			return err
		}
	}
	return nil
}

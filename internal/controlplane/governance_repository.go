package controlplane

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (repository *MemoryRepository) PutTeam(_ context.Context, team Team) (Team, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if err := validateLabelsFromDefinitions(team.Labels, repository.labelDefinitions); err != nil {
		return Team{}, err
	}
	if existing, ok := repository.teams[team.Name]; ok && team.Revision != existing.Revision+1 {
		return Team{}, ErrConflict
	} else if !ok && team.Revision != 1 {
		return Team{}, ErrConflict
	}
	repository.teams[team.Name] = cloneJSON(team)
	return cloneJSON(team), nil
}

func (repository *MemoryRepository) GetTeam(_ context.Context, name string) (Team, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	team, ok := repository.teams[name]
	if !ok {
		return Team{}, ErrNotFound
	}
	return cloneJSON(team), nil
}

func (repository *MemoryRepository) ListTeams(_ context.Context) ([]Team, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	result := make([]Team, 0, len(repository.teams))
	for _, team := range repository.teams {
		result = append(result, cloneJSON(team))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (repository *MemoryRepository) PutResourceGrant(_ context.Context, grant ResourceGrant) (ResourceGrant, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, ok := repository.resourceGrants[grant.ID]; ok && grant.Revision != existing.Revision+1 {
		return ResourceGrant{}, ErrConflict
	} else if !ok && grant.Revision != 1 {
		return ResourceGrant{}, ErrConflict
	}
	repository.resourceGrants[grant.ID] = cloneJSON(grant)
	return cloneJSON(grant), nil
}

func (repository *MemoryRepository) ListResourceGrants(_ context.Context, resourceRID string) ([]ResourceGrant, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	result := make([]ResourceGrant, 0, len(repository.resourceGrants))
	for _, grant := range repository.resourceGrants {
		if resourceRID == "" || grant.ResourceRID == resourceRID {
			result = append(result, cloneJSON(grant))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (repository *MemoryRepository) PutApprovalPolicy(_ context.Context, policy ApprovalPolicy) (ApprovalPolicy, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, ok := repository.approvalPolicies[policy.ID]; ok && policy.Revision != existing.Revision+1 {
		return ApprovalPolicy{}, ErrConflict
	} else if !ok && policy.Revision != 1 {
		return ApprovalPolicy{}, ErrConflict
	}
	repository.approvalPolicies[policy.ID] = cloneJSON(policy)
	return cloneJSON(policy), nil
}

func (repository *MemoryRepository) GetApprovalPolicy(_ context.Context, id string) (ApprovalPolicy, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	policy, ok := repository.approvalPolicies[id]
	if !ok {
		return ApprovalPolicy{}, ErrNotFound
	}
	return cloneJSON(policy), nil
}

func (repository *MemoryRepository) ListApprovalPolicies(_ context.Context) ([]ApprovalPolicy, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	result := make([]ApprovalPolicy, 0, len(repository.approvalPolicies))
	for _, policy := range repository.approvalPolicies {
		result = append(result, cloneJSON(policy))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (repository *MemoryRepository) CreateChangeRequest(_ context.Context, request ChangeRequest) (ChangeRequest, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, ok := repository.changeRequests[request.ID]; ok {
		if existing.PlanID == request.PlanID {
			return cloneJSON(existing), nil
		}
		return ChangeRequest{}, ErrConflict
	}
	if request.Revision == 0 {
		request.Revision = 1
	}
	repository.appendNativeInvalidation("change-request", request.ID, request.Revision, "approvals", "overview", "kpi")
	repository.changeRequests[request.ID] = cloneJSON(request)
	return cloneJSON(request), nil
}

func (repository *MemoryRepository) GetChangeRequest(_ context.Context, id string) (ChangeRequest, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	request, ok := repository.changeRequests[id]
	if !ok {
		return ChangeRequest{}, ErrNotFound
	}
	return cloneJSON(request), nil
}

func (repository *MemoryRepository) ListChangeRequests(_ context.Context, environment string) ([]ChangeRequest, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	result := make([]ChangeRequest, 0, len(repository.changeRequests))
	for _, request := range repository.changeRequests {
		if environment == "" || request.Environment == environment {
			result = append(result, cloneJSON(request))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (repository *MemoryRepository) UpdateChangeRequest(_ context.Context, id string, update func(*ChangeRequest) error) (ChangeRequest, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	request, ok := repository.changeRequests[id]
	if !ok {
		return ChangeRequest{}, ErrNotFound
	}
	request = cloneJSON(request)
	if err := update(&request); err != nil {
		return ChangeRequest{}, err
	}
	request.Revision++
	repository.appendNativeInvalidation("change-request", id, request.Revision, "approvals", "overview", "kpi")
	repository.changeRequests[id] = cloneJSON(request)
	return cloneJSON(request), nil
}

func (repository *MemoryRepository) AppendAuthorizationAudit(_ context.Context, event AuthorizationAuditEvent) (AuthorizationAuditEvent, error) {
	if err := ValidateAuditCategoryEvent(event); err != nil {
		return AuthorizationAuditEvent{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, ok := repository.authorizationAudit[event.ID]; ok {
		return cloneJSON(existing), nil
	}
	repository.authorizationAudit[event.ID] = cloneJSON(event)
	return cloneJSON(event), nil
}

func (repository *MemoryRepository) ListAuthorizationAudit(_ context.Context, resourceRID string) ([]AuthorizationAuditEvent, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	result := make([]AuthorizationAuditEvent, 0, len(repository.authorizationAudit))
	for _, event := range repository.authorizationAudit {
		if resourceRID == "" || event.ResourceRID == resourceRID {
			result = append(result, cloneJSON(event))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, nil
}

func (repository *FileRepository) PutTeam(_ context.Context, team Team) (Team, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	definitions, err := repository.listLabelDefinitionsUnlocked(true)
	if err != nil {
		return Team{}, err
	}
	if err := validateLabelsFromDefinitions(team.Labels, labelDefinitionIndex(definitions)); err != nil {
		return Team{}, err
	}
	if err := repository.checkRevision(repository.teamPath(team.Name), team.Revision); err != nil {
		return Team{}, err
	}
	if err := atomicWriteJSON(repository.teamPath(team.Name), team); err != nil {
		return Team{}, err
	}
	return team, nil
}
func (repository *FileRepository) GetTeam(_ context.Context, name string) (Team, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	var value Team
	if !identifierPattern.MatchString(name) {
		return value, ErrNotFound
	}
	if err := readJSON(repository.teamPath(name), &value); err != nil {
		return Team{}, err
	}
	return value, nil
}
func (repository *FileRepository) ListTeams(_ context.Context) ([]Team, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.listTeamsUnlocked()
}
func (repository *FileRepository) listTeamsUnlocked() ([]Team, error) {
	return listJSON[Team](repository.teamDirectory(), func(v Team) bool { return true }, func(a, b Team) bool { return a.Name < b.Name })
}
func (repository *FileRepository) PutResourceGrant(_ context.Context, grant ResourceGrant) (ResourceGrant, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if err := repository.checkRevision(repository.resourceGrantPath(grant.ID), grant.Revision); err != nil {
		return ResourceGrant{}, err
	}
	if err := atomicWriteJSON(repository.resourceGrantPath(grant.ID), grant); err != nil {
		return ResourceGrant{}, err
	}
	return grant, nil
}
func (repository *FileRepository) ListResourceGrants(_ context.Context, rid string) ([]ResourceGrant, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return listJSON[ResourceGrant](repository.resourceGrantDirectory(), func(v ResourceGrant) bool { return rid == "" || v.ResourceRID == rid }, func(a, b ResourceGrant) bool { return a.ID < b.ID })
}
func (repository *FileRepository) PutApprovalPolicy(_ context.Context, policy ApprovalPolicy) (ApprovalPolicy, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if err := repository.checkRevision(repository.approvalPolicyPath(policy.ID), policy.Revision); err != nil {
		return ApprovalPolicy{}, err
	}
	if err := atomicWriteJSON(repository.approvalPolicyPath(policy.ID), policy); err != nil {
		return ApprovalPolicy{}, err
	}
	return policy, nil
}
func (repository *FileRepository) GetApprovalPolicy(_ context.Context, id string) (ApprovalPolicy, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	var value ApprovalPolicy
	if !identifierPattern.MatchString(id) {
		return value, ErrNotFound
	}
	if err := readJSON(repository.approvalPolicyPath(id), &value); err != nil {
		return ApprovalPolicy{}, err
	}
	return value, nil
}
func (repository *FileRepository) ListApprovalPolicies(_ context.Context) ([]ApprovalPolicy, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return listJSON[ApprovalPolicy](repository.approvalPolicyDirectory(), func(v ApprovalPolicy) bool { return true }, func(a, b ApprovalPolicy) bool { return a.ID < b.ID })
}
func (repository *FileRepository) CreateChangeRequest(_ context.Context, request ChangeRequest) (ChangeRequest, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	var existing ChangeRequest
	err := readJSON(repository.changeRequestPath(request.ID), &existing)
	if err == nil {
		if existing.PlanID == request.PlanID {
			return existing, nil
		}
		return ChangeRequest{}, ErrConflict
	}
	if !errors.Is(err, ErrNotFound) {
		return ChangeRequest{}, err
	}
	if request.Revision == 0 {
		request.Revision = 1
	}
	if err := repository.writeChangeRequestWithOutbox(request.ID, request); err != nil {
		return ChangeRequest{}, err
	}
	return request, nil
}
func (repository *FileRepository) GetChangeRequest(_ context.Context, id string) (ChangeRequest, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	var value ChangeRequest
	if !strings.HasPrefix(id, "crq_") || strings.ContainsAny(id, `/\\`) {
		return value, ErrNotFound
	}
	if err := readJSON(repository.changeRequestPath(id), &value); err != nil {
		return ChangeRequest{}, err
	}
	return value, nil
}
func (repository *FileRepository) ListChangeRequests(_ context.Context, environment string) ([]ChangeRequest, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return listJSON[ChangeRequest](repository.changeRequestDirectory(), func(v ChangeRequest) bool { return environment == "" || v.Environment == environment }, func(a, b ChangeRequest) bool { return a.ID < b.ID })
}
func (repository *FileRepository) UpdateChangeRequest(_ context.Context, id string, update func(*ChangeRequest) error) (ChangeRequest, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	var request ChangeRequest
	if err := readJSON(repository.changeRequestPath(id), &request); err != nil {
		return ChangeRequest{}, err
	}
	if err := update(&request); err != nil {
		return ChangeRequest{}, err
	}
	request.Revision++
	if err := repository.writeChangeRequestWithOutbox(id, request); err != nil {
		return ChangeRequest{}, err
	}
	return request, nil
}
func (repository *FileRepository) AppendAuthorizationAudit(_ context.Context, event AuthorizationAuditEvent) (AuthorizationAuditEvent, error) {
	if err := ValidateAuditCategoryEvent(event); err != nil {
		return AuthorizationAuditEvent{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	var existing AuthorizationAuditEvent
	if err := readJSON(repository.authorizationAuditPath(event.ID), &existing); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return AuthorizationAuditEvent{}, err
	}
	if err := atomicWriteJSON(repository.authorizationAuditPath(event.ID), event); err != nil {
		return AuthorizationAuditEvent{}, err
	}
	return event, nil
}
func (repository *FileRepository) ListAuthorizationAudit(_ context.Context, rid string) ([]AuthorizationAuditEvent, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return listJSON[AuthorizationAuditEvent](repository.authorizationAuditDirectory(), func(v AuthorizationAuditEvent) bool { return rid == "" || v.ResourceRID == rid }, func(a, b AuthorizationAuditEvent) bool { return a.CreatedAt.Before(b.CreatedAt) })
}

func (repository *FileRepository) checkRevision(path string, revision uint64) error {
	var current struct {
		Revision uint64 `json:"revision"`
	}
	err := readJSON(path, &current)
	if err == nil && revision != current.Revision+1 {
		return ErrConflict
	}
	if errors.Is(err, ErrNotFound) && revision != 1 {
		return ErrConflict
	}
	return errIfNotNotFound(err)
}

func errIfNotNotFound(err error) error {
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func listJSON[T any](directory string, include func(T) bool, less func(T, T) bool) ([]T, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	result := make([]T, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var value T
		if err := readJSON(filepath.Join(directory, entry.Name()), &value); err != nil {
			return nil, err
		}
		if include(value) {
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return less(result[i], result[j]) })
	return result, nil
}

func (repository *FileRepository) teamPath(id string) string {
	return filepath.Join(repository.teamDirectory(), id+".json")
}
func (repository *FileRepository) resourceGrantPath(id string) string {
	return filepath.Join(repository.resourceGrantDirectory(), id+".json")
}
func (repository *FileRepository) approvalPolicyPath(id string) string {
	return filepath.Join(repository.approvalPolicyDirectory(), id+".json")
}
func (repository *FileRepository) changeRequestPath(id string) string {
	return filepath.Join(repository.changeRequestDirectory(), id+".json")
}
func (repository *FileRepository) authorizationAuditPath(id string) string {
	return filepath.Join(repository.authorizationAuditDirectory(), id+".json")
}

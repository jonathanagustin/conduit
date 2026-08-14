package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

const (
	ChangePendingApprovals = "PENDING_APPROVALS"
	ChangeApproved         = "APPROVED"
	ChangeRejected         = "REJECTED"
	ChangeCancelled        = "CANCELLED"
	ChangeError            = "ERROR"
)

var policyVariablePattern = regexp.MustCompile(`\{\{\s*policyVariable\.([A-Za-z][A-Za-z0-9_]*)\s*\}\}`)

type GovernanceRepository interface {
	PutTeam(context.Context, Team) (Team, error)
	GetTeam(context.Context, string) (Team, error)
	ListTeams(context.Context) ([]Team, error)
	PutResourceGrant(context.Context, ResourceGrant) (ResourceGrant, error)
	ListResourceGrants(context.Context, string) ([]ResourceGrant, error)
	PutApprovalPolicy(context.Context, ApprovalPolicy) (ApprovalPolicy, error)
	GetApprovalPolicy(context.Context, string) (ApprovalPolicy, error)
	ListApprovalPolicies(context.Context) ([]ApprovalPolicy, error)
	CreateChangeRequest(context.Context, ChangeRequest) (ChangeRequest, error)
	GetChangeRequest(context.Context, string) (ChangeRequest, error)
	ListChangeRequests(context.Context, string) ([]ChangeRequest, error)
	UpdateChangeRequest(context.Context, string, func(*ChangeRequest) error) (ChangeRequest, error)
	AppendAuthorizationAudit(context.Context, AuthorizationAuditEvent) (AuthorizationAuditEvent, error)
	ListAuthorizationAudit(context.Context, string) ([]AuthorizationAuditEvent, error)
}

type ChangeDiffEntry struct {
	Before any `json:"before,omitempty"`
	After  any `json:"after,omitempty"`
}

type ReferencedResource struct {
	RID         string            `json:"rid"`
	Name        string            `json:"name"`
	Labels      map[string]string `json:"labels"`
	AuthorRoles []string          `json:"authorRoles"`
	Resource    map[string]any    `json:"resource,omitempty"`
}

type EntityChange struct {
	ID             string         `json:"id"`
	Type           string         `json:"type"`
	InstallationID string         `json:"installationId"`
	Request        map[string]any `json:"request"`
	// Metadata records the origin of the change so a policy can tell automation
	// apart from an operator editing the same resource. It is the input the
	// published is_trusted_module_request and is_trusted_import_request
	// predicates read.
	Metadata            map[string]any                `json:"metadata,omitempty"`
	Diff                map[string]ChangeDiffEntry    `json:"diff"`
	ReferencedResources map[string]ReferencedResource `json:"referencedResources"`
}

type ChangeReview struct {
	Subject   string    `json:"subject"`
	Decision  string    `json:"decision"`
	Rationale string    `json:"rationale"`
	Teams     []string  `json:"teams,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type ChangeRequest struct {
	ID                    string                `json:"id"`
	PlanID                string                `json:"planId"`
	TargetKind            string                `json:"targetKind,omitempty"`
	TargetID              string                `json:"targetId,omitempty"`
	Environment           string                `json:"environment"`
	Title                 string                `json:"title"`
	Description           string                `json:"description,omitempty"`
	Author                Principal             `json:"author"`
	Editors               []string              `json:"editors,omitempty"`
	Labels                []string              `json:"labels,omitempty"`
	Status                string                `json:"status"`
	RequirementsSatisfied bool                  `json:"requirementsSatisfied"`
	Changes               []EntityChange        `json:"changes"`
	Evaluations           []PolicyEvaluation    `json:"evaluations"`
	Requirements          []ReviewerRequirement `json:"requirements"`
	Reviews               []ChangeReview        `json:"reviews,omitempty"`
	Execution             *ChangeExecution      `json:"execution,omitempty"`
	Error                 string                `json:"error,omitempty"`
	CreatedAt             time.Time             `json:"createdAt"`
	UpdatedAt             time.Time             `json:"updatedAt"`
	Revision              uint64                `json:"revision"`
}

type ChangeExecution struct {
	Status      string                `json:"status"`
	Items       []ChangeExecutionItem `json:"items"`
	StartedAt   time.Time             `json:"startedAt,omitempty"`
	CompletedAt time.Time             `json:"completedAt,omitempty"`
}

type ChangeExecutionItem struct {
	ChangeID    string `json:"changeId"`
	ResourceRID string `json:"resourceRid"`
	Status      string `json:"status"`
	ResultID    string `json:"resultId,omitempty"`
	Error       string `json:"error,omitempty"`
}

type GovernanceService struct {
	repository Repository
	lifecycle  *Lifecycle
	now        func() time.Time
	trust      PolicyTrust
}

func NewGovernanceService(repository Repository) *GovernanceService {
	return &GovernanceService{repository: repository, lifecycle: NewLifecycle(repository), now: time.Now}
}

// SetPolicyTrust declares which automation identities the published
// auto-approval predicates may trust, and for which Change Types. It is empty by
// default so that a deployment that never declares its service accounts cannot
// auto-approve anything.
func (service *GovernanceService) SetPolicyTrust(trust PolicyTrust) {
	service.trust = PolicyTrust{
		ServiceAccounts: cloneJSON(trust.ServiceAccounts),
		ModuleService:   strings.TrimSpace(trust.ModuleService),
		ImportService:   strings.TrimSpace(trust.ImportService),
	}
}

func (service *GovernanceService) ResolvePrincipal(ctx context.Context, claims IdentityClaims) (Principal, error) {
	claims.Username = strings.TrimSpace(claims.Username)
	if claims.Username == "" {
		return Principal{}, fmt.Errorf("%w: authenticated username is required", ErrUnauthorized)
	}
	principal := Principal{
		Username: claims.Username, Groups: normalizedStrings(claims.Groups), ServiceAccount: claims.ServiceAccount,
	}
	teams, err := service.repository.ListTeams(ctx)
	if err != nil {
		return Principal{}, err
	}
	groupSet := stringSet(principal.Groups)
	for _, team := range teams {
		if contains(team.AdminUsers, principal.Username) || contains(team.MemberUsers, principal.Username) ||
			intersects(groupSet, team.AdminGroups) || intersects(groupSet, team.MemberGroups) {
			principal.Teams = append(principal.Teams, team.Name)
		}
	}
	sort.Strings(principal.Teams)
	if strings.TrimSpace(claims.OnBehalfOf) != "" {
		if !principal.ServiceAccount || !service.Authorize(ctx, principal, "platform/root", OperationActOnBehalf) {
			service.audit(ctx, principal.Username, "ACT_ON_BEHALF", "platform/root", "DENY", "service account lacks delegation authority", nil)
			return Principal{}, ErrForbidden
		}
		delegated, err := service.ResolvePrincipal(ctx, IdentityClaims{Username: claims.OnBehalfOf})
		if err != nil {
			return Principal{}, err
		}
		principal.OnBehalfOf = &delegated
		service.audit(ctx, principal.Username, "ACT_ON_BEHALF", delegated.Username, "ALLOW", "delegated identity resolved", nil)
	}
	return principal, nil
}

func (service *GovernanceService) Authorize(ctx context.Context, principal Principal, resourceRID, operation string) bool {
	teams := stringSet(principal.Teams)
	grants, err := effectiveResourceGrants(ctx, service.repository, resourceRID)
	if err != nil {
		return false
	}
	for _, grant := range grants {
		if _, member := teams[grant.Team]; member && (contains(grant.Operations, operation) || rolesAllow(grant.Roles, operation)) {
			return true
		}
	}
	return false
}

func effectiveResourceGrants(ctx context.Context, repository GovernanceRepository, resourceRID string) ([]ResourceGrant, error) {
	result := []ResourceGrant{}
	for _, scope := range authorizationScopes(resourceRID) {
		grants, err := repository.ListResourceGrants(ctx, scope)
		if err != nil {
			return nil, err
		}
		result = append(result, grants...)
	}
	return result, nil
}

func (service *GovernanceService) AuthorizeLabelApplication(
	ctx context.Context, principal Principal, label, targetResource, targetWriteOperation string,
) bool {
	return service.Authorize(ctx, principal, "label/"+label, OperationApplyLabel) &&
		service.Authorize(ctx, principal, targetResource, targetWriteOperation)
}

// ValidateLabelContributions checks the label side of the two-key
// authorization rule. Transport adapters use it only after independently
// authorizing the caller to mutate the target resource.
func (service *GovernanceService) ValidateLabelContributions(
	ctx context.Context, principal Principal, previous, proposed map[string]string,
) error {
	definitions, err := service.repository.ListLabelDefinitions(ctx, false)
	if err != nil {
		return err
	}
	defined := labelDefinitionIndex(definitions)
	changed := map[string]struct{}{}
	for id, value := range proposed {
		if value != previous[id] {
			changed[id] = struct{}{}
		}
	}
	for id := range previous {
		if _, retained := proposed[id]; !retained {
			changed[id] = struct{}{}
		}
	}
	for id := range changed {
		if _, exists := defined[id]; exists && !service.Authorize(ctx, principal, "label/"+id, OperationApplyLabel) {
			return ErrForbidden
		}
	}
	return nil
}

func (service *GovernanceService) AuthorizeReleaseChannelContribution(
	ctx context.Context, principal Principal, channel, product string,
) bool {
	channelGrants, err := effectiveResourceGrants(ctx, service.repository, "release-channel/"+channel)
	if err != nil {
		return false
	}
	teams := stringSet(principal.Teams)
	restricted := false
	for _, grant := range channelGrants {
		if _, member := teams[grant.Team]; !member {
			continue
		}
		if contains(grant.Roles, "release-channel:contributor") || contains(grant.Operations, OperationProposeChange) {
			return true
		}
		if contains(grant.Roles, "release-channel:restricted-contributor") {
			restricted = true
		}
	}
	if !restricted {
		return false
	}
	productGrants, err := effectiveResourceGrants(ctx, service.repository, productRID(product))
	if err != nil {
		return false
	}
	for _, grant := range productGrants {
		if _, member := teams[grant.Team]; member &&
			(grant.Relationship == RelationshipOwner || grant.Relationship == RelationshipCoOwner) {
			return true
		}
	}
	return false
}

func (service *GovernanceService) PutTeam(ctx context.Context, team Team) (Team, error) {
	if !identifierPattern.MatchString(team.Name) || strings.TrimSpace(team.DisplayName) == "" {
		return Team{}, ValidationErrors{{Path: "team", Message: "name and displayName are required"}}
	}
	team.AdminUsers, team.AdminGroups = normalizedStrings(team.AdminUsers), normalizedStrings(team.AdminGroups)
	team.MemberUsers, team.MemberGroups = normalizedStrings(team.MemberUsers), normalizedStrings(team.MemberGroups)
	if len(team.AdminUsers)+len(team.AdminGroups) == 0 || len(team.MemberUsers)+len(team.MemberGroups) == 0 {
		return Team{}, ValidationErrors{{Path: "team", Message: "at least one administrator and one member user/group are required"}}
	}
	now := service.now().UTC()
	existing, err := service.repository.GetTeam(ctx, team.Name)
	switch {
	case err == nil:
		if team.Revision != existing.Revision {
			return Team{}, ErrConflict
		}
		team.CreatedAt, team.Revision = existing.CreatedAt, existing.Revision+1
	case errors.Is(err, ErrNotFound):
		team.CreatedAt, team.Revision = now, 1
	case err != nil:
		return Team{}, err
	}
	team.UpdatedAt = now
	return service.repository.PutTeam(ctx, team)
}

func (service *GovernanceService) PutGrant(ctx context.Context, grant ResourceGrant) (ResourceGrant, error) {
	if strings.TrimSpace(grant.ResourceRID) == "" || !identifierPattern.MatchString(grant.Team) ||
		(len(grant.Roles)+len(grant.Operations) == 0 && grant.Relationship == "") {
		return ResourceGrant{}, ValidationErrors{{Path: "grant", Message: "resourceRid, team, and a relationship, role, or operation are required"}}
	}
	if grant.Relationship != "" && grant.Relationship != RelationshipOwner && grant.Relationship != RelationshipCoOwner {
		return ResourceGrant{}, ValidationErrors{{Path: "grant.relationship", Message: "must be OWNER or CO_OWNER"}}
	}
	if _, err := service.repository.GetTeam(ctx, grant.Team); err != nil {
		return ResourceGrant{}, err
	}
	grant.Roles, grant.Operations = normalizedStrings(grant.Roles), normalizedStrings(grant.Operations)
	for _, role := range grant.Roles {
		if err := validateCanonicalRoleScope(role, grant.ResourceRID); err != nil {
			return ResourceGrant{}, ValidationErrors{{Path: "grant.roles", Message: err.Error()}}
		}
	}
	identity := sha256.Sum256([]byte(grant.ResourceRID + "\x00" + grant.Team))
	grant.ID = "grt_" + hex.EncodeToString(identity[:16])
	now := service.now().UTC()
	existing := findGrant(ctx, service.repository, grant.ID)
	if existing != nil {
		if grant.Revision != existing.Revision {
			return ResourceGrant{}, ErrConflict
		}
		grant.CreatedAt, grant.Revision = existing.CreatedAt, existing.Revision+1
	} else {
		grant.CreatedAt, grant.Revision = now, 1
	}
	grant.UpdatedAt = now
	return service.repository.PutResourceGrant(ctx, grant)
}

func (service *GovernanceService) PutPolicy(ctx context.Context, policy ApprovalPolicy) (ApprovalPolicy, error) {
	if !identifierPattern.MatchString(policy.ID) || strings.TrimSpace(policy.DisplayName) == "" || strings.TrimSpace(policy.Expression) == "" {
		return ApprovalPolicy{}, ValidationErrors{{Path: "policy", Message: "id, displayName, and expression are required"}}
	}
	if _, err := compilePolicy(policy, service.trust); err != nil {
		return ApprovalPolicy{}, ValidationErrors{{Path: "expression", Message: err.Error()}}
	}
	now := service.now().UTC()
	existing, err := service.repository.GetApprovalPolicy(ctx, policy.ID)
	switch {
	case err == nil:
		if policy.Revision != existing.Revision {
			return ApprovalPolicy{}, ErrConflict
		}
		policy.CreatedAt, policy.Revision = existing.CreatedAt, existing.Revision+1
	case errors.Is(err, ErrNotFound):
		policy.CreatedAt, policy.Revision = now, 1
	case err != nil:
		return ApprovalPolicy{}, err
	}
	policy.UpdatedAt = now
	return service.repository.PutApprovalPolicy(ctx, policy)
}

func (service *GovernanceService) ProposePlan(
	ctx context.Context, state DesiredState, author Principal, title, description string,
) (PlanRecord, ChangeRequest, error) {
	return service.proposePlan(ctx, state, author, title, description, nil)
}

func (service *GovernanceService) proposePlan(
	ctx context.Context, state DesiredState, author Principal, title, description string, optimizer *OptimizerPlanProvenance,
) (PlanRecord, ChangeRequest, error) {
	if strings.TrimSpace(author.Username) == "" {
		return PlanRecord{}, ChangeRequest{}, ErrUnauthorized
	}
	if err := service.validateLabelContributions(ctx, state, author); err != nil {
		return PlanRecord{}, ChangeRequest{}, err
	}
	record, err := service.lifecycle.createPlan(ctx, state, optimizer)
	if err != nil {
		return PlanRecord{}, ChangeRequest{}, err
	}
	request, err := service.evaluatePlan(ctx, record, author, title, description)
	if err != nil {
		return PlanRecord{}, ChangeRequest{}, err
	}
	request, err = service.repository.CreateChangeRequest(ctx, request)
	if err != nil {
		return PlanRecord{}, ChangeRequest{}, err
	}
	service.auditRequestCreated(ctx, request)
	record, err = service.syncPlan(ctx, request)
	return record, request, err
}

func (service *GovernanceService) validateLabelContributions(ctx context.Context, state DesiredState, author Principal) error {
	definitions, err := service.repository.ListLabelDefinitions(ctx, false)
	if err != nil {
		return err
	}
	defined := labelDefinitionIndex(definitions)
	if len(defined) == 0 {
		return nil
	}
	previous := DesiredState{}
	plans, err := service.repository.ListPlans(ctx, state.Environment.ID)
	if err != nil {
		return err
	}
	if latest, exists := latestPlansByEnvironment(plans)[state.Environment.ID]; exists && latest.DesiredState != nil {
		previous = *latest.DesiredState
	}
	check := func(id, value, oldValue, target string) error {
		if _, exists := defined[id]; !exists || value == oldValue {
			return nil
		}
		if !service.AuthorizeLabelApplication(ctx, author, id, target, OperationOperate) {
			return ErrForbidden
		}
		return nil
	}
	for id, value := range state.Environment.Labels {
		if err := check(id, value, previous.Environment.Labels[id], environmentRID(state.Environment.ID)); err != nil {
			return err
		}
	}
	for id, oldValue := range previous.Environment.Labels {
		if _, retained := state.Environment.Labels[id]; !retained {
			if err := check(id, "", oldValue, environmentRID(state.Environment.ID)); err != nil {
				return err
			}
		}
	}
	previousEntities, currentEntities := map[string]Entity{}, map[string]Entity{}
	for _, entity := range previous.Entities {
		previousEntities[entity.Name] = entity
	}
	for _, entity := range state.Entities {
		currentEntities[entity.Name] = entity
		for id, value := range entity.Labels {
			if err := check(id, value, previousEntities[entity.Name].Labels[id], installationRID(state.Environment.ID, entity.Name)); err != nil {
				return err
			}
		}
	}
	for name, entity := range previousEntities {
		for id, oldValue := range entity.Labels {
			if current, exists := currentEntities[name]; !exists || current.Labels[id] == "" {
				if err := check(id, "", oldValue, installationRID(state.Environment.ID, name)); err != nil {
					return err
				}
			}
		}
	}
	previousReleases, currentReleases := map[string]Release{}, map[string]Release{}
	for _, release := range previous.Releases {
		previousReleases[release.Product+":"+release.Version] = release
	}
	for _, release := range state.Releases {
		key := release.Product + ":" + release.Version
		currentReleases[key] = release
		for id, value := range release.Labels {
			if err := check(id, value, previousReleases[key].Labels[id], productRID(release.Product)); err != nil {
				return err
			}
		}
	}
	for key, release := range previousReleases {
		for id, oldValue := range release.Labels {
			if current, exists := currentReleases[key]; !exists || current.Labels[id] == "" {
				if err := check(id, "", oldValue, productRID(release.Product)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (service *GovernanceService) evaluatePlan(
	ctx context.Context, record PlanRecord, author Principal, title, description string,
) (ChangeRequest, error) {
	changes, err := service.planChanges(ctx, record.Plan, author)
	if err != nil {
		return ChangeRequest{}, err
	}
	if record.Optimizer != nil && len(changes) == 0 {
		return ChangeRequest{}, fmt.Errorf("%w: optimizer proposal produced no reviewable changes", ErrInvalidTransition)
	}
	if record.Optimizer != nil {
		for index := range changes {
			if changes[index].Metadata == nil {
				changes[index].Metadata = map[string]any{}
			}
			changes[index].Metadata["source"] = "optimizer"
			changes[index].Metadata["optimizerProposalId"] = record.Optimizer.ProposalID
			changes[index].Metadata["optimizerSnapshotId"] = record.Optimizer.SnapshotID
			changes[index].Metadata["optimizerSolverReceiptId"] = record.Optimizer.SolverReceiptID
		}
	}
	if strings.TrimSpace(title) == "" {
		title = "Reconcile " + record.Plan.Environment + " generation " + record.Plan.Generation
	}
	request, err := service.evaluateChangeRequest(ctx, ChangeRequest{
		PlanID: record.ID, TargetKind: "PLAN", TargetID: record.ID, Environment: record.Plan.Environment,
		Title: title, Description: description, Author: author, Changes: changes,
	})
	if err == nil && record.Optimizer != nil && request.Status != ChangeRejected && request.Status != ChangeError {
		request.Requirements = append(request.Requirements, ReviewerRequirement{
			ID: "optimizer-revalidation:" + record.ID, ChangeID: changes[0].ID,
			PolicyID: "platform.optimizer.revalidation", Kind: "OPTIMIZER_REVALIDATION",
			Reason: "fresh optimizer evidence is required at approval",
		})
		request.Status = ChangePendingApprovals
		request.RequirementsSatisfied = false
	}
	return request, err
}

func (service *GovernanceService) ProposeChanges(
	ctx context.Context, targetKind, targetID, environment string, changes []EntityChange,
	author Principal, title, description string,
) (ChangeRequest, error) {
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
	request, err = service.repository.CreateChangeRequest(ctx, request)
	if err == nil {
		service.auditRequestCreated(ctx, request)
	}
	return request, err
}

func (service *GovernanceService) evaluateChangeRequest(
	ctx context.Context, request ChangeRequest,
) (ChangeRequest, error) {
	now := service.now().UTC()
	policies, err := service.repository.ListApprovalPolicies(ctx)
	if err != nil {
		return ChangeRequest{}, err
	}
	enabled := make([]ApprovalPolicy, 0)
	for _, policy := range policies {
		if policy.Enabled {
			enabled = append(enabled, policy)
		}
	}
	request.Title, request.Description = strings.TrimSpace(request.Title), strings.TrimSpace(request.Description)
	request.Status, request.CreatedAt, request.UpdatedAt, request.Revision = ChangePendingApprovals, now, now, 1
	if request.Title == "" {
		request.Title = "Review " + strings.ToLower(request.TargetKind) + " " + request.TargetID
	}
	policyErrors := []string{}
	rejections := []string{}
	identity := sha256.Sum256([]byte(request.TargetKind + "\x00" + request.TargetID))
	request.ID = "crq_" + hex.EncodeToString(identity[:16])
	for _, change := range request.Changes {
		opinions := 0
		evaluationFailed := false
		for _, policy := range enabled {
			result, evaluationErr := evaluatePolicy(policy, policyInput(change, request.Author, request.Editors, now), service.trust)
			if evaluationErr != nil {
				evaluationFailed = true
				policyErrors = append(policyErrors, fmt.Sprintf("policy %s on change %s: %v", policy.ID, change.ID, evaluationErr))
				continue
			}
			evaluation := PolicyEvaluation{ChangeID: change.ID, PolicyID: policy.ID, Outcome: result.Kind, Reason: result.Reason}
			switch result.Kind {
			case "NO_OP":
			case "APPROVE":
				opinions++
			case "REJECT":
				opinions++
				rejections = append(rejections, result.Reason)
			default:
				opinions++
				requirement := result.Requirement(change.ID, policy.ID)
				evaluation.Requirement = &requirement
				request.Requirements = append(request.Requirements, requirement)
			}
			request.Evaluations = append(request.Evaluations, evaluation)
		}
		if opinions == 0 && !evaluationFailed {
			policyErrors = append(policyErrors, "all enabled policies returned no_op for change "+change.ID)
		}
	}
	if len(enabled) == 0 {
		policyErrors = append(policyErrors, "no enabled approval policies")
	}
	if len(policyErrors) > 0 {
		request.Status, request.Error = ChangeError, strings.Join(policyErrors, "; ")
	} else if len(rejections) > 0 {
		request.Status, request.Error = ChangeRejected, strings.Join(normalizedStrings(rejections), "; ")
	}
	request.RequirementsSatisfied = len(request.Requirements) == 0 && request.Status == ChangePendingApprovals
	if request.RequirementsSatisfied && !hasDoNotMerge(request.Labels) {
		request.Status = ChangeApproved
	}
	return request, nil
}

func (service *GovernanceService) Approve(
	ctx context.Context, changeRequestID string, principal Principal, rationale string,
) (ChangeRequest, PlanRecord, error) {
	return service.approveTransactional(ctx, changeRequestID, principal, rationale, nil)
}

type optimizerApprovalPermit struct {
	planID         string
	proposalID     string
	revalidationID string
	acquisitionID  string
	evaluatedAt    time.Time
}

func (service *GovernanceService) approveTransactional(
	ctx context.Context, changeRequestID string, principal Principal, rationale string, permit *optimizerApprovalPermit,
) (ChangeRequest, PlanRecord, error) {
	transactional, ok := service.repository.(TransactionalRepository)
	if !ok {
		return service.approveOnce(ctx, changeRequestID, principal, rationale, permit)
	}
	var request ChangeRequest
	var plan PlanRecord
	var operationErr error
	err := transactional.Transaction(ctx, func(transaction Repository) error {
		transactionService := NewGovernanceService(transaction)
		transactionService.now = service.now
		request, plan, operationErr = transactionService.approveOnce(ctx, changeRequestID, principal, rationale, permit)
		// An application adapter records a durable failed execution on the
		// Change Request before returning its outward conflict. Preserve that
		// evidence; all other errors roll the transaction back.
		if operationErr != nil && request.Execution != nil && request.Execution.Status == ExecutionFailed {
			return nil
		}
		return operationErr
	})
	if err != nil {
		if auditErr := service.audit(ctx, principal.Username, "CHANGE_REQUEST_APPROVE", changeRequestID, "DENY", errorReason(err), nil); auditErr != nil {
			return ChangeRequest{}, PlanRecord{}, errors.Join(err, fmt.Errorf("persist denied approval audit: %w", auditErr))
		}
		return ChangeRequest{}, PlanRecord{}, err
	}
	if operationErr != nil {
		return request, plan, operationErr
	}
	return request, plan, nil
}

func (service *GovernanceService) approveOnce(
	ctx context.Context, changeRequestID string, principal Principal, rationale string, permit *optimizerApprovalPermit,
) (ChangeRequest, PlanRecord, error) {
	rationale = strings.TrimSpace(rationale)
	if rationale == "" {
		return ChangeRequest{}, PlanRecord{}, ErrInvalidTransition
	}
	candidate, err := service.repository.GetChangeRequest(ctx, changeRequestID)
	if err != nil {
		return ChangeRequest{}, PlanRecord{}, err
	}
	if candidate.PlanID != "" {
		plan, planErr := service.repository.GetPlan(ctx, candidate.PlanID)
		if planErr != nil {
			return ChangeRequest{}, PlanRecord{}, planErr
		}
		if plan.Optimizer != nil && (permit == nil || permit.planID != plan.ID || permit.proposalID != plan.Optimizer.ProposalID) {
			service.audit(ctx, principal.Username, "CHANGE_REQUEST_APPROVE", changeRequestID, "DENY", "optimizer plan requires bound approval revalidation", nil)
			return ChangeRequest{}, PlanRecord{}, ErrInvalidTransition
		}
		if plan.Optimizer == nil && permit != nil {
			return ChangeRequest{}, PlanRecord{}, ErrInvalidTransition
		}
	}
	eligible := map[string]struct{}{}
	for _, requirement := range candidate.Requirements {
		if permit != nil && requirement.Kind == "OPTIMIZER_REVALIDATION" {
			eligible[requirement.ID] = struct{}{}
			continue
		}
		if requirementSatisfied(ctx, service.repository, requirement, principal) {
			eligible[requirement.ID] = struct{}{}
		}
	}
	if len(eligible) == 0 {
		service.audit(ctx, principal.Username, "CHANGE_REQUEST_APPROVE", changeRequestID, "DENY", ErrForbidden.Error(), nil)
		return ChangeRequest{}, PlanRecord{}, ErrForbidden
	}
	// Persist the non-authoritative revalidation link before the Change Request
	// update. If the later review CAS loses a race, the Plan remains awaiting
	// approval and a retry can safely replace this link. The reverse order could
	// leave an approved Change Request permanently unable to synchronize.
	if permit != nil {
		if strings.TrimSpace(permit.revalidationID) == "" || strings.TrimSpace(permit.acquisitionID) == "" || !optimizerEvidenceFreshAt(permit.evaluatedAt, service.now().UTC()) {
			return ChangeRequest{}, PlanRecord{}, ErrInvalidTransition
		}
		if _, err := service.repository.UpdatePlan(ctx, permit.planID, func(record *PlanRecord) error {
			if record.Optimizer == nil || record.Optimizer.ProposalID != permit.proposalID {
				return ErrConflict
			}
			record.Optimizer.ApprovalRevalidationID = permit.revalidationID
			record.Optimizer.ApprovalAcquisitionID = permit.acquisitionID
			return nil
		}); err != nil {
			return ChangeRequest{}, PlanRecord{}, err
		}
	}
	request, err := service.repository.UpdateChangeRequest(ctx, changeRequestID, func(request *ChangeRequest) error {
		if permit != nil && !optimizerEvidenceFreshAt(permit.evaluatedAt, service.now().UTC()) {
			return fmt.Errorf("%w: optimizer approval revalidation expired before the approval write", ErrInvalidTransition)
		}
		if request.Status != ChangePendingApprovals {
			return fmt.Errorf("%w: cannot approve change request in %s", ErrInvalidTransition, request.Status)
		}
		for _, review := range request.Reviews {
			if review.Subject == principal.Username {
				return ErrConflict
			}
		}
		satisfied := 0
		for index := range request.Requirements {
			if _, ok := eligible[request.Requirements[index].ID]; ok {
				request.Requirements[index].SatisfiedBy = append(request.Requirements[index].SatisfiedBy, principal.Username)
				satisfied++
			}
		}
		if satisfied == 0 {
			return ErrForbidden
		}
		request.Reviews = append(request.Reviews, ChangeReview{
			Subject: principal.Username, Decision: "APPROVE", Rationale: rationale,
			Teams: append([]string(nil), principal.Teams...), CreatedAt: service.now().UTC(),
		})
		request.RequirementsSatisfied = allRequirementsSatisfied(request.Requirements)
		if request.RequirementsSatisfied && !hasDoNotMerge(request.Labels) {
			request.Status = ChangeApproved
		}
		request.UpdatedAt = service.now().UTC()
		return nil
	})
	decision := "ALLOW"
	if err != nil {
		decision = "DENY"
	}
	auditErr := service.audit(ctx, principal.Username, "CHANGE_REQUEST_APPROVE", changeRequestID, decision, errorReason(err), nil)
	if err != nil {
		return ChangeRequest{}, PlanRecord{}, err
	}
	if auditErr != nil {
		return ChangeRequest{}, PlanRecord{}, fmt.Errorf("persist approval audit: %w", auditErr)
	}
	plan, err := service.syncPlan(ctx, request)
	if err == nil && request.TargetKind == EnvironmentProfileChangeTarget && request.Status == ChangeApproved {
		profiles := NewEnvironmentProfileService(service.repository)
		profiles.now = service.now
		_, request, err = profiles.ApplyApprovedChangeRequest(ctx, request)
	}
	if err == nil && request.TargetKind == CatalogRetirementChangeTarget && request.Status == ChangeApproved {
		retirements := NewCatalogRetirementService(service.repository)
		retirements.now = service.now
		_, request, err = retirements.ApplyApprovedChangeRequest(ctx, request)
	}
	if err == nil && request.TargetKind == LabelDefinitionChangeTarget && request.Status == ChangeApproved {
		labels := NewLabelDefinitionService(service.repository)
		labels.now = service.now
		_, request, err = labels.ApplyApprovedChangeRequest(ctx, request)
	}
	return request, plan, err
}

func (service *GovernanceService) Reject(
	ctx context.Context, id string, principal Principal, rationale string,
) (ChangeRequest, PlanRecord, error) {
	if strings.TrimSpace(rationale) == "" {
		return ChangeRequest{}, PlanRecord{}, ErrInvalidTransition
	}
	candidate, err := service.repository.GetChangeRequest(ctx, id)
	if err != nil {
		return ChangeRequest{}, PlanRecord{}, err
	}
	if !principalCanReview(ctx, service.repository, candidate.Requirements, principal) {
		service.audit(ctx, principal.Username, "CHANGE_REQUEST_REJECT", id, "DENY", ErrForbidden.Error(), nil)
		return ChangeRequest{}, PlanRecord{}, ErrForbidden
	}
	request, err := service.repository.UpdateChangeRequest(ctx, id, func(request *ChangeRequest) error {
		if request.Status != ChangePendingApprovals {
			return ErrInvalidTransition
		}
		request.Status, request.RequirementsSatisfied = ChangeRejected, false
		request.Reviews = append(request.Reviews, ChangeReview{
			Subject: principal.Username, Decision: "REJECT", Rationale: strings.TrimSpace(rationale),
			Teams: append([]string(nil), principal.Teams...), CreatedAt: service.now().UTC(),
		})
		request.UpdatedAt = service.now().UTC()
		return nil
	})
	decision := "ALLOW"
	if err != nil {
		decision = "DENY"
	}
	service.audit(ctx, principal.Username, "CHANGE_REQUEST_REJECT", id, decision, errorReason(err), nil)
	if err != nil {
		return ChangeRequest{}, PlanRecord{}, err
	}
	plan, err := service.syncPlan(ctx, request)
	return request, plan, err
}

func (service *GovernanceService) Reopen(
	ctx context.Context, id string, principal Principal,
) (ChangeRequest, PlanRecord, error) {
	request, err := service.repository.GetChangeRequest(ctx, id)
	if err != nil {
		return ChangeRequest{}, PlanRecord{}, err
	}
	if request.Author.Username != principal.Username {
		service.audit(ctx, principal.Username, "CHANGE_REQUEST_REOPEN", id, "DENY", "only the author may reopen", nil)
		return ChangeRequest{}, PlanRecord{}, ErrForbidden
	}
	if request.Status != ChangeRejected && request.Status != ChangeCancelled {
		return ChangeRequest{}, PlanRecord{}, ErrInvalidTransition
	}
	reopened, err := service.evaluateChangeRequest(ctx, ChangeRequest{
		PlanID: request.PlanID, TargetKind: request.TargetKind, TargetID: request.TargetID,
		Environment: request.Environment, Title: request.Title, Description: request.Description,
		Author: request.Author, Editors: normalizedStrings(append(request.Editors, principal.Username)),
		Labels: append([]string(nil), request.Labels...), Changes: cloneJSON(request.Changes),
	})
	if err != nil {
		return ChangeRequest{}, PlanRecord{}, err
	}
	reopened.ID, reopened.CreatedAt, reopened.Revision = request.ID, request.CreatedAt, request.Revision
	request, err = service.repository.UpdateChangeRequest(ctx, id, func(current *ChangeRequest) error {
		*current = reopened
		current.UpdatedAt = service.now().UTC()
		return nil
	})
	if err != nil {
		return ChangeRequest{}, PlanRecord{}, err
	}
	service.audit(ctx, principal.Username, "CHANGE_REQUEST_REOPEN", id, "ALLOW", "author reopened request", nil)
	plan, err := service.syncPlan(ctx, request)
	return request, plan, err
}

func (service *GovernanceService) Cancel(
	ctx context.Context, id string, principal Principal, reason string,
) (ChangeRequest, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ChangeRequest{}, ErrInvalidTransition
	}
	request, err := service.repository.UpdateChangeRequest(ctx, id, func(request *ChangeRequest) error {
		if request.Author.Username != principal.Username {
			return ErrForbidden
		}
		if request.Status != ChangePendingApprovals && request.Status != ChangeApproved {
			return ErrInvalidTransition
		}
		request.Status = ChangeCancelled
		request.RequirementsSatisfied = false
		request.Reviews = append(request.Reviews, ChangeReview{
			Subject: principal.Username, Decision: "CANCEL", Rationale: reason,
			Teams: append([]string(nil), principal.Teams...), CreatedAt: service.now().UTC(),
		})
		request.UpdatedAt = service.now().UTC()
		return nil
	})
	decision := "ALLOW"
	if err != nil {
		decision = "DENY"
	}
	service.audit(ctx, principal.Username, "CHANGE_REQUEST_CANCEL", id, decision, errorReason(err), nil)
	return request, err
}

func (service *GovernanceService) SetLabels(
	ctx context.Context, id string, principal Principal, labels []string,
) (ChangeRequest, PlanRecord, error) {
	request, err := service.repository.GetChangeRequest(ctx, id)
	if err != nil {
		return ChangeRequest{}, PlanRecord{}, err
	}
	if request.Author.Username != principal.Username && !principalCanReview(ctx, service.repository, request.Requirements, principal) {
		return ChangeRequest{}, PlanRecord{}, ErrForbidden
	}
	request, err = service.repository.UpdateChangeRequest(ctx, id, func(request *ChangeRequest) error {
		if request.Status == ChangeRejected || request.Status == ChangeCancelled || request.Status == ChangeError {
			return ErrInvalidTransition
		}
		request.Labels = normalizedStrings(labels)
		if request.RequirementsSatisfied && !hasDoNotMerge(request.Labels) {
			request.Status = ChangeApproved
		} else {
			request.Status = ChangePendingApprovals
		}
		request.UpdatedAt = service.now().UTC()
		return nil
	})
	if err != nil {
		return ChangeRequest{}, PlanRecord{}, err
	}
	service.audit(ctx, principal.Username, "CHANGE_REQUEST_LABELS", id, "ALLOW", "labels updated", map[string]any{"labels": labels})
	plan, err := service.syncPlan(ctx, request)
	return request, plan, err
}

func (service *GovernanceService) syncPlan(ctx context.Context, request ChangeRequest) (PlanRecord, error) {
	if request.PlanID == "" {
		return PlanRecord{}, nil
	}
	return service.repository.UpdatePlan(ctx, request.PlanID, func(plan *PlanRecord) error {
		plan.ChangeRequestID = request.ID
		plan.RequiredApprovals = len(request.Requirements)
		plan.Approvals = plan.Approvals[:0]
		for _, review := range request.Reviews {
			if review.Decision == "APPROVE" {
				plan.Approvals = append(plan.Approvals, Approval{Subject: review.Subject, Rationale: review.Rationale, CreatedAt: review.CreatedAt})
			}
		}
		switch request.Status {
		case ChangeApproved:
			if plan.Optimizer != nil && plan.Optimizer.ApprovalRevalidationID == "" {
				plan.Status = PlanAwaitingApproval
				return nil
			}
			if plan.Status == PlanAwaitingApproval || plan.Status == PlanApproved || plan.Status == PlanCancelled {
				plan.Status = PlanApproved
				plan.Cancellation = nil
			}
		case ChangePendingApprovals, ChangeRejected, ChangeCancelled, ChangeError:
			if plan.Status == PlanAwaitingApproval || plan.Status == PlanApproved || (plan.Status == PlanCancelled && request.Status == ChangePendingApprovals) {
				plan.Status = PlanAwaitingApproval
				if request.Status == ChangePendingApprovals {
					plan.Cancellation = nil
				}
			}
		}
		return nil
	})
}

func (service *GovernanceService) planChanges(ctx context.Context, plan Plan, author Principal) ([]EntityChange, error) {
	resourceAuthor := effectivePrincipal(author)
	observed := map[string]ReportedEntity{}
	if report, err := service.repository.GetEnvironmentReport(ctx, plan.Environment); err == nil {
		for _, entity := range report.Entities {
			observed[entity.InstallationID] = entity
		}
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	changes := make([]EntityChange, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		current, exists := observed[step.InstallationID]
		changeType := ChangeTypeCreateEntity
		switch {
		// Ordinary desired-state removal already arrives as an uninstall step.
		// It is a deletion, so it carries the published DELETE_ENTITY type and
		// stays reviewable under the policies that guard install and upgrade.
		case step.Action == planActionUninstall || step.Action == PlanActionUnmanage:
			changeType = ChangeTypeDeleteEntity
		case exists && current.ConfigDigest != step.ConfigDigest:
			changeType = ChangeTypeUpdateEntity
		case exists:
			changeType = ChangeTypeUpdateEntitySettings
		}
		diff := map[string]ChangeDiffEntry{}
		addDiff(diff, "version", current.Version, step.Version, exists)
		addDiff(diff, "releaseChannel", "", step.ReleaseChannel, exists)
		addDiff(diff, "releaseDigest", current.ReleaseDigest, step.ReleaseDigest, exists)
		addDiff(diff, "configDigest", current.ConfigDigest, step.ConfigDigest, exists)
		addDiff(diff, "namespace", current.Namespace, step.Namespace, exists)
		if step.Action == PlanActionUnmanage {
			diff["managed"] = ChangeDiffEntry{Before: true, After: false}
		}
		resources := map[string]ReferencedResource{
			"environment": service.referencedResource(ctx, environmentRID(plan.Environment), plan.Environment, resourceAuthor),
			"product":     service.referencedResource(ctx, productRID(step.Product), step.Product, resourceAuthor),
		}
		if exists || changeType == ChangeTypeDeleteEntity {
			resources["installation"] = service.referencedResource(ctx, installationRID(plan.Environment, step.InstallationID), step.InstallationID, resourceAuthor)
		}
		change := EntityChange{
			ID:   "chg_" + stableShortID(plan.Environment+"\x00"+step.InstallationID+"\x00"+changeType),
			Type: changeType, InstallationID: step.InstallationID, Diff: diff, ReferencedResources: resources,
			Request: map[string]any{
				"apolloEntityId": step.InstallationID, "apolloEntityType": "helm-chart",
				"releaseChannel": step.ReleaseChannel, "configDigest": step.ConfigDigest,
				"unmanageOnly":      step.Action == PlanActionUnmanage,
				"secretCoordinates": reviewedSecretCoordinates(step.SecretAccess),
				"recallIds":         append([]string(nil), step.RecallIDs...), "recallRollOff": step.RecallRollOff,
				"entity": map[string]any{"product": step.Product, "k8sNamespace": step.Namespace, "releaseChannel": step.ReleaseChannel},
			},
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func reviewedSecretCoordinates(accesses []SecretAccess) []map[string]any {
	coordinates := make([]map[string]any, 0, len(accesses))
	for _, access := range accesses {
		coordinates = append(coordinates, map[string]any{
			"requirement": access.Requirement, "assignment": access.Assignment, "revision": access.Revision,
			"kubernetesSecretName": access.KubernetesSecretName, "keys": append([]string(nil), access.Keys...),
		})
	}
	return coordinates
}

func (service *GovernanceService) bundleImportChanges(
	ctx context.Context, envelope BundleEnvelope, environment string, author Principal,
) ([]EntityChange, error) {
	catalog, err := service.repository.GetImportedCatalog(ctx, environment)
	if errors.Is(err, ErrNotFound) {
		catalog = ImportedCatalog{Resources: map[string]BundleResource{}}
	} else if err != nil {
		return nil, err
	}
	resourceAuthor := effectivePrincipal(author)
	changes := []EntityChange{}
	for _, resource := range envelope.Manifest.Resources {
		if resource.Type == "ENVIRONMENT" && resource.ID != environment {
			continue
		}
		key := resource.Type + ":" + resource.ID
		before, existed := catalog.Resources[key]
		diff := map[string]ChangeDiffEntry{}
		if resource.Deleted {
			diff["resource"] = ChangeDiffEntry{Before: before, After: nil}
		} else {
			diff["resource"] = ChangeDiffEntry{Before: func() any {
				if existed {
					return before
				}
				return nil
			}(), After: resource}
		}
		changes = append(changes, EntityChange{
			ID:   "chg_" + stableShortID(environment+"\x00"+key+"\x00"+resource.Digest),
			Type: importChangeTypePrefix + resource.Type, InstallationID: resource.ID,
			Request: map[string]any{"bundleId": envelope.Manifest.ID, "resourceType": resource.Type, "resourceId": resource.ID, "digest": resource.Digest, "deleted": resource.Deleted},
			Metadata: map[string]any{
				MetadataChangeSource: ChangeSourceEnvironmentImport,
				MetadataBundleID:     envelope.Manifest.ID,
			},
			Diff: diff,
			ReferencedResources: map[string]ReferencedResource{
				"environment": service.referencedResource(ctx, environmentRID(environment), environment, resourceAuthor),
			},
		})
	}
	if len(changes) == 0 {
		return nil, ValidationErrors{{Path: "bundle.resources", Message: "no resources apply to target Environment"}}
	}
	return changes, nil
}

func (service *GovernanceService) referencedResource(
	ctx context.Context, rid, name string, author Principal,
) ReferencedResource {
	roles := []string{}
	owners, coOwners := []string{}, []string{}
	grants, _ := effectiveResourceGrants(ctx, service.repository, rid)
	teamSet := stringSet(author.Teams)
	for _, grant := range grants {
		switch grant.Relationship {
		case RelationshipOwner:
			owners = append(owners, grant.Team)
		case RelationshipCoOwner:
			coOwners = append(coOwners, grant.Team)
		}
		if _, member := teamSet[grant.Team]; member {
			roles = append(roles, grant.Roles...)
		}
	}
	resource := map[string]any{"ownerTeams": normalizedStrings(owners), "coOwnerTeams": normalizedStrings(coOwners)}
	if parts := strings.Split(rid, "/"); len(parts) == 2 && parts[0] == "environment" {
		if profile, err := service.repository.GetEnvironmentProfile(ctx, parts[1]); err == nil {
			for key, value := range environmentProfilePolicyResource(profile) {
				resource[key] = value
			}
		}
	}
	return ReferencedResource{
		RID: rid, Name: name, Labels: map[string]string{}, AuthorRoles: normalizedStrings(roles),
		Resource: resource,
	}
}

func (service *GovernanceService) audit(
	ctx context.Context, subject, action, resource, decision, reason string, eventContext map[string]any,
) error {
	now := service.now().UTC()
	identity := sha256.Sum256([]byte(subject + "\x00" + action + "\x00" + resource + "\x00" + decision + "\x00" + now.Format(time.RFC3339Nano)))
	succeeded, failed := []string{}, []string{}
	if decision == "ALLOW" {
		succeeded = []string{resource}
	} else {
		failed = []string{resource}
	}
	event := AuthorizationAuditEvent{
		ID: "aud_" + hex.EncodeToString(identity[:16]), SchemaVersion: AuditSchemaV3,
		Categories: []string{AuditCategoryAuthorizationCheck}, TraceID: resource,
		Subject: subject, Action: action, ResourceRID: resource, Decision: decision, Reason: reason,
		RequestFields: map[string]any{
			"authorizationCheckTargets":    []string{resource},
			"authorizationCheckOperations": []string{action},
		},
		ResultFields: map[string]any{
			"authorizationCheckSucceededTargets": succeeded,
			"authorizationCheckFailedTargets":    failed,
			"authorizationCheckResultMessage":    reason,
		},
		Context: eventContext, CreatedAt: now,
	}
	if decision == "ALLOW" {
		switch action {
		case "CHANGE_REQUEST_APPROVE":
			event.Categories = append(event.Categories, AuditCategoryRequestApprove)
			event.RequestFields["approvedRequestIds"] = []string{resource}
			event.RequestFields["approveRequestUserId"] = subject
		case "CHANGE_REQUEST_REJECT":
			event.Categories = append(event.Categories, AuditCategoryRequestDisapprove)
			event.RequestFields["disapprovedRequestIds"] = []string{resource}
			event.RequestFields["disapproveRequestUserId"] = subject
		case "CHANGE_REQUEST_CANCEL":
			event.Categories = append(event.Categories, AuditCategoryRequestCancel)
			event.RequestFields["canceledRequestIds"] = []string{resource}
		case "CHANGE_REQUEST_REOPEN", "CHANGE_REQUEST_LABELS":
			event.Categories = append(event.Categories, AuditCategoryRequestUpdate)
			event.RequestFields["updatedRequestIds"] = []string{resource}
		}
	}
	event.Categories = normalizedAuditCategories(event.Categories)
	if err := ValidateAuditCategoryEvent(event); err != nil {
		return err
	}
	_, err := service.repository.AppendAuthorizationAudit(ctx, event)
	return err
}

func (service *GovernanceService) auditRequestCreated(ctx context.Context, request ChangeRequest) {
	now := service.now().UTC()
	identity := sha256.Sum256([]byte("requestCreate\x00" + request.ID + "\x00" + now.Format(time.RFC3339Nano)))
	event := AuthorizationAuditEvent{
		ID: "aud_" + hex.EncodeToString(identity[:16]), SchemaVersion: AuditSchemaV3,
		Categories: []string{AuditCategoryRequestCreate}, TraceID: request.ID,
		Subject: request.Author.Username, Action: "CHANGE_REQUEST_CREATE", ResourceRID: request.ID,
		Decision: "ALLOW", Reason: "change request persisted",
		RequestFields: map[string]any{
			"createdRequestAffectedResources": []string{request.TargetID},
			"createdRequestDescription":       request.Description,
		},
		ResultFields: map[string]any{"createdRequestIds": []string{request.ID}}, CreatedAt: now,
	}
	if ValidateAuditCategoryEvent(event) == nil {
		_, _ = service.repository.AppendAuthorizationAudit(ctx, event)
	}
}

type policyResult struct {
	Kind, Reason, Team, ResourceRID, Role, Operation string
	Alternatives                                     []policyResult
}

func (result policyResult) Requirement(changeID, policyID string) ReviewerRequirement {
	requirement := ReviewerRequirement{
		ChangeID: changeID, PolicyID: policyID, Kind: result.Kind, Team: result.Team,
		ResourceRID: result.ResourceRID, Role: result.Role, Operation: result.Operation, Reason: result.Reason,
	}
	for _, alternative := range result.Alternatives {
		requirement.Alternatives = append(requirement.Alternatives, alternative.Requirement(changeID, policyID))
	}
	payload, _ := json.Marshal(requirement)
	digest := sha256.Sum256(payload)
	requirement.ID = "req_" + hex.EncodeToString(digest[:16])
	return requirement
}

func compilePolicy(policy ApprovalPolicy, trust PolicyTrust) (cel.Program, error) {
	environment, err := policyEnvironment(trust)
	if err != nil {
		return nil, err
	}
	expression := policyVariablePattern.ReplaceAllString(policy.Expression, `policyVariable.$1`)
	ast, issues := environment.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}
	return environment.Program(ast)
}

func evaluatePolicy(policy ApprovalPolicy, input map[string]any, trust PolicyTrust) (policyResult, error) {
	program, err := compilePolicy(policy, trust)
	if err != nil {
		return policyResult{}, err
	}
	variables := make(map[string]any, len(policy.Variables))
	for name, variable := range policy.Variables {
		switch variable.Type {
		case PolicyString:
			variables[name] = variable.String
		case PolicyStringList:
			variables[name] = append([]string(nil), variable.List...)
		default:
			return policyResult{}, fmt.Errorf("variable %s has unsupported type %s", name, variable.Type)
		}
	}
	input["policyVariable"] = variables
	value, _, err := program.Eval(input)
	if err != nil {
		return policyResult{}, err
	}
	native, err := value.ConvertToNative(reflect.TypeOf(""))
	if err != nil {
		return policyResult{}, fmt.Errorf("policy must return a result function: %w", err)
	}
	var normalized map[string]any
	if err := json.Unmarshal([]byte(native.(string)), &normalized); err != nil {
		return policyResult{}, fmt.Errorf("normalize policy result: %w", err)
	}
	return decodePolicyResult(normalized)
}

func policyEnvironment(trust PolicyTrust) (*cel.Env, error) {
	result := func(kind string, args ...ref.Val) ref.Val {
		value := map[string]any{"kind": kind}
		if len(args) > 0 {
			value["reason"] = fmt.Sprint(args[len(args)-1].Value())
		}
		switch kind {
		case RequirementTeam:
			value["team"] = fmt.Sprint(args[0].Value())
		case RequirementRole:
			value["resourceRid"], value["role"] = fmt.Sprint(args[0].Value()), fmt.Sprint(args[1].Value())
		case RequirementOperation:
			value["resourceRid"], value["operation"] = fmt.Sprint(args[0].Value()), fmt.Sprint(args[1].Value())
		case RequirementAny:
			alternatives, err := args[0].ConvertToNative(reflect.TypeOf([]any{}))
			if err != nil {
				return types.NewErr("invalid require_any alternatives: %v", err)
			}
			decoded := make([]map[string]any, 0, len(alternatives.([]any)))
			for _, alternative := range alternatives.([]any) {
				var candidate map[string]any
				if err := json.Unmarshal([]byte(fmt.Sprint(alternative)), &candidate); err != nil {
					return types.NewErr("invalid require_any alternative: %v", err)
				}
				decoded = append(decoded, candidate)
			}
			value["alternatives"] = decoded
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return types.NewErr("encode policy result: %v", err)
		}
		return types.String(encoded)
	}
	options := []cel.EnvOption{
		cel.Variable("type", cel.StringType), cel.Variable("author", cel.DynType), cel.Variable("onBehalfOf", cel.DynType),
		cel.Variable("editors", cel.ListType(cel.StringType)), cel.Variable("metadata", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("request", cel.DynType), cel.Variable("diff", cel.DynType), cel.Variable("referencedResources", cel.DynType),
		cel.Variable("createdAt", cel.TimestampType), cel.Variable("now", cel.TimestampType), cel.Variable("policyVariable", cel.DynType),
		cel.Function("approve", cel.Overload("approve_string", []*cel.Type{cel.StringType}, cel.DynType,
			cel.UnaryBinding(func(arg ref.Val) ref.Val { return result("APPROVE", arg) }))),
		cel.Function("reject", cel.Overload("reject_string", []*cel.Type{cel.StringType}, cel.DynType,
			cel.UnaryBinding(func(arg ref.Val) ref.Val { return result("REJECT", arg) }))),
		cel.Function("no_op", cel.Overload("no_op", []*cel.Type{}, cel.DynType,
			cel.FunctionBinding(func(args ...ref.Val) ref.Val { return result("NO_OP", args...) }))),
		cel.Function("require_team", cel.Overload("require_team_strings", []*cel.Type{cel.StringType, cel.StringType}, cel.DynType,
			cel.BinaryBinding(func(left, right ref.Val) ref.Val { return result(RequirementTeam, left, right) }))),
		cel.Function("require_role", cel.Overload("require_role_strings", []*cel.Type{cel.StringType, cel.StringType, cel.StringType}, cel.DynType,
			cel.FunctionBinding(func(args ...ref.Val) ref.Val { return result(RequirementRole, args...) }))),
		cel.Function("require_operation", cel.Overload("require_operation_strings", []*cel.Type{cel.StringType, cel.StringType, cel.StringType}, cel.DynType,
			cel.FunctionBinding(func(args ...ref.Val) ref.Val { return result(RequirementOperation, args...) }))),
		cel.Function("require_any", cel.Overload("require_any_list_string", []*cel.Type{cel.ListType(cel.DynType), cel.StringType}, cel.DynType,
			cel.BinaryBinding(func(left, right ref.Val) ref.Val { return result(RequirementAny, left, right) }))),
	}
	return cel.NewEnv(append(options, policyPredicates(trust)...)...)
}

func decodePolicyResult(value map[string]any) (policyResult, error) {
	result := policyResult{Kind: stringValue(value, "kind"), Reason: stringValue(value, "reason")}
	result.Team, result.ResourceRID = stringValue(value, "team"), stringValue(value, "resourceRid")
	result.Role, result.Operation = stringValue(value, "role"), stringValue(value, "operation")
	if result.Kind == RequirementAny {
		alternatives, ok := value["alternatives"].([]any)
		if !ok {
			return policyResult{}, errors.New("require_any alternatives are invalid")
		}
		for _, raw := range alternatives {
			candidate, ok := raw.(map[string]any)
			if !ok {
				return policyResult{}, errors.New("require_any contains an invalid requirement")
			}
			decoded, err := decodePolicyResult(candidate)
			if err != nil {
				return policyResult{}, err
			}
			result.Alternatives = append(result.Alternatives, decoded)
		}
	}
	if !contains([]string{"APPROVE", "REJECT", "NO_OP", RequirementTeam, RequirementRole, RequirementOperation, RequirementAny}, result.Kind) {
		return policyResult{}, fmt.Errorf("unsupported policy result %q", result.Kind)
	}
	return result, nil
}

func stringValue(value map[string]any, key string) string {
	if value[key] == nil {
		return ""
	}
	return fmt.Sprint(value[key])
}

func policyInput(change EntityChange, author Principal, editors []string, now time.Time) map[string]any {
	resources := make(map[string]any, len(change.ReferencedResources))
	for name, resource := range change.ReferencedResources {
		resources[name] = map[string]any{
			"rid": resource.RID, "name": resource.Name, "labels": resource.Labels,
			"authorRoles": resource.AuthorRoles, "resource": resource.Resource,
		}
	}
	diff := make(map[string]any, len(change.Diff))
	for path, entry := range change.Diff {
		diff[path] = map[string]any{"before": entry.Before, "after": entry.After}
	}
	onBehalfOf := map[string]any{}
	if author.OnBehalfOf != nil {
		onBehalfOf = map[string]any{
			"username": author.OnBehalfOf.Username, "teams": author.OnBehalfOf.Teams,
			"serviceAccount": author.OnBehalfOf.ServiceAccount,
		}
	}
	metadata := change.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	return map[string]any{
		"type":       change.Type,
		"author":     map[string]any{"username": author.Username, "teams": author.Teams, "serviceAccount": author.ServiceAccount},
		"onBehalfOf": onBehalfOf, "editors": editors, "metadata": metadata, "request": change.Request,
		"diff": diff, "referencedResources": resources, "createdAt": now, "now": now,
	}
}

func requirementSatisfied(ctx context.Context, repository Repository, requirement ReviewerRequirement, principal Principal) bool {
	switch requirement.Kind {
	case RequirementTeam:
		return contains(principal.Teams, requirement.Team)
	case RequirementRole, RequirementOperation:
		grants, err := effectiveResourceGrants(ctx, repository, requirement.ResourceRID)
		if err != nil {
			return false
		}
		teams := stringSet(principal.Teams)
		for _, grant := range grants {
			if _, member := teams[grant.Team]; !member {
				continue
			}
			if requirement.Kind == RequirementRole && contains(grant.Roles, requirement.Role) {
				return true
			}
			if requirement.Kind == RequirementOperation &&
				(contains(grant.Operations, requirement.Operation) || rolesAllow(grant.Roles, requirement.Operation)) {
				return true
			}
		}
	case RequirementAny:
		for _, candidate := range requirement.Alternatives {
			if requirementSatisfied(ctx, repository, candidate, principal) {
				return true
			}
		}
	}
	return false
}

func principalCanReview(ctx context.Context, repository Repository, requirements []ReviewerRequirement, principal Principal) bool {
	for _, requirement := range requirements {
		if requirementSatisfied(ctx, repository, requirement, principal) {
			return true
		}
	}
	return false
}

func allRequirementsSatisfied(requirements []ReviewerRequirement) bool {
	for _, requirement := range requirements {
		if len(requirement.SatisfiedBy) == 0 {
			return false
		}
	}
	return true
}

func addDiff(diff map[string]ChangeDiffEntry, path string, before, after any, existed bool) {
	if !existed || !reflect.DeepEqual(before, after) {
		entry := ChangeDiffEntry{After: after}
		if existed {
			entry.Before = before
		}
		diff[path] = entry
	}
}

func findGrant(ctx context.Context, repository Repository, id string) *ResourceGrant {
	grants, err := repository.ListResourceGrants(ctx, "")
	if err != nil {
		return nil
	}
	for _, grant := range grants {
		if grant.ID == id {
			copy := grant
			return &copy
		}
	}
	return nil
}

func normalizedStrings(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func intersects(set map[string]struct{}, values []string) bool {
	for _, value := range values {
		if _, exists := set[value]; exists {
			return true
		}
	}
	return false
}

func hasDoNotMerge(labels []string) bool { return contains(labels, "Do not merge") }

func environmentRID(environment string) string { return "environment/" + environment }
func productRID(product string) string         { return "product/" + product }
func moduleRID(module string) string           { return "module/" + module }
func installationRID(environment, installation string) string {
	return "installation/" + environment + "/" + installation
}
func stableShortID(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:16])
}
func errorReason(err error) string {
	if err == nil {
		return "authorized"
	}
	return err.Error()
}

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
)

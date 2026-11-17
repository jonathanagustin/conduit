package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
)

const (
	ChangePendingApprovals = "PENDING_APPROVALS"
	ChangeApproved         = "APPROVED"
	ChangeRejected         = "REJECTED"
	ChangeCancelled        = "CANCELLED"
	ChangeError            = "ERROR"
)

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
	UpdateChangeRequestAndPlan(context.Context, string, bool, func(*ChangeRequest, *PlanRecord) error) (ChangeRequest, PlanRecord, error)
	AppendAuthorizationAudit(context.Context, AuthorizationAuditEvent) (AuthorizationAuditEvent, error)
	ListAuthorizationAudit(context.Context, string) ([]AuthorizationAuditEvent, error)
	PageAuthorizationAudit(context.Context, AuthorizationAuditPageQuery) (AuthorizationAuditPage, error)
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
	// PriorReviews holds the reviews of every earlier round, oldest first.
	//
	// Reopening rebuilds the record by re-evaluating against the policies in
	// force now, which necessarily discards Reviews -- a reviewer who approved
	// the old evaluation did not approve this one. But a REJECTED request's
	// reviews carry the rationale a human wrote for refusing it, and that was
	// the only copy: reopening deleted the answer to "why was this refused
	// before?" precisely when someone had decided to try again (#103).
	//
	// Kept separate from Reviews rather than left in it, because approveOnce
	// (below) walks Reviews to refuse a second approval from one subject. A
	// carried-forward approval must not satisfy the new round, and must not
	// block the same reviewer from approving it either.
	PriorReviews []ChangeReview   `json:"priorReviews,omitempty"`
	Execution    *ChangeExecution `json:"execution,omitempty"`
	Error        string           `json:"error,omitempty"`
	CreatedAt    time.Time        `json:"createdAt"`
	UpdatedAt    time.Time        `json:"updatedAt"`
	Revision     uint64           `json:"revision"`
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
	// announcements is zero unless SetAnnouncements was called. A service built
	// over a transaction's repository never has it set, which is what keeps an
	// announcement from leaving before its write commits.
	announcements governanceAnnouncements
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

// Authorize asks Governance, rather than answering for it. The rule about what
// a grant conveys has one home, and it is not this package.
func (service *GovernanceService) Authorize(ctx context.Context, principal Principal, resourceRID, operation string) bool {
	return governanceAuthorizer(service.repository).Allows(ctx, principal, resourceRID, operation)
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

// PutGrant writes a grant without recording who wrote it. It exists for
// fixtures, which is the only thing that calls it: every production path goes
// through PutGrantAs, because a permission change nobody is named for is the
// gap this pair was built to close.
func (service *GovernanceService) PutGrant(ctx context.Context, grant ResourceGrant) (ResourceGrant, error) {
	return service.PutGrantAs(ctx, grant, "")
}

// PutGrantAs writes a grant and records what it became.
//
// The authorization decision is already audited before this is reached:
// requireResourceOperation emits an authorizationCheck for permissions:manage
// on the resource, ALLOW or DENY. What that event cannot say is what the grant
// changed to, and the record does not keep the answer either -- a grant is
// overwritten in place with Revision+1 and no history retained. So "who granted
// the approver role that let this change through" was unanswerable, which is a
// hole exactly where someone would edit.
//
// configureInfra rather than a new category: the audit vocabulary is a bounded
// subset of audit.3 and unknown categories fail closed, so inventing a name
// Foundry does not publish would be the wrong move. Configuring who holds what
// on a resource is what that category already means.
//
// Roles and operations are recorded before as well as after, and the before
// half is the load-bearing one. An event saying only what a grant became cannot
// answer whether an operation was added or removed, and removal is the
// direction someone covering their tracks would use.
func (service *GovernanceService) PutGrantAs(
	ctx context.Context, grant ResourceGrant, subject string,
) (ResourceGrant, error) {
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
	stored, err := service.repository.PutResourceGrant(ctx, grant)
	if err != nil || subject == "" {
		return stored, err
	}
	// Emitted after the write, so the record cannot claim a change that did not
	// land. A failure to audit is returned rather than swallowed: a permission
	// change that succeeded while its record did not is the state this exists to
	// prevent, and reporting success would restore the gap in a quieter form.
	if auditErr := service.auditGrantConfigured(ctx, subject, existing, stored); auditErr != nil {
		return ResourceGrant{}, auditErr
	}
	service.announceGrantChanged(ctx, subject, existing, stored)
	return stored, nil
}

// auditGrantConfigured records the content of a permission change. The subject
// is the acting principal rather than the team the grant names -- the team is
// who gained authority, and the question the audit trail could not answer was
// who conferred it.
func (service *GovernanceService) auditGrantConfigured(
	ctx context.Context, subject string, before *ResourceGrant, after ResourceGrant,
) error {
	now := service.now().UTC()
	identity := sha256.Sum256([]byte("configureInfra\x00" + after.ID + "\x00" +
		strconv.FormatUint(after.Revision, 10) + "\x00" + now.Format(time.RFC3339Nano)))
	previous := map[string]any{"roles": []string{}, "operations": []string{}, "relationship": "", "revision": uint64(0)}
	if before != nil {
		previous = map[string]any{
			"roles": before.Roles, "operations": before.Operations,
			"relationship": before.Relationship, "revision": before.Revision,
		}
	}
	event := AuthorizationAuditEvent{
		ID: "aud_" + hex.EncodeToString(identity[:16]), SchemaVersion: AuditSchemaV3,
		Categories: []string{AuditCategoryConfigureInfra}, TraceID: after.ResourceRID,
		Subject: subject, Action: "RESOURCE_GRANT_CONFIGURE", ResourceRID: after.ResourceRID,
		Decision: "ALLOW", Reason: "resource grant written",
		RequestFields: map[string]any{
			"configureInfraTargets": []string{after.ResourceRID},
			"grantTeam":             after.Team,
			"grantBefore":           previous,
			"grantAfter": map[string]any{
				"roles": after.Roles, "operations": after.Operations,
				"relationship": after.Relationship, "revision": after.Revision,
			},
		},
		ResultFields: map[string]any{"configureInfraRequestId": after.ID},
		CreatedAt:    now,
	}
	if err := ValidateAuditCategoryEvent(event); err != nil {
		return err
	}
	_, err := service.repository.AppendAuthorizationAudit(ctx, event)
	return err
}

func (service *GovernanceService) PutPolicy(ctx context.Context, policy ApprovalPolicy) (ApprovalPolicy, error) {
	if !identifierPattern.MatchString(policy.ID) || strings.TrimSpace(policy.DisplayName) == "" || strings.TrimSpace(policy.Expression) == "" {
		return ApprovalPolicy{}, ValidationErrors{{Path: "policy", Message: "id, displayName, and expression are required"}}
	}
	if _, err := governance.CompilePolicy(policy, service.trust); err != nil {
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
	return service.proposePlanWithAdditionalChanges(ctx, state, author, title, description, optimizer, nil)
}

func (service *GovernanceService) proposePlanWithAdditionalChanges(
	ctx context.Context, state DesiredState, author Principal, title, description string, optimizer *OptimizerPlanProvenance, additional []EntityChange,
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
	request, err := service.evaluatePlanWithAdditionalChanges(ctx, record, author, title, description, additional)
	if err != nil {
		return PlanRecord{}, ChangeRequest{}, err
	}
	// The verdict this call computed, read before CreateChangeRequest may hand
	// back a record stored under an earlier policy. A pln_ id is a digest of the
	// desired state, so re-proposing state that has already been proposed
	// collides -- and supersedePendingPlans cancels approved Plans without
	// touching their Change Requests, so "Plan CANCELLED, Change Request
	// APPROVED" is a routine state rather than a partial failure. Synchronizing
	// the stale APPROVED record onto it clears the cancellation and makes the
	// Plan executable again, which is the same replay ProposeChanges refuses,
	// aimed at deployments instead of pipelines.
	verdict := request.Status
	request, err = service.repository.CreateChangeRequest(ctx, request)
	if err != nil {
		return PlanRecord{}, ChangeRequest{}, err
	}
	service.auditRequestCreated(ctx, request)
	record, err = service.syncPlanAs(ctx, request, verdict)
	if err == nil {
		service.announceAwaitingApproval(ctx, request)
	}
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

func (service *GovernanceService) evaluatePlanWithAdditionalChanges(
	ctx context.Context, record PlanRecord, author Principal, title, description string, additional []EntityChange,
) (ChangeRequest, error) {
	changes, err := service.planChanges(ctx, record.Plan, author)
	if err != nil {
		return ChangeRequest{}, err
	}
	changes = append(changes, cloneJSON(additional)...)
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

// ProposeChanges also reports whether the policy in force approves the proposal
// it was just given, which is not answerable from the returned record.
//
// Every caller here passes a natural key as TargetID -- a pipeline id, an
// import record id, a label target -- and a Change Request id is a digest of
// the target alone, so a second proposal collides with the first and receives
// that record back. The evaluation this call performed, carrying this author's
// content and the verdict of the policy in force now, is discarded. An APPROVED
// record is therefore a pre-approval token that outlives the policy that
// approved it: a caller applying on the record's status alone performs actions
// the current policy rejects.
//
// The second return is the current verdict, and it is deliberately not "did
// this call create the record". Creation is the wrong question -- archiving a
// pipeline, unarchiving it and archiving it again is an ordinary sequence whose
// second archive collides, and gating on creation would refuse it under a
// policy that has not changed at all, leaving no route that can perform it.
//
// It is not an error to receive the stored record: reading it is the whole
// point of an idempotent re-proposal. What is refused is acting on a decision
// nobody is taking now.
func (service *GovernanceService) ProposeChanges(
	ctx context.Context, targetKind, targetID, environment string, changes []EntityChange,
	author Principal, title, description string,
) (ChangeRequest, bool, error) {
	if strings.TrimSpace(targetKind) == "" || strings.TrimSpace(targetID) == "" ||
		strings.TrimSpace(environment) == "" || len(changes) == 0 || strings.TrimSpace(author.Username) == "" {
		return ChangeRequest{}, false, ValidationErrors{{Path: "changeRequest", Message: "target, Environment, changes, and author are required"}}
	}
	taxonomyErrors := ValidationErrors{}
	for index, change := range changes {
		taxonomyErrors = append(taxonomyErrors, validateChangeType(index, change)...)
	}
	if len(taxonomyErrors) > 0 {
		return ChangeRequest{}, false, taxonomyErrors
	}
	request, err := service.evaluateChangeRequest(ctx, ChangeRequest{
		TargetKind: targetKind, TargetID: targetID, Environment: environment,
		Title: title, Description: description, Author: author, Changes: cloneJSON(changes),
	})
	if err != nil {
		return ChangeRequest{}, false, err
	}
	// Read before the create call, which may replace the record with a stored
	// one carrying a different verdict.
	policyApproves := request.Status == ChangeApproved
	request, err = service.repository.CreateChangeRequest(ctx, request)
	if err == nil {
		service.auditRequestCreated(ctx, request)
		service.announceAwaitingApproval(ctx, request)
	}
	return request, policyApproves, err
}

// PreviewChanges runs the same taxonomy check and policy evaluation as
// ProposeChanges and does not store the request. A caller that wants the
// record still calls ProposeChanges, which is the path that can persist one
// and the path that does not approve it on the caller's behalf.
func (service *GovernanceService) PreviewChanges(
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
	return service.evaluateChangeRequest(ctx, ChangeRequest{
		TargetKind: targetKind, TargetID: targetID, Environment: environment,
		Title: title, Description: description, Author: author, Changes: cloneJSON(changes),
	})
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
		return ChangeRequest{}, PlanRecord{}, fmt.Errorf("%w: approval rationale is required", ErrInvalidTransition)
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
	// Separation of duties. A reviewer requirement asks for a second party, and
	// nothing here checked that it got one: eligibility was decided purely by
	// "is this principal in the required team, or holding the required role",
	// and the author is usually in the team that owns the resource. The only
	// identity guard was the duplicate-review check below, which stops the same
	// subject approving twice and never stopped the author approving once.
	//
	// Refused rather than made configurable, because the configuration already
	// exists a layer up at the time it can be stated honestly: a deployment
	// wanting no independent review writes a policy that raises no requirement,
	// and evaluateChangeRequest approves at proposal time with the evaluation
	// recorded. A self-approval switch would instead let a reviewer requirement
	// appear to exist while being void, which is worse than not raising one.
	authorApproving := governance.SameHuman(principal, candidate.Author)
	eligible := map[string]struct{}{}
	for _, requirement := range candidate.Requirements {
		// The optimizer's revalidation requirement is exempt, and it is the only
		// one that can be. It is not policy-raised -- evaluatePlan appends it
		// unconditionally and no policy can waive it -- and it asks for
		// machine-verified evidence freshness rather than for a second party:
		// "fresh optimizer evidence is required at approval". Reviewer
		// independence for an optimizer change is carried by the other
		// requirements, which this branch does not touch. Excluding the author
		// here would leave an operator permanently unable to present the
		// revalidation receipt for their own plan even where policy raised no
		// reviewer at all -- wedged by a rule meant to require a reviewer that
		// nothing had asked for.
		if permit != nil && requirement.Kind == "OPTIMIZER_REVALIDATION" {
			eligible[requirement.ID] = struct{}{}
			continue
		}
		if authorApproving {
			continue
		}
		if requirementSatisfied(ctx, service.repository, requirement, principal) {
			eligible[requirement.ID] = struct{}{}
		}
	}
	if len(eligible) == 0 {
		// Named rather than folded into the generic refusal: "forbidden" sends
		// an author looking for a grant they will never be given, and this
		// audit record is what an operator reads to find out which rule
		// stopped them.
		refusal := ErrForbidden
		if authorApproving {
			refusal = fmt.Errorf("%w: the author of a change request cannot approve it", ErrForbidden)
		}
		// Audited here rather than only by the caller, because the
		// non-transactional branch of approveTransactional returns this error
		// directly and never reaches the wrapper's DENY audit.
		service.audit(ctx, principal.Username, "CHANGE_REQUEST_APPROVE", changeRequestID, "DENY", refusal.Error(), nil)
		return ChangeRequest{}, PlanRecord{}, refusal
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
		// The same rule again, against the serialized record rather than the
		// copy read before the update, and stated as the invariant it is: the
		// author reaches this write only through an optimizer revalidation
		// permit. Author is immutable on every path that exists today -- Reopen
		// carries it forward and nothing else writes it -- so this cannot fire
		// now. It costs one comparison and it is what a future mutation path
		// would have to get past.
		if permit == nil && governance.SameHuman(principal, request.Author) {
			return fmt.Errorf("%w: the author of a change request cannot approve it", ErrForbidden)
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
		reviewedAt := service.now().UTC()
		if err := recordModuleOverrideReviewer(request, principal, eligible, reviewedAt); err != nil {
			return err
		}
		request.Reviews = append(request.Reviews, ChangeReview{
			Subject: principal.Username, Decision: "APPROVE", Rationale: rationale,
			Teams: append([]string(nil), principal.Teams...), CreatedAt: reviewedAt,
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
		return ChangeRequest{}, PlanRecord{}, fmt.Errorf("%w: rejection rationale is required", ErrInvalidTransition)
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
	if request.TargetKind == ModuleOverrideTarget {
		return ChangeRequest{}, PlanRecord{}, fmt.Errorf("%w: create a fresh Module override proposal instead of reopening an expired binding", ErrInvalidTransition)
	}
	if request.Author.Username != principal.Username {
		service.audit(ctx, principal.Username, "CHANGE_REQUEST_REOPEN", id, "DENY", "only the author may reopen", nil)
		return ChangeRequest{}, PlanRecord{}, ErrForbidden
	}
	// ERROR is admitted alongside the two decisions because it is not one. A
	// request reaches ERROR only from evaluateChangeRequest -- a policy that
	// failed to evaluate, every policy returning no_op, or no enabled policies
	// at all -- so nobody vetoed anything and there is no judgement to overrule.
	// Reject refuses any status but PENDING_APPROVALS, so an errored record's
	// Reviews are empty and reopening overwrites machine output only. That
	// reasoning holds for ERROR and for nothing else: a REJECTED record's
	// Reviews carry a human's rationale, which is why they are carried into
	// PriorReviews below rather than discarded with the evaluation.
	//
	// Excluding it was the worse half of a wedge. A crq_ id is derived from its
	// target, so the errored record occupies that target permanently and every
	// later proposer receives it back; with Reopen closed, a deployment that
	// proposed before its approval policies existed burnt every target it
	// touched, one target at a time, with no recourse open to anyone at all --
	// not even the author, who is the only principal Reopen admits.
	//
	// Reopening re-evaluates against the policies in force now, so if the
	// configuration is still broken this errors again and costs nothing.
	if request.Status != ChangeRejected && request.Status != ChangeCancelled &&
		request.Status != ChangeError {
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
	// Oldest first, so a request reopened twice reads as a history rather than
	// as a set. evaluateChangeRequest built `reopened` from scratch and knows
	// nothing about earlier rounds, so this is the only place the chain can be
	// joined up.
	reopened.PriorReviews = append(append([]ChangeReview(nil), request.PriorReviews...), request.Reviews...)
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
	if err == nil {
		service.announceAwaitingApproval(ctx, request)
	}
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

// SetLabels reports whether this call is the one that moved the request into
// APPROVED, which is not the same question as whether it now reads APPROVED.
//
// Labelling is the only review route that can be called on an already-approved
// request and leave it approved: approveOnce refuses any status but
// PENDING_APPROVALS, and Reject, Cancel and Reopen each leave a status that is
// not APPROVED. Its caller applies approved changes, so without the
// distinction, labelling an old approved record performs its action again --
// under whatever policy is in force now rather than the one that approved it,
// and with no decision taken by anyone.
//
// A genuine PENDING_APPROVALS to APPROVED transition here is a real approval:
// it is what removing a do-not-merge label means, and it still applies.
func (service *GovernanceService) SetLabels(
	ctx context.Context, id string, principal Principal, labels []string,
) (ChangeRequest, PlanRecord, bool, error) {
	return service.transformLabels(ctx, id, principal, func([]string) []string { return labels }, false)
}

func (service *GovernanceService) syncPlan(ctx context.Context, request ChangeRequest) (PlanRecord, error) {
	if request.PlanID == "" {
		return PlanRecord{}, nil
	}
	_, plan, err := service.repository.UpdateChangeRequestAndPlan(ctx, request.ID, false, func(current *ChangeRequest, plan *PlanRecord) error {
		if plan == nil {
			return ErrNotFound
		}
		return projectChangeRequestPlan(plan, *current, current.Status)
	})
	return plan, err
}

// syncPlanAs projects a proposal under its explicitly evaluated verdict. An
// existing stored request may reflect a different policy round, so proposal
// cannot simply inherit its status. Ordinary reviews use syncPlan, which reads
// the current stored request atomically with the Plan update.
func (service *GovernanceService) syncPlanAs(ctx context.Context, request ChangeRequest, verdict string) (PlanRecord, error) {
	if request.PlanID == "" {
		return PlanRecord{}, nil
	}
	return service.repository.UpdatePlan(ctx, request.PlanID, func(plan *PlanRecord) error { return projectChangeRequestPlan(plan, request, verdict) })
}

func projectChangeRequestPlan(plan *PlanRecord, request ChangeRequest, verdict string) error {
	plan.ChangeRequestID = request.ID
	plan.RequiredApprovals = len(request.Requirements)
	plan.Approvals = plan.Approvals[:0]
	for _, review := range request.Reviews {
		if review.Decision == "APPROVE" {
			plan.Approvals = append(plan.Approvals, Approval{Subject: review.Subject, Rationale: review.Rationale, CreatedAt: review.CreatedAt})
		}
	}
	switch verdict {
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
		if plan.Status == PlanAwaitingApproval || plan.Status == PlanApproved || (plan.Status == PlanCancelled && verdict == ChangePendingApprovals) {
			plan.Status = PlanAwaitingApproval
			if verdict == ChangePendingApprovals {
				plan.Cancellation = nil
			}
		}
	}
	return nil
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
	// The base record is Governance's, so every surface that files one files the
	// same shape; what is added below is this package's Change Request
	// vocabulary, which no other surface emits.
	event := governance.NewAuthorizationCheckEvent(
		service.now().UTC(), subject, action, resource, decision, reason, eventContext)
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

var ErrForbidden = errors.New("forbidden")

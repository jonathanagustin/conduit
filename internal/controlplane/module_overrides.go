package controlplane

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	overrideapp "github.com/jonathanagustin/conduit/internal/installoverride/application"
	override "github.com/jonathanagustin/conduit/internal/installoverride/domain"
	overrideports "github.com/jonathanagustin/conduit/internal/installoverride/ports"
)

const ModuleOverrideTarget = "CONDUIT_NATIVE_MODULE_OVERRIDE"
const OperationApproveModuleOverride = governance.OperationApproveModuleOverride

// ModuleOverrideService binds native review to existing Module metadata writes.
// CONDUIT-INVENTED: installoverride.ModuleMetadataAdmission
// Displaced-by: a published or sanitized Module override contract establishing metadata admission, independent reviewer authority, exact release/state binding and audited intent-only secret semantics.
type ModuleOverrideService struct {
	repository Repository
	options    HandlerOptions
	actor      Principal
}

func NewModuleOverrideService(repository Repository, options HandlerOptions, actor Principal) *ModuleOverrideService {
	return &ModuleOverrideService{repository, options, cloneJSON(actor)}
}

func (s *ModuleOverrideService) authority(draft overrideports.ModuleDraft) *moduleOverrideAuthority {
	return &moduleOverrideAuthority{repository: s.repository, options: s.options, actor: s.actor, draft: cloneJSON(draft)}
}

func (s *ModuleOverrideService) ProposeModuleOverride(ctx context.Context, draft overrideports.ModuleDraft, dryRun bool) (overrideports.ModuleReview, error) {
	a := s.authority(draft)
	canonical, err := a.canonicalDraft(ctx)
	if err != nil {
		return overrideports.ModuleReview{}, err
	}
	a.draft = canonical
	if dryRun {
		snapshot, identity, err := a.ResolveProposal(ctx, a.selector())
		if err != nil {
			return overrideports.ModuleReview{}, err
		}
		now := time.Now().UTC()
		p, err := override.NewProposal(snapshot, canonical.Flags, identity, canonical.Rationale, now, now.Add(override.MaximumReviewLifetime))
		return overrideports.ModuleReview{Status: "PREVIEW", Proposal: p, DryRun: true, SecretRequirementsEffect: "INTENT_ONLY", ValueTier: override.Native}, err
	}
	kernel, err := overrideapp.New(a)
	if err != nil {
		return overrideports.ModuleReview{}, err
	}
	result, err := kernel.Propose(ctx, overrideapp.Intent{Target: a.selector(), Flags: canonical.Flags, Rationale: canonical.Rationale, ReviewLifetime: override.MaximumReviewLifetime})
	if err != nil {
		return overrideports.ModuleReview{}, err
	}
	return overrideports.ModuleReview{ReviewID: result.ReviewID, Status: result.Status, Proposal: result.Proposal, SecretRequirementsEffect: "INTENT_ONLY", ValueTier: override.Native}, nil
}

func (s *ModuleOverrideService) ApplyModuleOverride(ctx context.Context, id string) (overrideports.ModuleAdmission, error) {
	request, err := s.repository.GetChangeRequest(ctx, id)
	if err != nil {
		return overrideports.ModuleAdmission{}, err
	}
	a := s.authority(overrideports.ModuleDraft{Environment: request.Environment})
	if !a.allowed(ctx, OperationOperate) {
		return overrideports.ModuleAdmission{}, ErrForbidden
	}
	if _, err := NewGovernanceService(s.repository).GetReadableChangeRequest(ctx, id, s.actor, s.options); err != nil {
		return overrideports.ModuleAdmission{}, err
	}
	p, draft, err := decodeModuleOverride(request)
	if err != nil {
		return overrideports.ModuleAdmission{}, err
	}
	a.draft = draft
	if request.Execution != nil && request.Execution.Status == ExecutionSucceeded {
		return overrideports.ModuleAdmission{ReviewID: id, ProposalID: p.ID, InstallationID: p.Snapshot.Target.InstallationID, Module: p.Snapshot.Target.Resource, Version: p.Snapshot.Target.Version, Status: "ALREADY_APPLIED", SecretRequirementsEffect: "INTENT_ONLY", ValueTier: override.Native}, nil
	}
	kernel, err := overrideapp.New(a)
	if err != nil {
		return overrideports.ModuleAdmission{}, err
	}
	var result overrideports.ModuleAdmission
	err = kernel.WithPermit(ctx, id, p.Snapshot.Target.Selector(), func(permit override.Permit) error {
		if a.working == nil {
			return override.ErrCorrupt
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		candidate, snapshot, err := a.working.candidate(ctx)
		if err != nil {
			return err
		}
		if snapshot.Target != p.Snapshot.Target || !permit.ValidAt(time.Now().UTC()) {
			return override.ErrBlocked
		}
		for _, b := range snapshot.Blockers {
			if !permit.Allows(time.Now().UTC(), snapshot.Target, b) {
				return override.ErrBlocked
			}
		}
		repository := a.working.repository
		if err := a.working.audit(ctx, "MODULE_OVERRIDE_ADMISSION_AUTHORIZED", id, p.ID); err != nil {
			return err
		}
		var stored ModuleInstallation
		if draft.Action == "INSTALL" {
			stored, err = repository.CreateModuleInstallation(ctx, candidate)
		} else {
			stored, err = repository.UpdateModuleInstallation(ctx, draft.InstallationID, func(record *ModuleInstallation) error {
				if record.Revision != p.Snapshot.Target.BaselineRevision {
					return ErrConflict
				}
				record.Version = candidate.Version
				record.Variables = cloneJSON(candidate.Variables)
				record.Entities = cloneJSON(candidate.Entities)
				record.UpdatedAt = time.Now().UTC()
				return nil
			})
		}
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		_, err = repository.UpdateChangeRequest(ctx, id, func(record *ChangeRequest) error {
			if record.Status != ChangeApproved || record.Revision != a.working.reviewRevision {
				return ErrConflict
			}
			record.Execution = &ChangeExecution{Status: ExecutionSucceeded, StartedAt: now, CompletedAt: now, Items: []ChangeExecutionItem{{ChangeID: record.Changes[0].ID, ResourceRID: environmentRID(draft.Environment), Status: ExecutionSucceeded, ResultID: stored.ID}}}
			record.UpdatedAt = now
			return nil
		})
		if err != nil {
			return err
		}
		if err := a.working.audit(ctx, "MODULE_OVERRIDE_METADATA_APPLIED", id, p.ID); err != nil {
			return err
		}
		result = overrideports.ModuleAdmission{ReviewID: id, ProposalID: p.ID, InstallationID: stored.ID, Module: stored.Module, Version: stored.Version, Revision: stored.Revision, Status: "APPLIED", SecretRequirementsEffect: "INTENT_ONLY", ValueTier: override.Native}
		return nil
	})
	return result, err
}

type moduleOverrideAuthority struct {
	repository     Repository
	options        HandlerOptions
	actor          Principal
	draft          overrideports.ModuleDraft
	working        *moduleOverrideAuthority
	reviewRevision uint64
}

func (a *moduleOverrideAuthority) allowed(ctx context.Context, operation string) bool {
	return NewGovernanceService(a.repository).AuthorizeResourceOperation(ctx, a.actor, a.options, environmentRID(a.draft.Environment), operation)
}

func (a *moduleOverrideAuthority) selector() override.Selector {
	return override.Selector{Kind: "MODULE", Action: a.draft.Action, Environment: a.draft.Environment, InstallationID: a.draft.InstallationID, Resource: a.draft.Module, Version: a.draft.Version}
}

func (a *moduleOverrideAuthority) canonicalDraft(ctx context.Context) (overrideports.ModuleDraft, error) {
	d := cloneJSON(a.draft)
	if d.Action != "INSTALL" && d.Action != "UPDATE" {
		return d, ErrInvalidTransition
	}
	if !identifierPattern.MatchString(d.Environment) || (!d.Flags.IgnoreRecalls && !d.Flags.IgnoreSecretRequirements) {
		return d, ErrInvalidTransition
	}
	a.draft = d
	if !a.allowed(ctx, OperationOperate) {
		return d, ErrForbidden
	}
	if d.Action == "UPDATE" {
		current, err := a.repository.GetModuleInstallation(ctx, d.InstallationID)
		if err != nil {
			return d, err
		}
		if current.Environment != d.Environment || (d.Module != "" && current.Module != d.Module) || current.Status != "ACTIVE" || len(d.Variables) != 0 {
			return d, ErrConflict
		}
		d.Module = current.Module
		d.ReleaseChannel = current.ReleaseChannel
		if d.ReleaseChannel == "" {
			d.ReleaseChannel = "RELEASE"
		}
	} else {
		identity := sha256.Sum256([]byte(d.Environment + "\x00" + d.Module))
		id := "modi_" + hex.EncodeToString(identity[:16])
		if d.InstallationID != "" && d.InstallationID != id {
			return d, ErrConflict
		}
		d.InstallationID = id
	}
	if d.ReleaseChannel == "" {
		plans, err := a.repository.ListPlans(ctx, d.Environment)
		if err != nil {
			return d, err
		}
		d.ReleaseChannel = moduleInstallChannel("", plans, d.Environment)
		if d.ReleaseChannel == "" {
			d.ReleaseChannel = "RELEASE"
		}
	}
	if d.Version == "" || d.Version == "latest" {
		if d.Flags.IgnoreRecalls {
			releases, err := a.repository.ListModuleReleases(ctx, d.Module)
			if err != nil {
				return d, err
			}
			for i := len(releases) - 1; i >= 0; i-- {
				if containsString(releases[i].ReleaseChannels, d.ReleaseChannel) {
					d.Version = releases[i].Version
					break
				}
			}
		} else {
			release, err := NewModuleService(a.repository).resolveReleaseForChannel(ctx, d.Module, "latest", d.ReleaseChannel)
			if err != nil {
				return d, err
			}
			d.Version = release.Version
		}
	}
	a.draft = d
	if a.selector().Validate() != nil {
		return d, ErrInvalidTransition
	}
	if len(d.Variables) > 256 {
		return d, ErrInvalidTransition
	}
	encoded, err := json.Marshal(d)
	if err != nil || len(encoded) > 128<<10 {
		return d, ErrInvalidTransition
	}
	return d, nil
}

func (a *moduleOverrideAuthority) candidate(ctx context.Context) (ModuleInstallation, override.Snapshot, error) {
	if !a.allowed(ctx, OperationOperate) {
		return ModuleInstallation{}, override.Snapshot{}, ErrForbidden
	}
	d := a.draft
	release, err := a.repository.GetModuleRelease(ctx, d.Module, d.Version)
	if err != nil {
		return ModuleInstallation{}, override.Snapshot{}, err
	}
	if release.Name != d.Module || release.Version != d.Version || !containsString(release.ReleaseChannels, d.ReleaseChannel) {
		return ModuleInstallation{}, override.Snapshot{}, ErrConflict
	}
	service := NewModuleService(a.repository)
	var candidate ModuleInstallation
	var revision uint64
	if d.Action == "INSTALL" {
		candidate, err = service.prepareModuleInstallation(ctx, d.Environment, release, d.ReleaseChannel, d.Variables)
	} else {
		current, getErr := a.repository.GetModuleInstallation(ctx, d.InstallationID)
		if getErr != nil {
			return candidate, override.Snapshot{}, getErr
		}
		if current.Environment != d.Environment || current.Module != d.Module || current.Status != "ACTIVE" {
			return candidate, override.Snapshot{}, ErrConflict
		}
		previous, getErr := a.repository.GetModuleRelease(ctx, d.Module, current.Version)
		if getErr != nil {
			return candidate, override.Snapshot{}, getErr
		}
		if !moduleUpdateIsNewer(previous, release) {
			return candidate, override.Snapshot{}, ErrInvalidTransition
		}
		values, validation := resolveModuleVariables(release.Lock.Variables, current.Variables)
		if len(validation) > 0 {
			return candidate, override.Snapshot{}, ValidationErrors(validation)
		}
		entities := renderLockedModuleEntities(release.Lock.Entities, values)
		byKey := map[string]InstalledModuleEntity{}
		for _, entity := range current.Entities {
			byKey[entity.Key] = entity
		}
		for i, entity := range entities {
			previous, exists := byKey[entity.Key]
			if exists {
				entities[i], err = reconcileModuleEntityConfig(&previous, entity, false)
			} else {
				entities[i], err = reconcileModuleEntityConfig(nil, entity, true)
			}
			if err != nil {
				return candidate, override.Snapshot{}, err
			}
		}
		candidate = cloneJSON(current)
		candidate.Version = release.Version
		candidate.Variables = values
		candidate.Entities = entities
		revision = current.Revision
	}
	if err != nil {
		return candidate, override.Snapshot{}, err
	}
	if candidate.ID != d.InstallationID {
		return candidate, override.Snapshot{}, override.ErrCorrupt
	}
	installations, err := a.repository.ListModuleInstallations(ctx, d.Environment)
	if err != nil {
		return candidate, override.Snapshot{}, err
	}
	sort.Slice(installations, func(i, j int) bool { return installations[i].ID < installations[j].ID })
	plans, err := a.repository.ListPlans(ctx, d.Environment)
	if err != nil {
		return candidate, override.Snapshot{}, err
	}
	stateRef := ""
	if latest, ok := latestPlansByEnvironment(plans)[d.Environment]; ok {
		stateRef = latest.DesiredStateDigest
	}
	authority, err := a.authorityDigest(ctx)
	if err != nil {
		return candidate, override.Snapshot{}, err
	}
	immutableRelease := release
	immutableRelease.Recalled = false
	immutableRelease.ReleaseChannels = nil
	snapshot := override.Snapshot{Target: override.Target{Kind: "MODULE", Action: d.Action, Environment: d.Environment, InstallationID: d.InstallationID, Resource: d.Module, Version: d.Version, BaselineRevision: revision, ReleaseDigest: moduleOverrideDigest(immutableRelease), ConfigurationDigest: moduleOverrideDigest(struct {
		Variables map[string]string
		Entities  []InstalledModuleEntity
	}{candidate.Variables, candidate.Entities}), StateDigest: moduleOverrideDigest(struct {
		Installations  []ModuleInstallation
		Channel, State string
	}{installations, d.ReleaseChannel, stateRef})}, AuthorityDigest: authority, SecretRequirementsIntentOnly: true}
	if release.Recalled {
		snapshot.Blockers = []override.Blocker{{Kind: override.Recall, ID: "module-recall:" + strings.TrimPrefix(moduleOverrideDigest([]string{d.Module, d.Version}), "sha256:"), Digest: moduleOverrideDigest(struct {
			Name, Version, Digest string
			Recalled              bool
		}{release.Name, release.Version, release.Digest, true}), Overridable: true}}
	}
	if err := snapshot.Validate(); err != nil {
		return candidate, snapshot, err
	}
	return candidate, snapshot, nil
}

func moduleOverrideDigest(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func (a *moduleOverrideAuthority) authorityDigest(ctx context.Context) (string, error) {
	teams, err := a.repository.ListTeams(ctx)
	if err != nil {
		return "", err
	}
	policies, err := a.repository.ListApprovalPolicies(ctx)
	if err != nil {
		return "", err
	}
	grants, err := effectiveResourceGrants(ctx, a.repository, environmentRID(a.draft.Environment))
	if err != nil {
		return "", err
	}
	sort.Slice(grants, func(i, j int) bool { return grants[i].ID < grants[j].ID })
	bootstrap := normalizedStrings(a.options.BootstrapAdministrators)
	return moduleOverrideDigest(struct {
		Teams          []Team
		Policies       []ApprovalPolicy
		Grants         []ResourceGrant
		Bootstrap      []string
		RoleOperations map[string][]string
	}{teams, policies, grants, bootstrap, governance.CanonicalRoleOperations}), nil
}

func (a *moduleOverrideAuthority) ResolveProposal(ctx context.Context, target override.Selector) (override.Snapshot, override.Identity, error) {
	if target != a.selector() {
		return override.Snapshot{}, override.Identity{}, override.ErrInvalid
	}
	_, snapshot, err := a.candidate(ctx)
	return snapshot, override.Identity{Actor: a.actor.Username, Effective: effectivePrincipal(a.actor).Username}, err
}

func moduleOverrideBoundary(ctx context.Context, repository Repository, operation func(Repository) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if transactional, ok := repository.(TransactionalRepository); ok {
		return transactional.Transaction(ctx, func(working Repository) error {
			if err := operation(working); err != nil {
				return err
			}
			return ctx.Err()
		})
	}
	if file, ok := repository.(*FileRepository); ok {
		file.mu.Lock()
		defer file.mu.Unlock()
		if err := operation(&FileRepository{root: file.root, transactionLocal: true}); err != nil {
			return err
		}
		return ctx.Err()
	}
	return fmt.Errorf("%w: Module overrides require a serialized repository", ErrInvalidTransition)
}

func (a *moduleOverrideAuthority) CreateReview(ctx context.Context, p override.Proposal) (overrideports.ReviewAck, error) {
	var ack overrideports.ReviewAck
	var opening ChangeRequest
	err := moduleOverrideBoundary(ctx, a.repository, func(repository Repository) error {
		working := *a
		working.repository = repository
		_, current, err := working.candidate(ctx)
		if err != nil {
			return err
		}
		if !bytes.Equal(mustModuleOverrideJSON(current), mustModuleOverrideJSON(p.Snapshot)) {
			return ErrConflict
		}
		if p.Author.Actor != a.actor.Username || p.Author.Effective != effectivePrincipal(a.actor).Username || p.Flags != a.draft.Flags || p.Rationale != strings.TrimSpace(a.draft.Rationale) {
			return override.ErrCorrupt
		}
		encoded, err := override.Encode(p)
		if err != nil {
			return err
		}
		draft, err := json.Marshal(a.draft)
		if err != nil {
			return err
		}
		service := NewGovernanceService(repository)
		change := EntityChange{ID: "chg_" + stableShortID(p.ID), Type: ModuleOverrideTarget, Diff: map[string]ChangeDiffEntry{"version": {After: p.Snapshot.Target.Version}, "ignoreRecalls": {After: p.Flags.IgnoreRecalls}, "ignoreSecretRequirements": {After: p.Flags.IgnoreSecretRequirements}, "secretRequirementsEffect": {After: "INTENT_ONLY"}}, Request: map[string]any{"proposal": string(encoded), "draft": string(draft), "valueTier": override.Native}, ReferencedResources: map[string]ReferencedResource{"environment": service.referencedResource(ctx, environmentRID(a.draft.Environment), a.draft.Environment, effectivePrincipal(a.actor))}}
		request, err := service.evaluateChangeRequest(ctx, ChangeRequest{TargetKind: ModuleOverrideTarget, TargetID: p.ID, Environment: a.draft.Environment, Title: "Review Module metadata override", Description: p.Rationale, Author: a.actor, Changes: []EntityChange{change}})
		if err != nil {
			return err
		}
		if request.Status != ChangeRejected && request.Status != ChangeError {
			request.Requirements = append(request.Requirements, ReviewerRequirement{ID: "module-override:" + p.ID, ChangeID: change.ID, PolicyID: "conduit.module-override.review", Kind: governance.RequirementOperation, ResourceRID: environmentRID(a.draft.Environment), Operation: OperationApproveModuleOverride, Reason: "independent explicit Module override review"})
			request.RequirementsSatisfied = false
			request.Status = ChangePendingApprovals
		}
		stored, err := repository.CreateChangeRequest(ctx, request)
		if err != nil {
			return err
		}
		if _, _, err := decodeModuleOverride(stored); err != nil {
			return err
		}
		if err := working.audit(ctx, "MODULE_OVERRIDE_PROPOSED", stored.ID, p.ID); err != nil {
			return err
		}
		ack = overrideports.ReviewAck{ReviewID: stored.ID, ProposalID: p.ID, Status: stored.Status}
		opening = stored
		return nil
	})
	if err == nil {
		service := NewGovernanceService(a.repository)
		service.SetAnnouncements(a.options.Announcements)
		service.announceAwaitingApproval(ctx, opening)
	}
	return ack, err
}

func mustModuleOverrideJSON(value any) []byte { encoded, _ := json.Marshal(value); return encoded }

func decodeModuleOverride(request ChangeRequest) (override.Proposal, overrideports.ModuleDraft, error) {
	if request.TargetKind != ModuleOverrideTarget || len(request.Changes) != 1 || request.Changes[0].Type != ModuleOverrideTarget {
		return override.Proposal{}, overrideports.ModuleDraft{}, ErrInvalidTransition
	}
	encoded, ok := request.Changes[0].Request["proposal"].(string)
	if !ok {
		return override.Proposal{}, overrideports.ModuleDraft{}, ErrInvalidTransition
	}
	p, err := override.Decode([]byte(encoded))
	if err != nil || p.ID != request.TargetID || p.Snapshot.Target.Environment != request.Environment || p.Author.Actor != request.Author.Username || p.Author.Effective != effectivePrincipal(request.Author).Username {
		return override.Proposal{}, overrideports.ModuleDraft{}, ErrConflict
	}
	document, ok := request.Changes[0].Request["draft"].(string)
	if !ok || len(document) > 128<<10 {
		return p, overrideports.ModuleDraft{}, ErrConflict
	}
	decoder := json.NewDecoder(strings.NewReader(document))
	decoder.DisallowUnknownFields()
	var draft overrideports.ModuleDraft
	if decoder.Decode(&draft) != nil || decoder.Decode(new(any)) != io.EOF {
		return p, draft, ErrConflict
	}
	if (override.Selector{Kind: "MODULE", Action: draft.Action, Environment: draft.Environment, InstallationID: draft.InstallationID, Resource: draft.Module, Version: draft.Version}) != p.Snapshot.Target.Selector() || draft.Flags != p.Flags || strings.TrimSpace(draft.Rationale) != p.Rationale {
		return p, draft, ErrConflict
	}
	return p, draft, nil
}

func (a *moduleOverrideAuthority) WithApproved(ctx context.Context, id string, target override.Selector, callback func(override.Proposal, override.Approval, override.Snapshot) error) error {
	return moduleOverrideBoundary(ctx, a.repository, func(repository Repository) error {
		working := *a
		working.repository = repository
		working.working = nil
		if !working.allowed(ctx, OperationOperate) {
			return ErrForbidden
		}
		request, err := repository.GetChangeRequest(ctx, id)
		if err != nil {
			return err
		}
		if _, err := NewGovernanceService(repository).GetReadableChangeRequest(ctx, id, a.actor, a.options); err != nil {
			return err
		}
		p, draft, err := decodeModuleOverride(request)
		if err != nil {
			return err
		}
		working.draft = draft
		working.reviewRevision = request.Revision
		if target != p.Snapshot.Target.Selector() || request.Execution != nil {
			return ErrConflict
		}
		_, current, err := working.candidate(ctx)
		if err != nil {
			return err
		}
		var reviewer override.Identity
		var reviewedAt time.Time
		mandatory := "module-override:" + p.ID
		var satisfied []string
		for _, requirement := range request.Requirements {
			if requirement.ID == mandatory && requirement.Kind == governance.RequirementOperation && requirement.Operation == OperationApproveModuleOverride && requirement.ResourceRID == environmentRID(draft.Environment) {
				satisfied = requirement.SatisfiedBy
			}
		}
		for _, review := range request.Reviews {
			if review.Decision == "APPROVE" && containsString(satisfied, review.Subject) {
				proof, proofErr := moduleOverrideReviewerProof(request, review.Subject, review.CreatedAt)
				if proofErr != nil {
					return proofErr
				}
				reviewer = proof.Identity
				reviewedAt = review.CreatedAt
				break
			}
		}
		approval := override.Approval{ReviewID: request.ID, ProposalID: p.ID, Revision: request.Revision, Status: request.Status, RequirementsSatisfied: request.RequirementsSatisfied, DoNotMerge: hasDoNotMerge(request.Labels), Reviewer: reviewer, ReviewedAt: reviewedAt, AuthorityDigest: current.AuthorityDigest}
		a.working = &working
		defer func() { a.working = nil }()
		return callback(p, approval, current)
	})
}

func (a *moduleOverrideAuthority) audit(ctx context.Context, action, reviewID, proposalID string) error {
	event := governance.NewAuthorizationCheckEvent(time.Now().UTC(), a.actor.Username, action, environmentRID(a.draft.Environment), "ALLOW", "explicit reviewed Module metadata workflow", map[string]any{"changeRequestId": reviewID, "proposalId": proposalID, "valueTier": override.Native, "secretRequirementsEffect": "INTENT_ONLY", "actor": a.actor.Username, "effectiveSubject": effectivePrincipal(a.actor).Username, "ignoreRecalls": a.draft.Flags.IgnoreRecalls, "ignoreSecretRequirements": a.draft.Flags.IgnoreSecretRequirements})
	if action == "MODULE_OVERRIDE_PROPOSED" {
		event.Categories = append(event.Categories, AuditCategoryRequestCreate)
		event.RequestFields["createdRequestAffectedResources"] = []string{environmentRID(a.draft.Environment), a.draft.InstallationID}
		event.RequestFields["createdRequestDescription"] = strings.TrimSpace(a.draft.Rationale)
		event.ResultFields["createdRequestIds"] = []string{reviewID}
	}
	if action == "MODULE_OVERRIDE_METADATA_APPLIED" {
		event.Categories = append(event.Categories, AuditCategoryConfigureInfra)
		event.RequestFields["configureInfraTargets"] = []string{a.draft.InstallationID}
		event.ResultFields["configureInfraRequestId"] = reviewID
	}
	_, err := a.repository.AppendAuthorizationAudit(ctx, event)
	return err
}

var _ overrideports.Authority = (*moduleOverrideAuthority)(nil)
var _ overrideports.ModuleController = (*ModuleOverrideService)(nil)

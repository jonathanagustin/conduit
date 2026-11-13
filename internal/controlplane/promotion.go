package controlplane

import (
	"context"
	"errors"

	"fmt"
	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	"sort"
	"strings"
)

func (lifecycle *Lifecycle) EvaluatePromotion(
	ctx context.Context, request PromotionEvaluationRequest,
) (PromotionRecord, error) {
	now := lifecycle.now().UTC()
	if label := authoredSecurityLabel(request.Release.Labels); label != "" {
		return PromotionRecord{}, fmt.Errorf("%w: release label %s is scanner-owned", ErrInvalidTransition, label)
	}
	if !coordinatePattern.MatchString(request.Release.Product) || !versionPattern.MatchString(request.Release.Version) {
		return PromotionRecord{}, fmt.Errorf("%w: release Product and semantic version are required", ErrInvalidTransition)
	}
	if !releaseChannelPattern.MatchString(request.FromChannel) || !releaseChannelPattern.MatchString(request.ToChannel) ||
		request.FromChannel == request.ToChannel {
		return PromotionRecord{}, fmt.Errorf("%w: distinct valid source and target channels are required", ErrInvalidTransition)
	}
	if request.Policy.MinimumHealthyEntities < 0 || request.Policy.MaximumUnhealthyEntities < 0 ||
		request.Policy.MinimumHealthyDurationSeconds < 0 || request.Policy.MaximumEvidenceAgeSeconds < 0 ||
		request.Policy.MaximumEvaluationDurationSeconds < 0 || request.Policy.RolloutWindowSeconds < 0 ||
		request.Policy.RolloutWindowSeconds > 31*24*60*60 {
		return PromotionRecord{}, fmt.Errorf("%w: promotion thresholds cannot be negative", ErrInvalidTransition)
	}
	if err := validatePromotionStage(request.Policy); err != nil {
		return PromotionRecord{}, err
	}
	control, err := lifecycle.repository.GetPromotionStageControl(
		ctx, request.Release.Product, request.FromChannel, request.ToChannel,
	)
	if err != nil {
		return PromotionRecord{}, err
	}
	if control.Status == PromotionStagePaused && request.Termination == "" {
		return PromotionRecord{}, promotionStagePausedError(control)
	}
	existing, err := lifecycle.repository.ListPromotions(ctx, request.Release.Product)
	if err != nil {
		return PromotionRecord{}, err
	}
	var previous *PromotionRecord
	if request.PreviousEvaluationID != "" {
		for index := range existing {
			if existing[index].ID == request.PreviousEvaluationID {
				previous = &existing[index]
				break
			}
		}
		if previous == nil || previous.RunID == "" {
			return PromotionRecord{}, fmt.Errorf("%w: promotion predecessor not found", ErrInvalidTransition)
		}
		if !request.EvaluationStartedAt.IsZero() && !request.EvaluationStartedAt.Equal(previous.EvaluationStartedAt) {
			return PromotionRecord{}, fmt.Errorf("%w: promotion start time changed", ErrInvalidTransition)
		}
		request.EvaluationStartedAt = previous.EvaluationStartedAt
	} else if request.EvaluationStartedAt.IsZero() {
		request.EvaluationStartedAt = now
	}
	if request.Termination != "" && (previous == nil || (request.Termination != catalog.PromotionAborted && request.Termination != catalog.PromotionCancelled) || strings.TrimSpace(request.TerminationReason) == "") {
		return PromotionRecord{}, fmt.Errorf("%w: termination requires a predecessor, ABORTED or CANCELLED, and a reason", ErrInvalidTransition)
	}
	if request.Termination == "" && request.TerminationReason != "" {
		return PromotionRecord{}, fmt.Errorf("%w: reason requires termination", ErrInvalidTransition)
	}
	if !releaseOnChannel(request.Release, existing, request.FromChannel) {
		return PromotionRecord{}, fmt.Errorf("%w: release is not reachable on source channel %s", ErrInvalidTransition, request.FromChannel)
	}
	if promotionChannelReachable(existing, request.Release.Product, request.Release.Version, request.ToChannel, request.FromChannel) {
		return PromotionRecord{}, fmt.Errorf("%w: promotion would create a channel cycle", ErrInvalidTransition)
	}

	request.Release.Labels = effectiveReleaseLabels(request.Release)
	// Before anything narrows or decides, because both the canary filter below
	// and the record minted at the end read these labels.
	request.Evidence, err = stampEnvironmentLabels(ctx, lifecycle.repository, request.Evidence)
	if err != nil {
		return PromotionRecord{}, err
	}
	topologyEvidence := request.Evidence
	if stageType(request.Policy) == PromotionCanary {
		topologyEvidence = catalog.CanaryEvidence(request.Evidence, request.Policy.EntityFilters)
	}
	nonParticipating, err := nonParticipatingEvidenceEnvironments(ctx, lifecycle.repository, topologyEvidence)
	if err != nil {
		return PromotionRecord{}, err
	}
	boundConflicts, err := intermittentEvidenceBoundConflicts(
		ctx, lifecycle.repository, topologyEvidence, request.Policy, now)
	if err != nil {
		return PromotionRecord{}, err
	}
	blockers := append(nonParticipating, boundConflicts...)
	blockers = append(blockers, evaluatePromotion(request, now)...)
	if request.Termination != "" {
		blockers = append(blockers, catalog.PromotionBlocker{Kind: catalog.PromotionBlockerTerminated, Message: strings.TrimSpace(request.TerminationReason)})
	}
	sortBlockers(blockers)
	status := PromotionPassed
	if len(blockers) > 0 {
		status = PromotionBlocked
	}
	record := PromotionRecord{
		Product: request.Release.Product, Version: request.Release.Version,
		FromChannel: request.FromChannel, ToChannel: request.ToChannel,
		Labels: cloneStringMap(request.Release.Labels), Policy: request.Policy,
		Evidence:            append([]PromotionHealthEvidence(nil), request.Evidence...),
		EvaluationStartedAt: request.EvaluationStartedAt.UTC(),
		Status:              status, Blockers: blockers, Reasons: blockerMessages(blockers),
		EvaluatedAt: now, Revision: 1,
	}
	record.Lifecycle, record.LifecycleReason = catalog.PromotionLifecycleFor(status, blockers)
	record.ValueTier = "CONDUIT_NATIVE"
	if request.Termination != "" {
		record.Lifecycle = request.Termination
		record.LifecycleReason = strings.TrimSpace(request.TerminationReason)
	}
	sort.Slice(record.Evidence, func(i, j int) bool {
		left := record.Evidence[i].Environment + "/" + record.Evidence[i].InstallationID
		right := record.Evidence[j].Environment + "/" + record.Evidence[j].InstallationID
		return left < right
	})
	if previous != nil {
		record.RunID = previous.RunID
		record.PreviousEvaluationID = previous.ID
	} else {
		// Use the immutable first evaluation's identity as the run coordinate.
		runIdentity, err := promotionIdentity(record)
		if err != nil {
			return PromotionRecord{}, err
		}
		record.RunID = "run_" + strings.TrimPrefix(runIdentity, "pro_")
	}
	identity, err := promotionIdentity(record)
	if err != nil {
		return PromotionRecord{}, err
	}
	record.ID = identity
	created, err := lifecycle.repository.CreatePromotionUnlessStagePaused(ctx, record)
	if err != nil {
		return PromotionRecord{}, err
	}
	if err := lifecycle.reconcilePromotionSuppressions(ctx, created); err != nil {
		return PromotionRecord{}, err
	}
	return created, nil
}

// stampEnvironmentLabels attaches to each piece of health evidence the labels
// this Hub holds for the Environment that submitted it, so a canary stage can
// nominate a tier by label rather than by name.
//
// It overwrites whatever the caller sent, and that is the whole guard. Evidence
// is caller-submitted and carries no attestation -- the same reason a triggered
// recall condition blocks rather than recalls -- so a principal free to declare
// its own Environment labels could write itself into any canary tier, or every
// other Environment out of one, and the stage would decide a fleet-wide
// promotion on a set the operator never nominated.
//
// The labels come from the Environment's most recently reviewed generation, the
// same place the promotion topology is read from, so a canary tier and a
// vouching decision cannot disagree about what an Environment is. An Environment
// the Hub has never reviewed contributes no labels and so matches no label
// requirement; it is not admitted to a tier on the strength of a name.
func stampEnvironmentLabels(
	ctx context.Context, repository Repository, evidence []PromotionHealthEvidence,
) ([]PromotionHealthEvidence, error) {
	if len(evidence) == 0 {
		return evidence, nil
	}
	// One read per distinct Environment, not per report: a fleet submits many
	// Entities per Environment and the loop below is on the evaluation path.
	resolved := make(map[string]map[string]string, len(evidence))
	stamped := append([]PromotionHealthEvidence(nil), evidence...)
	for index := range stamped {
		environment := stamped[index].Environment
		labels, known := resolved[environment]
		if !known {
			plans, err := repository.ListPlans(ctx, environment)
			if err != nil && !errors.Is(err, ErrNotFound) {
				return nil, err
			}
			state := latestDesiredStates(plans)[environment]
			labels = cloneStringMap(state.Environment.Labels)
			resolved[environment] = labels
		}
		stamped[index].EnvironmentLabels = labels
	}
	return stamped, nil
}

func (lifecycle *Lifecycle) SetPromotionStagePaused(
	ctx context.Context, product, fromChannel, toChannel, subject, rationale string, paused bool,
) (PromotionStageControl, error) {
	product = strings.TrimSpace(product)
	fromChannel = strings.TrimSpace(fromChannel)
	toChannel = strings.TrimSpace(toChannel)
	subject = strings.TrimSpace(subject)
	rationale = strings.TrimSpace(rationale)
	if !coordinatePattern.MatchString(product) {
		return PromotionStageControl{}, fmt.Errorf("%w: valid Product is required", ErrInvalidTransition)
	}
	if !releaseChannelPattern.MatchString(fromChannel) || !releaseChannelPattern.MatchString(toChannel) || fromChannel == toChannel {
		return PromotionStageControl{}, fmt.Errorf("%w: distinct valid source and target channels are required", ErrInvalidTransition)
	}
	if subject == "" || rationale == "" {
		return PromotionStageControl{}, fmt.Errorf("%w: subject and rationale are required", ErrInvalidTransition)
	}
	desired := PromotionStageActive
	if paused {
		desired = PromotionStagePaused
	}
	return lifecycle.repository.UpdatePromotionStageControl(ctx, product, fromChannel, toChannel, func(control *PromotionStageControl) error {
		if control.Status == desired {
			return nil
		}
		control.Status = desired
		control.ChangedBy = subject
		control.Rationale = rationale
		control.ChangedAt = lifecycle.now().UTC()
		return nil
	})
}

func promotionStagePausedError(control PromotionStageControl) error {
	return fmt.Errorf(
		"%w: promotion stage %s %s to %s is paused by %s: %s",
		ErrInvalidTransition, control.Product, control.FromChannel, control.ToChannel, control.ChangedBy, control.Rationale,
	)
}

func validatePersistedPromotionStageControl(
	control PromotionStageControl, product, fromChannel, toChannel string,
) error {
	if control.ID != promotionStageControlID(product, fromChannel, toChannel) || control.Product != product ||
		control.FromChannel != fromChannel || control.ToChannel != toChannel ||
		(control.Status != PromotionStageActive && control.Status != PromotionStagePaused) || control.Revision == 0 ||
		strings.TrimSpace(control.ChangedBy) == "" || strings.TrimSpace(control.Rationale) == "" || control.ChangedAt.IsZero() {
		return fmt.Errorf("%w: invalid persisted promotion stage control", ErrConflict)
	}
	return nil
}

func authoredSecurityLabel(labels map[string]string) string {
	for _, label := range []string{SecurityScannedLabel, SecurityScanOutcomeLabel} {
		if _, exists := labels[label]; exists {
			return label
		}
	}
	return ""
}

// effectiveReleaseLabels adds Apollo's public vulnerability-promotion labels
// only from a complete, exact-digest assessment. Product-authored values for
// these reserved names are deliberately ignored and rejected at ingress.
func effectiveReleaseLabels(release Release) map[string]string {
	labels := make(map[string]string, len(release.Labels)+2)
	for key, value := range release.Labels {
		if key != SecurityScannedLabel && key != SecurityScanOutcomeLabel {
			labels[key] = value
		}
	}
	if release.VulnerabilityAssessment == nil {
		return labels
	}
	var errs []ValidationError
	validateVulnerabilityAssessment("release", release, &errs)
	if len(errs) != 0 {
		return labels
	}
	labels[SecurityScannedLabel] = "true"
	labels[SecurityScanOutcomeLabel] = strings.ToLower(release.VulnerabilityAssessment.Outcome)
	return labels
}

func cloneStringMap(value map[string]string) map[string]string {
	if value == nil {
		return nil
	}
	clone := make(map[string]string, len(value))
	for key, item := range value {
		clone[key] = item
	}
	return clone
}

func validatePromotions(promotions []PromotionRecord, releases []Release, errs *[]ValidationError) {
	seen := make(map[string]struct{}, len(promotions))
	for index, promotion := range promotions {
		path := fmt.Sprintf("promotions[%d]", index)
		expectedID, identityErr := promotionIdentity(promotion)
		if identityErr != nil || promotion.ID != expectedID {
			*errs = append(*errs, ValidationError{Path: path + ".id", Message: "must match the immutable promotion record contents"})
		}
		if _, exists := seen[promotion.ID]; exists {
			*errs = append(*errs, ValidationError{Path: path + ".id", Message: "must be unique"})
		}
		seen[promotion.ID] = struct{}{}
		if promotion.Status != PromotionPassed && promotion.Status != PromotionBlocked {
			*errs = append(*errs, ValidationError{Path: path + ".status", Message: "must be PASSED or BLOCKED"})
		}
		if !releaseChannelPattern.MatchString(promotion.FromChannel) || !releaseChannelPattern.MatchString(promotion.ToChannel) ||
			promotion.FromChannel == promotion.ToChannel {
			*errs = append(*errs, ValidationError{Path: path + ".toChannel", Message: "must define distinct valid source and target channels"})
		}
		if promotion.Policy.MinimumHealthyEntities < 0 || promotion.Policy.MaximumUnhealthyEntities < 0 ||
			promotion.Policy.MinimumHealthyDurationSeconds < 0 || promotion.Policy.MaximumEvidenceAgeSeconds < 0 ||
			promotion.Policy.MaximumEvaluationDurationSeconds < 0 || promotion.Policy.RolloutWindowSeconds < 0 ||
			promotion.Policy.RolloutWindowSeconds > 31*24*60*60 {
			*errs = append(*errs, ValidationError{Path: path + ".policy", Message: "thresholds cannot be negative"})
		}
		if promotion.Revision != 1 {
			*errs = append(*errs, ValidationError{Path: path + ".revision", Message: "must be 1 for an immutable decision"})
		}
		if promotion.EvaluatedAt.IsZero() {
			*errs = append(*errs, ValidationError{Path: path + ".evaluatedAt", Message: "must not be empty"})
		}
		var matched Release
		found := false
		for _, release := range releases {
			if release.Product == promotion.Product && release.Version == promotion.Version {
				matched = release
				found = true
				break
			}
		}
		if !found {
			*errs = append(*errs, ValidationError{Path: path + ".version", Message: "must reference a catalog Release"})
			continue
		}
		if !stringMapsEqual(effectiveReleaseLabels(matched), promotion.Labels) {
			*errs = append(*errs, ValidationError{Path: path + ".labels", Message: "must match the catalog Release labels"})
		}
		matched.Labels = effectiveReleaseLabels(matched)
		blockers := evaluatePromotion(PromotionEvaluationRequest{
			Release: matched, FromChannel: promotion.FromChannel, ToChannel: promotion.ToChannel,
			Policy: promotion.Policy, Evidence: promotion.Evidence, EvaluationStartedAt: promotion.EvaluationStartedAt,
		}, promotion.EvaluatedAt)
		// Whether an Environment may vouch for a Release is a property of the
		// Environment, not of the policy or the evidence, and this validator
		// has neither a repository nor a context to go and read it. Re-derived
		// alone it therefore always comes back empty -- so a record the Hub
		// itself produced for a non-participating Environment failed to
		// reproduce here, and no Plan could carry one.
		//
		// Taking these blockers on trust only ever makes a promotion fail to
		// pass, which is the safe direction; a record still cannot claim
		// PASSED it has not earned, because every blocker that is derivable
		// still has to derive. This is only possible because the kind is on
		// the record: matching the sentence would be guessing.
		blockers = append(blockers, blockersOfKind(promotion.Blockers, PromotionBlockerEnvironmentCannotVouch)...)
		// Trusted on the same terms and for the same reason: it is derived from
		// the Environment's connection setting, which this validator cannot
		// read, and it only ever adds a blocker. The EVIDENCE_STALE it
		// accompanies is derivable and still has to derive, so a record cannot
		// use this kind to claim anything it has not earned.
		blockers = append(blockers, blockersOfKind(promotion.Blockers, PromotionBlockerEvidenceBoundConflict)...)
		// Nothing about a label-selected canary set is trusted here, and it must
		// not be. Both blockers above are additive -- taking them on trust can
		// only make a promotion fail to pass -- but which Environments a filter
		// admits is not: narrowing a canary set drops unhealthy evidence and can
		// turn a BLOCKED decision into a PASSED one. Trusting that would let a
		// peer claim a promotion its own inputs do not support.
		//
		// It does not need trusting. EvaluatePromotion stamps the Environment
		// labels onto the evidence before it decides, and the record carries
		// them, so the re-derivation above selects from exactly the values the
		// original decision selected from -- with no repository, and with no
		// dependence on what those Environments are labelled today.
		if promotion.Lifecycle == catalog.PromotionCancelled || promotion.Lifecycle == catalog.PromotionAborted {
			blockers = append(blockers, blockersOfKind(promotion.Blockers, catalog.PromotionBlockerTerminated)...)
		}
		sortBlockers(blockers)
		expectedStatus := PromotionPassed
		if len(blockers) > 0 {
			expectedStatus = PromotionBlocked
		}
		if promotion.RunID == "" {
			if err := catalog.ValidatePromotionContinuation(promotion, nil); err != nil {
				*errs = append(*errs, ValidationError{Path: path + ".lifecycle", Message: err.Error()})
			}
		}
		if promotion.RunID != "" {
			others := append([]PromotionRecord(nil), promotions[:index]...)
			others = append(others, promotions[index+1:]...)
			// A root is followed by descendants; only another root competes with it.
			ancestors := others
			if promotion.PreviousEvaluationID == "" {
				ancestors = nil
				for _, other := range others {
					if other.RunID == promotion.RunID && other.PreviousEvaluationID == "" {
						ancestors = append(ancestors, other)
					}
				}
			}
			if err := catalog.ValidatePromotionContinuation(promotion, ancestors); err != nil {
				*errs = append(*errs, ValidationError{Path: path + ".lifecycle", Message: err.Error()})
			}
			phase, reason := catalog.PromotionLifecycleFor(expectedStatus, blockers)
			terminated := blockersOfKind(blockers, catalog.PromotionBlockerTerminated)
			if len(terminated) > 0 {
				phase, reason = promotion.Lifecycle, terminated[0].Message
			}
			if promotion.Lifecycle != phase || promotion.LifecycleReason != reason || promotion.ValueTier != "CONDUIT_NATIVE" {
				*errs = append(*errs, ValidationError{Path: path + ".lifecycle", Message: "must reproduce from the recorded decision and terminal reason"})
			}
		}
		if promotion.Status != expectedStatus || !stringSlicesEqual(promotion.Reasons, blockerMessages(blockers)) {
			*errs = append(*errs, ValidationError{Path: path + ".status", Message: "must reproduce from the recorded policy and evidence"})
		}
		// A record from a Hub older than Blockers carries Reasons alone, and
		// its decision still reproduces, so it is accepted on Reasons. When
		// Blockers are present they have to agree as well: Reasons cannot tell
		// two kinds apart that happen to render the same sentence, so checking
		// only Reasons would let a peer attach whatever kinds it liked.
		if len(promotion.Blockers) > 0 && !promotionBlockersEqual(promotion.Blockers, blockers) {
			*errs = append(*errs, ValidationError{Path: path + ".blockers", Message: "must reproduce from the recorded policy and evidence"})
		}
		if promotion.Status == PromotionPassed {
			others := append([]PromotionRecord(nil), promotions[:index]...)
			others = append(others, promotions[index+1:]...)
			if !releaseOnChannel(matched, others, promotion.FromChannel) {
				*errs = append(*errs, ValidationError{Path: path + ".fromChannel", Message: "must be an existing membership before promotion"})
			}
		}
	}
}

func stringMapsEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

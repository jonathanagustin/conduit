package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type PromotionStatus string

type PromotionStageControlStatus string

const (
	PromotionPassed  PromotionStatus = "PASSED"
	PromotionBlocked PromotionStatus = "BLOCKED"

	PromotionStageActive PromotionStageControlStatus = "ACTIVE"
	PromotionStagePaused PromotionStageControlStatus = "PAUSED"

	SecurityScannedLabel     = "vulnerability-scanner.palantir.build/security-scanned"
	SecurityScanOutcomeLabel = "vulnerability-scanner.palantir.build/security-scan-outcome"
)

// PromotionStageControl is the current, revisioned operator control for one
// Product channel edge. PromotionRecord remains immutable decision evidence;
// pausing an edge never rewrites an earlier evaluation.
type PromotionStageControl struct {
	ID          string                      `json:"id"`
	Product     string                      `json:"product"`
	FromChannel string                      `json:"fromChannel"`
	ToChannel   string                      `json:"toChannel"`
	Status      PromotionStageControlStatus `json:"status"`
	ChangedBy   string                      `json:"changedBy,omitempty"`
	Rationale   string                      `json:"rationale,omitempty"`
	ChangedAt   time.Time                   `json:"changedAt,omitempty"`
	Revision    uint64                      `json:"revision"`
}

type PromotionPolicy struct {
	// Type selects the published stage shape, defaulting to a timed stage.
	// Provenance (reviewed 2026-08-02):
	// https://www.palantir.com/docs/apollo/managing-release-channels/configure-promotion-pipeline/
	Type string `json:"type,omitempty"`
	// RequiredLabels requires one exact value per label. LabelRequirements is the
	// published form and accepts a set of acceptable values per label; both are
	// evaluated so existing policies keep working.
	RequiredLabels    map[string]string           `json:"requiredLabels,omitempty"`
	LabelRequirements []PromotionLabelRequirement `json:"labelRequirements,omitempty"`
	// NoDelay waives a timed stage's healthy-duration wait, which is how a stage
	// promotes on label evidence alone.
	NoDelay                  bool `json:"noDelay,omitempty"`
	MinimumHealthyEntities   int  `json:"minimumHealthyEntities"`
	MaximumUnhealthyEntities int  `json:"maximumUnhealthyEntities"`
	// No defaults: every duration is caller-supplied and zero means unbounded.
	// Upstream publishes concrete ones -- a 48-hour weekday soak, a 7-day canary
	// availability timeout, and a 30-day overall promotion timeout
	// (https://www.palantir.com/docs/apollo/core/release-channels, reviewed
	// 2026-08-07). The mechanism matches, including weekday-only accrual via
	// ExcludeWeekends; the numbers are not carried, so a policy written without
	// them promotes on evidence that upstream would have timed out. Adopting
	// them changes what existing policies do, which is why they are recorded
	// here rather than quietly defaulted.
	MinimumHealthyDurationSeconds    int  `json:"minimumHealthyDurationSeconds"`
	MaximumEvidenceAgeSeconds        int  `json:"maximumEvidenceAgeSeconds,omitempty"`
	MaximumEvaluationDurationSeconds int  `json:"maximumEvaluationDurationSeconds,omitempty"`
	RolloutWindowSeconds             int  `json:"rolloutWindowSeconds,omitempty"`
	ExcludeWeekends                  bool `json:"excludeWeekends,omitempty"`
	// EntityFilters names the canary set and Conditions are the OR-composed rules
	// evaluated against it. Both belong to a canary stage only.
	EntityFilters []PromotionEntityFilter `json:"entityFilters,omitempty"`
	Conditions    []PromotionCondition    `json:"conditions,omitempty"`
}

type PromotionHealthEvidence struct {
	Environment    string    `json:"environment"`
	InstallationID string    `json:"installationId"`
	Health         string    `json:"health"`
	HealthySince   time.Time `json:"healthySince,omitempty"`
	ObservedAt     time.Time `json:"observedAt"`
}

type PromotionEvaluationRequest struct {
	Release             Release                   `json:"release"`
	FromChannel         string                    `json:"fromChannel"`
	ToChannel           string                    `json:"toChannel"`
	Policy              PromotionPolicy           `json:"policy"`
	Evidence            []PromotionHealthEvidence `json:"evidence"`
	EvaluationStartedAt time.Time                 `json:"evaluationStartedAt,omitempty"`
}

// PromotionBlocker is one reason a Release did not pass a channel boundary.
// The same argument as UpdateBlocker: Kind is a closed set so an operator can
// group blockers, filter them, and act on them, and Subjects names what the
// blocker is about. A message alone is prose, and prose has to be parsed back
// apart to be useful -- which matters more here than anywhere else, because
// these reasons are what a promotion re-evaluation is compared against to
// decide whether the decision changed.
type PromotionBlocker struct {
	Kind     string   `json:"kind"`
	Message  string   `json:"message"`
	Subjects []string `json:"subjects,omitempty"`
}

// The blocker kinds. Grouped by what produced them: the Release's labels, the
// health evidence submitted for it, the Environments that submitted it, a
// timed stage's own thresholds, and a canary stage's conditions.
//
// INFERRED identifiers over VERIFIED categories. The published docs name what
// blocks a promotion -- label requirements, health requirements, maintenance
// windows, and three timeouts -- so the grouping is evidence-shaped
// (https://www.palantir.com/docs/apollo/core/release-channels, reviewed
// 2026-08-07). The constant STRINGS are ours: no recovered artifact carries an
// Apollo promotion-blocker enum, so nothing here should be read as upstream's
// wire vocabulary or matched against it.
const (
	PromotionBlockerRequiredLabelMissing    = "REQUIRED_LABEL_MISSING"
	PromotionBlockerRequiredLabelNotAllowed = "REQUIRED_LABEL_VALUE_NOT_ALLOWED"

	PromotionBlockerEvidenceDuplicate           = "EVIDENCE_DUPLICATE"
	PromotionBlockerEvidenceInvalid             = "EVIDENCE_INVALID"
	PromotionBlockerEvidenceStale               = "EVIDENCE_STALE"
	PromotionBlockerEvidenceHealthySinceInvalid = "EVIDENCE_HEALTHY_SINCE_INVALID"
	PromotionBlockerEvidenceHealthUnknown       = "EVIDENCE_HEALTH_UNKNOWN"

	PromotionBlockerEnvironmentCannotVouch = "ENVIRONMENT_CANNOT_VOUCH"

	PromotionBlockerEvaluationWindowInvalid = "EVALUATION_WINDOW_INVALID"
	PromotionBlockerEvaluationTimedOut      = "EVALUATION_TIMED_OUT"
	PromotionBlockerHealthyBelowMinimum     = "HEALTHY_ENTITIES_BELOW_MINIMUM"
	PromotionBlockerUnhealthyAboveMaximum   = "UNHEALTHY_ENTITIES_ABOVE_MAXIMUM"

	PromotionBlockerCanaryNoConditions        = "CANARY_NO_CONDITIONS"
	PromotionBlockerCanaryNoEvidence          = "CANARY_CONDITION_NO_EVIDENCE"
	PromotionBlockerCanaryTooFewEntities      = "CANARY_CONDITION_TOO_FEW_ENTITIES"
	PromotionBlockerCanaryShareBelowThreshold = "CANARY_HEALTHY_SHARE_BELOW_THRESHOLD"
	PromotionBlockerCanaryRecallTriggered     = "CANARY_RECALL_TRIGGERED"
	PromotionBlockerCanaryUnsupportedAction   = "CANARY_CONDITION_UNSUPPORTED_ACTION"
	PromotionBlockerCanaryNoConditionMet      = "CANARY_NO_PROMOTE_CONDITION_MET"
)

// sortBlockers orders blockers by the message they render to, which is the
// order Reasons has always been in. Keeping the two in step is what lets
// Reasons stay a projection of Blockers rather than a second source of truth.
func sortBlockers(blockers []PromotionBlocker) {
	sort.Slice(blockers, func(i, j int) bool { return blockers[i].Message < blockers[j].Message })
}

// blockersOfKind is how a caller acts on one category of blocker without
// reading the prose -- the reason Kind exists.
func blockersOfKind(blockers []PromotionBlocker, kind string) []PromotionBlocker {
	var matched []PromotionBlocker
	for _, blocker := range blockers {
		if blocker.Kind == kind {
			matched = append(matched, blocker)
		}
	}
	return matched
}

func promotionBlockersEqual(left, right []PromotionBlocker) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Kind != right[index].Kind || left[index].Message != right[index].Message ||
			!stringSlicesEqual(left[index].Subjects, right[index].Subjects) {
			return false
		}
	}
	return true
}

func blockerMessages(blockers []PromotionBlocker) []string {
	if len(blockers) == 0 {
		return nil
	}
	messages := make([]string, 0, len(blockers))
	for _, blocker := range blockers {
		messages = append(messages, blocker.Message)
	}
	return messages
}

// PromotionRecord is immutable evidence that one Release did or did not pass
// one channel boundary at a particular instant. A later evaluation creates a
// new record; it never edits the earlier decision.
type PromotionRecord struct {
	ID          string                    `json:"id"`
	Product     string                    `json:"product"`
	Version     string                    `json:"version"`
	FromChannel string                    `json:"fromChannel"`
	ToChannel   string                    `json:"toChannel"`
	Labels      map[string]string         `json:"labels,omitempty"`
	Policy      PromotionPolicy           `json:"policy"`
	Evidence    []PromotionHealthEvidence `json:"evidence"`
	Status      PromotionStatus           `json:"status"`
	// Blockers is the structured form; Reasons is derived from it and kept
	// because it is what the record has always published on the wire.
	Blockers            []PromotionBlocker `json:"blockers,omitempty"`
	Reasons             []string           `json:"reasons,omitempty"`
	EvaluationStartedAt time.Time          `json:"evaluationStartedAt,omitempty"`
	EvaluatedAt         time.Time          `json:"evaluatedAt"`
	Revision            uint64             `json:"revision"`
}

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
	if control.Status == PromotionStagePaused {
		return PromotionRecord{}, promotionStagePausedError(control)
	}
	existing, err := lifecycle.repository.ListPromotions(ctx, request.Release.Product)
	if err != nil {
		return PromotionRecord{}, err
	}
	if !releaseOnChannel(request.Release, existing, request.FromChannel) {
		return PromotionRecord{}, fmt.Errorf("%w: release is not reachable on source channel %s", ErrInvalidTransition, request.FromChannel)
	}
	if promotionChannelReachable(existing, request.Release.Product, request.Release.Version, request.ToChannel, request.FromChannel) {
		return PromotionRecord{}, fmt.Errorf("%w: promotion would create a channel cycle", ErrInvalidTransition)
	}

	request.Release.Labels = effectiveReleaseLabels(request.Release)
	topologyEvidence := request.Evidence
	if stageType(request.Policy) == PromotionCanary {
		topologyEvidence = canaryEvidence(request.Evidence, request.Policy.EntityFilters)
	}
	nonParticipating, err := nonParticipatingEvidenceEnvironments(ctx, lifecycle.repository, topologyEvidence)
	if err != nil {
		return PromotionRecord{}, err
	}
	blockers := append(nonParticipating, evaluatePromotion(request, now)...)
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
	sort.Slice(record.Evidence, func(i, j int) bool {
		left := record.Evidence[i].Environment + "/" + record.Evidence[i].InstallationID
		right := record.Evidence[j].Environment + "/" + record.Evidence[j].InstallationID
		return left < right
	})
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

func defaultPromotionStageControl(product, fromChannel, toChannel string) PromotionStageControl {
	return PromotionStageControl{
		ID: promotionStageControlID(product, fromChannel, toChannel), Product: product,
		FromChannel: fromChannel, ToChannel: toChannel, Status: PromotionStageActive,
	}
}

func promotionStageControlID(product, fromChannel, toChannel string) string {
	digest := sha256.Sum256([]byte(product + "\x00" + fromChannel + "\x00" + toChannel))
	return "psc_" + hex.EncodeToString(digest[:16])
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
	validateVulnerabilityAssessment("release", release, nil, &errs)
	if len(errs) != 0 {
		return labels
	}
	labels[SecurityScannedLabel] = "true"
	labels[SecurityScanOutcomeLabel] = strings.ToLower(release.VulnerabilityAssessment.Outcome)
	return labels
}

func evaluatePromotion(request PromotionEvaluationRequest, now time.Time) []PromotionBlocker {
	blockers := evaluateLabelRequirements(request.Release, request.Policy)
	if request.Policy.MaximumEvaluationDurationSeconds > 0 {
		if request.EvaluationStartedAt.IsZero() || request.EvaluationStartedAt.After(now) {
			blockers = append(blockers, PromotionBlocker{
				Kind: PromotionBlockerEvaluationWindowInvalid, Message: "evaluation start time is invalid",
			})
		} else if now.Sub(request.EvaluationStartedAt) > time.Duration(request.Policy.MaximumEvaluationDurationSeconds)*time.Second {
			blockers = append(blockers, PromotionBlocker{
				Kind: PromotionBlockerEvaluationTimedOut, Message: "promotion evaluation exceeded its overall timeout",
			})
		}
	}
	if stageType(request.Policy) == PromotionCanary {
		blockers = append(blockers, evaluateCanaryStage(request, now)...)
		sortBlockers(blockers)
		return blockers
	}
	// A timed stage measures how long the Release has held healthy. "No delay"
	// waives that wait entirely, which is how a stage promotes on labels alone.
	minimumDuration := time.Duration(request.Policy.MinimumHealthyDurationSeconds) * time.Second
	if request.Policy.NoDelay {
		minimumDuration = 0
	}
	valid, evidenceBlockers := validateEvidence(request.Evidence, request.Policy, now)
	blockers = append(blockers, evidenceBlockers...)
	tally := countHeld(valid, request.Policy, minimumDuration)
	if tally.Healthy < request.Policy.MinimumHealthyEntities {
		blockers = append(blockers, PromotionBlocker{
			Kind: PromotionBlockerHealthyBelowMinimum,
			Message: fmt.Sprintf("healthy entity count %d is below required %d",
				tally.Healthy, request.Policy.MinimumHealthyEntities),
		})
	}
	if tally.Unhealthy > request.Policy.MaximumUnhealthyEntities {
		blockers = append(blockers, PromotionBlocker{
			Kind: PromotionBlockerUnhealthyAboveMaximum,
			Message: fmt.Sprintf("unhealthy entity count %d exceeds allowed %d",
				tally.Unhealthy, request.Policy.MaximumUnhealthyEntities),
		})
	}
	sortBlockers(blockers)
	return blockers
}

func promotionChannelReachable(promotions []PromotionRecord, product, version, from, to string) bool {
	if from == to {
		return true
	}
	visited := map[string]struct{}{from: {}}
	queue := []string{from}
	for len(queue) > 0 {
		channel := queue[0]
		queue = queue[1:]
		for _, promotion := range promotions {
			if promotion.Status != PromotionPassed || promotion.Product != product || promotion.Version != version || promotion.FromChannel != channel {
				continue
			}
			if promotion.ToChannel == to {
				return true
			}
			if _, exists := visited[promotion.ToChannel]; !exists {
				visited[promotion.ToChannel] = struct{}{}
				queue = append(queue, promotion.ToChannel)
			}
		}
	}
	return false
}

func weekdayDuration(start, end time.Time) time.Duration {
	if !end.After(start) {
		return 0
	}
	var total time.Duration
	for cursor := start; cursor.Before(end); {
		next := cursor.Truncate(24 * time.Hour).Add(24 * time.Hour)
		if next.After(end) {
			next = end
		}
		if cursor.Weekday() != time.Saturday && cursor.Weekday() != time.Sunday {
			total += next.Sub(cursor)
		}
		cursor = next
	}
	return total
}

func promotionIdentity(record PromotionRecord) (string, error) {
	record.ID = ""
	record.Revision = 0
	contents, err := json.Marshal(record)
	if err != nil {
		return "", fmt.Errorf("encode promotion record: %w", err)
	}
	digest := sha256.Sum256(contents)
	return "pro_" + hex.EncodeToString(digest[:16]), nil
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
		sortBlockers(blockers)
		expectedStatus := PromotionPassed
		if len(blockers) > 0 {
			expectedStatus = PromotionBlocked
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

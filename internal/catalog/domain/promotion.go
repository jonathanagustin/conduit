package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"time"
)

// The health vocabulary a promotion reads. It is restated rather than imported
// for the same reason the recall rules restate what they need: this package
// depends on nothing, and a promotion that had to import the context which
// happens to define these strings would be a promotion that could not be
// decided on its own.
const (
	HealthHealthy   = "HEALTHY"
	HealthUnhealthy = "UNHEALTHY"
)

// A DNS label, restated for the same reason. Health evidence names an
// Environment, and evidence naming something that is not an Environment
// identifier is evidence about nothing.
var dnsLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)

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
	// EnvironmentLabels is the labels the Hub held for this Environment at the
	// instant of evaluation, stamped on by the Hub and never by the submitter.
	// It exists so PromotionEntityFilter can select a canary tier by label
	// instead of by name -- the evaluator cannot read an Environment, and a
	// filter matching on labels it cannot see would silently select nothing.
	//
	// It is carried on the record rather than resolved again at replay, and that
	// is load-bearing. Label-based selection is not additive: narrowing a canary
	// set can make a promotion PASS, so it cannot be taken on trust the way an
	// Environment-dependent BLOCKER is (see validatePromotions). Freezing the
	// labels the decision actually used is what keeps a record re-derivable from
	// itself, and keeps a later relabelling from rewriting a decision already
	// made.
	EnvironmentLabels map[string]string `json:"environmentLabels,omitempty"`
}

type PromotionEvaluationRequest struct {
	PreviousEvaluationID string                    `json:"previousEvaluationId,omitempty"`
	Termination          PromotionLifecycle        `json:"termination,omitempty"`
	TerminationReason    string                    `json:"terminationReason,omitempty"`
	Release              Release                   `json:"release"`
	FromChannel          string                    `json:"fromChannel"`
	ToChannel            string                    `json:"toChannel"`
	Policy               PromotionPolicy           `json:"policy"`
	Evidence             []PromotionHealthEvidence `json:"evidence"`
	EvaluationStartedAt  time.Time                 `json:"evaluationStartedAt,omitempty"`
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
	// PromotionBlockerEvidenceBoundConflict names the case where an operator's
	// two statements disagree: an Environment declared to report intermittently
	// cannot satisfy an evidence-age bound shorter than its connection cadence.
	// It accompanies EVIDENCE_STALE rather than replacing it -- the promotion is
	// blocked either way, and the generic blocker still has to derive for the
	// record to reproduce -- so what this adds is a true sentence about which
	// two settings are in conflict, in place of a staleness report that reads
	// like an outage.
	PromotionBlockerEvidenceBoundConflict = "ENVIRONMENT_EVIDENCE_BOUND_CONFLICT"

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

// SortBlockers orders blockers by the message they render to, which is the
// order Reasons has always been in. Keeping the two in step is what lets
// Reasons stay a projection of Blockers rather than a second source of truth.
func SortBlockers(blockers []PromotionBlocker) {
	sort.Slice(blockers, func(i, j int) bool { return blockers[i].Message < blockers[j].Message })
}

// BlockersOfKind is how a caller acts on one category of blocker without
// reading the prose -- the reason Kind exists.
func BlockersOfKind(blockers []PromotionBlocker, kind string) []PromotionBlocker {
	var matched []PromotionBlocker
	for _, blocker := range blockers {
		if blocker.Kind == kind {
			matched = append(matched, blocker)
		}
	}
	return matched
}

func PromotionBlockersEqual(left, right []PromotionBlocker) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Kind != right[index].Kind || left[index].Message != right[index].Message ||
			!sameStrings(left[index].Subjects, right[index].Subjects) {
			return false
		}
	}
	return true
}

func BlockerMessages(blockers []PromotionBlocker) []string {
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
	RunID                string                    `json:"runId,omitempty"`
	PreviousEvaluationID string                    `json:"previousEvaluationId,omitempty"`
	Lifecycle            PromotionLifecycle        `json:"lifecycle,omitempty"`
	LifecycleReason      string                    `json:"lifecycleReason,omitempty"`
	ValueTier            string                    `json:"valueTier,omitempty"`
	ID                   string                    `json:"id"`
	Product              string                    `json:"product"`
	Version              string                    `json:"version"`
	FromChannel          string                    `json:"fromChannel"`
	ToChannel            string                    `json:"toChannel"`
	Labels               map[string]string         `json:"labels,omitempty"`
	Policy               PromotionPolicy           `json:"policy"`
	Evidence             []PromotionHealthEvidence `json:"evidence"`
	Status               PromotionStatus           `json:"status"`
	// Blockers is the structured form; Reasons is derived from it and kept
	// because it is what the record has always published on the wire.
	Blockers            []PromotionBlocker `json:"blockers,omitempty"`
	Reasons             []string           `json:"reasons,omitempty"`
	EvaluationStartedAt time.Time          `json:"evaluationStartedAt,omitempty"`
	EvaluatedAt         time.Time          `json:"evaluatedAt"`
	Revision            uint64             `json:"revision"`
}

func DefaultPromotionStageControl(product, fromChannel, toChannel string) PromotionStageControl {
	return PromotionStageControl{
		ID: PromotionStageControlID(product, fromChannel, toChannel), Product: product,
		FromChannel: fromChannel, ToChannel: toChannel, Status: PromotionStageActive,
	}
}

func PromotionStageControlID(product, fromChannel, toChannel string) string {
	digest := sha256.Sum256([]byte(product + "\x00" + fromChannel + "\x00" + toChannel))
	return "psc_" + hex.EncodeToString(digest[:16])
}

func EvaluatePromotion(request PromotionEvaluationRequest, now time.Time) []PromotionBlocker {
	blockers := EvaluateLabelRequirements(request.Release, request.Policy)
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
	if StageType(request.Policy) == PromotionCanary {
		blockers = append(blockers, EvaluateCanaryStage(request, now)...)
		SortBlockers(blockers)
		return blockers
	}
	// A timed stage measures how long the Release has held healthy. "No delay"
	// waives that wait entirely, which is how a stage promotes on labels alone.
	minimumDuration := time.Duration(request.Policy.MinimumHealthyDurationSeconds) * time.Second
	if request.Policy.NoDelay {
		minimumDuration = 0
	}
	valid, evidenceBlockers := ValidateEvidence(request.Evidence, request.Policy, now)
	blockers = append(blockers, evidenceBlockers...)
	tally := CountHeld(valid, request.Policy, minimumDuration)
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
	SortBlockers(blockers)
	return blockers
}

func PromotionChannelReachable(promotions []PromotionRecord, product, version, from, to string) bool {
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

func WeekdayDuration(start, end time.Time) time.Duration {
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

func PromotionIdentity(record PromotionRecord) (string, error) {
	record.ID = ""
	record.Revision = 0
	contents, err := json.Marshal(record)
	if err != nil {
		return "", fmt.Errorf("encode promotion record: %w", err)
	}
	digest := sha256.Sum256(contents)
	return "pro_" + hex.EncodeToString(digest[:16]), nil
}

func sameStrings(left, right []string) bool {
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

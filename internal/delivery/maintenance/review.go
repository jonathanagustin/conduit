package maintenance

import (
	"context"
	"errors"
	"strings"
	"time"
)

const MaximumOverrideDuration = 3600 * time.Hour
const Native = "CONDUIT_NATIVE"

type EntityTarget struct {
	Environment string `json:"environment"`
	Entity      string `json:"entity"`
}

// OverrideDraft carries a bounded maintenance proposal through existing governance.
//
// CONDUIT-INVENTED: maintenance.ScheduledEntityWindow
// Displaced-by: a published or sanitized future Entity maintenance proposal and pre-effect preview-binding contract.
type OverrideDraft struct {
	ExpectedChangeRequestID string `json:"expectedChangeRequestId,omitempty"`
	// StartsAt is a native scheduled proposal; nil retains submission-time semantics.
	StartsAt         *time.Time     `json:"startsAt,omitempty"`
	Entities         []EntityTarget `json:"entities"`
	Duration         string         `json:"duration"`
	Downtime         bool           `json:"downtime"`
	Reason           string         `json:"reason"`
	ComplianceTicket string         `json:"complianceTicket,omitempty"`
	DryRun           bool           `json:"dryRun"`
}
type OverrideReceipt struct {
	ChangeRequestID string `json:"changeRequestId"`
	EntityTarget
	Status   string    `json:"status"`
	StartsAt time.Time `json:"startsAt"`
	EndsAt   time.Time `json:"endsAt"`
	Type     string    `json:"type"`
}
type OverrideBatch struct {
	ValueTier    string            `json:"valueTier"`
	DryRun       bool              `json:"dryRun"`
	Requests     []OverrideReceipt `json:"requests"`
	FailedTarget *EntityTarget     `json:"failedTarget,omitempty"`
	Failure      string            `json:"failure,omitempty"`
}
type OverrideController interface {
	CreateMaintenanceOverrides(context.Context, OverrideDraft) (OverrideBatch, error)
}

func ValidateDraft(draft OverrideDraft) (time.Duration, error) {
	duration, err := time.ParseDuration(draft.Duration)
	if err != nil || duration <= 0 || duration > MaximumOverrideDuration || len(draft.Entities) == 0 || len(draft.Entities) > 256 || strings.TrimSpace(draft.Reason) == "" || len(draft.Reason) > 4096 || len(draft.ComplianceTicket) > 512 {
		return 0, errors.New("maintenance overrides require entities, a reason and a positive duration no greater than 3600h")
	}
	if draft.StartsAt != nil && (draft.StartsAt.IsZero() || draft.StartsAt.Add(duration).Year() > 9999) {
		return 0, errors.New("maintenance start must be nonzero with a representable end")
	}
	if draft.ExpectedChangeRequestID != "" && (len(draft.ExpectedChangeRequestID) > 256 || len(draft.Entities) != 1 || draft.StartsAt == nil) {
		return 0, errors.New("preview binding requires one Entity and an explicit start")
	}
	for _, target := range draft.Entities {
		if target.Environment == "" || target.Entity == "" || len(target.Environment) > 256 || len(target.Entity) > 256 || strings.ContainsAny(target.Environment+target.Entity, "\r\n\x00:") {
			return 0, errors.New("invalid maintenance override target")
		}
	}
	return duration, nil
}

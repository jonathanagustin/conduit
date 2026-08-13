package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type SuppressionScope string
type SuppressionSource string

const (
	SuppressionEnvironment SuppressionScope = "ENVIRONMENT"
	SuppressionEntity      SuppressionScope = "ENTITY"

	SuppressionManual              SuppressionSource = "MANUAL"
	SuppressionPlanFailure         SuppressionSource = "PLAN_FAILURE"
	SuppressionPromotion           SuppressionSource = "PROMOTION"
	SuppressionEnvironmentEscalate SuppressionSource = "ENVIRONMENT_ESCALATION"

	defaultAutoSuppressionThreshold = 30
)

type SuppressionRecord struct {
	ID             string            `json:"id"`
	Environment    string            `json:"environment"`
	InstallationID string            `json:"installationId,omitempty"`
	Scope          SuppressionScope  `json:"scope"`
	Source         SuppressionSource `json:"source"`
	Reason         string            `json:"reason"`
	CreatedBy      string            `json:"createdBy"`
	PlanID         string            `json:"planId,omitempty"`
	PromotionID    string            `json:"promotionId,omitempty"`
	Product        string            `json:"product,omitempty"`
	Version        string            `json:"version,omitempty"`
	StartsAt       time.Time         `json:"startsAt"`
	EndsAt         *time.Time        `json:"endsAt,omitempty"`
	CreatedAt      time.Time         `json:"createdAt"`
	RemovedAt      *time.Time        `json:"removedAt,omitempty"`
	RemovedBy      string            `json:"removedBy,omitempty"`
	Threshold      int               `json:"autoSuppressionThreshold,omitempty"`
	Revision       uint64            `json:"revision"`
}

func (lifecycle *Lifecycle) reconcilePromotionSuppressions(ctx context.Context, promotion PromotionRecord) error {
	environments := map[string]struct{}{}
	for _, evidence := range promotion.Evidence {
		environments[evidence.Environment] = struct{}{}
		if promotion.Status != PromotionBlocked || evidence.Health != "UNHEALTHY" {
			continue
		}
		now := lifecycle.now().UTC()
		record := SuppressionRecord{
			Environment: evidence.Environment, InstallationID: evidence.InstallationID, Scope: SuppressionEntity,
			Source: SuppressionPromotion, Reason: "promotion health gate failed for " + promotion.Product + ":" + promotion.Version,
			CreatedBy: "platform-delivery", PromotionID: promotion.ID, Product: promotion.Product, Version: promotion.Version,
			StartsAt: now, CreatedAt: now, Threshold: defaultAutoSuppressionThreshold, Revision: 1,
		}
		record.ID = suppressionID(record)
		if _, err := lifecycle.repository.CreateSuppression(ctx, record); err != nil && !errors.Is(err, ErrConflict) {
			return err
		}
		if err := lifecycle.reconcileEnvironmentEscalation(ctx, evidence.Environment, defaultAutoSuppressionThreshold); err != nil {
			return err
		}
	}
	if promotion.Status != PromotionPassed {
		return nil
	}
	for environment := range environments {
		records, err := lifecycle.repository.ListSuppressions(ctx, environment)
		if err != nil {
			return err
		}
		for _, record := range records {
			if record.Source != SuppressionPromotion || record.Product != promotion.Product || record.Version != promotion.Version || !record.Active(lifecycle.now().UTC()) {
				continue
			}
			if _, err := lifecycle.RemoveSuppression(ctx, record.ID, "platform-delivery"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (record SuppressionRecord) Active(at time.Time) bool {
	return record.RemovedAt == nil && !record.StartsAt.After(at) && (record.EndsAt == nil || record.EndsAt.After(at))
}

func (lifecycle *Lifecycle) CreateManualSuppression(
	ctx context.Context,
	environment string,
	installationID string,
	reason string,
	createdBy string,
	startsAt time.Time,
	endsAt *time.Time,
) (SuppressionRecord, error) {
	environment = strings.TrimSpace(environment)
	installationID = strings.TrimSpace(installationID)
	reason = strings.TrimSpace(reason)
	createdBy = strings.TrimSpace(createdBy)
	if !dnsLabelPattern.MatchString(environment) || reason == "" || createdBy == "" {
		return SuppressionRecord{}, fmt.Errorf("%w: Environment, reason, and creator are required", ErrInvalidTransition)
	}
	if startsAt.IsZero() {
		startsAt = lifecycle.now().UTC()
	} else {
		startsAt = startsAt.UTC()
	}
	if endsAt != nil {
		end := endsAt.UTC()
		endsAt = &end
		if !end.After(startsAt) {
			return SuppressionRecord{}, fmt.Errorf("%w: suppression end must be after its start", ErrInvalidTransition)
		}
	}
	scope := SuppressionEnvironment
	if installationID != "" {
		scope = SuppressionEntity
	}
	record := SuppressionRecord{
		Environment: environment, InstallationID: installationID, Scope: scope,
		Source: SuppressionManual, Reason: reason, CreatedBy: createdBy,
		StartsAt: startsAt, EndsAt: endsAt, CreatedAt: lifecycle.now().UTC(), Revision: 1,
	}
	record.ID = suppressionID(record)
	return lifecycle.repository.CreateSuppression(ctx, record)
}

func (lifecycle *Lifecycle) RemoveSuppression(
	ctx context.Context, suppressionID, removedBy string,
) (SuppressionRecord, error) {
	removedBy = strings.TrimSpace(removedBy)
	if removedBy == "" {
		return SuppressionRecord{}, fmt.Errorf("%w: remover is required", ErrInvalidTransition)
	}
	record, err := lifecycle.repository.UpdateSuppression(ctx, suppressionID, func(record *SuppressionRecord) error {
		if record.RemovedAt != nil {
			return fmt.Errorf("%w: suppression is already removed", ErrConflict)
		}
		now := lifecycle.now().UTC()
		record.RemovedAt = &now
		record.RemovedBy = removedBy
		return nil
	})
	if err != nil {
		return SuppressionRecord{}, err
	}
	if record.Scope == SuppressionEntity && record.Source != SuppressionManual {
		if err := lifecycle.reconcileEnvironmentEscalation(ctx, record.Environment, record.Threshold); err != nil {
			return SuppressionRecord{}, err
		}
	}
	return record, nil
}

func (lifecycle *Lifecycle) createPlanFailureSuppression(
	ctx context.Context, plan Plan, planID, installationID, failure string,
) error {
	if strings.TrimSpace(installationID) == "" {
		return nil
	}
	threshold := plan.AutoSuppressionThreshold
	if threshold == 0 {
		threshold = defaultAutoSuppressionThreshold
	}
	now := lifecycle.now().UTC()
	record := SuppressionRecord{
		Environment: plan.Environment, InstallationID: installationID, Scope: SuppressionEntity,
		Source: SuppressionPlanFailure, Reason: strings.TrimSpace(failure), CreatedBy: "platform-delivery",
		PlanID: planID, StartsAt: now, CreatedAt: now, Threshold: threshold, Revision: 1,
	}
	record.ID = suppressionID(record)
	if _, err := lifecycle.repository.CreateSuppression(ctx, record); err != nil && !errors.Is(err, ErrConflict) {
		return err
	}
	return lifecycle.reconcileEnvironmentEscalation(ctx, plan.Environment, threshold)
}

func (lifecycle *Lifecycle) reconcileEnvironmentEscalation(ctx context.Context, environment string, threshold int) error {
	if threshold == 0 {
		threshold = defaultAutoSuppressionThreshold
	}
	records, err := lifecycle.repository.ListSuppressions(ctx, environment)
	if err != nil {
		return err
	}
	now := lifecycle.now().UTC()
	automaticallySuppressedEntities := make(map[string]struct{})
	var escalation *SuppressionRecord
	for index := range records {
		record := records[index]
		if record.Source == SuppressionEnvironmentEscalate && record.Active(now) {
			copy := record
			escalation = &copy
		}
		if record.Scope == SuppressionEntity && record.Source != SuppressionManual && record.Active(now) {
			automaticallySuppressedEntities[record.InstallationID] = struct{}{}
		}
	}
	if len(automaticallySuppressedEntities) > threshold && escalation == nil {
		record := SuppressionRecord{
			Environment: environment, Scope: SuppressionEnvironment, Source: SuppressionEnvironmentEscalate,
			Reason:    fmt.Sprintf("more than %d Entities are automatically suppressed", threshold),
			CreatedBy: "platform-delivery", StartsAt: now, CreatedAt: now, Threshold: threshold, Revision: 1,
		}
		record.ID = suppressionID(record)
		_, err = lifecycle.repository.CreateSuppression(ctx, record)
		return err
	}
	if len(automaticallySuppressedEntities) <= threshold && escalation != nil {
		_, err = lifecycle.repository.UpdateSuppression(ctx, escalation.ID, func(record *SuppressionRecord) error {
			record.RemovedAt = &now
			record.RemovedBy = "platform-delivery"
			return nil
		})
	}
	return err
}

func activeSuppressionForPlan(records []SuppressionRecord, plan Plan, now time.Time) *SuppressionRecord {
	installations := make(map[string]struct{}, len(plan.Steps))
	for _, step := range plan.Steps {
		installations[step.InstallationID] = struct{}{}
	}
	active := make([]SuppressionRecord, 0)
	for _, record := range records {
		if !record.Active(now) {
			continue
		}
		if record.Scope == SuppressionEnvironment {
			active = append(active, record)
			continue
		}
		if _, exists := installations[record.InstallationID]; exists {
			active = append(active, record)
		}
	}
	if len(active) == 0 {
		return nil
	}
	sort.Slice(active, func(i, j int) bool { return active[i].ID < active[j].ID })
	return &active[0]
}

func suppressionID(record SuppressionRecord) string {
	identity := strings.Join([]string{
		record.Environment, record.InstallationID, string(record.Scope), string(record.Source),
		record.PlanID, record.PromotionID, record.Product, record.Version, record.StartsAt.UTC().Format(time.RFC3339Nano), record.Reason,
	}, "\x00")
	digest := sha256.Sum256([]byte(identity))
	return "sup_" + hex.EncodeToString(digest[:16])
}

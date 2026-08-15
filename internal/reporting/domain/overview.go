package domain

import (
	"sort"
	"time"
)

// MaximumOverviewItems is how many items a panel shows. The count it reports is
// the true one, so a bounded panel never understates how much is wrong.
const MaximumOverviewItems = 5

// OverviewStaleAfter is how old an environment report may be before the fleet
// counts it stale. It is also the age the overview's fleet snapshot is taken
// at, so the staleness a panel counts is the staleness it was built from.
const OverviewStaleAfter = 5 * time.Minute

// A panel is CURRENT when its source answered, and UNAVAILABLE when it did not.
// The distinction is the point: an empty CURRENT panel says there is nothing
// wrong, and an UNAVAILABLE one says nobody knows.
const (
	OverviewCurrent     = "CURRENT"
	OverviewUnavailable = "UNAVAILABLE"
)

// Panel is one section of the overview.
type Panel[T any] struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
	Items  []T    `json:"items"`
}

// The four item shapes an overview panel serves.
type (
	OverviewChangeRequest struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Environment string `json:"environment"`
	}

	OverviewPlan struct {
		ID          string `json:"id"`
		Environment string `json:"environment"`
	}

	OverviewInstallation struct {
		Environment    string `json:"environment"`
		InstallationID string `json:"installationId"`
		Health         string `json:"health"`
	}

	OverviewPromotion struct {
		ID          string `json:"id"`
		Product     string `json:"product"`
		Version     string `json:"version"`
		FromChannel string `json:"fromChannel"`
		ToChannel   string `json:"toChannel"`
	}
)

// OverviewProjection is the whole view. Every panel is independently available,
// because the stores behind them fail independently and a reader is better
// served by five working panels and one honest gap than by nothing.
type OverviewProjection struct {
	GeneratedAt       time.Time                    `json:"generatedAt"`
	StaleAfterSeconds int64                        `json:"staleAfterSeconds"`
	AwaitingApprovals Panel[OverviewChangeRequest] `json:"awaitingApprovals"`
	FailedPlans       Panel[OverviewPlan]          `json:"failedPlans"`
	RunningPlans      Panel[OverviewPlan]          `json:"runningPlans"`
	UnhealthyEntities Panel[OverviewInstallation]  `json:"unhealthyEntities"`
	StaleReports      Panel[struct{}]              `json:"staleReports"`
	BlockedPromotions Panel[OverviewPromotion]     `json:"blockedPromotions"`
}

// PlanPhase is Reporting's reading of a plan's status, and the whole of it.
// What makes a plan failed or running is Delivery's rule; a snapshot arrives
// already classified so that this package cannot hold a second copy of it.
type PlanPhase string

const (
	PlanPhaseFailed  PlanPhase = "FAILED"
	PlanPhaseRunning PlanPhase = "RUNNING"
	PlanPhaseOther   PlanPhase = "OTHER"
)

// The four snapshot shapes an overview is built from. Each carries only what a
// panel names, plus the one classification its owning context has already made.
type (
	PlanSummary struct {
		ID          string
		Environment string
		CreatedAt   time.Time
		Phase       PlanPhase
	}

	// InstallationSummary is one fleet installation as the overview reads it.
	// Health is a value this package understands; Stale is the fleet
	// projection's own answer at the snapshot's moment.
	InstallationSummary struct {
		Environment    string
		InstallationID string
		Health         string
		Stale          bool
	}

	// ChangeRequestSummary carries Governance's judgement that a change is
	// awaiting approval, rather than a status string this package interprets.
	ChangeRequestSummary struct {
		ID               string
		Title            string
		Environment      string
		AwaitingApproval bool
	}

	// PromotionSummary carries Catalog's judgement that a promotion is
	// blocked, for the same reason.
	PromotionSummary struct {
		ID          string
		Product     string
		Version     string
		FromChannel string
		ToChannel   string
		Blocked     bool
	}
)

// NewOverviewProjection is the view before anything has answered: every panel
// unavailable. Building up from here is what makes an unanswered store show as
// a gap rather than as "nothing is wrong".
func NewOverviewProjection(now time.Time) OverviewProjection {
	if now.IsZero() {
		now = time.Now()
	}
	return OverviewProjection{
		GeneratedAt:       now.UTC(),
		StaleAfterSeconds: int64(OverviewStaleAfter / time.Second),
		AwaitingApprovals: UnavailablePanel[OverviewChangeRequest](),
		FailedPlans:       UnavailablePanel[OverviewPlan](),
		RunningPlans:      UnavailablePanel[OverviewPlan](),
		UnhealthyEntities: UnavailablePanel[OverviewInstallation](),
		StaleReports:      UnavailablePanel[struct{}](),
		BlockedPromotions: UnavailablePanel[OverviewPromotion](),
	}
}

// CurrentPanel reports the true count and shows at most MaximumOverviewItems.
func CurrentPanel[T any](items []T) Panel[T] {
	count := len(items)
	if len(items) > MaximumOverviewItems {
		items = items[:MaximumOverviewItems]
	}
	if items == nil {
		items = []T{}
	}
	return Panel[T]{Status: OverviewCurrent, Count: count, Items: items}
}

// UnavailablePanel is a panel whose source did not answer.
func UnavailablePanel[T any]() Panel[T] {
	return Panel[T]{Status: OverviewUnavailable, Items: []T{}}
}

// PlanPanels splits the plans a reader may see into the failed and running
// panels, newest first with ties broken by identifier so that two builds of the
// same snapshot agree on which five are shown.
func PlanPanels(plans []PlanSummary) (failed, running Panel[OverviewPlan]) {
	ordered := append([]PlanSummary(nil), plans...)
	sort.Slice(ordered, func(i, j int) bool {
		if !ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].CreatedAt.After(ordered[j].CreatedAt)
		}
		return ordered[i].ID < ordered[j].ID
	})
	failedItems, runningItems := make([]OverviewPlan, 0), make([]OverviewPlan, 0)
	for _, plan := range ordered {
		item := OverviewPlan{ID: plan.ID, Environment: plan.Environment}
		switch plan.Phase {
		case PlanPhaseFailed:
			failedItems = append(failedItems, item)
		case PlanPhaseRunning:
			runningItems = append(runningItems, item)
		}
	}
	return CurrentPanel(failedItems), CurrentPanel(runningItems)
}

// InstallationPanels reads the fleet: which installations are unhealthy, and
// how many reports are too old to believe.
//
// UNKNOWN is not unhealthy. An installation nobody has reported on is already
// counted by the stale panel, and listing it as unhealthy would report the same
// silence twice under two different names.
func InstallationPanels(
	installations []InstallationSummary,
) (unhealthy Panel[OverviewInstallation], stale Panel[struct{}]) {
	items := make([]OverviewInstallation, 0)
	staleCount := 0
	for _, installation := range installations {
		if installation.Health != HealthHealthy && installation.Health != HealthUnknown {
			items = append(items, OverviewInstallation{
				Environment: installation.Environment, InstallationID: installation.InstallationID,
				Health: installation.Health,
			})
		}
		if installation.Stale {
			staleCount++
		}
	}
	// The stale panel is a count with nothing to list: naming the stale
	// installations would be a second fleet view, and the fleet view is where
	// that belongs.
	return CurrentPanel(items), Panel[struct{}]{Status: OverviewCurrent, Count: staleCount, Items: []struct{}{}}
}

// AwaitingApprovalsPanel lists the changes waiting on a person.
func AwaitingApprovalsPanel(requests []ChangeRequestSummary) Panel[OverviewChangeRequest] {
	items := make([]OverviewChangeRequest, 0)
	for _, request := range requests {
		if request.AwaitingApproval {
			items = append(items, OverviewChangeRequest{
				ID: request.ID, Title: request.Title, Environment: request.Environment,
			})
		}
	}
	return CurrentPanel(items)
}

// BlockedPromotionsPanel lists the promotions that did not pass.
func BlockedPromotionsPanel(promotions []PromotionSummary) Panel[OverviewPromotion] {
	items := make([]OverviewPromotion, 0)
	for _, promotion := range promotions {
		if promotion.Blocked {
			items = append(items, OverviewPromotion{
				ID: promotion.ID, Product: promotion.Product, Version: promotion.Version,
				FromChannel: promotion.FromChannel, ToChannel: promotion.ToChannel,
			})
		}
	}
	return CurrentPanel(items)
}

package controlplane

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

var errInvalidPlanCursor = errors.New("invalid Plan cursor")

type PlanQuery struct {
	Environment    string `json:"environment,omitempty"`
	Status         string `json:"status,omitempty"`
	InstallationID string `json:"installationId,omitempty"`
	Search         string `json:"search,omitempty"`
	View           string `json:"view"`
}

type PlanSummary struct {
	ID                string     `json:"id"`
	Environment       string     `json:"environment"`
	Status            PlanStatus `json:"status"`
	StepCount         int        `json:"stepCount"`
	ApprovalCount     int        `json:"approvalCount"`
	RequiredApprovals int        `json:"requiredApprovals"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	Revision          uint64     `json:"revision"`
}

type PlanPage struct {
	Plans []PlanRecord `json:"plans"`
	Total int          `json:"total"`
	PageMetadata
}

type PlanSummaryPage struct {
	Plans []PlanSummary `json:"plans"`
	Total int           `json:"total"`
	PageMetadata
}

func paginatePlans(records []PlanRecord, query PlanQuery, cursor string, limit int) (PlanPage, error) {
	filtered := filterPlans(records, query)
	page, metadata, err := paginateOrdered(filtered, planOrderKey, fingerprintJSON(query), cursor, limit, errInvalidPlanCursor)
	return PlanPage{Plans: page, Total: len(filtered), PageMetadata: metadata}, err
}

func summarizePlanPage(page PlanPage) PlanSummaryPage {
	summaries := make([]PlanSummary, 0, len(page.Plans))
	for _, record := range page.Plans {
		summaries = append(summaries, PlanSummaryFor(record))
	}
	return PlanSummaryPage{Plans: summaries, Total: page.Total, PageMetadata: page.PageMetadata}
}

// PlanSummaryFor projects an already-authorized native result; it grants no read authority.
func PlanSummaryFor(record PlanRecord) PlanSummary {
	return PlanSummary{ID: record.ID, Environment: record.Plan.Environment, Status: record.Status, StepCount: len(record.Plan.Steps), ApprovalCount: len(record.Approvals), RequiredApprovals: record.RequiredApprovals, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, Revision: record.Revision}
}

func filterPlans(records []PlanRecord, query PlanQuery) []PlanRecord {
	search := normalizedSearch(query.Search)
	filtered := make([]PlanRecord, 0, len(records))
	for _, record := range records {
		if query.Environment != "" && record.Plan.Environment != query.Environment {
			continue
		}
		if query.Status != "" && !strings.EqualFold(string(record.Status), query.Status) {
			continue
		}
		if query.InstallationID != "" && !planTargetsInstallation(record, query.InstallationID) {
			continue
		}
		if search != "" && !planMatchesSearch(record, search) {
			continue
		}
		filtered = append(filtered, record)
	}
	sort.Slice(filtered, func(i, j int) bool { return planOrderKey(filtered[i]) < planOrderKey(filtered[j]) })
	return filtered
}

func planMatchesSearch(record PlanRecord, search string) bool {
	if strings.Contains(strings.ToLower(record.ID), search) ||
		strings.Contains(strings.ToLower(record.Plan.Environment), search) ||
		strings.Contains(strings.ToLower(string(record.Status)), search) {
		return true
	}
	for _, step := range record.Plan.Steps {
		if strings.Contains(strings.ToLower(step.InstallationID), search) ||
			strings.Contains(strings.ToLower(step.Product), search) ||
			strings.Contains(strings.ToLower(step.Entity), search) {
			return true
		}
	}
	return false
}

// Inverting the signed-sortable nanosecond value makes lexical key order match
// newest-first time order. The unique ID closes ties without cursor ambiguity.
func planOrderKey(record PlanRecord) string {
	sortable := uint64(record.CreatedAt.UnixNano()) ^ (uint64(1) << 63)
	return fmt.Sprintf("%016x\x00%s", math.MaxUint64-sortable, record.ID)
}

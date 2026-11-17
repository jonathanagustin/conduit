package controlplane

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

var (
	errInvalidChangeRequestCursor = errors.New("invalid Change Request cursor")
	errInvalidPromotionCursor     = errors.New("invalid Promotion cursor")
)

type ChangeRequestQuery struct {
	Environment string `json:"environment,omitempty"`
	Status      string `json:"status,omitempty"`
	Search      string `json:"search,omitempty"`
}

type ChangeRequestPage struct {
	ChangeRequests []ChangeRequest `json:"changeRequests"`
	Total          int             `json:"total"`
	PageMetadata
}

type PromotionQuery struct {
	Product string `json:"product,omitempty"`
	Status  string `json:"status,omitempty"`
	Search  string `json:"search,omitempty"`
}

type PromotionPage struct {
	Promotions []PromotionRecord `json:"promotions"`
	Total      int               `json:"total"`
	PageMetadata
}

func paginateChangeRequests(records []ChangeRequest, query ChangeRequestQuery, cursor string, limit int) (ChangeRequestPage, error) {
	return paginateChangeRequestsInSpace(records, query, cursor, limit, "")
}

func paginateChangeRequestsInSpace(records []ChangeRequest, query ChangeRequestQuery, cursor string, limit int, spaceID string) (ChangeRequestPage, error) {
	query = normalizedChangeRequestQuery(query)
	filtered := make([]ChangeRequest, 0, len(records))
	for _, record := range records {
		if query.Environment != "" && record.Environment != query.Environment {
			continue
		}
		if query.Status != "" && !strings.EqualFold(record.Status, query.Status) {
			continue
		}
		if query.Search != "" && !changeRequestMatchesSearch(record, query.Search) {
			continue
		}
		filtered = append(filtered, record)
	}
	sort.Slice(filtered, func(i, j int) bool { return changeRequestOrderKey(filtered[i]) < changeRequestOrderKey(filtered[j]) })
	page, metadata, err := paginateOrdered(
		filtered, changeRequestOrderKey, fingerprintInSpace(query, spaceID), cursor, limit, errInvalidChangeRequestCursor,
	)
	return ChangeRequestPage{ChangeRequests: page, Total: len(filtered), PageMetadata: metadata}, err
}

func paginatePromotions(records []PromotionRecord, query PromotionQuery, cursor string, limit int) (PromotionPage, error) {
	return paginatePromotionsInSpace(records, query, cursor, limit, "")
}

func paginatePromotionsInSpace(records []PromotionRecord, query PromotionQuery, cursor string, limit int, spaceID string) (PromotionPage, error) {
	query = normalizedPromotionQuery(query)
	filtered := make([]PromotionRecord, 0, len(records))
	for _, record := range records {
		if query.Product != "" && record.Product != query.Product {
			continue
		}
		if query.Status != "" && !strings.EqualFold(string(record.Status), query.Status) {
			continue
		}
		if query.Search != "" && !promotionMatchesSearch(record, query.Search) {
			continue
		}
		filtered = append(filtered, record)
	}
	sort.Slice(filtered, func(i, j int) bool { return promotionOrderKey(filtered[i]) < promotionOrderKey(filtered[j]) })
	page, metadata, err := paginateOrdered(
		filtered, promotionOrderKey, fingerprintInSpace(query, spaceID), cursor, limit, errInvalidPromotionCursor,
	)
	return PromotionPage{Promotions: page, Total: len(filtered), PageMetadata: metadata}, err
}

func normalizedChangeRequestQuery(query ChangeRequestQuery) ChangeRequestQuery {
	query.Environment = strings.TrimSpace(query.Environment)
	query.Status = strings.ToUpper(strings.TrimSpace(query.Status))
	query.Search = normalizedSearch(query.Search)
	return query
}

func normalizedPromotionQuery(query PromotionQuery) PromotionQuery {
	query.Product = strings.TrimSpace(query.Product)
	query.Status = strings.ToUpper(strings.TrimSpace(query.Status))
	query.Search = normalizedSearch(query.Search)
	return query
}

func changeRequestMatchesSearch(record ChangeRequest, search string) bool {
	for _, candidate := range []string{
		record.ID, record.PlanID, record.Environment, record.Title, record.Description,
		record.Author.Username, record.Status, record.TargetKind, record.TargetID,
	} {
		if strings.Contains(strings.ToLower(candidate), search) {
			return true
		}
	}
	return false
}

func promotionMatchesSearch(record PromotionRecord, search string) bool {
	for _, candidate := range []string{
		record.ID, record.Product, record.Version, record.FromChannel, record.ToChannel, string(record.Status),
	} {
		if strings.Contains(strings.ToLower(candidate), search) {
			return true
		}
	}
	return false
}

func changeRequestOrderKey(record ChangeRequest) string {
	priority := 1
	if record.Status == ChangePendingApprovals {
		priority = 0
	}
	return fmt.Sprintf("%d%s\x00%s", priority, descendingTimeKey(record.UpdatedAt), record.ID)
}

func promotionOrderKey(record PromotionRecord) string {
	return descendingTimeKey(record.EvaluatedAt) + "\x00" + record.ID
}

func descendingTimeKey(value time.Time) string {
	sortable := uint64(value.UnixNano()) ^ (uint64(1) << 63)
	return fmt.Sprintf("%016x", math.MaxUint64-sortable)
}

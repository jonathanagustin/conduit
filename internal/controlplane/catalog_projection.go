package controlplane

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

var (
	errInvalidEnvironmentCursor = errors.New("invalid Environment cursor")
	errInvalidProductCursor     = errors.New("invalid Product cursor")
)

const (
	SpokeLive        = "live"
	SpokeStale       = "stale"
	SpokeUnreachable = "unreachable"
	// SpokeLivenessFreshFor is three default spoke poll intervals. One missed
	// poll stays live. A report older than this is stale. No report, no agent,
	// or no observation time is unreachable and is still returned.
	SpokeLivenessFreshFor = 45 * time.Second
)

type SpokeLiveness struct {
	AgentID    string    `json:"agentId"`
	State      string    `json:"state"`
	ObservedAt time.Time `json:"observedAt,omitempty"`
}

func spokeLiveness(report *EnvironmentReport, now time.Time) SpokeLiveness {
	if report == nil || report.AgentID == "" || report.ObservedAt.IsZero() {
		agentID := ""
		if report != nil {
			agentID = report.AgentID
		}
		return SpokeLiveness{AgentID: agentID, State: SpokeUnreachable}
	}
	age := now.Sub(report.ObservedAt)
	if age < 0 {
		age = 0
	}
	state := SpokeStale
	if age <= SpokeLivenessFreshFor {
		state = SpokeLive
	}
	return SpokeLiveness{AgentID: report.AgentID, State: state, ObservedAt: report.ObservedAt.UTC()}
}

func attachSpokeLiveness(records []EnvironmentCatalogRecord, now time.Time) {
	for index := range records {
		records[index].Spoke = spokeLiveness(records[index].Reported, now)
	}
}

type EnvironmentCatalogRecord struct {
	Environment Environment        `json:"environment"`
	Reported    *EnvironmentReport `json:"reported,omitempty"`
	Spoke       SpokeLiveness      `json:"spoke"`
}

type ProductCatalogRecord struct {
	Product      string               `json:"product"`
	Releases     []Release            `json:"releases"`
	Publications []ProductPublication `json:"publications,omitempty"`
}

type EnvironmentCatalogPage struct {
	Environments []EnvironmentCatalogRecord `json:"environments"`
	Total        int                        `json:"total"`
	PageMetadata
}

type ProductCatalogPage struct {
	Products []ProductCatalogRecord `json:"products"`
	Total    int                    `json:"total"`
	PageMetadata
}

type EnvironmentReportSummary struct {
	Environment string    `json:"environment"`
	AgentID     string    `json:"agentId,omitempty"`
	ObservedAt  time.Time `json:"observedAt,omitempty"`
	EntityCount int       `json:"entityCount"`
}

type EnvironmentCatalogSummary struct {
	Environment Environment               `json:"environment"`
	Reported    *EnvironmentReportSummary `json:"reported,omitempty"`
	Spoke       SpokeLiveness             `json:"spoke"`
}

type EnvironmentCatalogSummaryPage struct {
	Environments []EnvironmentCatalogSummary `json:"environments"`
	Total        int                         `json:"total"`
	PageMetadata
}

type ProductCatalogSummary struct {
	Product       string `json:"product"`
	ReleaseCount  int    `json:"releaseCount"`
	LatestVersion string `json:"latestVersion,omitempty"`
}

type ProductCatalogSummaryPage struct {
	Products []ProductCatalogSummary `json:"products"`
	Total    int                     `json:"total"`
	PageMetadata
}

func ListEnvironmentCatalog(ctx context.Context, repository Repository) ([]EnvironmentCatalogRecord, error) {
	plans, err := repository.ListPlans(ctx, "")
	if err != nil {
		return nil, err
	}
	reports, err := repository.ListEnvironmentReports(ctx)
	if err != nil {
		return nil, err
	}
	reportByEnvironment := make(map[string]EnvironmentReport, len(reports))
	for _, report := range reports {
		reportByEnvironment[report.Environment] = report
	}
	states := latestDesiredStates(plans)
	ids := make(map[string]struct{}, len(states)+len(reports))
	for id := range states {
		ids[id] = struct{}{}
	}
	for id := range reportByEnvironment {
		ids[id] = struct{}{}
	}
	records := make([]EnvironmentCatalogRecord, 0, len(ids))
	for id := range ids {
		record := EnvironmentCatalogRecord{Environment: Environment{ID: id}}
		if state, exists := states[id]; exists {
			record.Environment = cloneJSON(state.Environment)
		}
		if report, exists := reportByEnvironment[id]; exists {
			copy := cloneJSON(report)
			record.Reported = &copy
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Environment.ID < records[j].Environment.ID })
	return records, nil
}

func ListProductCatalog(ctx context.Context, repository Repository) ([]ProductCatalogRecord, error) {
	plans, err := repository.ListPlans(ctx, "")
	if err != nil {
		return nil, err
	}
	byProductVersion := map[string]map[string]Release{}
	for _, state := range latestDesiredStates(plans) {
		for _, release := range state.Releases {
			if byProductVersion[release.Product] == nil {
				byProductVersion[release.Product] = map[string]Release{}
			}
			byProductVersion[release.Product][release.Version] = cloneJSON(release)
		}
	}
	publications, err := repository.ListProductPublications(ctx, "")
	if err != nil {
		return nil, err
	}
	publicationsByProduct := map[string][]ProductPublication{}
	for _, publication := range publications {
		publicationsByProduct[publication.Product] = append(publicationsByProduct[publication.Product], cloneJSON(publication))
		if byProductVersion[publication.Product] == nil {
			byProductVersion[publication.Product] = map[string]Release{}
		}
		if publication.Release == nil {
			continue
		}
		release := cloneJSON(*publication.Release)
		if byProductVersion[release.Product] == nil {
			byProductVersion[release.Product] = map[string]Release{}
		}
		if _, exists := byProductVersion[release.Product][release.Version]; !exists {
			byProductVersion[release.Product][release.Version] = release
		}
	}
	products := make([]ProductCatalogRecord, 0, len(byProductVersion))
	for product, versions := range byProductVersion {
		releases := make([]Release, 0, len(versions))
		for _, release := range versions {
			releases = append(releases, release)
		}
		sort.Slice(releases, func(i, j int) bool { return compareVersions(releases[i].Version, releases[j].Version) < 0 })
		products = append(products, ProductCatalogRecord{
			Product: product, Releases: releases, Publications: publicationsByProduct[product],
		})
	}
	sort.Slice(products, func(i, j int) bool { return products[i].Product < products[j].Product })
	return products, nil
}

func paginateEnvironmentCatalog(
	records []EnvironmentCatalogRecord, search, cursor string, limit int,
) (EnvironmentCatalogPage, error) {
	return paginateEnvironmentCatalogInSpace(records, search, cursor, limit, "")
}

func paginateEnvironmentCatalogInSpace(records []EnvironmentCatalogRecord, search, cursor string, limit int, spaceID string) (EnvironmentCatalogPage, error) {
	normalized := normalizedSearch(search)
	filtered := make([]EnvironmentCatalogRecord, 0, len(records))
	for _, record := range records {
		if normalized == "" || strings.Contains(strings.ToLower(record.Environment.ID), normalized) {
			filtered = append(filtered, record)
		}
	}
	fingerprint := fingerprintInSpace(struct {
		Kind   string `json:"kind"`
		Search string `json:"search"`
	}{"environments", normalized}, spaceID)
	page, metadata, err := paginateOrdered(
		filtered, func(record EnvironmentCatalogRecord) string { return record.Environment.ID },
		fingerprint, cursor, limit, errInvalidEnvironmentCursor,
	)
	if err != nil {
		return EnvironmentCatalogPage{}, err
	}
	return EnvironmentCatalogPage{Environments: page, Total: len(filtered), PageMetadata: metadata}, nil
}

func paginateProductCatalog(records []ProductCatalogRecord, search, cursor string, limit int) (ProductCatalogPage, error) {
	return paginateProductCatalogInSpace(records, search, cursor, limit, "")
}

func paginateProductCatalogInSpace(records []ProductCatalogRecord, search, cursor string, limit int, spaceID string) (ProductCatalogPage, error) {
	normalized := normalizedSearch(search)
	filtered := make([]ProductCatalogRecord, 0, len(records))
	for _, record := range records {
		if normalized == "" || strings.Contains(strings.ToLower(record.Product), normalized) {
			filtered = append(filtered, record)
		}
	}
	fingerprint := fingerprintInSpace(struct {
		Kind   string `json:"kind"`
		Search string `json:"search"`
	}{"products", normalized}, spaceID)
	page, metadata, err := paginateOrdered(
		filtered, func(record ProductCatalogRecord) string { return record.Product },
		fingerprint, cursor, limit, errInvalidProductCursor,
	)
	if err != nil {
		return ProductCatalogPage{}, err
	}
	return ProductCatalogPage{Products: page, Total: len(filtered), PageMetadata: metadata}, nil
}

func summarizeEnvironmentCatalog(page EnvironmentCatalogPage) EnvironmentCatalogSummaryPage {
	summaries := make([]EnvironmentCatalogSummary, 0, len(page.Environments))
	for _, record := range page.Environments {
		summary := EnvironmentCatalogSummary{Environment: record.Environment, Spoke: record.Spoke}
		if record.Reported != nil {
			summary.Reported = &EnvironmentReportSummary{
				Environment: record.Reported.Environment, AgentID: record.Reported.AgentID,
				ObservedAt: record.Reported.ObservedAt, EntityCount: len(record.Reported.Entities),
			}
		}
		summaries = append(summaries, summary)
	}
	return EnvironmentCatalogSummaryPage{Environments: summaries, Total: page.Total, PageMetadata: page.PageMetadata}
}

func summarizeProductCatalog(page ProductCatalogPage) ProductCatalogSummaryPage {
	summaries := make([]ProductCatalogSummary, 0, len(page.Products))
	for _, record := range page.Products {
		summary := ProductCatalogSummary{Product: record.Product, ReleaseCount: len(record.Releases)}
		if len(record.Releases) > 0 {
			summary.LatestVersion = record.Releases[len(record.Releases)-1].Version
		}
		summaries = append(summaries, summary)
	}
	return ProductCatalogSummaryPage{Products: summaries, Total: page.Total, PageMetadata: page.PageMetadata}
}

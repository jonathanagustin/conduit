package operator

import (
	"context"
	"fmt"
	"net/url"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

// PlanListQuery is the filter set GET /v1/plans already accepts. Empty fields
// are omitted so a filter the caller did not set cannot change the page.
type PlanListQuery struct {
	Environment    string
	Status         string
	InstallationID string
	Search         string
}

// PlanListResult is every visible Plan summary, after the client has followed
// the Hub's cursors. Total is the Hub's filtered count, not the page size.
type PlanListResult struct {
	Plans []controlplane.PlanSummary `json:"plans"`
	Total int                        `json:"total"`
}

// PackagingRunQuery is the filter set GET /v1/packaging-runs already accepts.
type PackagingRunQuery struct {
	Phase   string
	Outcome string
	PlanID  string
	Search  string
}

// PackagingRunList is the packaging-run index. Authority fields come from the
// Hub's page and are not relabeled by the client.
type PackagingRunList struct {
	Tier              string             `json:"tier"`
	Configured        bool               `json:"configured"`
	Authority         string             `json:"authority"`
	PlanAuthority     string             `json:"planAuthority"`
	ApprovalAuthority string             `json:"approvalAuthority"`
	Items             []domain.RunStatus `json:"items"`
	Total             int                `json:"total"`
}

type packagingRunPage struct {
	Tier              string             `json:"tier"`
	Configured        bool               `json:"configured"`
	Authority         string             `json:"authority"`
	PlanAuthority     string             `json:"planAuthority"`
	ApprovalAuthority string             `json:"approvalAuthority"`
	Items             []domain.RunStatus `json:"items"`
	Total             int                `json:"total"`
	NextCursor        string             `json:"nextCursor"`
}

func (adapter *HTTPControlPlane) ListPlans(ctx context.Context, query PlanListQuery) (PlanListResult, error) {
	values := url.Values{}
	values.Set("view", "summary")
	values.Set("limit", "200")
	setQuery(values, "environment", query.Environment)
	setQuery(values, "status", query.Status)
	setQuery(values, "installationId", query.InstallationID)
	setQuery(values, "search", query.Search)
	plans, total, err := followCursor(ctx, adapter, "/v1/plans?"+values.Encode(), func(page controlplane.PlanSummaryPage) ([]controlplane.PlanSummary, int, string) {
		return page.Plans, page.Total, page.NextCursor
	})
	if err != nil {
		return PlanListResult{}, err
	}
	if plans == nil {
		plans = []controlplane.PlanSummary{}
	}
	return PlanListResult{Plans: plans, Total: total}, nil
}

func (adapter *HTTPControlPlane) ListPackagingRuns(ctx context.Context, query PackagingRunQuery) (PackagingRunList, error) {
	values := url.Values{}
	values.Set("view", "summary")
	values.Set("limit", "200")
	setQuery(values, "phase", query.Phase)
	setQuery(values, "outcome", query.Outcome)
	setQuery(values, "planId", query.PlanID)
	setQuery(values, "search", query.Search)
	var list PackagingRunList
	first := true
	items, total, err := followCursor(ctx, adapter, "/v1/packaging-runs?"+values.Encode(), func(page packagingRunPage) ([]domain.RunStatus, int, string) {
		if first {
			list.Tier = page.Tier
			list.Configured = page.Configured
			list.Authority = page.Authority
			list.PlanAuthority = page.PlanAuthority
			list.ApprovalAuthority = page.ApprovalAuthority
			first = false
		}
		return page.Items, page.Total, page.NextCursor
	})
	if err != nil {
		return PackagingRunList{}, err
	}
	if items == nil {
		items = []domain.RunStatus{}
	}
	list.Items = items
	list.Total = total
	return list, nil
}

func setQuery(values url.Values, key, value string) {
	if value != "" {
		values.Set(key, value)
	}
}

// followCursor walks a Hub page until nextCursor is empty. A repeated cursor
// is a stuck index, and continuing would request the same page forever.
func followCursor[P any, T any](
	ctx context.Context, adapter *HTTPControlPlane, path string, read func(P) ([]T, int, string),
) ([]T, int, error) {
	seen := map[string]struct{}{}
	var all []T
	total := 0
	cursor := ""
	for {
		target := path
		if cursor != "" {
			target += "&cursor=" + url.QueryEscape(cursor)
		}
		var page P
		if err := adapter.request(ctx, "GET", target, nil, &page); err != nil {
			return nil, 0, err
		}
		items, pageTotal, next := read(page)
		all = append(all, items...)
		total = pageTotal
		if next == "" {
			return all, total, nil
		}
		if _, duplicate := seen[next]; duplicate {
			return nil, 0, fmt.Errorf("operator control plane repeated a page cursor")
		}
		seen[next] = struct{}{}
		cursor = next
	}
}

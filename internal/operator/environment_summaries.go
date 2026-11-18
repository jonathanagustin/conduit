package operator

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	environmentsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1/environmentsv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EnvironmentSummaryList is an explicitly narrow read, not a full Environment.
type EnvironmentSummaryList struct {
	Environments []EnvironmentSummary `json:"environments"`
	Total        int                  `json:"total"`
}

// EnvironmentSummary preserves report presence without configuration fields.
type EnvironmentSummary struct {
	Environment EnvironmentIdentity       `json:"environment"`
	Reported    *EnvironmentReportSummary `json:"reported,omitempty"`
	Spoke       EnvironmentSpokeSummary   `json:"spoke"`
}

// EnvironmentIdentity keeps generation opaque.
type EnvironmentIdentity struct {
	ID         string `json:"id"`
	Generation string `json:"generation,omitempty"`
}

// EnvironmentReportSummary contains observed counts, never Entity bodies.
type EnvironmentReportSummary struct {
	Environment string     `json:"environment"`
	AgentID     string     `json:"agentId,omitempty"`
	ObservedAt  *time.Time `json:"observedAt,omitempty"`
	EntityCount int        `json:"entityCount"`
}

// EnvironmentSpokeSummary retains open state and absent observation time.
type EnvironmentSpokeSummary struct {
	AgentID    string     `json:"agentId"`
	State      string     `json:"state"`
	ObservedAt *time.Time `json:"observedAt,omitempty"`
}

// EnvironmentSummaryReader is the optional outbound port for narrow reads.
type EnvironmentSummaryReader interface {
	ListEnvironmentSummaries(context.Context, string) (EnvironmentSummaryList, error)
}

// ListEnvironmentSummaries does not substitute full records when unavailable.
func (service Service) ListEnvironmentSummaries(ctx context.Context, search string) (EnvironmentSummaryList, error) {
	reader, ok := service.controlPlane.(EnvironmentSummaryReader)
	if !ok {
		return EnvironmentSummaryList{}, fmt.Errorf("Environment summary reads are not configured")
	}
	return reader.ListEnvironmentSummaries(ctx, search)
}

// SetEnvironmentReadTransport selects only explicitly requested summary reads.
func (adapter *HTTPControlPlane) SetEnvironmentReadTransport(mode string) error {
	if mode != "" && mode != "rest" && mode != "connect" {
		return fmt.Errorf("Environment read transport must be rest or connect")
	}
	adapter.environmentReadTransport = mode
	return nil
}

// ListEnvironmentSummaries follows native cursors with no transport fallback.
func (adapter *HTTPControlPlane) ListEnvironmentSummaries(ctx context.Context, search string) (EnvironmentSummaryList, error) {
	result := EnvironmentSummaryList{Environments: []EnvironmentSummary{}}
	cursor := ""
	seen := map[string]bool{}
	seenIDs := map[string]bool{}
	for {
		var rows []EnvironmentSummary
		var total int64
		var limit, size int32
		var next string
		if adapter.environmentReadTransport == "connect" {
			client := environmentsv1connect.NewEnvironmentReadServiceClient(connect.NewClient(connecthttp.NewTransport(adapter.client, adapter.baseURL.String(), connecthttp.WithReadMaxBytes(maxOperatorResponseBytes))))
			requested := int32(200)
			page, err := client.ListEnvironmentSummaries(ctx, &environmentsv1.ListEnvironmentSummariesRequest{Limit: &requested, Search: search, Cursor: cursor, SpaceId: adapter.spaceID})
			if err != nil {
				return EnvironmentSummaryList{}, err
			}
			total, limit, size, next = page.Total, page.Limit, page.PageSize, page.NextCursor
			for _, record := range page.Environments {
				row, err := environmentSummaryFromWire(record)
				if err != nil {
					return EnvironmentSummaryList{}, err
				}
				rows = append(rows, row)
			}
		} else {
			q := url.Values{"view": {"summary"}, "limit": {"200"}}
			if search != "" {
				q.Set("search", search)
			}
			if cursor != "" {
				q.Set("cursor", cursor)
			}
			var page controlplane.EnvironmentCatalogSummaryPage
			if err := adapter.request(ctx, http.MethodGet, adapter.withSpaceSelection("/v1/environments?"+q.Encode()), nil, &page); err != nil {
				return EnvironmentSummaryList{}, err
			}
			if page.Limit < 1 || page.Limit > 200 || page.PageSize < 0 || page.PageSize > 200 {
				return EnvironmentSummaryList{}, invalidEnvironmentSummary()
			}
			total, limit, size, next = int64(page.Total), int32(page.Limit), int32(page.PageSize), page.NextCursor
			for _, record := range page.Environments {
				row := EnvironmentSummary{Environment: EnvironmentIdentity{ID: record.Environment.ID, Generation: record.Environment.Generation}, Spoke: EnvironmentSpokeSummary{AgentID: record.Spoke.AgentID, State: record.Spoke.State, ObservedAt: nonzeroEnvironmentTime(record.Spoke.ObservedAt)}}
				if record.Reported != nil {
					row.Reported = &EnvironmentReportSummary{Environment: record.Reported.Environment, AgentID: record.Reported.AgentID, ObservedAt: nonzeroEnvironmentTime(record.Reported.ObservedAt), EntityCount: record.Reported.EntityCount}
				}
				rows = append(rows, row)
			}
		}
		maximum := int64(^uint(0) >> 1)
		if total < int64(len(rows)) || total > maximum || limit < 1 || limit > 200 || size != int32(len(rows)) || size > limit {
			return EnvironmentSummaryList{}, invalidEnvironmentSummary()
		}
		for _, row := range rows {
			if row.Environment.ID == "" || row.Spoke.State == "" || (row.Reported != nil && (row.Reported.Environment != row.Environment.ID || row.Reported.EntityCount < 0)) {
				return EnvironmentSummaryList{}, invalidEnvironmentSummary()
			}
			if seenIDs[row.Environment.ID] {
				return EnvironmentSummaryList{}, invalidEnvironmentSummary()
			}
			seenIDs[row.Environment.ID] = true
		}
		result.Total = int(total)
		result.Environments = append(result.Environments, rows...)
		if next == "" {
			return result, nil
		}
		if seen[next] {
			return EnvironmentSummaryList{}, fmt.Errorf("operator control plane repeated an Environment page cursor")
		}
		seen[next] = true
		cursor = next
	}
}

func invalidEnvironmentSummary() error {
	return fmt.Errorf("operator control plane returned an invalid Environment summary page")
}

func nonzeroEnvironmentTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	value = value.UTC()
	return &value
}

func environmentSummaryWireTime(value *timestamppb.Timestamp) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	if value.CheckValid() != nil {
		return nil, invalidEnvironmentSummary()
	}
	instant := value.AsTime()
	return &instant, nil
}

func environmentSummaryFromWire(record *environmentsv1.EnvironmentSummary) (EnvironmentSummary, error) {
	if record == nil || record.Environment == nil || record.Spoke == nil {
		return EnvironmentSummary{}, invalidEnvironmentSummary()
	}
	observed, err := environmentSummaryWireTime(record.Spoke.ObservedAt)
	if err != nil {
		return EnvironmentSummary{}, err
	}
	row := EnvironmentSummary{Environment: EnvironmentIdentity{ID: record.Environment.Id, Generation: record.Environment.Generation}, Spoke: EnvironmentSpokeSummary{AgentID: record.Spoke.AgentId, State: record.Spoke.State, ObservedAt: observed}}
	if record.Reported != nil {
		maximum := int64(^uint(0) >> 1)
		if record.Reported.EntityCount < 0 || record.Reported.EntityCount > maximum {
			return EnvironmentSummary{}, invalidEnvironmentSummary()
		}
		observed, err := environmentSummaryWireTime(record.Reported.ObservedAt)
		if err != nil {
			return EnvironmentSummary{}, err
		}
		row.Reported = &EnvironmentReportSummary{Environment: record.Reported.Environment, AgentID: record.Reported.AgentId, ObservedAt: observed, EntityCount: int(record.Reported.EntityCount)}
	}
	return row, nil
}

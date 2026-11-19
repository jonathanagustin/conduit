package operator

import (
	"context"
	"errors"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	environmentsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1/environmentsv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func invalidEnvironmentCatalog() error {
	return errors.New("operator control plane returned invalid full Environment index")
}

// listEnvironmentsConnect uses the configured transport without replay/fallback.
// Any failed page discards the aggregate; totals are current-page metadata, not
// an assertion that a multi-page read holds an immutable catalog snapshot.
func (adapter *HTTPControlPlane) listEnvironmentsConnect(ctx context.Context) ([]controlplane.EnvironmentCatalogRecord, error) {
	client := environmentsv1connect.NewEnvironmentCatalogServiceClient(connect.NewClient(connecthttp.NewTransport(adapter.client, adapter.baseURL.String(), connecthttp.WithReadMaxBytes(maxOperatorResponseBytes))))
	records := make([]controlplane.EnvironmentCatalogRecord, 0)
	seenCursors := map[string]bool{}
	seenIDs := map[string]bool{}
	request := &environmentsv1.ListEnvironmentsRequest{Limit: new(int32(20))}
	if adapter.spaceID != "" {
		request.SpaceId = new(adapter.spaceID)
	}
	for {
		page, err := client.ListEnvironments(ctx, request)
		if err != nil {
			return nil, err
		}
		if page == nil || page.Limit != 20 || page.PageSize < 0 || page.PageSize > 20 || int(page.PageSize) != len(page.Environments) || page.Total < int64(len(page.Environments)) || page.Total > int64(^uint(0)>>1) || (page.NextCursor != "" && len(page.Environments) == 0) {
			return nil, invalidEnvironmentCatalog()
		}
		for _, item := range page.Environments {
			if item == nil || item.Environment == nil || item.Environment.Id == "" || seenIDs[item.Environment.Id] {
				return nil, invalidEnvironmentCatalog()
			}
			record, err := environmentDetailFromWire(item, item.Environment.Id)
			if err != nil {
				return nil, err
			}
			seenIDs[item.Environment.Id] = true
			records = append(records, record)
		}
		if page.NextCursor == "" {
			return records, nil
		}
		if seenCursors[page.NextCursor] {
			return nil, invalidEnvironmentCatalog()
		}
		seenCursors[page.NextCursor] = true
		request.Cursor = page.NextCursor
	}
}

package httpapi

import (
	"context"

	"github.com/jonathanagustin/conduit/internal/planning/domain"
	api "github.com/jonathanagustin/conduit/internal/planning/transport"
)

type turnCanceller interface {
	CancelTurn(context.Context, string, string) (*domain.TurnCancellationReceipt, error)
}

func (strictServer) CancelPlanningTurn(ctx context.Context, request api.CancelPlanningTurnRequestObject) (api.CancelPlanningTurnResponseObject, error) {
	if !domain.ValidTurnRunID(request.RunID) {
		return nil, wireError{400, "INVALID_RUN_ID", "invalid planning run id"}
	}
	canceller, ok := service(ctx).(turnCanceller)
	if !ok {
		return nil, wireError{501, "CANCELLATION_NOT_CONFIGURED", "planning turn cancellation is not configured"}
	}
	receipt, err := canceller.CancelTurn(ctx, request.SessionID, request.RunID)
	if err != nil {
		return nil, err
	}
	if !receipt.ValidFor(request.RunID) {
		return nil, wireError{502, "INVALID_CANCELLATION_RECEIPT", "planning cancellation acknowledgement is unavailable"}
	}
	return dto[api.CancelPlanningTurn200JSONResponse](receipt)
}

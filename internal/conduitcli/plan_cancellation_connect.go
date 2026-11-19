package conduitcli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jonathanagustin/conduit/internal/operator"
	"github.com/spf13/cobra"
)

type planCancellationReceipter interface {
	CancelPlanConnect(context.Context, string, string) (operator.PlanCancellationReceipt, error)
}

func executePlanCancellation(command *cobra.Command, state *commandState, id, reason, transport string) error {
	if transport != "rest" && transport != "connect" {
		return fmt.Errorf("--write-transport must be rest or connect")
	}
	switch strings.ToLower(strings.TrimSpace(state.output)) {
	case "json", "yaml", "csv", "table":
	default:
		return fmt.Errorf("unsupported Plan cancellation output; use json, yaml, csv, or table")
	}
	return state.execute(command, func(service Service) (any, error) {
		if transport == "rest" {
			return planView(service.CancelPlan(command.Context(), id, reason))
		}
		remote, ok := service.(planCancellationReceipter)
		if !ok {
			return nil, fmt.Errorf("Connect Plan cancellation is unavailable for this operator service")
		}
		receipt, err := remote.CancelPlanConnect(command.Context(), id, reason)
		if err != nil {
			return nil, err
		}
		return planCancellationReceiptView{receipt}, nil
	})
}

type planCancellationReceiptView struct {
	operator.PlanCancellationReceipt
}

func (view planCancellationReceiptView) MarshalCSV() ([][]string, error) {
	return [][]string{
		{"apiVersion", "kind", "id", "environment", "status", "subject", "reason", "revision"},
		{view.APIVersion, view.Kind, view.Plan.ID, view.Plan.Environment, string(view.Plan.Status), view.Cancellation.Subject, view.Cancellation.Reason, strconv.FormatUint(view.Plan.Revision, 10)},
	}, nil
}

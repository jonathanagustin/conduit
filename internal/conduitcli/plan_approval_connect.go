package conduitcli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jonathanagustin/conduit/internal/operator"
	"github.com/spf13/cobra"
)

type planApprovalReceipter interface {
	ApprovePlanConnect(context.Context, string, string) (operator.PlanApprovalReceipt, error)
}

func executePlanApproval(command *cobra.Command, state *commandState, id, rationale, transport string) error {
	if transport != "rest" && transport != "connect" {
		return fmt.Errorf("--write-transport must be rest or connect")
	}
	switch strings.ToLower(strings.TrimSpace(state.output)) {
	case "json", "yaml", "csv", "table":
	default:
		return fmt.Errorf("unsupported Plan approval output; use json, yaml, csv, or table")
	}
	return state.execute(command, func(service Service) (any, error) {
		if transport == "rest" {
			return planView(service.ApprovePlan(command.Context(), id, rationale))
		}
		remote, ok := service.(planApprovalReceipter)
		if !ok {
			return nil, fmt.Errorf("Connect Plan approval is unavailable for this operator service")
		}
		receipt, err := remote.ApprovePlanConnect(command.Context(), id, rationale)
		if err != nil {
			return nil, err
		}
		return planApprovalReceiptView{receipt}, nil
	})
}

type planApprovalReceiptView struct{ operator.PlanApprovalReceipt }

func (view planApprovalReceiptView) MarshalCSV() ([][]string, error) {
	return [][]string{
		{"apiVersion", "kind", "id", "environment", "status", "approvals", "requiredApprovals", "revision"},
		{view.APIVersion, view.Kind, view.Plan.ID, view.Plan.Environment, string(view.Plan.Status), strconv.Itoa(view.Plan.ApprovalCount), strconv.Itoa(view.Plan.RequiredApprovals), strconv.FormatUint(view.Plan.Revision, 10)},
	}, nil
}

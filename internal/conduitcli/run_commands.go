package conduitcli

import (
	"github.com/jonathanagustin/conduit/internal/operator"
	"github.com/spf13/cobra"
)

func runCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{
		Use:   "run",
		Short: "Inspect packaging runs reported by the Hub",
	}
	var phase, outcome, planID, search string
	list := &cobra.Command{
		Use:   "list",
		Short: "List packaging runs",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return state.execute(command, func(service Service) (any, error) {
				listed, err := service.ListPackagingRuns(command.Context(), operator.PackagingRunQuery{
					Phase: phase, Outcome: outcome, PlanID: planID, Search: search,
				})
				if err != nil {
					return nil, err
				}
				return packagingRunList{PackagingRunList: listed}, nil
			})
		},
	}
	list.Flags().StringVar(&state.config.PackagingReadTransport, "read-transport", "rest", "packaging index transport: rest or connect; failures are never retried over the other transport")
	list.Flags().StringVar(&phase, "phase", "", "phase reported by the Hub")
	list.Flags().StringVar(&outcome, "outcome", "", "outcome reported by the Hub")
	list.Flags().StringVar(&planID, "plan-id", "", "Plan id the run is aimed at")
	list.Flags().StringVar(&search, "search", "", "case-insensitive search applied by the Hub")
	command.AddCommand(list)
	return command
}

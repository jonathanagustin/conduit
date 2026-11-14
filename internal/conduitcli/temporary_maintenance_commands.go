package conduitcli

import (
	"errors"
	"strings"

	"github.com/jonathanagustin/conduit/internal/apollo"
	"github.com/jonathanagustin/conduit/internal/delivery/maintenance"
	"github.com/jonathanagustin/conduit/internal/operator"
	"github.com/spf13/cobra"
)

// CONDUIT-INVENTED: maintenance.CLIProjection
// Displaced-by: a published or sanitized temporary Entity maintenance override submission and partial-result CLI output contract.
func entityMaintenanceOverrideCommand(state *commandState) *cobra.Command {
	group := &cobra.Command{Use: "maintenancewindowoverride", Aliases: []string{"mwo"}, Short: "Experimental: propose governed temporary Entity maintenance windows"}
	draft := maintenance.OverrideDraft{}
	create := &cobra.Command{Use: "create AEID [AEID...]", Short: "Experimental: one reviewed request per unique Entity; default no downtime", Args: cobra.MinimumNArgs(1), RunE: func(command *cobra.Command, args []string) error {
		format := strings.ToLower(strings.TrimSpace(state.output))
		if format != "json" && format != "yaml" {
			return errors.New("maintenance override submissions require json or yaml output")
		}
		draft.Entities = nil
		for _, aeid := range args {
			environment, entity, ok := apollo.ParseHelmChartEntityID(aeid)
			if !ok {
				return errors.New("expected an aeid:ENVIRONMENT:helm-chart:ENTITY identifier")
			}
			draft.Entities = append(draft.Entities, maintenance.EntityTarget{Environment: environment, Entity: entity})
		}
		if _, err := maintenance.ValidateDraft(draft); err != nil {
			return err
		}
		var result maintenance.OverrideBatch
		err := state.execute(command, func(service Service) (any, error) {
			port, ok := service.(operator.TemporaryMaintenanceControlPlane)
			if !ok {
				return nil, errors.New("temporary Entity maintenance overrides unavailable")
			}
			var err error
			result, err = port.CreateMaintenanceOverrides(command.Context(), draft)
			return result, err
		})
		if err != nil {
			return err
		}
		if result.Failure != "" {
			return errors.New(result.Failure)
		}
		return nil
	}}
	create.Flags().StringVar(&draft.Duration, "duration", "", "positive duration, at most 3600h (for example 30m or 2h)")
	create.Flags().StringVar(&draft.Reason, "reason", "", "request description explaining the override")
	create.Flags().StringVar(&draft.ComplianceTicket, "compliance-ticket", "", "compliance ticket retained with each request")
	create.Flags().BoolVar(&draft.Downtime, "downtime", false, "allow downtime; otherwise only no-downtime work is eligible")
	create.Flags().BoolVar(&draft.DryRun, "dry-run", false, "preview normal approval decisions without creating requests")
	_ = create.MarkFlagRequired("duration")
	_ = create.MarkFlagRequired("reason")
	group.AddCommand(create)
	return group
}

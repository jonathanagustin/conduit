package conduitcli

import (
	"errors"
	"strings"

	"github.com/jonathanagustin/conduit/internal/operator"
	"github.com/jonathanagustin/conduit/internal/spaces/domain"
	"github.com/spf13/cobra"
)

// CONDUIT-INVENTED: spaces.CLIManagement
// Displaced-by: a published or sanitized Apollo Space management CLI contract defining command paths, arguments and mutation semantics.
func spaceCommand(state *commandState) *cobra.Command {
	group := &cobra.Command{Use: "space", Short: "Manage Conduit-native grouping; membership grants no resource access"}
	run := func(command *cobra.Command, operation func(operator.SpaceControlPlane) (any, error)) error {
		return state.execute(command, func(service Service) (any, error) {
			spaces, ok := service.(operator.SpaceControlPlane)
			if !ok {
				return nil, errors.New("native Space management is unavailable from this service")
			}
			return operation(spaces)
		})
	}
	var description string
	create := &cobra.Command{Use: "create NAME", Args: cobra.ExactArgs(1), Short: "Create a native Space with authenticated provenance", RunE: func(cmd *cobra.Command, args []string) error {
		return run(cmd, func(service operator.SpaceControlPlane) (any, error) {
			return service.CreateSpace(cmd.Context(), args[0], description)
		})
	}}
	create.Flags().StringVar(&description, "description", "", "Space description")
	group.AddCommand(create)
	group.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, Short: "List visible native Spaces", RunE: func(cmd *cobra.Command, _ []string) error {
		return run(cmd, func(service operator.SpaceControlPlane) (any, error) { return service.ListSpaces(cmd.Context()) })
	}})
	group.AddCommand(&cobra.Command{Use: "get SPACE_ID", Args: cobra.ExactArgs(1), Short: "Read one visible native Space", RunE: func(cmd *cobra.Command, args []string) error {
		return run(cmd, func(service operator.SpaceControlPlane) (any, error) { return service.GetSpace(cmd.Context(), args[0]) })
	}})
	group.AddCommand(&cobra.Command{Use: "bind SPACE_ID RESOURCE_KIND RESOURCE_ID", Args: cobra.ExactArgs(3), Short: "Immutably assign an ENVIRONMENT, PRODUCT or BUNDLE grouping anchor", RunE: func(cmd *cobra.Command, args []string) error {
		resource := domain.Resource{Kind: strings.ToUpper(args[1]), ID: args[2]}
		if err := resource.Validate(); err != nil {
			return err
		}
		return run(cmd, func(service operator.SpaceControlPlane) (any, error) {
			return service.BindSpaceResource(cmd.Context(), args[0], resource)
		})
	}})
	group.AddCommand(&cobra.Command{Use: "bindings SPACE_ID", Args: cobra.ExactArgs(1), Short: "List independently authorized grouping anchors", RunE: func(cmd *cobra.Command, args []string) error {
		return run(cmd, func(service operator.SpaceControlPlane) (any, error) {
			return service.ListSpaceBindings(cmd.Context(), args[0])
		})
	}})
	return group
}

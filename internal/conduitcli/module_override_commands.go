package conduitcli

import (
	"errors"
	"strings"

	override "github.com/jonathanagustin/conduit/internal/installoverride/domain"
	"github.com/jonathanagustin/conduit/internal/installoverride/ports"
	"github.com/jonathanagustin/conduit/internal/operator"
	"github.com/spf13/cobra"
)

// CONDUIT-INVENTED: installoverride.ModuleCLI
// Displaced-by: a published or sanitized reviewed Module override CLI contract establishing request/apply stages, installed-Module update paths and intent-only secret output.
func addModuleOverrideCommands(group *cobra.Command, state *commandState) {
	overrides := &cobra.Command{Use: "override", Short: "Apply a separately approved Conduit Module metadata override"}
	overrides.AddCommand(&cobra.Command{Use: "apply CHANGE_REQUEST_ID", Short: "Admit metadata only after exact-scope independent review", Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		if !override.ValidID(args[0]) {
			return errors.New("expected a Change Request ID")
		}
		return runModuleOverride(command, state, func(port operator.ModuleOverrideControlPlane) (any, error) {
			return port.ApplyModuleOverride(command.Context(), args[0])
		})
	}})
	group.AddCommand(overrides)
	installations := &cobra.Command{Use: "installation", Short: "Conduit-native installed-Module metadata operations"}
	var environment, version, rationale string
	var ignoreRecalls, ignoreSecrets, dryRun bool
	update := &cobra.Command{Use: "update INSTALLATION_ID", Short: "Propose an independently reviewed installed-Module override; not Module-definition publication", Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		return proposeModuleOverride(command, state, ports.ModuleDraft{Action: "UPDATE", Environment: environment, InstallationID: args[0], Version: version, Flags: override.Flags{IgnoreRecalls: ignoreRecalls, IgnoreSecretRequirements: ignoreSecrets}, Rationale: rationale}, dryRun)
	}}
	update.Flags().StringVarP(&environment, "environment", "e", "", "Environment ID")
	update.Flags().StringVar(&version, "version", "", "exact target Module version; omitted selects the tracked channel")
	update.Flags().StringVar(&rationale, "rationale", "", "reason retained in the independently reviewed request")
	update.Flags().BoolVar(&ignoreRecalls, "ignore-recalls", false, "request a reviewed exception for the exact recalled Module release")
	update.Flags().BoolVar(&ignoreSecrets, "ignore-secret-requirements", false, "record reviewed intent only; Module metadata has no runtime secret gate")
	update.Flags().BoolVar(&dryRun, "dry-run", false, "preview against current Hub facts without recording review or Module metadata")
	_ = update.MarkFlagRequired("environment")
	_ = update.MarkFlagRequired("rationale")
	installations.AddCommand(update)
	group.AddCommand(installations)
}

func runModuleOverride(command *cobra.Command, state *commandState, operation func(operator.ModuleOverrideControlPlane) (any, error)) error {
	format := strings.ToLower(strings.TrimSpace(state.output))
	if format != "json" && format != "yaml" {
		return errors.New("Module override mutations require json or yaml output")
	}
	return state.execute(command, func(service Service) (any, error) {
		port, ok := service.(operator.ModuleOverrideControlPlane)
		if !ok {
			return nil, errors.New("reviewed Module overrides are unavailable from this service")
		}
		return operation(port)
	})
}

func proposeModuleOverride(command *cobra.Command, state *commandState, draft ports.ModuleDraft, dryRun bool) error {
	if (!draft.Flags.IgnoreRecalls && !draft.Flags.IgnoreSecretRequirements) || strings.TrimSpace(draft.Rationale) == "" {
		return errors.New("select a reviewed override flag and supply --rationale")
	}
	return runModuleOverride(command, state, func(port operator.ModuleOverrideControlPlane) (any, error) {
		return port.ProposeModuleOverride(command.Context(), draft, dryRun)
	})
}

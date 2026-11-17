package conduitcli

import (
	"errors"
	"strings"

	"github.com/jonathanagustin/conduit/internal/modelgovernor"
	"github.com/jonathanagustin/conduit/internal/operator"
	"github.com/spf13/cobra"
)

// CONDUIT-INVENTED: modelgovernor.CLICommands
// Displaced-by: a published or owner-approved CLI contract for reading model usage against a budget and for exporting and verifying one AI run's transcript.
func aiCommand(state *commandState) *cobra.Command {
	group := &cobra.Command{Use: "ai", Short: "Read model usage and export AI transcripts; this does not call a model"}
	run := func(cmd *cobra.Command, operation func(operator.AIGovernanceControlPlane) (any, error)) error {
		return state.execute(cmd, func(service Service) (any, error) {
			port, ok := service.(operator.AIGovernanceControlPlane)
			if !ok {
				return nil, errors.New("model usage and AI transcripts are unavailable from this service")
			}
			return operation(port)
		})
	}
	structured := func() error {
		format := strings.ToLower(strings.TrimSpace(state.output))
		if format != "json" && format != "yaml" {
			return errors.New("model usage and AI transcripts require json or yaml output")
		}
		return nil
	}

	var all bool
	var owner string
	var days int
	usage := &cobra.Command{
		Use: "usage", Short: "Show your own model usage and remaining daily budget; --all shows every owner and needs an administrator",
		Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			if err := structured(); err != nil {
				return err
			}
			if days < 0 || days > 92 {
				return errors.New("--days must be between 1 and 92")
			}
			if owner != "" && !all {
				return errors.New("--owner needs --all: without it the Hub answers about the caller only")
			}
			return run(cmd, func(port operator.AIGovernanceControlPlane) (any, error) {
				return port.ModelUsage(cmd.Context(), all, owner, days)
			})
		}}
	usage.Flags().BoolVar(&all, "all", false, "every owner's usage (administrator only)")
	usage.Flags().StringVar(&owner, "owner", "", "with --all, one owner's usage")
	usage.Flags().IntVar(&days, "days", 0, "UTC days to include, today counted (default 1)")
	group.AddCommand(usage)

	transcript := &cobra.Command{Use: "transcript", Short: "List and export stored AI transcripts (administrator only; every export is audited)"}
	var listOwner, session string
	list := &cobra.Command{
		Use: "list", Short: "List runs that have a stored transcript, without their text", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := structured(); err != nil {
				return err
			}
			return run(cmd, func(port operator.AIGovernanceControlPlane) (any, error) {
				return port.ListAITranscripts(cmd.Context(), listOwner, session)
			})
		}}
	list.Flags().StringVar(&listOwner, "owner", "", "only this owner's runs")
	list.Flags().StringVar(&session, "session", "", "only this session's runs, for example assistant-thread/<id>")
	transcript.AddCommand(list)

	transcript.AddCommand(&cobra.Command{
		Use:   "export RUN_ID",
		Short: "Export one run's exchanges, verified against the audit log; exits non-zero when verification fails",
		Args:  cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := structured(); err != nil {
				return err
			}
			if !modelgovernor.ValidRunID(args[0]) {
				return errors.New("expected a run id of the form airun_<32 hex digits>")
			}
			verified := false
			if err := run(cmd, func(port operator.AIGovernanceControlPlane) (any, error) {
				export, err := port.ExportAITranscript(cmd.Context(), args[0])
				if err != nil {
					return nil, err
				}
				verified, _ = export["verified"].(bool)
				return export, nil
			}); err != nil {
				return err
			}
			if !verified {
				// The document was printed: its mismatches say what is wrong.
				return errors.New("the transcript does not verify against the audit log; see mismatches in the output")
			}
			return nil
		}})
	group.AddCommand(transcript)
	return group
}

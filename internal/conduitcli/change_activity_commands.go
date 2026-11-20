package conduitcli

import (
	"errors"
	activity "github.com/jonathanagustin/conduit/internal/changeactivity/domain"
	"github.com/jonathanagustin/conduit/internal/operator"
	"github.com/spf13/cobra"
	"strings"
)

// CONDUIT-INVENTED: changeactivity.CLIProjection
// Displaced-by: a published or sanitized CLI discussion/label result contract establishing native-ID projection, preview output and a discussion-list operation.
func addChangeActivityCommands(group *cobra.Command, state *commandState) {
	run := func(cmd *cobra.Command, operation func(operator.ChangeActivityControlPlane) (any, error)) error {
		return state.execute(cmd, func(service Service) (any, error) {
			port, ok := service.(operator.ChangeActivityControlPlane)
			if !ok {
				return nil, errors.New("change request discussion and label mutations are unavailable from this service")
			}
			return operation(port)
		})
	}
	structured := func() error {
		format := strings.ToLower(strings.TrimSpace(state.output))
		if format != "json" && format != "yaml" {
			return errors.New("change request mutations require json or yaml output")
		}
		return nil
	}
	var text string
	comment := &cobra.Command{Use: "comment REQUEST_RID", Short: "Add a discussion comment; this does not approve a Change Request", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := structured(); err != nil {
			return err
		}
		if !activity.ValidRequestID(args[0]) || !activity.ValidText(text) {
			return errors.New("expected an existing request ID and nonempty bounded comment")
		}
		return run(cmd, func(port operator.ChangeActivityControlPlane) (any, error) {
			return port.CommentChangeRequest(cmd.Context(), args[0], text)
		})
	}}
	comment.Flags().StringVarP(&text, "comment", "c", "", "discussion text (required)")
	_ = comment.MarkFlagRequired("comment")
	group.AddCommand(comment)
	comments := &cobra.Command{Use: "comments REQUEST_RID", Short: "List Conduit-native discussion comments with server-recorded authors and times", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return run(cmd, func(port operator.ChangeActivityControlPlane) (any, error) {
			return port.ListChangeRequestComments(cmd.Context(), args[0])
		})
	}}
	comments.Flags().StringVar(&state.config.DiscussionReadTransport, "read-transport", "rest", "discussion read transport: rest or connect; failures are never replayed")
	group.AddCommand(comments)
	labels := &cobra.Command{Use: "label", Short: "Add or remove one existing Change Request label"}
	for _, operation := range []string{"add", "remove"} {
		var name string
		var dryRun bool
		command := &cobra.Command{Use: operation + " REQUEST_RID", Short: "Change one label without replacing unrelated labels", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := structured(); err != nil {
				return err
			}
			if !activity.ValidRequestID(args[0]) || name == "" || name != strings.TrimSpace(name) {
				return errors.New("expected one existing request ID and label name")
			}
			return run(cmd, func(port operator.ChangeActivityControlPlane) (any, error) {
				return port.MutateChangeRequestLabel(cmd.Context(), args[0], name, strings.ToUpper(operation), dryRun)
			})
		}}
		command.Flags().StringVar(&name, "label-name", "", "existing label name (required)")
		_ = command.MarkFlagRequired("label-name")
		command.Flags().BoolVar(&dryRun, "dry-run", false, "preview without changing discussion, labels, approval or Plan state")
		labels.AddCommand(command)
	}
	group.AddCommand(labels)
}

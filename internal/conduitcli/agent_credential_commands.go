package conduitcli

import (
	"errors"
	"fmt"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
	"github.com/jonathanagustin/conduit/internal/operator"
	"github.com/spf13/cobra"
)

// CONDUIT-INVENTED: identity.AgentRevocationCLI
// Displaced-by: a published or sanitized Apollo agent credential management CLI contract defining command paths, arguments and revocation semantics.
func agentCredentialCommand(state *commandState) *cobra.Command {
	group := &cobra.Command{
		Use:   "agent-credential",
		Short: "Revoke Agent credentials at the Hub and see when Agent certificates expire",
		Long: "Manage the Hub's deny-list for Agent client certificates.\n\n" +
			"A revocation is refused on every Agent route of every Hub replica from the next request.\n" +
			"It issues nothing and rotates nothing: see docs/operations/agent-enrolment.md.",
	}
	run := func(command *cobra.Command, operation func(operator.AgentCredentialControlPlane) (any, error)) error {
		return state.execute(command, func(service Service) (any, error) {
			credentials, ok := service.(operator.AgentCredentialControlPlane)
			if !ok {
				return nil, errors.New("Agent credential management is unavailable from this service")
			}
			return operation(credentials)
		})
	}
	group.AddCommand(&cobra.Command{
		Use: "list ENVIRONMENT", Args: cobra.ExactArgs(1),
		Short: "List each Agent's last presented certificate, its expiry and whether it is revoked",
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd, func(service operator.AgentCredentialControlPlane) (any, error) {
				return service.ListAgentCredentials(cmd.Context(), args[0])
			})
		},
	})
	group.AddCommand(&cobra.Command{
		Use: "revocations ENVIRONMENT", Args: cobra.ExactArgs(1), Short: "List the revocations in force for an Environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd, func(service operator.AgentCredentialControlPlane) (any, error) {
				return service.ListAgentRevocations(cmd.Context(), args[0])
			})
		},
	})
	var agentID, serial, reason, notBefore string
	var wholeEnvironment bool
	revoke := &cobra.Command{
		Use: "revoke ENVIRONMENT", Args: cobra.ExactArgs(1),
		Short: "Refuse one certificate (--serial), one Agent (--agent-id) or every Agent (--all-agents) of an Environment",
		Long: "Exactly one of --serial, --agent-id and --all-agents selects what is revoked.\n\n" +
			"--serial refuses that certificate only; the Agent's next certificate works at once.\n" +
			"--agent-id and --all-agents refuse every certificate issued at or before --not-before,\n" +
			"which defaults to now, so a certificate issued afterwards is admitted without a second step.\n" +
			"Re-enrol with a new private key: a certificate reissued for a leaked key readmits its holder.",
		RunE: func(cmd *cobra.Command, args []string) error {
			draft := domain.AgentRevocation{Reason: reason}
			selected := 0
			if serial != "" {
				draft.Scope, draft.Serial = domain.RevocationSerial, serial
				selected++
			}
			if agentID != "" {
				draft.Scope, draft.AgentID = domain.RevocationAgent, agentID
				selected++
			}
			if wholeEnvironment {
				draft.Scope = domain.RevocationEnvironment
				selected++
			}
			if selected != 1 {
				return errors.New("exactly one of --serial, --agent-id and --all-agents is required")
			}
			if notBefore != "" {
				instant, err := time.Parse(time.RFC3339, notBefore)
				if err != nil {
					return fmt.Errorf("--not-before must be an RFC 3339 instant: %w", err)
				}
				draft.NotBefore = instant
			}
			// The same validation the Hub applies, run here so a mistake costs no
			// round trip. The actor and instant are placeholders; the Hub's are used.
			draft.Environment = args[0]
			if _, err := domain.NewAgentRevocation(draft, "cli", time.Now()); err != nil {
				return err
			}
			return run(cmd, func(service operator.AgentCredentialControlPlane) (any, error) {
				return service.AddAgentRevocation(cmd.Context(), args[0], draft)
			})
		},
	}
	revoke.Flags().StringVar(&serial, "serial", "", "certificate serial in hexadecimal, as `openssl x509 -noout -serial` prints it")
	revoke.Flags().StringVar(&agentID, "agent-id", "", "Agent identity to refuse until a certificate is issued after --not-before")
	revoke.Flags().BoolVar(&wholeEnvironment, "all-agents", false, "refuse every Agent of the Environment")
	revoke.Flags().StringVar(&notBefore, "not-before", "", "RFC 3339 instant; certificates issued at or before it are refused (default: now)")
	revoke.Flags().StringVar(&reason, "reason", "", "why, for the audit trail (required)")
	_ = revoke.MarkFlagRequired("reason")
	group.AddCommand(revoke)
	group.AddCommand(&cobra.Command{
		Use: "unrevoke ENVIRONMENT REVOCATION_ID", Args: cobra.ExactArgs(2),
		Short: "Lift one revocation, readmitting the credentials it refused",
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd, func(service operator.AgentCredentialControlPlane) (any, error) {
				return service.RemoveAgentRevocation(cmd.Context(), args[0], args[1])
			})
		},
	})
	return group
}

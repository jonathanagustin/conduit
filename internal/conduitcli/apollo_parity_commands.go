package conduitcli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/spf13/cobra"
)

// The commands Apollo published between the 2026-08-01 and 2026-08-15 crawls.
//
// `internal/conduitcli/contract_test.go` requires the tree to carry every
// command in the pinned apollo-cli inventory, and the 2026-08-15 re-crawl added
// fourteen. That test asks whether the command EXISTS, not whether it answers,
// and the distinction is the point: an operator who runs a published command
// should be told what Conduit does about it, not meet "unknown command" and be
// left guessing whether they typed it wrong.
//
// So each one below either answers from something Conduit holds, or refuses and
// says exactly what is missing. That is the same choice the Apollo-shaped
// GraphQL interface makes when it declares a recovered coordinate it cannot
// serve: refusing states a true thing, while silence and null both state
// something false. A refusal here is not a stub -- it is the honest answer
// until the backing exists, and it names what the backing would be.

func addApolloParityCommands(root *cobra.Command, state *commandState) {
	root.AddCommand(targetHubCommand())
	if entity := findCommand(root, "entity"); entity != nil {
		entity.AddCommand(entityGetCommand(state))
	}
	if environment := findCommand(root, "environment"); environment != nil {
		environment.AddCommand(environmentSettingCommand(state))
	}
	if cve := findCommand(root, "cve"); cve != nil {
		cve.AddCommand(cveListCommand())
	}
	if terminal := findCommand(root, "terminal"); terminal != nil {
		terminal.AddCommand(terminalBoundaryGroup("grant", "just-in-time terminal access grants"))
		terminal.AddCommand(terminalBoundaryGroup("policy", "terminal access policies"))
	}
}

func findCommand(parent *cobra.Command, name string) *cobra.Command {
	for _, child := range parent.Commands() {
		if child.Name() == name {
			return child
		}
	}
	return nil
}

// --- answered ---------------------------------------------------------------

// entityGetCommand resolves an Apollo entity ID to the Entity behind it.
//
// The ID is structured -- `aeid:ENVIRONMENT:helm-chart:NAME`, built by
// internal/apollo/application.helmChartEntityID -- so it is parsed rather than
// searched for. Scanning every Environment for a matching name would find the
// wrong Entity whenever two Environments run one with the same name, which is
// the normal case rather than an edge one.
func entityGetCommand(state *commandState) *cobra.Command {
	return &cobra.Command{
		Use: "get AEID", Short: "Get an Entity by Apollo entity ID", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			environment, name, err := parseApolloEntityID(args[0])
			if err != nil {
				return err
			}
			return state.execute(command, func(service Service) (any, error) {
				entities, err := service.ListEntities(command.Context(), environment)
				if err != nil {
					return nil, err
				}
				for _, entity := range entities {
					if entity.Name == name {
						return map[string]any{"apolloEntityId": args[0], "environment": environment, "entity": entity}, nil
					}
				}
				return nil, fmt.Errorf("no Entity %q in Environment %q for Apollo entity ID %s", name, environment, args[0])
			})
		},
	}
}

// parseApolloEntityID splits the published form and refuses anything else.
//
// It refuses rather than guessing because a malformed ID that resolved by
// prefix match would silently answer for a different Entity, and this command
// exists to answer for exactly one.
func parseApolloEntityID(id string) (environment, name string, err error) {
	parts := strings.Split(id, ":")
	if len(parts) != 4 || parts[0] != "aeid" || parts[2] != "helm-chart" {
		return "", "", fmt.Errorf(
			"%q is not an Apollo entity ID; the published form is aeid:ENVIRONMENT:helm-chart:NAME", id,
		)
	}
	if strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[3]) == "" {
		return "", "", fmt.Errorf("%q names an empty Environment or Entity", id)
	}
	return parts[1], parts[3], nil
}

// environmentSettingCommand is the settings document Apollo calls deployment.yml.
//
// Conduit already converts both directions --
// controlplane.ApolloEnvironmentSettingsFromDesiredState and
// ApplyApolloEnvironmentSettings -- because CEL diff paths are expressed against
// this document's vocabulary. These commands are that conversion made reachable.
//
// One deliberate divergence from apollo-cli, stated rather than hidden: these
// read a local desired-state document rather than the server's. Conduit's CLI is
// file-oriented throughout (`--file` on every mutating command), because desired
// state here is authored and reviewed rather than edited in place. Reading the
// server would need a settings endpoint that does not exist, and inventing one
// to match a flag's shape would be the wrong way round.
func environmentSettingCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{
		Use:   "setting",
		Short: "Read and replace Environment settings (deployment.yml)",
	}
	command.AddCommand(environmentSettingGetCommand(state))
	command.AddCommand(environmentSettingEditCommand(state))
	return command
}

func environmentSettingGetCommand(state *commandState) *cobra.Command {
	var file string
	command := &cobra.Command{
		Use: "get", Short: "Project a desired-state document into Environment settings (deployment.yml)",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			desired, err := readDesiredState(file)
			if err != nil {
				return err
			}
			encoded, err := controlplane.EncodeApolloEnvironmentSettings(
				controlplane.ApolloEnvironmentSettingsFromDesiredState(desired),
			)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(command.OutOrStdout(), encoded)
			return err
		},
	}
	command.Flags().StringVarP(&file, "file", "f", "", "desired-state YAML or JSON to project")
	_ = command.MarkFlagRequired("file")
	return command
}

func environmentSettingEditCommand(state *commandState) *cobra.Command {
	var file, settingsFile, content string
	command := &cobra.Command{
		Use: "edit", Short: "Open a change request replacing Environment settings (deployment.yml)",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			// No editor is opened. apollo-cli defaults to one; a CLI that opens
			// $EDITOR cannot be driven from a pipeline, and every other mutating
			// command here takes its document explicitly. Requiring one of the two
			// explicit forms keeps that consistent instead of adding a third mode
			// nobody else in this tree has.
			if (content == "") == (settingsFile == "") {
				return errors.New("exactly one of --content or --settings-file is required")
			}
			desired, err := readDesiredState(file)
			if err != nil {
				return err
			}
			document := content
			if settingsFile != "" {
				// Read as text rather than through decodeDocument: the settings
				// document is handed to DecodeApolloEnvironmentSettings, which owns
				// its schema and rejects unknown keys. Round-tripping it through a
				// generic YAML decode first would drop that strictness.
				raw, readErr := os.ReadFile(settingsFile)
				if readErr != nil {
					return fmt.Errorf("open settings document: %w", readErr)
				}
				if len(raw) > maxDocumentBytes {
					return fmt.Errorf("settings document exceeds %d bytes", maxDocumentBytes)
				}
				document = string(raw)
			}
			settings, err := controlplane.DecodeApolloEnvironmentSettings(document)
			if err != nil {
				return err
			}
			updated, err := controlplane.ApplyApolloEnvironmentSettings(desired, settings)
			if err != nil {
				return err
			}
			return state.execute(command, func(service Service) (any, error) {
				return service.ProposePlan(command.Context(), updated)
			})
		},
	}
	command.Flags().StringVarP(&file, "file", "f", "", "desired-state YAML or JSON the settings replace into")
	command.Flags().StringVar(&settingsFile, "settings-file", "", "file holding the complete settings document")
	command.Flags().StringVar(&content, "content", "", "complete inline settings document")
	// No --reason flag: ProposePlan takes a DesiredState and nothing else, so a
	// rationale would be accepted and dropped. A flag that silently does nothing
	// is worse than an absent one.
	_ = command.MarkFlagRequired("file")
	return command
}

// --- refused, with the reason ------------------------------------------------

// cveListCommand refuses because the column the command exists to print is the
// one Conduit does not hold.
//
// Apollo lists a due date per CVE per container image. Conduit scans exact
// digests and records grace-period findings, which is when a finding stops
// being tolerated -- not a date the finding is owed by. Printing grace as a due
// date would relabel one contract as another, and an operator reading "Overdue"
// would believe a deadline had been missed that Conduit never tracked.
func cveListCommand() *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "Unavailable: Conduit records grace-period findings, not CVE due dates",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New(
				"cve list is unavailable: Apollo reports a due date per CVE per container image, and Conduit " +
					"holds no due-date model. It scans exact artifact digests and records grace-period " +
					"findings, which say when a finding stops being tolerated rather than when it is owed by; " +
					"reporting one as the other would misstate the contract. Use `conduit cve check` for the " +
					"assessment Conduit does produce",
			)
		},
	}
}

// targetHubCommand refuses because Conduit has target Hubs but no registry of
// them.
//
// A target Hub here is a name an operator supplies per Export Pipeline --
// BuildForTarget(pipelineID, targetHub) -- and the versions recorded against it
// are keyed by that pair. Nothing enumerates the set, so "list transfer target
// hubs" has no source to read. Answering from the pipelines that happen to have
// recorded versions would report the Hubs someone remembered to record, not the
// Hubs that exist, and an operator would read a short list as a complete one.
func targetHubCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "target-hub",
		Short: "Unavailable: Conduit names target Hubs per Export Pipeline and keeps no registry of them",
	}
	unavailable := func(what string) error {
		return fmt.Errorf(
			"target-hub %s is unavailable: a target Hub is a name supplied per Export Pipeline, and Conduit "+
				"records versions against the (pipeline, target Hub) pair rather than registering the Hub "+
				"itself. There is no set to enumerate, and listing the Hubs that happen to have recorded "+
				"versions would report what was remembered rather than what exists. Use `conduit environment "+
				"version compare` for the versions Conduit does hold", what,
		)
	}
	command.RunE = func(*cobra.Command, []string) error { return unavailable("") }
	command.AddCommand(&cobra.Command{
		Use: "list", Short: "Unavailable: no target Hub registry exists", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return unavailable("list") },
	})
	command.AddCommand(&cobra.Command{
		Use: "get ID", Short: "Unavailable: no target Hub registry exists", Args: cobra.ExactArgs(1),
		RunE: func(*cobra.Command, []string) error { return unavailable("get") },
	})
	pipeline := &cobra.Command{
		Use: "pipeline", Short: "Unavailable: no target Hub registry exists",
		RunE: func(*cobra.Command, []string) error { return unavailable("pipeline") },
	}
	pipeline.AddCommand(&cobra.Command{
		Use: "list TARGET_HUB_ID", Short: "Unavailable: no target Hub registry exists", Args: cobra.ExactArgs(1),
		RunE: func(*cobra.Command, []string) error { return unavailable("pipeline list") },
	})
	command.AddCommand(pipeline)
	return command
}

// terminalBoundaryGroup extends the existing `terminal` refusal to the
// subcommands Apollo added, so the boundary is stated wherever an operator
// arrives at it rather than only at the parent.
//
// This is a declared boundary, not a gap: Conduit has no SSH, kubectl exec,
// host API, or cluster-admin execution path, and reference/debug-access.md
// keeps just-in-time access with the audited broker that owns the Environment.
// Growing an Agent into a privileged shell service is the thing that document
// refuses, so these will not gain backing by being asked for again.
func terminalBoundaryGroup(name, subject string) *cobra.Command {
	refuse := func() error {
		return fmt.Errorf(
			"terminal %s is intentionally unavailable: Conduit has no SSH, kubectl exec, host API, or "+
				"cluster-admin execution path, so it neither issues nor records %s. Use the audited "+
				"break-glass tooling owned by the Environment; see docs/apollo/reference/debug-access.md",
			name, subject,
		)
	}
	command := &cobra.Command{
		Use: name, Short: "Intentionally unavailable: " + subject + " stay with the Environment's broker",
		RunE: func(*cobra.Command, []string) error { return refuse() },
	}
	command.AddCommand(&cobra.Command{
		Use: "list", Short: "Intentionally unavailable: " + subject + " stay with the Environment's broker",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return refuse() },
	})
	return command
}

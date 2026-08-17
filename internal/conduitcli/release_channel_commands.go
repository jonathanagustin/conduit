package conduitcli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/spf13/cobra"
)

func releaseChannelCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "release-channel", Short: "Govern Release Channels in Git-owned desired state"}
	command.AddCommand(releaseChannelCreateCommand(state))
	command.AddCommand(releaseChannelUpdateCommand(state))
	command.AddCommand(releaseChannelListCommand(state))
	command.AddCommand(releaseChannelGetCommand(state, "get"))
	command.AddCommand(releaseChannelGetCommand(state, "export"))
	command.AddCommand(releaseChannelImportCommand(state))
	command.AddCommand(releaseChannelDiffCommand(state))
	command.AddCommand(releaseChannelAddCommand(state))
	return command
}

func releaseChannelCreateCommand(state *commandState) *cobra.Command {
	var stateFile, description string
	var requirements, values []string
	var dryRun bool
	command := &cobra.Command{
		Use: "create CHANNEL_NAME", Short: "Propose a new Release Channel definition", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			desired, err := readDesiredState(stateFile)
			if err != nil {
				return err
			}
			if findReleaseChannel(&desired, args[0]) != nil {
				return fmt.Errorf("Release Channel %q already exists", args[0])
			}
			labels, err := parseLabelRequirements(requirements, values)
			if err != nil {
				return err
			}
			desired.ReleaseChannels = append(desired.ReleaseChannels, controlplane.ReleaseChannelDefinition{
				Name: args[0], Description: description, LabelRequirements: labels,
			})
			return proposeOrPrintDesired(command, state, desired, dryRun)
		},
	}
	command.Flags().StringVar(&stateFile, "state-file", "", "next-generation desired-state YAML or JSON")
	command.Flags().StringVar(&description, "description", "", "Release Channel description")
	command.Flags().StringArrayVar(&requirements, "label-requirement", nil, "label requirement ID or ID=value")
	command.Flags().StringArrayVar(&values, "label-requirement-value", nil, "accepted value paired by position")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "validate without proposing")
	_ = command.MarkFlagRequired("state-file")
	return command
}

func releaseChannelUpdateCommand(state *commandState) *cobra.Command {
	var stateFile, description, archiveRationale string
	var requirements, values []string
	var archive, unarchive, clear, dryRun bool
	command := &cobra.Command{
		Use: "update CHANNEL_NAME", Short: "Propose a Release Channel update", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if archive && unarchive {
				return errors.New("--archive and --unarchive are mutually exclusive")
			}
			if archive && strings.TrimSpace(archiveRationale) == "" {
				return errors.New("--archive-rationale is required with --archive")
			}
			desired, err := readDesiredState(stateFile)
			if err != nil {
				return err
			}
			definition := findReleaseChannel(&desired, args[0])
			if definition == nil {
				return fmt.Errorf("Release Channel %q does not exist", args[0])
			}
			if description != "" {
				definition.Description = description
			}
			if archive {
				definition.Archived = true
			}
			if unarchive {
				definition.Archived = false
			}
			if clear {
				definition.LabelRequirements = nil
			}
			if len(requirements) > 0 || len(values) > 0 {
				labels, err := parseLabelRequirements(requirements, values)
				if err != nil {
					return err
				}
				definition.LabelRequirements = labels
			}
			return proposeOrPrintDesired(command, state, desired, dryRun)
		},
	}
	command.Flags().StringVar(&stateFile, "state-file", "", "next-generation desired-state YAML or JSON")
	command.Flags().StringVar(&description, "description", "", "new description")
	command.Flags().StringArrayVar(&requirements, "label-requirement", nil, "label requirement ID or ID=value")
	command.Flags().StringArrayVar(&values, "label-requirement-value", nil, "accepted value paired by position")
	command.Flags().BoolVar(&clear, "clear-label-requirements", false, "remove all requirements")
	command.Flags().BoolVar(&archive, "archive", false, "archive the channel")
	command.Flags().StringVar(&archiveRationale, "archive-rationale", "", "archive rationale retained in review")
	command.Flags().BoolVar(&unarchive, "unarchive", false, "unarchive the channel")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "validate without proposing")
	_ = command.MarkFlagRequired("state-file")
	return command
}

func releaseChannelListCommand(state *commandState) *cobra.Command {
	var stateFile, pageToken string
	var pageSize int
	command := &cobra.Command{
		Use: "list", Short: "List Release Channel definitions", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if pageSize < 0 {
				return errors.New("--page-size cannot be negative")
			}
			desired, err := readDesiredState(stateFile)
			if err != nil {
				return err
			}
			channels := append([]controlplane.ReleaseChannelDefinition(nil), desired.ReleaseChannels...)
			sort.Slice(channels, func(i, j int) bool { return channels[i].Name < channels[j].Name })
			if pageToken != "" {
				start := sort.Search(len(channels), func(index int) bool { return channels[index].Name > pageToken })
				channels = channels[start:]
			}
			nextPageToken := ""
			if pageSize > 0 && len(channels) > pageSize {
				nextPageToken = channels[pageSize-1].Name
				channels = channels[:pageSize]
			}
			return writeOutput(state.runtime.Stdout, state.output, map[string]any{
				"releaseChannels": channels, "nextPageToken": nextPageToken,
			})
		},
	}
	command.Flags().StringVar(&stateFile, "state-file", "", "desired-state YAML or JSON")
	command.Flags().IntVar(&pageSize, "page-size", 0, "maximum results")
	command.Flags().StringVar(&pageToken, "page-token", "", "continue after this Release Channel name")
	_ = command.MarkFlagRequired("state-file")
	return command
}

func releaseChannelGetCommand(state *commandState, action string) *cobra.Command {
	var stateFile string
	command := &cobra.Command{
		Use: action + " CHANNEL_NAME", Short: strings.ToUpper(action[:1]) + action[1:] + " a Release Channel definition", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			desired, err := readDesiredState(stateFile)
			if err != nil {
				return err
			}
			definition := findReleaseChannel(&desired, args[0])
			if definition == nil {
				return fmt.Errorf("Release Channel %q does not exist", args[0])
			}
			return writeOutput(state.runtime.Stdout, state.output, definition)
		},
	}
	command.Flags().StringVar(&stateFile, "state-file", "", "desired-state YAML or JSON")
	if action == "export" {
		command.Flags().BoolP("latest", "l", false, "export the current Git-owned definition")
	}
	_ = command.MarkFlagRequired("state-file")
	return command
}

// releaseChannelImportCommand is the inverse of export, and it exists because
// export had none. A Release Channel definition could be lifted out of one
// desired state and there was no supported way to put it into another, which
// cuts against the air-gap story the rest of the export/import surface
// supports: an operator moving a channel between Hubs had to hand-edit YAML.
//
// Deliberately NOT implemented alongside it: `release-channel populate`. Apollo
// names it in a changelog entry and publishes no reference page for it, so what
// it does is not established -- it might fill a channel with releases (which
// `add` already does), or seed a channel from another, or something else. This
// repository refuses a coordinate it cannot answer rather than guessing at one,
// and a command is a coordinate. `import` is here because its meaning follows
// from `export` and nothing has to be invented to know it.
func releaseChannelImportCommand(state *commandState) *cobra.Command {
	var stateFile, definitionFile string
	var overwrite, dryRun bool
	command := &cobra.Command{
		Use: "import", Short: "Propose a Release Channel definition exported from another desired state",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			desired, err := readDesiredState(stateFile)
			if err != nil {
				return err
			}
			var definition controlplane.ReleaseChannelDefinition
			if err = decodeDocument(definitionFile, &definition); err != nil {
				return err
			}
			if strings.TrimSpace(definition.Name) == "" {
				return errors.New("the imported definition has no Release Channel name")
			}
			existing := findReleaseChannel(&desired, definition.Name)
			// Refused rather than merged. An import that quietly replaced a
			// channel would let a definition carried between Hubs drop label
			// requirements the receiving Hub had added, and the operator would
			// see a successful import either way.
			if existing != nil && !overwrite {
				return fmt.Errorf("Release Channel %q already exists; pass --overwrite to replace it", definition.Name)
			}
			if existing != nil {
				*existing = definition
			} else {
				desired.ReleaseChannels = append(desired.ReleaseChannels, definition)
			}
			return proposeOrPrintDesired(command, state, desired, dryRun)
		},
	}
	command.Flags().StringVar(&stateFile, "state-file", "", "next-generation desired-state YAML or JSON")
	command.Flags().StringVar(&definitionFile, "definition-file", "", "exported Release Channel definition")
	command.Flags().BoolVar(&overwrite, "overwrite", false, "replace an existing Release Channel of the same name")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "validate without proposing")
	for _, flag := range []string{"state-file", "definition-file"} {
		_ = command.MarkFlagRequired(flag)
	}
	return command
}

func releaseChannelDiffCommand(state *commandState) *cobra.Command {
	var stateFile, source, target string
	var all bool
	command := &cobra.Command{
		Use: "diff", Short: "Diff two Release Channel definitions", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			desired, err := readDesiredState(stateFile)
			if err != nil {
				return err
			}
			left, right := findReleaseChannel(&desired, source), findReleaseChannel(&desired, target)
			if left == nil || right == nil {
				return errors.New("source and target Release Channels must exist")
			}
			return writeOutput(state.runtime.Stdout, state.output, map[string]any{"equal": reflect.DeepEqual(left, right), "all": all, "source": left, "target": right})
		},
	}
	command.Flags().StringVar(&stateFile, "state-file", "", "desired-state YAML or JSON")
	command.Flags().StringVarP(&source, "source-channel", "s", "", "source Release Channel")
	command.Flags().StringVarP(&target, "target-channel", "t", "", "target Release Channel")
	command.Flags().BoolVar(&all, "all", false, "include unchanged fields")
	for _, flag := range []string{"state-file", "source-channel", "target-channel"} {
		_ = command.MarkFlagRequired(flag)
	}
	return command
}

func releaseChannelAddCommand(state *commandState) *cobra.Command {
	var stateFile, coordinatesFile, channel string
	var yes bool
	command := &cobra.Command{
		Use: "add", Short: "Propose adding immutable Product Releases to a Release Channel", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if !yes {
				return errors.New("--yes is required for non-interactive Release mutation")
			}
			desired, err := readDesiredState(stateFile)
			if err != nil {
				return err
			}
			coordinates, err := readCoordinateLines(coordinatesFile)
			if err != nil {
				return err
			}
			remaining := map[string]struct{}{}
			for _, coordinate := range coordinates {
				remaining[coordinate] = struct{}{}
			}
			for index := range desired.Releases {
				coordinate := desired.Releases[index].Product + ":" + desired.Releases[index].Version
				if _, selected := remaining[coordinate]; selected {
					if !containsString(desired.Releases[index].Channels, channel) {
						desired.Releases[index].Channels = append(desired.Releases[index].Channels, channel)
					}
					delete(remaining, coordinate)
				}
			}
			if len(remaining) > 0 {
				return fmt.Errorf("%d Product Release coordinates were not present in desired state", len(remaining))
			}
			return proposeOrPrintDesired(command, state, desired, false)
		},
	}
	command.Flags().StringVar(&stateFile, "state-file", "", "next-generation desired-state YAML or JSON")
	command.Flags().StringVarP(&coordinatesFile, "maven-coordinates-file", "f", "", "newline-delimited group:name:version coordinates")
	command.Flags().StringVarP(&channel, "release-channel", "r", "", "target Release Channel")
	command.Flags().BoolVarP(&yes, "yes", "y", false, "confirm non-interactive mutation")
	for _, flag := range []string{"state-file", "maven-coordinates-file", "release-channel"} {
		_ = command.MarkFlagRequired(flag)
	}
	return command
}

func findReleaseChannel(desired *controlplane.DesiredState, name string) *controlplane.ReleaseChannelDefinition {
	for index := range desired.ReleaseChannels {
		if desired.ReleaseChannels[index].Name == name {
			return &desired.ReleaseChannels[index]
		}
	}
	return nil
}

func parseLabelRequirements(requirements, values []string) (map[string][]string, error) {
	result := map[string][]string{}
	if len(values) > 0 && len(values) != len(requirements) {
		return nil, errors.New("--label-requirement and --label-requirement-value counts must match")
	}
	for index, requirement := range requirements {
		id, value, inline := strings.Cut(requirement, "=")
		if len(values) > 0 {
			value, inline = values[index], true
		}
		if strings.TrimSpace(id) == "" || !inline || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("label requirement %q must have a value", requirement)
		}
		result[id] = append(result[id], value)
	}
	return result, nil
}

func readCoordinateLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	coordinates := []string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		coordinate := strings.TrimSpace(scanner.Text())
		if coordinate == "" || strings.HasPrefix(coordinate, "#") {
			continue
		}
		if _, _, err := splitProductCoordinate(coordinate); err != nil {
			return nil, err
		}
		coordinates = append(coordinates, coordinate)
	}
	return coordinates, scanner.Err()
}

func proposeOrPrintDesired(command *cobra.Command, state *commandState, desired controlplane.DesiredState, dryRun bool) error {
	if errs := controlplane.Validate(desired); len(errs) > 0 {
		return controlplane.ValidationErrors(errs)
	}
	if dryRun {
		return writeOutput(state.runtime.Stdout, state.output, map[string]any{"dryRun": true, "desiredState": desired})
	}
	return state.execute(command, func(service Service) (any, error) {
		return service.ProposePlan(command.Context(), desired)
	})
}

func terminalCommand() *cobra.Command {
	var sessionID string
	command := &cobra.Command{
		Use: "terminal", Short: "Explain why direct workload terminals are outside Conduit's trust model", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("terminal access is intentionally unavailable: Conduit has no SSH, kubectl exec, host API, or cluster-admin execution path; use audited break-glass tooling owned by the Environment")
		},
	}
	command.Flags().StringVar(&sessionID, "session-id", "", "external audited break-glass session identifier (never executed by Conduit)")
	return command
}

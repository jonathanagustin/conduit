package conduitcli

import (
	"bufio"
	"encoding/json"
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
	command.AddCommand(releaseChannelRemoveCommand(state))
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
	var stateFile, description string
	var requirements, values []string
	var archive, unarchive, clear, dryRun bool
	command := &cobra.Command{
		Use: "update CHANNEL_NAME", Short: "Propose a Release Channel update", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if archive && unarchive {
				return errors.New("--archive and --unarchive are mutually exclusive")
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
	// No --archive-rationale flag: archiving edits the desired-state document
	// and proposes it, and a Plan proposal carries the DesiredState alone, so
	// the rationale was required alongside --archive and then dropped while the
	// help promised it was "retained in review" (#175). Same reasoning as the
	// Environment commands; the divergence from apollo-cli's published flag list
	// is declared in docs/operations/TENANT-FIDELITY.md.
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
	var latest bool
	command := &cobra.Command{
		Use: action + " CHANNEL_NAME", Short: strings.ToUpper(action[:1]) + action[1:] + " a Release Channel definition", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			// Refused rather than ignored. --latest asks for the Hub's current
			// definition; this command reads the document named by --state-file
			// and nothing else, so accepting the flag reported a local file as
			// the Hub's live state and said nothing about the substitution.
			// `--latest` and `--latest=false` produced byte-identical output,
			// which is the shape of a flag that never reaches a decision.
			if latest {
				return errors.New(
					"--latest is unavailable: `release-channel export` projects the desired-state document " +
						"named by --state-file, and Conduit serves no Release Channel definition from the Hub " +
						"to export instead. Answering from the local document while the flag claimed otherwise " +
						"would report a file the operator supplied as the Hub's current definition. Point " +
						"--state-file at the Git-owned desired state you want exported and drop the flag",
				)
			}
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
		// Kept registered, not deleted: apollo-cli publishes -l/--latest on this
		// command, so an operator carrying a script across will pass it. Meeting
		// it with a stated boundary tells them what Conduit exports; meeting it
		// with "unknown flag" leaves them guessing whether they mistyped.
		command.Flags().BoolVarP(&latest, "latest", "l", false, "unavailable: export reads the document named by --state-file")
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

// releaseChannelDiffCommand reports what differs between two Release Channels,
// and --all is what makes it report everything instead.
//
// The flag documented "include unchanged fields" while the command printed
// both definitions whole either way, so the only thing --all changed was the
// `"all"` value echoed back beside output that never varied. Two channels
// differing in one label requirement rendered as two complete documents an
// operator had to compare by eye -- the work the command exists to have done.
//
// --releases changes what is diffed: instead of the two channels' definition
// fields, it compares the *set of Releases* on each channel and reports the
// asymmetric difference, which is the promotion worklist -- the Releases on the
// source a promotion would carry to the target. A channel name that differs by
// one field and a channel that is a whole Release behind are different
// questions; the definition diff answered only the first.
func releaseChannelDiffCommand(state *commandState) *cobra.Command {
	var stateFile, source, target string
	var all, releases bool
	command := &cobra.Command{
		Use: "diff", Short: "Diff two Release Channels by definition or by Release set", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			desired, err := readDesiredState(stateFile)
			if err != nil {
				return err
			}
			left, right := findReleaseChannel(&desired, source), findReleaseChannel(&desired, target)
			if left == nil || right == nil {
				return errors.New("source and target Release Channels must exist")
			}
			if releases {
				onlySource, onlyTarget, both := releaseChannelReleaseDiff(&desired, source, target)
				output := map[string]any{
					"sourceChannel": source, "targetChannel": target,
					"onlyOnSource": onlySource, "onlyOnTarget": onlyTarget,
					"equal": len(onlySource) == 0 && len(onlyTarget) == 0,
				}
				if all {
					output["onBoth"] = both
				}
				return writeOutput(state.runtime.Stdout, state.output, output)
			}
			equal := reflect.DeepEqual(left, right)
			if all {
				return writeOutput(state.runtime.Stdout, state.output, map[string]any{"equal": equal, "all": true, "source": left, "target": right})
			}
			narrowedSource, narrowedTarget, err := differingFields(left, right)
			if err != nil {
				return err
			}
			return writeOutput(state.runtime.Stdout, state.output, map[string]any{"equal": equal, "all": false, "source": narrowedSource, "target": narrowedTarget})
		},
	}
	command.Flags().StringVar(&stateFile, "state-file", "", "desired-state YAML or JSON")
	command.Flags().StringVarP(&source, "source-channel", "s", "", "source Release Channel")
	command.Flags().StringVarP(&target, "target-channel", "t", "", "target Release Channel")
	command.Flags().BoolVar(&all, "all", false, "include unchanged fields (definitions) or shared Releases (--releases)")
	command.Flags().BoolVar(&releases, "releases", false, "diff the Releases on each channel to find promotions, not the channel definitions")
	for _, flag := range []string{"state-file", "source-channel", "target-channel"} {
		_ = command.MarkFlagRequired(flag)
	}
	return command
}

// releaseChannelReleaseDiff buckets the desired state's Releases by which of two
// channels they are on. onlySource is the promotion worklist: Releases the
// target channel does not yet carry. A Release is identified by product@version,
// and one on neither channel is not part of this comparison at all.
func releaseChannelReleaseDiff(desired *controlplane.DesiredState, source, target string) (onlySource, onlyTarget, both []string) {
	for _, release := range desired.Releases {
		onSource := containsString(release.Channels, source)
		onTarget := containsString(release.Channels, target)
		if !onSource && !onTarget {
			continue
		}
		identity := release.Product + "@" + release.Version
		switch {
		case onSource && onTarget:
			both = append(both, identity)
		case onSource:
			onlySource = append(onlySource, identity)
		default:
			onlyTarget = append(onlyTarget, identity)
		}
	}
	sort.Strings(onlySource)
	sort.Strings(onlyTarget)
	sort.Strings(both)
	return onlySource, onlyTarget, both
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

// differingFields narrows two definitions to the fields whose values differ,
// which is what `release-channel diff` reports without --all.
//
// The comparison runs over the JSON projection rather than the Go struct for
// two reasons: the keys it emits are then the keys the output already uses, so
// an operator reading a narrowed diff can look the field up in the full
// document unchanged; and a field carrying `omitempty` that is absent on one
// side counts as a difference rather than quietly comparing equal to the other
// side's zero value.
func differingFields(left, right any) (map[string]any, map[string]any, error) {
	leftFields, err := jsonFields(left)
	if err != nil {
		return nil, nil, err
	}
	rightFields, err := jsonFields(right)
	if err != nil {
		return nil, nil, err
	}
	union := map[string]struct{}{}
	for key := range leftFields {
		union[key] = struct{}{}
	}
	for key := range rightFields {
		union[key] = struct{}{}
	}
	narrowedLeft, narrowedRight := map[string]any{}, map[string]any{}
	for key := range union {
		leftValue, leftPresent := leftFields[key]
		rightValue, rightPresent := rightFields[key]
		if leftPresent && rightPresent && reflect.DeepEqual(leftValue, rightValue) {
			continue
		}
		if leftPresent {
			narrowedLeft[key] = leftValue
		}
		if rightPresent {
			narrowedRight[key] = rightValue
		}
	}
	return narrowedLeft, narrowedRight, nil
}

func jsonFields(value any) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode Release Channel definition: %w", err)
	}
	fields := map[string]any{}
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, fmt.Errorf("normalize Release Channel definition: %w", err)
	}
	return fields, nil
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

func terminalCommand(state *commandState) *cobra.Command {
	var sessionID string
	command := &cobra.Command{
		Use: "terminal", Short: "Manage debug request metadata; interactive broker execution remains unavailable", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("interactive terminal broker execution is not available yet; terminal request records non-executing metadata, and audited break-glass tooling remains Environment-owned")
		},
	}
	command.Flags().StringVar(&sessionID, "session-id", "", "external audited break-glass session identifier (never executed by Conduit)")
	command.AddCommand(debugRequestCommand(state))
	return command
}

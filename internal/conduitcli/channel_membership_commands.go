package conduitcli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/operator"
	review "github.com/jonathanagustin/conduit/internal/planreview/domain"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v4"
)

const maximumMembershipDocument = 1 << 20

// CONDUIT-INVENTED: channelmembership.CLIProjection
// Displaced-by: a published or sanitized channel-membership editor schema, state-loading and submission/result contract.
func productReleaseChannelCommand(state *commandState) *cobra.Command {
	group := &cobra.Command{Use: "release-channel", Short: "Govern Product Release channel memberships"}
	var stateFile, rationale, nextGeneration string
	var dryRun bool
	command := &cobra.Command{Use: "edit MAVEN_COORDINATE", Short: "Edit a Release's channel memberships in $EDITOR", Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		if err := membershipOutputFormat(state); err != nil {
			return err
		}
		desired, err := readDesiredState(stateFile)
		if err != nil {
			return err
		}
		baselineDigest := membershipStateDigest(desired)
		if nextGeneration == "" || nextGeneration == desired.Environment.Generation {
			return errors.New("--next-generation must identify the new reviewed Git generation")
		}
		desired.Environment.Generation = nextGeneration
		index, err := membershipReleaseIndex(desired, args[0])
		if err != nil {
			return err
		}
		rationaleSupplied := command.Flags().Changed("rationale")
		initial := struct {
			Channels  []string `yaml:"releaseChannels"`
			Rationale *string  `yaml:"rationale,omitempty"`
		}{Channels: append([]string{}, desired.Releases[index].Channels...)}
		if !rationaleSupplied {
			initial.Rationale = new(string)
		}
		encoded, err := yaml.Marshal(initial)
		if err != nil {
			return err
		}
		contents, cancelled, err := editMembershipDocument(command, state, encoded)
		if err != nil {
			return err
		}
		if cancelled {
			return membershipCancelled(state)
		}
		channels, editedRationale, empty, err := parseMembershipEdit(contents)
		if empty {
			return membershipCancelled(state)
		}
		if err != nil {
			return err
		}
		if rationaleSupplied && editedRationale != nil {
			return errors.New("rationale was supplied by flag; do not add a second editor rationale")
		}
		if !rationaleSupplied && editedRationale != nil {
			rationale = *editedRationale
		}
		channels, err = normalizeMembershipChannels(desired, channels, desired.Releases[index].Channels)
		if err != nil {
			return err
		}
		if slices.Equal(channels, sortedMemberships(desired.Releases[index].Channels)) {
			return membershipCancelled(state)
		}
		desired.Releases[index].Channels = channels
		return submitMembershipProposal(command, state, desired, []string{args[0]}, rationale, baselineDigest, dryRun)
	}}
	command.Flags().StringVar(&stateFile, "state-file", "", "current Git-owned desired-state document (Conduit-native)")
	_ = command.MarkFlagRequired("state-file")
	command.Flags().StringVar(&nextGeneration, "next-generation", "", "new reviewed Git generation for the proposed memberships (Conduit-native)")
	_ = command.MarkFlagRequired("next-generation")
	command.Flags().StringVar(&rationale, "rationale", "", "rationale persisted in the proposal's Change Request")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "preview the edited memberships without submitting a Plan")
	group.AddCommand(command)
	return group
}

func releaseChannelRemoveCommand(state *commandState) *cobra.Command {
	var stateFile, file, rationale, nextGeneration string
	var coordinates []string
	var dryRun bool
	command := &cobra.Command{Use: "remove CHANNEL_NAME", Short: "[Experimental] Propose removing Release memberships from a channel", Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		if err := membershipOutputFormat(state); err != nil {
			return err
		}
		desired, err := readDesiredState(stateFile)
		if err != nil {
			return err
		}
		baselineDigest := membershipStateDigest(desired)
		if nextGeneration == "" || nextGeneration == desired.Environment.Generation {
			return errors.New("--next-generation must identify the new reviewed Git generation")
		}
		desired.Environment.Generation = nextGeneration
		if _, err := normalizeMembershipChannels(desired, []string{args[0]}, []string{args[0]}); err != nil {
			return err
		}
		selected := append([]string{}, coordinates...)
		if command.Flags().Changed("maven-coordinates-file") {
			contents, err := readMembershipFile(file)
			if err != nil {
				return err
			}
			var fromFile []string
			if err := decodeMembershipYAML(contents, &fromFile); err != nil {
				return err
			}
			selected = append(selected, fromFile...)
		}
		selected = sortedMemberships(selected)
		if len(selected) == 0 {
			return errors.New("select at least one --maven-coordinate or YAML-list --maven-coordinates-file")
		}
		for _, coordinate := range selected {
			index, err := membershipReleaseIndex(desired, coordinate)
			if err != nil {
				return err
			}
			desired.Releases[index].Channels = slices.DeleteFunc(append([]string{}, desired.Releases[index].Channels...), func(channel string) bool { return channel == args[0] })
		}
		return submitMembershipProposal(command, state, desired, selected, rationale, baselineDigest, dryRun)
	}}
	command.Flags().StringVar(&stateFile, "state-file", "", "current Git-owned desired-state document (Conduit-native)")
	_ = command.MarkFlagRequired("state-file")
	command.Flags().StringVar(&nextGeneration, "next-generation", "", "new reviewed Git generation for the proposed memberships (Conduit-native)")
	_ = command.MarkFlagRequired("next-generation")
	command.Flags().StringArrayVar(&coordinates, "maven-coordinate", nil, "group:name:version coordinate; repeatable")
	command.Flags().StringVarP(&file, "maven-coordinates-file", "f", "", "YAML list of group:name:version coordinates")
	command.Flags().StringVar(&rationale, "rationale", "", "rationale persisted in the proposal's Change Request")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "preview memberships and coordinates without submitting a Plan")
	return command
}

func membershipOutputFormat(state *commandState) error {
	if state.output != "json" && state.output != "yaml" {
		return errors.New("membership proposals require json or yaml output")
	}
	return nil
}

func membershipCancelled(state *commandState) error {
	return writeOutput(state.runtime.Stdout, state.output, map[string]any{"cancelled": true, "submitted": false, "valueTier": review.Native})
}

func sortedMemberships(values []string) []string {
	result := append([]string{}, values...)
	sort.Strings(result)
	return slices.Compact(result)
}

func normalizeMembershipChannels(desired controlplane.DesiredState, channels []string, retained ...[]string) ([]string, error) {
	known := map[string]bool{}
	for _, definition := range desired.ReleaseChannels {
		known[definition.Name] = !definition.Archived
	}
	for _, channel := range channels {
		if len(retained) > 0 && slices.Contains(retained[0], channel) {
			for _, definition := range desired.ReleaseChannels {
				if definition.Name == channel {
					known[channel] = true
				}
			}
		}
		if channel == "" || channel != strings.TrimSpace(channel) || !known[channel] {
			return nil, fmt.Errorf("unknown or archived Release Channel %q", channel)
		}
	}
	return sortedMemberships(channels), nil
}

func membershipReleaseIndex(desired controlplane.DesiredState, coordinate string) (int, error) {
	if _, _, err := splitProductCoordinate(coordinate); err != nil {
		return 0, err
	}
	for index, release := range desired.Releases {
		if release.Product+":"+release.Version == coordinate {
			return index, nil
		}
	}
	return 0, fmt.Errorf("Product Release coordinate %q is not present in desired state", coordinate)
}

func readMembershipFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maximumMembershipDocument+1))
	if err == nil && len(contents) > maximumMembershipDocument {
		return nil, errors.New("membership document exceeds 1 MiB")
	}
	return contents, err
}

func decodeMembershipYAML(contents []byte, value any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("invalid membership YAML: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("expected exactly one membership YAML document")
	}
	return nil
}

func parseMembershipEdit(contents []byte) ([]string, *string, bool, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(contents, &node); err != nil {
		return nil, nil, false, err
	}
	if len(node.Content) == 0 || node.Content[0].Tag == "!!null" {
		return nil, nil, true, nil
	}
	if node.Content[0].Kind == yaml.SequenceNode {
		var channels []string
		err := decodeMembershipYAML(contents, &channels)
		return channels, nil, false, err
	}
	var edited struct {
		Channels  *[]string `yaml:"releaseChannels"`
		Rationale *string   `yaml:"rationale"`
	}
	if err := decodeMembershipYAML(contents, &edited); err != nil {
		return nil, nil, false, err
	}
	if edited.Channels == nil {
		return nil, nil, false, errors.New("releaseChannels must be an explicit YAML list; [] removes all memberships")
	}
	return *edited.Channels, edited.Rationale, false, nil
}

func editMembershipDocument(command *cobra.Command, state *commandState, initial []byte) ([]byte, bool, error) {
	directory, err := os.MkdirTemp("", "conduit-membership-")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "membership.yaml")
	if err := os.WriteFile(path, initial, 0o600); err != nil {
		return nil, false, err
	}
	editor := state.runtime.Getenv("EDITOR")
	if strings.TrimSpace(editor) == "" {
		editor = "vi"
	}
	process := exec.CommandContext(command.Context(), "sh", "-c", editor+" \"$1\"", "conduit-membership-editor", path)
	process.Stdin, process.Stdout, process.Stderr = state.runtime.Stdin, state.runtime.Stdout, state.runtime.Stderr
	if err := process.Run(); err != nil {
		if command.Context().Err() != nil {
			return nil, false, command.Context().Err()
		}
		var exited *exec.ExitError
		if errors.As(err, &exited) {
			return nil, true, nil
		}
		return nil, false, err
	}
	contents, err := readMembershipFile(path)
	if err != nil {
		return nil, false, err
	}
	return contents, bytes.Equal(initial, contents) || len(bytes.TrimSpace(contents)) == 0, nil
}

func submitMembershipProposal(command *cobra.Command, state *commandState, desired controlplane.DesiredState, coordinates []string, rationale, baselineDigest string, dryRun bool) error {
	context := review.Review{Title: "Update Product Release channel memberships", Rationale: rationale, ValueTier: review.Native, BaseDesiredStateDigest: baselineDigest}
	if err := context.Validate(); err != nil {
		return err
	}
	if errs := controlplane.Validate(desired); len(errs) > 0 {
		return controlplane.ValidationErrors(errs)
	}
	if dryRun {
		return writeOutput(state.runtime.Stdout, state.output, map[string]any{"dryRun": true, "submitted": false, "mavenCoordinates": coordinates, "desiredState": desired, "review": context, "valueTier": review.Native})
	}
	return state.execute(command, func(service Service) (any, error) {
		port, ok := service.(operator.ReviewedPlanControlPlane)
		if !ok {
			return nil, errors.New("reviewed Plan proposals are unavailable from this service")
		}
		plan, err := port.ProposeReviewedPlan(command.Context(), desired, context)
		if err != nil {
			return nil, err
		}
		return map[string]any{"submitted": true, "mavenCoordinates": coordinates, "plan": plan, "valueTier": review.Native}, nil
	})
}

func membershipStateDigest(desired controlplane.DesiredState) string {
	encoded, _ := json.Marshal(desired)
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:])
}

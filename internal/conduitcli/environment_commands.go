package conduitcli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v4"
)

func addEnvironmentParityCommands(command *cobra.Command, state *commandState) {
	command.AddCommand(environmentDeleteCommand(state))
	command.AddCommand(environmentArtifactRegistryCommand(state))
	command.AddCommand(environmentConfigCommand(state))
	command.AddCommand(environmentReleaseChannelCommand(state))
	command.AddCommand(environmentVersionCommand(state))
}

func environmentDeleteCommand(state *commandState) *cobra.Command {
	var file string
	command := &cobra.Command{
		Use: "delete ENVIRONMENT_ID", Short: "Propose uninstalling all Entities from an Environment", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			desired, err := readDesiredState(file)
			if err != nil {
				return err
			}
			if desired.Environment.ID != args[0] {
				return errors.New("desired-state Environment does not match the requested Environment")
			}
			desired.Entities = nil
			return state.execute(command, func(service Service) (any, error) {
				return service.ProposePlan(command.Context(), desired)
			})
		},
	}
	command.Flags().StringVarP(&file, "file", "f", "", "next-generation desired-state document")
	// No --rationale flag, for the same reason environment setting edit has no
	// --reason: the only downstream call is ProposePlan, which takes a
	// DesiredState and nothing else, so a rationale has nowhere to travel. It
	// used to be *required* here and advertised as "retained in review context"
	// (#175), which is worse than an ignored optional flag -- the requirement
	// manufactures confidence in an audit record nobody ever wrote. Carrying it
	// instead would mean a new field on the Plan proposal in
	// internal/controlplane, which is exactly on its recorded line ceiling and
	// may only move down.
	_ = command.MarkFlagRequired("file")
	return command
}

func environmentArtifactRegistryCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "artifact-registry", Short: "Govern Environment Artifact Registry assignments"}
	command.AddCommand(environmentArtifactRegistryListCommand(state))
	command.AddCommand(environmentArtifactRegistryMutationCommand(state, true))
	command.AddCommand(environmentArtifactRegistryMutationCommand(state, false))
	return command
}

func environmentArtifactRegistryListCommand(state *commandState) *cobra.Command {
	return &cobra.Command{
		Use: "list ENVIRONMENT_ID", Short: "List Artifact Registries assigned to an Environment", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return state.execute(command, func(service Service) (any, error) {
				records, err := service.ListArtifactRegistries(command.Context())
				filtered := make([]controlplane.ArtifactRegistryRecord, 0)
				for _, record := range records {
					if record.Environment == args[0] {
						filtered = append(filtered, record)
					}
				}
				return map[string]any{"artifactRegistries": filtered}, err
			})
		},
	}
}

func environmentArtifactRegistryMutationCommand(state *commandState, assign bool) *cobra.Command {
	action := "unassign"
	if assign {
		action = "assign"
	}
	var file string
	command := &cobra.Command{
		Use:   action + " ENVIRONMENT_ID ARTIFACT_REGISTRY_ID [ARTIFACT_REGISTRY_ID...]",
		Short: strings.ToUpper(action[:1]) + action[1:] + " Artifact Registries through a reviewed desired-state Plan",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			desired, err := readDesiredState(file)
			if err != nil {
				return err
			}
			if desired.Environment.ID != args[0] {
				return errors.New("desired-state Environment does not match the requested Environment")
			}
			selected := map[string]struct{}{}
			for _, id := range args[1:] {
				selected[id] = struct{}{}
			}
			return state.execute(command, func(service Service) (any, error) {
				if assign {
					records, err := service.ListArtifactRegistries(command.Context())
					if err != nil {
						return nil, err
					}
					available := map[string]controlplane.ArtifactRegistryAssignment{}
					for _, record := range records {
						available[record.Assignment.ID] = record.Assignment
					}
					existing := map[string]struct{}{}
					for _, assignment := range desired.Environment.ArtifactRegistries {
						existing[assignment.ID] = struct{}{}
					}
					for id := range selected {
						assignment, exists := available[id]
						if !exists {
							return nil, fmt.Errorf("Artifact Registry %q is not visible", id)
						}
						if _, exists := existing[id]; !exists {
							desired.Environment.ArtifactRegistries = append(desired.Environment.ArtifactRegistries, assignment)
						}
					}
				} else {
					kept := desired.Environment.ArtifactRegistries[:0]
					for _, assignment := range desired.Environment.ArtifactRegistries {
						if _, remove := selected[assignment.ID]; !remove {
							kept = append(kept, assignment)
						}
					}
					desired.Environment.ArtifactRegistries = kept
				}
				return service.ProposePlan(command.Context(), desired)
			})
		},
	}
	command.Flags().StringVarP(&file, "file", "f", "", "next-generation desired-state document")
	// No --compliance-ticket flag, for the reason recorded on environment
	// delete: apollo-cli publishes one here, but ProposePlan carries the
	// DesiredState alone, so the ticket was demanded and then discarded (#175).
	// The divergence from the published flag list is declared in
	// docs/operations/TENANT-FIDELITY.md.
	_ = command.MarkFlagRequired("file")
	return command
}

func environmentConfigCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "config", Short: "Govern Environment profile configuration"}
	var applyFile string
	apply := &cobra.Command{
		Use: "apply [ENVIRONMENT_ID]", Short: "Propose Environment profile configuration", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			var profile controlplane.EnvironmentProfile
			if err := decodeDocument(applyFile, &profile); err != nil {
				return err
			}
			return state.execute(command, func(service Service) (any, error) {
				return service.PutEnvironmentProfile(command.Context(), args[0], profile)
			})
		},
	}
	apply.Flags().StringVar(&applyFile, "environment-config", "", "Environment profile YAML or JSON")
	_ = apply.MarkFlagRequired("environment-config")
	command.AddCommand(apply)
	var outputDir string
	get := &cobra.Command{
		Use: "get [ENVIRONMENT_ID]", Short: "Get Environment profile configuration", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return state.execute(command, func(service Service) (any, error) {
				profile, err := service.GetEnvironmentProfile(command.Context(), args[0])
				if err != nil || outputDir == "" {
					return profile, err
				}
				encoded, err := yaml.Marshal(profile)
				if err != nil {
					return nil, err
				}
				path := filepath.Join(outputDir, "environment-profile.yaml")
				if err := writeNewFile(path, encoded); err != nil {
					return nil, err
				}
				return map[string]any{"path": path, "profile": profile}, nil
			})
		},
	}
	get.Flags().StringVar(&outputDir, "output-dir", "", "optional directory for environment-profile.yaml")
	command.AddCommand(get)
	return command
}

func environmentReleaseChannelCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "release-channel", Short: "Set an Environment default Release Channel"}
	var releaseChannel, file string
	set := &cobra.Command{
		Use: "set ENVIRONMENT_ID", Short: "Propose a default Release Channel in desired state", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			desired, err := readDesiredState(file)
			if err != nil {
				return err
			}
			if desired.Environment.ID != args[0] {
				return errors.New("desired-state Environment does not match the requested Environment")
			}
			desired.Environment.DefaultReleaseChannel = releaseChannel
			return state.execute(command, func(service Service) (any, error) {
				return service.ProposePlan(command.Context(), desired)
			})
		},
	}
	set.Flags().StringVarP(&releaseChannel, "release-channel", "r", "", "default Release Channel")
	set.Flags().StringVarP(&file, "file", "f", "", "next-generation desired-state document")
	// No --compliance-ticket flag here either; see the note on the Artifact
	// Registry mutation above (#175).
	for _, flag := range []string{"release-channel", "file"} {
		_ = set.MarkFlagRequired(flag)
	}
	command.AddCommand(set)
	return command
}

// environmentVersionCommand carries no --all flag, though apollo-cli publishes
// one on `version compare`.
//
// This command already returns every Entity the Environment reports unless
// --release-channel narrows the set, so --all could only name the default or
// contradict the filter; there is no third selection for it to make. It was
// registered anyway and echoed straight back as `"all": false` beside a result
// it never touched, which reads as a filter the operator can trust.
func environmentVersionCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "version", Short: "Compare exact Entity versions"}
	var releaseChannel string
	compare := &cobra.Command{
		Use: "compare ENVIRONMENT_ID", Short: "Return exact versions reported by an Environment", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return state.execute(command, func(service Service) (any, error) {
				versions, err := service.GetEnvironmentVersions(command.Context(), args[0])
				if err != nil || releaseChannel == "" {
					return map[string]any{"versions": versions}, err
				}
				entities, err := service.ListEntities(command.Context(), args[0])
				if err != nil {
					return nil, err
				}
				environment, err := service.GetEnvironment(command.Context(), args[0])
				if err != nil {
					return nil, err
				}
				selected := selectedInstallationIDs(entities, environment.Environment, releaseChannel)
				filtered := make([]controlplane.TargetHubEntityVersion, 0, len(versions))
				for _, version := range versions {
					if _, included := selected[version.InstallationID]; included {
						filtered = append(filtered, version)
					}
				}
				return map[string]any{"versions": filtered, "releaseChannel": releaseChannel}, nil
			})
		},
	}
	compare.Flags().StringVarP(&releaseChannel, "release-channel", "r", "", "include only Entities assigned to this Release Channel")
	command.AddCommand(compare)
	return command
}

// selectedInstallationIDs returns the installations whose Entity resolves to the
// requested Release Channel, keyed the way a Plan mints installation identities.
//
// The identity of an installation is not always the Entity name. Plan expansion
// in internal/controlplane mints "<entity>@<namespaceRID>" for a universal
// Entity, one per eligible namespace, and the Agent reports that string back
// unchanged as the installation ID. Keying this set by Entity name alone
// therefore matched nothing a universal Entity ever reports, so the Release
// Channel filter dropped every universal installation with no error and no
// diagnostic: an operator comparing versions to find drift read the short result
// as "no drift" rather than "not compared".
//
// Every declared namespace is enumerated rather than only the eligible ones. A
// namespace the Entity is not eligible for has no installation to report, so the
// surplus key cannot match anything, and reproducing the eligibility rules here
// would only give them a second place to drift from the planner's.
func selectedInstallationIDs(
	entities []controlplane.Entity, environment controlplane.Environment, releaseChannel string,
) map[string]struct{} {
	selected := map[string]struct{}{}
	for _, entity := range entities {
		channel := entity.ReleaseChannel
		if channel == "" {
			channel = environment.DefaultReleaseChannel
		}
		if channel != releaseChannel {
			continue
		}
		if entity.Universal == nil {
			selected[entity.Name] = struct{}{}
			continue
		}
		for _, namespace := range environment.Namespaces {
			selected[entity.Name+"@"+namespace.RID] = struct{}{}
		}
	}
	return selected
}

func readDesiredState(path string) (controlplane.DesiredState, error) {
	var desired controlplane.DesiredState
	if err := decodeDocument(path, &desired); err != nil {
		return controlplane.DesiredState{}, err
	}
	if errs := controlplane.Validate(desired); len(errs) > 0 {
		return controlplane.DesiredState{}, controlplane.ValidationErrors(errs)
	}
	return desired, nil
}

package conduitcli

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

func addEntityParityCommands(command *cobra.Command, state *commandState) {
	command.AddCommand(entityEnforceCommand(state))
	command.AddCommand(entityAggregateCommand(state))
}

func entityEnforceCommand(state *commandState) *cobra.Command {
	var file, reason, version string
	command := &cobra.Command{
		Use: "enforce INSTALLATION_ID", Short: "Issue an exact governed Entity enforcement command", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if strings.TrimSpace(reason) == "" {
				return errors.New("--reason is required")
			}
			desired, err := readDesiredState(file)
			if err != nil {
				return err
			}
			commandType := controlplane.CommandEnforceConfig
			if version != "" {
				commandType = controlplane.CommandEnforceConfigAndVersion
			}
			request := controlplane.IssueCommandRequest{
				DesiredState: desired, Type: commandType, InstallationID: args[0], TargetVersion: version, Rationale: reason,
			}
			return state.execute(command, func(service Service) (any, error) {
				return service.IssueEntityCommand(command.Context(), request)
			})
		},
	}
	command.Flags().StringVarP(&file, "file", "f", "", "exact desired-state YAML or JSON")
	command.Flags().StringVar(&reason, "reason", "", "enforcement rationale")
	command.Flags().StringVarP(&version, "version", "v", "", "exact target version")
	_ = command.MarkFlagRequired("file")
	return command
}

func entityAggregateCommand(state *commandState) *cobra.Command {
	var outputDir, releaseChannel string
	var generateLowest, generateVersions bool
	command := &cobra.Command{
		Use: "aggregate", Short: "Generate a deterministic Product version aggregate", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return state.execute(command, func(service Service) (any, error) {
				products, err := service.ListProducts(command.Context())
				if err != nil {
					return nil, err
				}
				versions := map[string]any{}
				for _, product := range products {
					candidates := make([]string, 0)
					for _, release := range product.Releases {
						if releaseChannel == "" || containsString(release.Channels, releaseChannel) {
							candidates = append(candidates, release.Version)
						}
					}
					if len(candidates) == 0 {
						continue
					}
					sort.Strings(candidates)
					selected := candidates[len(candidates)-1]
					if generateLowest {
						selected = candidates[0]
					}
					if generateVersions {
						versions[product.Product] = map[string]any{"selected": selected, "available": candidates}
					} else {
						versions[product.Product] = selected
					}
				}
				result := map[string]any{"releaseChannel": releaseChannel, "products": versions}
				if outputDir == "" {
					return result, nil
				}
				encoded, err := yaml.Marshal(result)
				if err != nil {
					return nil, err
				}
				path := filepath.Join(outputDir, "entity-aggregate.yaml")
				if err := writeNewFile(path, encoded); err != nil {
					return nil, err
				}
				return map[string]any{"path": path, "aggregate": result}, nil
			})
		},
	}
	command.Flags().StringVar(&outputDir, "output-dir", "", "optional output directory")
	command.Flags().StringVarP(&releaseChannel, "release-channel", "r", "", "filter by Release Channel")
	command.Flags().BoolVar(&generateLowest, "generate-lowest", false, "select the lowest eligible version")
	command.Flags().BoolVar(&generateVersions, "generate-versions", false, "include all eligible versions")
	return command
}

package conduitcli

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"

	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v4"
)

// partitionOrderableVersions splits candidates into the ones Apollo ordering can
// rank and the ones it cannot, each sorted deterministically.
//
// The split exists because CompareVersions is only a partial order: it falls
// back to strings.Compare when either side is non-orderable, so mixing the two
// classes in one sort is intransitive and its result depends on input order.
// Sorting them separately keeps each side's rule intact.
func partitionOrderableVersions(candidates []string) (orderable, unorderable []string) {
	for _, candidate := range candidates {
		if catalog.IsApolloOrderableVersion(candidate) {
			orderable = append(orderable, candidate)
			continue
		}
		unorderable = append(unorderable, candidate)
	}
	// The tiebreak is not decoration. Apollo ordering deliberately ignores a
	// snapshot's commit hash, so 2.0.0-5-gaaaaaaa and 2.0.0-5-gbbbbbbb compare
	// equal; sort.Slice is not stable and the candidate list is built from map
	// iteration, so without a total order the "latest" alternated between runs of
	// a command whose help promises a deterministic aggregate.
	sort.Slice(orderable, func(i, j int) bool {
		if comparison := catalog.CompareVersions(orderable[i], orderable[j]); comparison != 0 {
			return comparison < 0
		}
		return orderable[i] < orderable[j]
	})
	sort.Strings(unorderable)
	return orderable, unorderable
}

// selectAggregateVersion picks the endpoint the caller asked for. Callers must
// pass a non-empty orderable slice; an empty one has no latest version and that
// is the caller's decision to report.
func selectAggregateVersion(orderable []string, lowest bool) string {
	if lowest {
		return orderable[0]
	}
	return orderable[len(orderable)-1]
}

func addEntityCoverageCommands(command *cobra.Command, state *commandState) {
	command.AddCommand(entityMaintenanceOverrideCommand(state))
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
				// Products whose eligible Releases cannot be ranked at all.
				unrankable := map[string][]string{}
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
					// Version ordering is numeric per component, not lexicographic:
					// sort.Strings put "1.9.0" after "1.10.0" and this command then
					// published 1.9.0 as the latest version of the Product. Use the
					// same Apollo comparator the catalogue projection sorts Releases
					// with so the aggregate and the Hub agree on which is newest.
					//
					// Only orderable versions may be ranked. A Product Release may
					// declare a non-orderable version like 1.5.0-hotfix, and
					// internal/catalog/domain says of that class: "nothing may order
					// it against another version ... upgrade selection ... keep
					// rejecting these". CompareVersions honours that by falling back
					// to strings.Compare when either side is non-orderable, which
					// makes it a partial order -- 1.10.0 > 1.9.0 numerically,
					// 1.9.0 > 1.5.0-hotfix and 1.5.0-hotfix > 1.10.0 lexically. Feed
					// that cycle to sort.Slice and the result depends on input order,
					// so one hotfix sharing a channel brought the original defect
					// straight back: 1.9.0 selected over 1.10.0.
					//
					// So the non-orderable ones are partitioned out rather than given
					// an invented rank. They are reported, not dropped -- a Product
					// whose only Release is non-orderable has no latest version, and
					// saying so is the honest answer where silently omitting it would
					// read as "no Releases".
					orderable, unorderable := partitionOrderableVersions(candidates)
					if generateVersions {
						entry := map[string]any{"available": orderable}
						if len(orderable) > 0 {
							entry["selected"] = selectAggregateVersion(orderable, generateLowest)
						}
						if len(unorderable) > 0 {
							entry["unorderable"] = unorderable
						}
						versions[product.Product] = entry
						continue
					}
					if len(orderable) == 0 {
						// Reported beside the aggregate rather than instead of it.
						// This first refused the whole command, which was the wrong
						// blast radius: a Product whose only Releases are
						// non-orderable is a documented, ordinary state, so one
						// hotfix-only Product denied every other Product its
						// perfectly rankable answer. Naming it here keeps the
						// reason visible -- silently omitting it would read as "this
						// Product has no Releases" -- without discarding the rest.
						unrankable[product.Product] = unorderable
						continue
					}
					versions[product.Product] = selectAggregateVersion(orderable, generateLowest)
				}
				result := map[string]any{"releaseChannel": releaseChannel, "products": versions}
				if len(unrankable) > 0 {
					result["unrankable"] = unrankable
				}
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

package conduitcli

import (
	"errors"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/spf13/cobra"
)

func productPromotionCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "promotion", Short: "Govern Product promotion pipelines in desired state"}
	command.AddCommand(productPromotionGetCommand(state))
	command.AddCommand(productPromotionSetCommand(state, false))
	command.AddCommand(productPromotionSetCommand(state, true))
	command.AddCommand(productPromotionToggleCommand(state, true))
	command.AddCommand(productPromotionToggleCommand(state, false))
	return command
}

func productPromotionGetCommand(state *commandState) *cobra.Command {
	var stateFile string
	command := &cobra.Command{
		Use: "get PRODUCT_ID", Short: "Get a Product promotion pipeline from desired state", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			desired, err := readDesiredState(stateFile)
			if err != nil {
				return err
			}
			setting := findProductSettings(&desired, args[0])
			if setting == nil || setting.PromotionPipeline == nil {
				return fmt.Errorf("Product %q has no promotion pipeline", args[0])
			}
			return writeOutput(state.runtime.Stdout, state.output, setting.PromotionPipeline)
		},
	}
	command.Flags().StringVar(&stateFile, "state-file", "", "desired-state YAML or JSON")
	_ = command.MarkFlagRequired("state-file")
	return command
}

func productPromotionSetCommand(state *commandState, appendStages bool) *cobra.Command {
	action := "set"
	if appendStages {
		action = "append"
	}
	var stateFile, pipelineFile string
	var dryRun, override bool
	command := &cobra.Command{
		Use: action + " PRODUCT_ID", Short: "Propose a Product promotion pipeline update", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			desired, err := readDesiredState(stateFile)
			if err != nil {
				return err
			}
			var pipeline controlplane.ProductPromotionPipeline
			if err := decodeDocument(pipelineFile, &pipeline); err != nil {
				return err
			}
			setting := ensureProductSettings(&desired, args[0])
			if appendStages && setting.PromotionPipeline != nil {
				setting.PromotionPipeline.Stages = append(setting.PromotionPipeline.Stages, pipeline.Stages...)
				if override {
					setting.PromotionPipeline.Enabled = pipeline.Enabled
				}
			} else {
				setting.PromotionPipeline = &pipeline
			}
			if errs := controlplane.Validate(desired); len(errs) > 0 {
				return controlplane.ValidationErrors(errs)
			}
			if dryRun {
				return writeOutput(state.runtime.Stdout, state.output, map[string]any{"dryRun": true, "desiredState": desired})
			}
			return state.execute(command, func(service Service) (any, error) {
				return service.ProposePlan(command.Context(), desired)
			})
		},
	}
	command.Flags().StringVar(&stateFile, "state-file", "", "next-generation desired-state YAML or JSON")
	command.Flags().StringVarP(&pipelineFile, "file", "f", "", "typed Product promotion pipeline YAML or JSON")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "validate without proposing")
	// --override belongs to `append` alone, because overriding needs something
	// to override. `append` merges stages into a pipeline that already exists
	// and has to decide whose Enabled state survives; `set` replaces the
	// pipeline whole, so the document's Enabled state is the only one there is.
	// Both actions share this constructor and the flag was registered on both
	// while only the append branch read it, so `promotion set --override` was
	// accepted and dropped.
	if appendStages {
		command.Flags().BoolVar(&override, "override", false, "allow the appended document to replace enabled state")
	}
	for _, flag := range []string{"state-file", "file"} {
		_ = command.MarkFlagRequired(flag)
	}
	return command
}

func productPromotionToggleCommand(state *commandState, enabled bool) *cobra.Command {
	action := "disable"
	if enabled {
		action = "enable"
	}
	var stateFile string
	command := &cobra.Command{
		Use: action + " PRODUCT_ID", Short: "Propose changing Product promotion evaluation state", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			desired, err := readDesiredState(stateFile)
			if err != nil {
				return err
			}
			setting := findProductSettings(&desired, args[0])
			if setting == nil || setting.PromotionPipeline == nil {
				return errors.New("Product promotion pipeline does not exist")
			}
			setting.PromotionPipeline.Enabled = enabled
			return state.execute(command, func(service Service) (any, error) {
				return service.ProposePlan(command.Context(), desired)
			})
		},
	}
	command.Flags().StringVar(&stateFile, "state-file", "", "next-generation desired-state YAML or JSON")
	_ = command.MarkFlagRequired("state-file")
	return command
}

func findProductSettings(desired *controlplane.DesiredState, product string) *controlplane.ProductSettings {
	for index := range desired.ProductSettings {
		if desired.ProductSettings[index].Product == product {
			return &desired.ProductSettings[index]
		}
	}
	return nil
}

func ensureProductSettings(desired *controlplane.DesiredState, product string) *controlplane.ProductSettings {
	if setting := findProductSettings(desired, product); setting != nil {
		return setting
	}
	desired.ProductSettings = append(desired.ProductSettings, controlplane.ProductSettings{Product: product})
	return &desired.ProductSettings[len(desired.ProductSettings)-1]
}

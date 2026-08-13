package conduitcli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/spf13/cobra"
)

func labelCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "label", Short: "Read and govern labels in Git-owned desired state"}
	command.AddCommand(labelReadCommand(state, false))
	command.AddCommand(labelReadCommand(state, true))
	command.AddCommand(labelMutationCommand(state, false))
	command.AddCommand(labelMutationCommand(state, true))
	return command
}

func labelReadCommand(state *commandState, single bool) *cobra.Command {
	action := "list"
	if single {
		action = "get"
	}
	var file, labelID string
	command := &cobra.Command{
		Use: action + " RESOURCE_TYPE RESOURCE_ID", Short: strings.ToUpper(action[:1]) + action[1:] + " desired-state labels", Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			desired, err := readDesiredState(file)
			if err != nil {
				return err
			}
			labels, err := desiredStateLabels(&desired, args[0], args[1])
			if err != nil {
				return err
			}
			if single {
				if labelID == "" {
					return errors.New("--label-id is required")
				}
				value, exists := labels[labelID]
				if !exists {
					return fmt.Errorf("label %q is not set", labelID)
				}
				return writeOutput(state.runtime.Stdout, state.output, map[string]any{"labelId": labelID, "value": value})
			}
			return writeOutput(state.runtime.Stdout, state.output, map[string]any{"labels": labels})
		},
	}
	command.Flags().StringVarP(&file, "file", "f", "", "desired-state YAML or JSON")
	command.Flags().StringVar(&labelID, "label-id", "", "label definition ID")
	_ = command.MarkFlagRequired("file")
	return command
}

func labelMutationCommand(state *commandState, remove bool) *cobra.Command {
	action := "set"
	if remove {
		action = "delete"
	}
	var file, labelID, labelValue string
	var dryRun bool
	command := &cobra.Command{
		Use: action + " RESOURCE_TYPE RESOURCE_ID", Short: strings.ToUpper(action[:1]) + action[1:] + " a label through a reviewed Plan", Args: cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			if labelID == "" || (!remove && labelValue == "") {
				return errors.New("--label-id and a non-empty --label-value for set are required")
			}
			desired, err := readDesiredState(file)
			if err != nil {
				return err
			}
			labels, err := desiredStateLabels(&desired, args[0], args[1])
			if err != nil {
				return err
			}
			if remove {
				delete(labels, labelID)
			} else {
				labels[labelID] = labelValue
			}
			if dryRun {
				return writeOutput(state.runtime.Stdout, state.output, map[string]any{"dryRun": true, "desiredState": desired})
			}
			return state.execute(command, func(service Service) (any, error) {
				return service.ProposePlan(command.Context(), desired)
			})
		},
	}
	command.Flags().StringVarP(&file, "file", "f", "", "next-generation desired-state YAML or JSON")
	command.Flags().StringVar(&labelID, "label-id", "", "label definition ID")
	command.Flags().StringVar(&labelValue, "label-value", "", "label value")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "validate and print without proposing")
	_ = command.MarkFlagRequired("file")
	return command
}

func desiredStateLabels(desired *controlplane.DesiredState, resourceType, resourceID string) (map[string]string, error) {
	ensure := func(labels *map[string]string) map[string]string {
		if *labels == nil {
			*labels = map[string]string{}
		}
		return *labels
	}
	switch strings.ToLower(resourceType) {
	case "environment":
		if desired.Environment.ID != resourceID {
			return nil, errors.New("Environment resource ID does not match desired state")
		}
		return ensure(&desired.Environment.Labels), nil
	case "entity":
		for index := range desired.Entities {
			if desired.Entities[index].Name == resourceID {
				return ensure(&desired.Entities[index].Labels), nil
			}
		}
	case "product-release":
		for index := range desired.Releases {
			coordinate := desired.Releases[index].Product + ":" + desired.Releases[index].Version
			if coordinate == resourceID {
				return ensure(&desired.Releases[index].Labels), nil
			}
		}
	default:
		return nil, fmt.Errorf("resource type %q is not represented in desired state", resourceType)
	}
	return nil, fmt.Errorf("%s resource %q was not found", resourceType, resourceID)
}

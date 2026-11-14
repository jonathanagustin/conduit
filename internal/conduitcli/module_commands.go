package conduitcli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/spf13/cobra"
)

func addModuleCoverageCommands(command *cobra.Command, state *commandState) {
	command.AddCommand(modulePublishCommand(state, "create"))
	command.AddCommand(modulePublishCommand(state, "update"))
	command.AddCommand(moduleLockCommand(state))
	command.AddCommand(moduleFlattenCommand(state))
	command.AddCommand(moduleEvaluateCommand(state))
	command.AddCommand(moduleDiffCommand(state))
}

func modulePublishCommand(state *commandState, action string) *cobra.Command {
	var file, directory string
	var dryRun bool
	command := &cobra.Command{
		Use: action, Short: strings.ToUpper(action[:1]) + action[1:] + " immutable Module Releases", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			definitions, err := readModuleDefinitions(file, directory)
			if err != nil {
				return err
			}
			for _, definition := range definitions {
				if errs := controlplane.ValidateModuleDefinition(definition); len(errs) > 0 {
					return controlplane.ValidationErrors(errs)
				}
			}
			if dryRun {
				return writeOutput(state.runtime.Stdout, state.output, map[string]any{"valid": true, "definitions": definitions})
			}
			return state.execute(command, func(service Service) (any, error) {
				releases, err := service.PublishModules(command.Context(), definitions)
				return map[string]any{"releases": releases}, err
			})
		},
	}
	command.Flags().StringVarP(&file, "module-definition-file", "f", "", "Module definition YAML or JSON")
	command.Flags().StringVar(&directory, "module-directory", "", "directory of Module definitions")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "validate without publishing")
	return command
}

func moduleLockCommand(state *commandState) *cobra.Command {
	var file, directory string
	var verify bool
	command := &cobra.Command{
		Use: "lock", Short: "Resolve an immutable Module lock", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			definitions, err := readModuleDefinitions(file, directory)
			if err != nil {
				return err
			}
			if len(definitions) != 1 {
				return errors.New("module lock requires exactly one root definition")
			}
			return state.execute(command, func(service Service) (any, error) {
				lock, err := service.LockModule(command.Context(), definitions[0])
				if err != nil {
					return nil, err
				}
				if verify {
					if errs := verifyModuleLock(lock); len(errs) > 0 {
						return nil, controlplane.ValidationErrors(errs)
					}
				}
				// "verified" reports that the checks above ran and passed, not
				// that the flag was passed; without --verify nothing was
				// checked, so false is the honest answer.
				return map[string]any{"verified": verify, "lock": lock}, nil
			})
		},
	}
	command.Flags().StringVarP(&file, "module-definition-file", "f", "", "Module definition YAML or JSON")
	command.Flags().StringVar(&directory, "module-directory", "", "directory containing one root Module definition")
	command.Flags().BoolVar(&verify, "verify", false, "fail unless the resolved lock is complete and exactly pinned")
	return command
}

// lockContentDigestPattern is the digest the Hub computes over the resolved
// lock: SHA-256 of the lock document with its own digest field cleared,
// rendered as lowercase hex. Matching the shape, not merely non-emptiness,
// is what separates a resolved lock from a zero value that happened to carry
// a placeholder string.
var lockContentDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// moduleCoordinatePattern is the group:name Product coordinate a locked Entity
// must carry; it mirrors the coordinate grammar the Hub enforces when it
// validates a Module definition.
var moduleCoordinatePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]+:[a-zA-Z0-9_.-]+$`)

// verifyModuleLock asserts the properties a lock only has once the Hub has
// actually resolved it. `--verify` used to echo its own flag back into
// {"verified": true} without reading the lock at all, so a Hub that returned
// an empty document still exited 0 and a CI pipeline asserting `.verified ==
// true` passed on nothing (issue #173). The invariants come from the lock
// type: an identified root Module, a content digest over the resolved
// document, at least one Entity — a definition that validates has an Entity or
// a submodule, and submodule flattening bottoms out in Entities — each
// flattened Entity attributed to the Module it came from and pinned to an
// exact Product coordinate, and every submodule resolved to a concrete
// published version rather than the "local" placeholder the definition may
// have written.
func verifyModuleLock(lock controlplane.ModuleLock) []controlplane.ValidationError {
	errs := make([]controlplane.ValidationError, 0)
	add := func(path, message string) {
		errs = append(errs, controlplane.ValidationError{Path: path, Message: message})
	}
	if lock.Module == "" {
		add("lock.module", "must name the root Module the lock resolves")
	}
	if !lockContentDigestPattern.MatchString(lock.ContentDigest) {
		add("lock.contentDigest", "must be sha256:<64 lowercase hex characters> over the resolved lock")
	}
	if len(lock.Entities) == 0 {
		add("lock.entities", "must resolve at least one Entity")
	}
	for index, entity := range lock.Entities {
		path := fmt.Sprintf("lock.entities[%d]", index)
		if entity.SourceModule == "" {
			add(path+".sourceModule", "must name the Module the Entity was flattened from")
		}
		if entity.Type == "" {
			add(path+".type", "must name the Entity type")
		}
		if !moduleCoordinatePattern.MatchString(entity.ProductID) {
			add(path+".productId", "must be a group:name coordinate")
		}
		if entity.EntityName == "" {
			add(path+".entityName", "must name the Entity")
		}
	}
	for index, submodule := range lock.Submodules {
		path := fmt.Sprintf("lock.submodules[%d]", index)
		if submodule.Name == "" {
			add(path+".name", "must name the submodule")
		}
		switch submodule.Version {
		case "":
			add(path+".version", "must be pinned to an exact published version")
		case "local":
			// A definition may reference a co-located sibling as
			// "version: local"; resolution replaces that with the version the
			// sibling was published as. A lock that still says "local" is not
			// a lock anyone can reinstall from.
			add(path+".version", "must be resolved from \"local\" to the published version")
		}
	}
	return errs
}

func moduleFlattenCommand(state *commandState) *cobra.Command {
	var file string
	command := &cobra.Command{
		Use: "flatten", Short: "Flatten a Module into exact locked Entities", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			definition, err := readModuleDefinition(file)
			if err != nil {
				return err
			}
			return state.execute(command, func(service Service) (any, error) {
				lock, err := service.LockModule(command.Context(), definition)
				return map[string]any{"entities": lock.Entities, "submodules": lock.Submodules, "contentDigest": lock.ContentDigest}, err
			})
		},
	}
	command.Flags().StringVarP(&file, "module-definition-file", "f", "", "Module definition YAML or JSON")
	_ = command.MarkFlagRequired("module-definition-file")
	return command
}

func moduleEvaluateCommand(state *commandState) *cobra.Command {
	var file string
	var feature []string
	command := &cobra.Command{
		Use: "evaluate", Short: "Evaluate derived Module features from an exact lock", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			definition, err := readModuleDefinition(file)
			if err != nil {
				return err
			}
			if len(feature) == 0 {
				feature = []string{"product-dependencies"}
			}
			for _, requested := range feature {
				if requested != "product-dependencies" {
					return fmt.Errorf("unsupported Module evaluation feature %q", requested)
				}
			}
			return state.execute(command, func(service Service) (any, error) {
				lock, err := service.LockModule(command.Context(), definition)
				if err != nil {
					return nil, err
				}
				products := make([]string, 0, len(lock.Entities))
				for _, entity := range lock.Entities {
					products = append(products, entity.ProductID)
				}
				sort.Strings(products)
				return map[string]any{"productDependencies": products, "contentDigest": lock.ContentDigest}, nil
			})
		},
	}
	command.Flags().StringVarP(&file, "file", "f", "", "Module definition YAML or JSON")
	command.Flags().StringSliceVar(&feature, "feature", nil, "feature to evaluate")
	_ = command.MarkFlagRequired("file")
	return command
}

func moduleDiffCommand(state *commandState) *cobra.Command {
	var files []string
	command := &cobra.Command{
		Use: "diff", Short: "Diff two typed Module definitions", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if len(files) != 2 {
				return errors.New("module diff requires exactly two --file values")
			}
			left, err := readModuleDefinition(files[0])
			if err != nil {
				return err
			}
			right, err := readModuleDefinition(files[1])
			if err != nil {
				return err
			}
			return writeOutput(state.runtime.Stdout, state.output, map[string]any{"equal": reflect.DeepEqual(left, right), "before": left, "after": right})
		},
	}
	command.Flags().StringArrayVarP(&files, "file", "f", nil, "Module definition; specify exactly twice")
	return command
}

func readModuleDefinition(path string) (controlplane.ModuleDefinition, error) {
	var definition controlplane.ModuleDefinition
	if err := decodeDocument(path, &definition); err != nil {
		return controlplane.ModuleDefinition{}, err
	}
	return definition, nil
}

func readModuleDefinitions(file, directory string) ([]controlplane.ModuleDefinition, error) {
	if (file == "") == (directory == "") {
		return nil, errors.New("select exactly one of --module-definition-file or --module-directory")
	}
	if file != "" {
		definition, err := readModuleDefinition(file)
		return []controlplane.ModuleDefinition{definition}, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read Module directory: %w", err)
	}
	paths := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension == ".yaml" || extension == ".yml" || extension == ".json" {
			paths = append(paths, filepath.Join(directory, entry.Name()))
		}
	}
	sort.Strings(paths)
	definitions := make([]controlplane.ModuleDefinition, 0, len(paths))
	for _, path := range paths {
		definition, err := readModuleDefinition(path)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	if len(definitions) == 0 {
		return nil, errors.New("Module directory contains no YAML or JSON definitions")
	}
	return definitions, nil
}

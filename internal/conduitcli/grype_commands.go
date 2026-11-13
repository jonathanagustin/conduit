package conduitcli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jonathanagustin/conduit/internal/grypescanner"
	"github.com/spf13/cobra"
)

func grypeCommand(state *commandState) *cobra.Command {
	group := &cobra.Command{Use: "grype", Short: "Run local Grype scans with NVD normalization"}
	var outputFile string
	command := &cobra.Command{
		Use: "scan SOURCE [flags] [-- GRYPE_FLAGS]", Short: "Scan locally with installed Grype; no Hub data is fetched",
		Args: func(command *cobra.Command, args []string) error {
			count := command.ArgsLenAtDash()
			if count < 0 {
				count = len(args)
			}
			if count != 1 {
				return fmt.Errorf("exactly one source is required before Grype pass-through flags")
			}
			return nil
		},
		RunE: func(command *cobra.Command, args []string) error {
			format := strings.ToLower(strings.TrimSpace(state.output))
			if format != "json" && format != "yaml" {
				return fmt.Errorf("Grype reports support json or yaml output, got %q", state.output)
			}
			var flags []string
			if index := command.ArgsLenAtDash(); index >= 0 {
				flags = args[index:]
			}
			result, err := grypescanner.ScanLocal(command.Context(), args[0], flags, command.InOrStdin())
			if err != nil {
				return err
			}
			if outputFile != "" {
				contents, err := json.MarshalIndent(result.Report, "", "  ")
				if err != nil {
					return err
				}
				if err := writeLocalGrypeReport(outputFile, append(contents, '\n')); err != nil {
					return err
				}
			}
			if err := writeOutput(state.runtime.Stdout, format, result.Report); err != nil {
				return err
			}
			if result.ExitCode != 0 {
				return fmt.Errorf("Grype exited with status %d after producing a valid report", result.ExitCode)
			}
			return nil
		},
	}
	command.Flags().StringVar(&outputFile, "output-file", "", "write the complete normalized report as private JSON")
	group.AddCommand(command)
	return group
}

func writeLocalGrypeReport(destination string, contents []byte) error {
	file, err := os.CreateTemp(filepath.Dir(destination), ".conduit-grype-*.json")
	if err != nil {
		return fmt.Errorf("create Grype report: %w", err)
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return fmt.Errorf("write Grype report: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close Grype report: %w", err)
	}
	if err := os.Rename(file.Name(), destination); err != nil {
		return fmt.Errorf("save Grype report: %w", err)
	}
	return nil
}

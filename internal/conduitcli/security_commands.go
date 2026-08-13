package conduitcli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

func cveCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "cve", Short: "Scan immutable Product Release artifacts"}
	var file, productID, releaseChannel, environment string
	var details, rescan bool
	check := &cobra.Command{
		Use: "check", Short: "Run the configured vulnerability and malware scanner", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			var release controlplane.Release
			if err := decodeDocument(file, &release); err != nil {
				return err
			}
			if productID != "" && productID != release.Product {
				return fmt.Errorf("--product-id %s does not match input Release %s", productID, release.Product)
			}
			if releaseChannel != "" && !containsString(release.Channels, releaseChannel) {
				return fmt.Errorf("input Release is not assigned to Release Channel %s", releaseChannel)
			}
			return state.execute(command, func(service Service) (any, error) {
				if environment != "" {
					if _, err := service.GetEnvironment(command.Context(), environment); err != nil {
						return nil, err
					}
				}
				result, err := service.ScanRelease(command.Context(), release)
				if err != nil {
					return nil, err
				}
				output := map[string]any{
					"product": release.Product, "version": release.Version, "releaseChannel": releaseChannel,
					"environment": environment, "details": details, "freshProviderInvocation": true, "rescan": rescan,
				}
				if details {
					output["result"] = result
				} else {
					summary := map[string]any{}
					if result.VulnerabilityAssessment != nil {
						summary["vulnerability"] = map[string]any{
							"status": result.VulnerabilityAssessment.Status, "outcome": result.VulnerabilityAssessment.Outcome,
							"activeVulnerabilities": result.VulnerabilityAssessment.ActiveVulnerabilities,
							"suppressedFindings":    result.VulnerabilityAssessment.SuppressedFindings,
						}
					}
					if result.MalwareAssessment != nil {
						summary["malware"] = map[string]any{
							"status": result.MalwareAssessment.Status, "outcome": result.MalwareAssessment.Outcome,
							"findings": result.MalwareAssessment.Findings,
						}
					}
					output["summary"] = summary
				}
				return output, nil
			})
		},
	}
	check.Flags().StringVarP(&file, "maven-coordinates-file", "f", "", "typed Product Release YAML or JSON to scan")
	check.Flags().BoolVar(&details, "details", false, "include full normalized assessment evidence")
	check.Flags().BoolVar(&rescan, "rescan", false, "request a fresh provider scan")
	check.Flags().StringVarP(&productID, "product-id", "p", "", "validate against the Product in the input Release")
	check.Flags().StringVarP(&releaseChannel, "release-channel", "r", "", "require the input Release to be assigned to this channel")
	check.Flags().StringVarP(&environment, "environment", "e", "", "require visibility of this Environment before scanning")
	_ = check.MarkFlagRequired("maven-coordinates-file")
	command.AddCommand(check)
	return command
}

func helmChartCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "helm-chart", Short: "Validate Helm chart Products"}
	var chartPath, valuesPath, expectedName, expectedVersion, repositoryURL string
	validate := &cobra.Command{
		Use: "validate", Short: "Run Helm lint and deterministic template validation", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if strings.TrimSpace(chartPath) == "" {
				return errors.New("--chart-path is required")
			}
			lintArgs := []string{"lint", chartPath}
			templateArgs := []string{"template", "conduit-validation", chartPath}
			if valuesPath != "" {
				lintArgs = append(lintArgs, "--values", valuesPath)
				templateArgs = append(templateArgs, "--values", valuesPath)
			}
			lint, err := runBoundedCommand(command, "helm", lintArgs...)
			if err != nil {
				return err
			}
			rendered, err := runBoundedCommand(command, "helm", templateArgs...)
			if err != nil {
				return err
			}
			metadataDocument, err := runBoundedCommand(command, "helm", "show", "chart", chartPath)
			if err != nil {
				return err
			}
			var metadata struct {
				Name    string `yaml:"name"`
				Version string `yaml:"version"`
			}
			if err := yaml.Unmarshal([]byte(metadataDocument), &metadata); err != nil {
				return fmt.Errorf("decode Helm chart metadata: %w", err)
			}
			if expectedName != "" && metadata.Name != expectedName {
				return fmt.Errorf("chart name %s does not match --name %s", metadata.Name, expectedName)
			}
			if expectedVersion != "" && metadata.Version != expectedVersion {
				return fmt.Errorf("chart version %s does not match --version %s", metadata.Version, expectedVersion)
			}
			if repositoryURL != "" && !strings.HasPrefix(repositoryURL, "https://") && !strings.HasPrefix(repositoryURL, "oci://") {
				return errors.New("--repository-url must use https:// or oci://")
			}
			return writeOutput(state.runtime.Stdout, state.output, map[string]any{
				"valid": true, "lint": lint, "renderedBytes": len(rendered), "name": metadata.Name,
				"version": metadata.Version, "repositoryUrl": repositoryURL,
			})
		},
	}
	validate.Flags().StringVar(&chartPath, "chart-path", "", "local Helm chart path")
	validate.Flags().StringVar(&valuesPath, "values", "", "values file")
	validate.Flags().StringVar(&expectedName, "name", "", "expected chart name")
	validate.Flags().StringVar(&expectedVersion, "version", "", "expected chart version")
	validate.Flags().StringVar(&repositoryURL, "repository-url", "", "source repository recorded by the caller")
	command.AddCommand(validate)
	return command
}

func runBoundedCommand(command *cobra.Command, executable string, args ...string) (string, error) {
	process := exec.CommandContext(command.Context(), executable, args...)
	var stdout, stderr bytes.Buffer
	process.Stdout = &limitedWriter{writer: &stdout, remaining: maxDocumentBytes}
	process.Stderr = &limitedWriter{writer: &stderr, remaining: maxDocumentBytes}
	if err := process.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("%s validation failed: %s", executable, message)
	}
	return stdout.String(), nil
}

type limitedWriter struct {
	writer    io.Writer
	remaining int
}

func (writer *limitedWriter) Write(value []byte) (int, error) {
	original := len(value)
	if writer.remaining <= 0 {
		return original, nil
	}
	if len(value) > writer.remaining {
		value = value[:writer.remaining]
	}
	_, err := writer.writer.Write(value)
	writer.remaining -= len(value)
	return original, err
}

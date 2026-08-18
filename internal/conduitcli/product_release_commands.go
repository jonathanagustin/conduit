package conduitcli

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

// The schema is embedded so `conduit product-release definition schema` and the
// Go validator cannot drift apart in a release: they ship in the same binary and
// a test compares them.
//
//go:embed apollo-product-definition-v1.schema.json
var apolloProductDefinitionSchema []byte

func addProductReleaseParityCommands(command *cobra.Command, state *commandState) {
	command.AddCommand(productReleaseInitCommand(state))
	command.AddCommand(productReleaseVerifyCommand(state))
	command.AddCommand(productReleaseAssetCommand(state))
	command.AddCommand(productReleaseTerraformCommand(state))
	command.AddCommand(productReleaseDefinitionCommand(state))
}

// productReleaseDefinitionCommand validates the manifest.yml/configuration.yml
// pair. It is separate from `verify` because `verify` answers "can Platform
// execute this Helm Product", while this answers "is this a conformant Product
// Definition" -- a service.v1 Product has no chart and would fail the first
// question while being perfectly valid.
func productReleaseDefinitionCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "definition", Short: "Validate Apollo Product Definition documents"}
	command.AddCommand(&cobra.Command{
		Use: "schema", Short: "Print the Apollo Product Definition JSON Schema", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if _, err := state.runtime.Stdout.Write(apolloProductDefinitionSchema); err != nil {
				return fmt.Errorf("write Apollo Product Definition schema: %w", err)
			}
			_, err := state.runtime.Stdout.Write([]byte("\n"))
			return err
		},
	})
	var manifestPath, configurationPath, templatesDir string
	var printDefinition bool
	verify := &cobra.Command{
		Use: "verify", Short: "Strictly validate a manifest and configuration pair", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			definition, errs, err := readProductDefinition(manifestPath, configurationPath, templatesDir)
			if err != nil {
				return err
			}
			if len(errs) > 0 {
				return controlplane.ValidationErrors(errs)
			}
			result := map[string]any{"valid": true, "productType": definition.Manifest.ProductType}
			if printDefinition {
				result["definition"] = definition
			}
			return writeOutput(state.runtime.Stdout, state.output, result)
		},
	}
	verify.Flags().StringVar(&manifestPath, "manifest", "", "Product manifest.yml")
	verify.Flags().StringVar(&configurationPath, "configuration", "", "Product configuration.yml")
	verify.Flags().StringVar(&templatesDir, "templates-dir", "",
		"directory holding tmpl managed file templates (defaults to templates/ beside the configuration)")
	verify.Flags().BoolVar(&printDefinition, "print-definition", false, "include the normalized definition")
	_ = verify.MarkFlagRequired("manifest")
	command.AddCommand(verify)
	return command
}

// readProductDefinition decodes both documents. The configuration path is
// optional because a Product that takes no installation configuration still has
// a definition, and requiring an empty file would be a worse contract than
// allowing its absence.
func readProductDefinition(manifestPath, configurationPath, templatesDir string) (
	controlplane.ApolloProductDefinition, []controlplane.ValidationError, error,
) {
	manifestFile, err := os.Open(manifestPath)
	if err != nil {
		return controlplane.ApolloProductDefinition{}, nil, fmt.Errorf("open Product manifest: %w", err)
	}
	defer manifestFile.Close()
	// A nil reader is how the decoder spells "this Product ships no
	// configuration.yml", which is different from one that is present and empty.
	var configuration io.Reader
	if configurationPath != "" {
		configurationFile, err := os.Open(configurationPath)
		if err != nil {
			return controlplane.ApolloProductDefinition{}, nil, fmt.Errorf("open Product configuration: %w", err)
		}
		defer configurationFile.Close()
		configuration = configurationFile
	}
	templates, err := readDefinitionTemplates(configurationPath, templatesDir)
	if err != nil {
		return controlplane.ApolloProductDefinition{}, nil, err
	}
	return controlplane.DecodeApolloProductDefinitionWithTemplates(manifestFile, configuration, templates)
}

// readDefinitionTemplates lists the templates a tmpl managed file can refer to.
//
// It returns nil, rather than an empty slice, when there is nowhere to look. The
// distinction matters: nil means "the caller did not tell us what was packaged",
// and the validator then skips the template check instead of reporting every
// tmpl entry as missing a file it was never shown.
func readDefinitionTemplates(configurationPath, templatesDir string) ([]string, error) {
	explicit := templatesDir != ""
	if !explicit {
		if configurationPath == "" {
			return nil, nil
		}
		// The specification packages templates/ as a sibling of configuration.yml.
		templatesDir = filepath.Join(filepath.Dir(configurationPath), "templates")
	}
	entries, err := os.ReadDir(templatesDir)
	if err != nil {
		if !explicit && errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read Product Definition templates: %w", err)
	}
	templates := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		templates = append(templates, entry.Name())
	}
	return templates, nil
}

func productReleaseInitCommand(state *commandState) *cobra.Command {
	var manifestFile, outputDir string
	command := &cobra.Command{
		Use: "init", Short: "Initialize a Product Release directory from a validated manifest", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			manifest, err := readProductManifest(manifestFile)
			if err != nil {
				return err
			}
			encoded, err := yaml.Marshal(manifest)
			if err != nil {
				return err
			}
			path := filepath.Join(outputDir, "manifest.yml")
			if err := os.MkdirAll(outputDir, 0o755); err != nil {
				return err
			}
			if err := writeNewFile(path, encoded); err != nil {
				return err
			}
			return writeOutput(state.runtime.Stdout, state.output, map[string]any{"manifestPath": path, "manifest": manifest})
		},
	}
	command.Flags().StringVar(&manifestFile, "manifest-file", "", "validated helm-chart.v1 manifest")
	command.Flags().StringVar(&outputDir, "output-dir", "", "new Product Release directory")
	for _, flag := range []string{"manifest-file", "output-dir"} {
		_ = command.MarkFlagRequired(flag)
	}
	return command
}

func productReleaseVerifyCommand(state *commandState) *cobra.Command {
	var manifestPath string
	var dryRun, printManifest bool
	command := &cobra.Command{
		Use: "verify", Short: "Strictly validate a Product Release manifest", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			manifest, err := readProductManifest(manifestPath)
			if err != nil {
				return err
			}
			result := map[string]any{"valid": true, "dryRun": dryRun}
			if printManifest {
				result["manifest"] = manifest
			}
			return writeOutput(state.runtime.Stdout, state.output, result)
		},
	}
	command.Flags().StringVar(&manifestPath, "manifest", "", "Product manifest")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "perform validation without publication")
	command.Flags().BoolVar(&printManifest, "print-manifest", false, "include the normalized manifest")
	_ = command.MarkFlagRequired("manifest")
	return command
}

func productReleaseAssetCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "asset", Short: "Publish immutable Product Release assets"}
	var assetPath, coordinate, manifestFile string

	publish := &cobra.Command{
		Use: "publish", Short: "Upload an immutable content-addressed asset", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			// --manifest-file is reported alongside the asset as the manifest it
			// belongs to, so it has to actually name that Product Release. Echoing
			// an unread path let an asset be published against one coordinate while
			// citing another Product's manifest.
			// --maven-coordinate is only ever read by the cross-check below, so on
			// its own it named an owner nothing recorded. Requiring the manifest
			// keeps the flag in the one role it actually has.
			if coordinate != "" && manifestFile == "" {
				return errors.New(
					"--maven-coordinate is verified against --manifest-file; supply the manifest or drop the coordinate",
				)
			}
			if manifestFile != "" {
				manifest, err := readProductManifest(manifestFile)
				if err != nil {
					return err
				}
				if err := validateManifestCoordinate(manifest, coordinate); err != nil {
					return err
				}
			}
			file, err := os.Open(assetPath)
			if err != nil {
				return err
			}
			defer file.Close()
			mediaType := mime.TypeByExtension(filepath.Ext(assetPath))
			if mediaType == "" {
				mediaType = "application/octet-stream"
			}
			return state.execute(command, func(service Service) (any, error) {
				descriptor, err := service.UploadArtifact(command.Context(), mediaType, file)
				if err != nil {
					return nil, err
				}
				// Report the upload and the manifest cross-check, because those are
				// the two things that happened. The previous version also echoed the
				// coordinate, the images and the label as if the asset had been bound
				// to them; the Hub stores an anonymous content-addressed blob, so an
				// operator reading the output as evidence -- which is what this repo
				// asks them to do -- was reading a binding that does not exist.
				result := map[string]any{"descriptor": descriptor}
				if manifestFile != "" {
					result["manifestFile"] = manifestFile
					result["mavenCoordinate"] = coordinate
				}
				return result, nil
			})
		},
	}
	publish.Flags().StringVar(&assetPath, "asset-path", "", "asset file to upload")
	publish.Flags().StringVar(&coordinate, "maven-coordinate", "", "owning Product Release coordinate")
	publish.Flags().StringVar(&manifestFile, "manifest-file", "", "optional manifest associated with the asset")
	// No --images, --label-name or --label-value. UploadArtifact sends the file
	// body and nothing else, and POST /v1/artifacts/blobs stores an anonymous
	// content-addressed blob, so there is no field for any of them to travel in.
	// Apollo documents --images as declaring artifacts it checks for
	// availability; Conduit checks nothing. Accepting them and echoing them back
	// reported a binding that was never made, which is worse than an absent flag
	// -- an unknown-flag error tells the operator the truth immediately.
	_ = publish.MarkFlagRequired("asset-path")
	command.AddCommand(publish)
	return command
}

func productReleaseTerraformCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "terraform", Short: "Author Terraform Product Release manifests"}
	var coordinate, outputDir, repositoryURL string
	initCommand := &cobra.Command{
		Use: "init", Short: "Initialize a typed terraform.v1 Product manifest", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			product, version, err := splitProductCoordinate(coordinate)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(repositoryURL, "oci://") || !strings.Contains(repositoryURL, "@sha256:") {
				return errors.New("--terraform-repository-url must be an immutable oci:// reference containing @sha256:")
			}
			productParts := strings.Split(product, ":")
			manifest := controlplane.TerraformProductManifest{
				ManifestVersion: "1.0", ProductGroup: productParts[0], ProductName: productParts[1],
				ProductVersion: version, ProductType: "terraform.v1",
			}
			manifest.Extensions.TerraformRepositoryURL = repositoryURL
			if err := controlplane.ValidateTerraformProductManifest(manifest); err != nil {
				return err
			}
			encoded, err := yaml.Marshal(manifest)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(outputDir, 0o755); err != nil {
				return err
			}
			path := filepath.Join(outputDir, "manifest.yml")
			if err := writeNewFile(path, encoded); err != nil {
				return err
			}
			return writeOutput(state.runtime.Stdout, state.output, map[string]any{"manifestPath": path, "manifest": manifest})
		},
	}
	initCommand.Flags().StringVar(&coordinate, "maven-coordinate", "", "Product group:name:version")
	initCommand.Flags().StringVar(&outputDir, "output-dir", "", "new Product Release directory")
	initCommand.Flags().StringVar(&repositoryURL, "terraform-repository-url", "", "immutable OCI Terraform bundle URL")
	for _, flag := range []string{"maven-coordinate", "output-dir", "terraform-repository-url"} {
		_ = initCommand.MarkFlagRequired(flag)
	}
	command.AddCommand(initCommand)
	return command
}

func validateManifestCoordinate(manifest controlplane.ApolloProductManifest, coordinate string) error {
	expected := manifest.ProductGroup + ":" + manifest.ProductName + ":" + manifest.ProductVersion
	if coordinate != "" && coordinate != expected {
		return fmt.Errorf("manifest coordinate %s does not match %s", expected, coordinate)
	}
	return nil
}

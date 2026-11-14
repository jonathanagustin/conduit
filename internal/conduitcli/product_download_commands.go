package conduitcli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/jonathanagustin/conduit/internal/operator"
	assets "github.com/jonathanagustin/conduit/internal/productassets/domain"
	"github.com/spf13/cobra"
)

// CONDUIT-INVENTED: productassets.CLICatalogue
// Displaced-by: a published or owner-approved download-asset CLI contract establishing command paths, declarations, dry-run and catalogue read semantics.
func productReleaseDownloadsCommand(state *commandState) *cobra.Command {
	group := &cobra.Command{Use: "download-assets", Short: "Publish and read Conduit-native download metadata; no execution or signature verification"}
	run := func(cmd *cobra.Command, operation func(operator.DownloadControlPlane) (any, error)) error {
		return state.execute(cmd, func(service Service) (any, error) {
			p, ok := service.(operator.DownloadControlPlane)
			if !ok {
				return nil, errors.New("native download-asset catalogue is unavailable from this service")
			}
			return operation(p)
		})
	}
	var dryRun bool
	publish := &cobra.Command{Use: "publish DECLARATION_JSON", Args: cobra.ExactArgs(1), Short: "Catalogue immutable publisher-declared downloads; '-' reads stdin", RunE: func(cmd *cobra.Command, args []string) error {
		format := strings.ToLower(strings.TrimSpace(state.output))
		if format != "json" && format != "yaml" {
			return errors.New("download-asset publication requires json or yaml output")
		}
		input := state.runtime.Stdin
		if args[0] != "-" {
			file, err := os.Open(args[0])
			if err != nil {
				return errors.New("could not open download-asset declaration")
			}
			defer file.Close()
			input = file
		}
		contents, err := io.ReadAll(io.LimitReader(input, assets.MaxDocumentBytes+1))
		if err != nil || len(contents) > assets.MaxDocumentBytes {
			return errors.New("could not read a bounded download-asset declaration")
		}
		decoder := json.NewDecoder(bytes.NewReader(contents))
		decoder.DisallowUnknownFields()
		var manifest assets.Manifest
		if err := decoder.Decode(&manifest); err != nil {
			return errors.New("invalid download-asset declaration JSON")
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return errors.New("expected one bounded download-asset declaration")
		}
		canonical, err := manifest.Canonical()
		if err != nil {
			return err
		}
		if dryRun {
			digest, err := canonical.Digest()
			if err != nil {
				return err
			}
			return writeOutput(state.runtime.Stdout, state.output, map[string]any{"dryRun": true, "status": "NOT_PUBLISHED", "valueTier": assets.ValueTier, "manifest": canonical, "digest": digest})
		}
		return run(cmd, func(p operator.DownloadControlPlane) (any, error) {
			return p.PublishDownloadRelease(cmd.Context(), canonical)
		})
	}}
	publish.Flags().BoolVar(&dryRun, "dry-run", false, "validate and display metadata locally without contacting or publishing to the Hub")
	group.AddCommand(publish)
	group.AddCommand(&cobra.Command{Use: "get PRODUCT VERSION", Args: cobra.ExactArgs(2), Short: "Read authorized receipt-bound download metadata", RunE: func(cmd *cobra.Command, args []string) error {
		return run(cmd, func(p operator.DownloadControlPlane) (any, error) {
			return p.GetDownloadRelease(cmd.Context(), args[0], args[1])
		})
	}})
	group.AddCommand(&cobra.Command{Use: "list PRODUCT", Args: cobra.ExactArgs(1), Short: "List authorized download-asset releases", RunE: func(cmd *cobra.Command, args []string) error {
		return run(cmd, func(p operator.DownloadControlPlane) (any, error) {
			return p.ListDownloadReleases(cmd.Context(), args[0])
		})
	}})
	return group
}

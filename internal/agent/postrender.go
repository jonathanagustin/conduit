package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jonathanagustin/conduit/internal/apollo/workloadmetadata"
)

// Helm applies a chart by rendering it and sending the result to the API server.
// A post-renderer is the one supported point between those two steps, so it is
// where Platform stamps the documented Apollo workload metadata onto objects it
// did not author.
//
// Doing it here rather than by editing a chart's values keeps the requirement
// off Product authors: a chart does not have to expose a labels hook, or know
// that Apollo metadata exists, for its workloads to be identifiable.

// PostRenderCommand is the subcommand this binary answers when Helm invokes it
// between rendering and applying. The metadata travels as a file path argument
// because Helm gives a post-renderer no stdin beyond the manifests themselves.
const PostRenderCommand = "post-render"

// RunPostRenderer reads rendered manifests, stamps the metadata described by the
// JSON file at metadataPath, and writes the result.
func RunPostRenderer(metadataPath string, in io.Reader, out io.Writer) error {
	raw, err := os.ReadFile(metadataPath)
	if err != nil {
		return fmt.Errorf("read post-render metadata: %w", err)
	}
	var metadata workloadmetadata.ApolloWorkloadMetadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return fmt.Errorf("parse post-render metadata: %w", err)
	}
	rendered, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("read rendered manifests: %w", err)
	}
	stamped, err := workloadmetadata.ApplyApolloWorkloadMetadata(rendered, metadata)
	if err != nil {
		return err
	}
	_, err = out.Write(stamped)
	return err
}

// PostRendererPluginName is the Helm 4 plugin this binary installs for itself.
const PostRendererPluginName = "platform-apollo-metadata"

// postRendererPluginManifest is the plugin.yaml Helm 4 requires. The schema was
// established against Helm v4.1.1 rather than from documentation, which does not
// yet specify postrenderer/v1: `helm plugin list` reports the plugin, and Helm
// appends each --post-renderer-args value after the args declared here, so the
// subprocess is invoked as `<command> post-render <metadata-path>`.
const postRendererPluginManifest = `apiVersion: v1
name: ` + PostRendererPluginName + `
type: postrenderer/v1
runtime: subprocess
version: %s
runtimeConfig:
  platformCommand:
    - command: %q
      args: [%q]
`

// InstallPostRendererPlugin writes the Helm 4 post-renderer plugin that invokes
// this binary and points HELM_PLUGINS at it.
//
// It must only ever be called for Helm 4. Helm 3 does not recognise the
// postrenderer/v1 type and writes a plugin-load failure to stdout on every
// command, which corrupts the JSON that `helm list` and `helm status` are parsed
// from -- so merely making this visible to a Helm 3 breaks release lookup long
// before any post-rendering is attempted. HelmExecutor gates the call on the
// major version it detected.
//
// Any directory already on HELM_PLUGINS is preserved after ours, so installing
// this cannot hide an operator's own plugins, and repeated calls neither
// duplicate that entry nor rewrite an unchanged manifest.
func InstallPostRendererPlugin(root, version string) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate agent binary for post-render plugin: %w", err)
	}
	directory := filepath.Join(root, PostRendererPluginName)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create post-render plugin directory: %w", err)
	}
	if version == "" {
		version = "0.0.0"
	}
	manifest := fmt.Sprintf(postRendererPluginManifest, version, executable, PostRenderCommand)
	if err := os.WriteFile(filepath.Join(directory, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		return fmt.Errorf("write post-render plugin manifest: %w", err)
	}
	plugins := root
	if existing := os.Getenv("HELM_PLUGINS"); existing != "" {
		// Idempotent: this runs once per install, and appending the same root
		// every time would grow the variable without bound.
		for _, entry := range filepath.SplitList(existing) {
			if entry == root {
				return nil
			}
		}
		plugins = root + string(os.PathListSeparator) + existing
	}
	return os.Setenv("HELM_PLUGINS", plugins)
}

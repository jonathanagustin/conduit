package productmanifest

import (
	"errors"
	"fmt"
	"time"

	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	validationdomain "github.com/jonathanagustin/conduit/internal/validation/domain"
)

// ExecutableRelease translates the stricter immutable Helm document subset into
// Catalog values. Catalog owns reference grammar and channel defaults; delivery
// still decides execution admission. Mutable locators remain non-executable.
func ExecutableRelease(manifest Manifest) (catalog.Release, error) {
	if errs := ValidatePackaging(manifest); len(errs) > 0 {
		return catalog.Release{}, validationdomain.Problems(errs)
	}
	chart, ok := catalog.ParseImmutableArtifactReference(manifest.Extensions.HelmChart.RepositoryURL)
	if !ok {
		return catalog.Release{}, errors.New("Helm repository URL must be an immutable oci:// reference with an explicit tag and @sha256 digest")
	}
	images := make([]catalog.Artifact, 0, len(manifest.Extensions.Artifacts))
	for index, candidate := range manifest.Extensions.Artifacts {
		artifact, exact := catalog.ParseImmutableArtifactReference(candidate.URI)
		if !exact {
			return catalog.Release{}, fmt.Errorf("extensions.artifacts[%d].uri must contain an explicit tag and @sha256 digest", index)
		}
		images = append(images, artifact)
	}
	if len(images) == 0 {
		return catalog.Release{}, errors.New("at least one exact image artifact is required")
	}
	release := catalog.Release{
		Product: manifest.ProductGroup + ":" + manifest.ProductName, Version: manifest.ProductVersion,
		Chart: chart, Images: images, Channels: catalog.DefaultChannelsForVersion(manifest.ProductVersion),
		RolloutStrategy: manifest.Extensions.RolloutStrategy,
	}
	for _, dependency := range manifest.Extensions.ProductDependencies {
		release.Dependencies = append(release.Dependencies, catalog.Dependency{
			Product:        dependency.ProductGroup + ":" + dependency.ProductName,
			MinimumVersion: dependency.MinimumVersion, MaximumVersion: dependency.MaximumVersion, Optional: dependency.Optional,
		})
	}
	for _, incompatibility := range manifest.Extensions.ProductIncompatibilities {
		release.Incompatibilities = append(release.Incompatibilities, catalog.Incompatibility{
			Product:        incompatibility.ProductGroup + ":" + incompatibility.ProductName,
			MinimumVersion: incompatibility.MinimumVersion, MaximumVersion: incompatibility.MaximumVersion,
		})
	}
	if manifest.Extensions.ResourceReadinessTimeout != "" {
		duration, _ := time.ParseDuration(manifest.Extensions.ResourceReadinessTimeout)
		release.ResourceReadinessTimeoutSeconds = int(duration.Seconds())
	}
	if manifest.Extensions.ImagePrePullConfig != nil {
		duration, _ := time.ParseDuration(manifest.Extensions.ImagePrePullConfig.Timeout)
		release.ImagePrePullTimeoutSeconds = int(duration.Seconds())
	}
	return release, nil
}

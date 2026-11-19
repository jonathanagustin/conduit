package controlplane

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jonathanagustin/conduit/internal/apollo/productmanifest"
	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
)

// ApolloProductManifest preserves source callers while the document lives at its compatibility boundary.
type ApolloProductManifest = productmanifest.Manifest
type ApolloProductManifestExtensions = productmanifest.Extensions
type ApolloManifestVolume = productmanifest.Volume
type ApolloManifestVolumeType = productmanifest.VolumeType
type ApolloDurableVolume = productmanifest.DurableVolume
type ApolloImagePrePullConfig = productmanifest.ImagePrePullConfig
type ApolloManifestArtifact = productmanifest.Artifact
type ApolloManifestHelmChart = productmanifest.HelmChart
type ApolloManifestDependency = productmanifest.Dependency
type ApolloManifestIncompatibility = productmanifest.Incompatibility

func DecodeApolloProductManifest(reader io.Reader) (ApolloProductManifest, []ValidationError, error) {
	manifest, err := productmanifest.Decode(reader)
	if err != nil {
		return ApolloProductManifest{}, nil, err
	}
	return manifest, ValidateApolloProductManifest(manifest), nil
}

func ValidateApolloProductManifest(manifest ApolloProductManifest) []ValidationError {
	return productmanifest.ValidatePackaging(manifest)
}

// ExecutableReleaseFromApolloManifest adapts only the stricter subset that can
// preserve Conduit's exact-digest execution invariant. A valid public Apollo
// manifest may still be non-executable here when it uses mutable locators.
func ExecutableReleaseFromApolloManifest(manifest ApolloProductManifest) (Release, error) {
	if errs := ValidateApolloProductManifest(manifest); len(errs) > 0 {
		return Release{}, ValidationErrors(errs)
	}
	chart, ok := artifactFromImmutableOCIReference(manifest.Extensions.HelmChart.RepositoryURL)
	if !ok {
		return Release{}, errors.New("Helm repository URL must be an immutable oci:// reference with an explicit tag and @sha256 digest")
	}
	images := make([]Artifact, 0, len(manifest.Extensions.Artifacts))
	for index, candidate := range manifest.Extensions.Artifacts {
		artifact, exact := artifactFromImmutableOCIReference(candidate.URI)
		if !exact {
			return Release{}, fmt.Errorf("extensions.artifacts[%d].uri must contain an explicit tag and @sha256 digest", index)
		}
		images = append(images, artifact)
	}
	if len(images) == 0 {
		return Release{}, errors.New("at least one exact image artifact is required")
	}
	release := Release{
		Product: manifest.ProductGroup + ":" + manifest.ProductName, Version: manifest.ProductVersion,
		Chart: chart, Images: images, Channels: defaultChannelsForVersion(manifest.ProductVersion),
		RolloutStrategy: manifest.Extensions.RolloutStrategy,
	}
	for _, dependency := range manifest.Extensions.ProductDependencies {
		release.Dependencies = append(release.Dependencies, Dependency{
			Product:        dependency.ProductGroup + ":" + dependency.ProductName,
			MinimumVersion: dependency.MinimumVersion, MaximumVersion: dependency.MaximumVersion, Optional: dependency.Optional,
		})
	}
	for _, incompatibility := range manifest.Extensions.ProductIncompatibilities {
		release.Incompatibilities = append(release.Incompatibilities, Incompatibility{
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

// artifactFromImmutableOCIReference retains the shared Product-import entry point.
func artifactFromImmutableOCIReference(reference string) (Artifact, bool) {
	return catalog.ParseImmutableArtifactReference(reference)
}

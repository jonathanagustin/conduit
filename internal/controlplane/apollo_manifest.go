package controlplane

import (
	"io"

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

// ExecutableReleaseFromApolloManifest retains the existing compatibility entry point.
func ExecutableReleaseFromApolloManifest(manifest ApolloProductManifest) (Release, error) {
	return productmanifest.ExecutableRelease(manifest)
}

// artifactFromImmutableOCIReference retains the shared Product-import entry point.
func artifactFromImmutableOCIReference(reference string) (Artifact, bool) {
	return catalog.ParseImmutableArtifactReference(reference)
}

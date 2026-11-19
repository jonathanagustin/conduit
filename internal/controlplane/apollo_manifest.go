package controlplane

import (
	"errors"
	"fmt"
	"io"
	"strings"
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
	errs := make([]ValidationError, 0)
	add := func(path, message string) { errs = append(errs, ValidationError{Path: path, Message: message}) }
	if manifest.ManifestVersion != "1.0" {
		add("manifest-version", "must be 1.0")
	}
	if !coordinatePattern.MatchString(manifest.ProductGroup + ":" + manifest.ProductName) {
		add("product", "product-group and product-name must form a group:name coordinate")
	}
	if !versionPattern.MatchString(manifest.ProductVersion) {
		add("product-version", "must be an Apollo orderable version")
	}
	if manifest.ProductType != "helm-chart.v1" {
		add("product-type", "must be helm-chart.v1 for the Helm manifest adapter")
	}
	helm := manifest.Extensions.HelmChart
	if helm == nil {
		add("extensions.helm-chart", "is required")
	} else {
		if strings.TrimSpace(helm.Name) == "" {
			add("extensions.helm-chart.helm-chart-name", "must not be empty")
		}
		if !versionPattern.MatchString(helm.Version) {
			add("extensions.helm-chart.helm-chart-version", "must be an Apollo orderable version")
		}
		if !validApolloRepositoryURL(helm.RepositoryURL) {
			add("extensions.helm-chart.helm-repository-url", "must use https:// or oci://")
		}
	}
	if strategy := manifest.Extensions.RolloutStrategy; strategy != "" &&
		strategy != RolloutManageRollout && strategy != RolloutApplyChangesNoWait {
		add("extensions.rollout-strategy", "must be manageRollout or applyChangesNoWait")
	}
	if timeout := manifest.Extensions.ResourceReadinessTimeout; timeout != "" {
		if duration, err := time.ParseDuration(timeout); err != nil || duration <= 0 || duration > 24*time.Hour {
			add("extensions.resource-readiness-timeout", "must be a positive duration no greater than 24h")
		}
	}
	if config := manifest.Extensions.ImagePrePullConfig; config != nil {
		if duration, err := time.ParseDuration(config.Timeout); err != nil || duration < time.Minute || duration > 90*time.Minute {
			add("extensions.image-pre-pull-config.timeout", "must be between 1m and 90m")
		}
	}
	for index, artifact := range manifest.Extensions.Artifacts {
		path := fmt.Sprintf("extensions.artifacts[%d]", index)
		if artifact.Type != "oci" {
			add(path+".type", "must be oci")
		}
		if strings.TrimSpace(artifact.URI) == "" || strings.Contains(artifact.URI, "://") {
			add(path+".uri", "must be an OCI reference without a URL scheme")
		}
	}
	seenDependencies := make(map[string]struct{}, len(manifest.Extensions.ProductDependencies))
	for index, dependency := range manifest.Extensions.ProductDependencies {
		path := fmt.Sprintf("extensions.product-dependencies[%d]", index)
		coordinate := dependency.ProductGroup + ":" + dependency.ProductName
		if !coordinatePattern.MatchString(coordinate) {
			add(path, "product-group and product-name must form a coordinate")
		}
		if _, exists := seenDependencies[coordinate]; exists {
			add(path, "must not duplicate a Product dependency")
		}
		seenDependencies[coordinate] = struct{}{}
		validateApolloManifestRange(path, dependency.MinimumVersion, dependency.MaximumVersion, true, &errs)
		if dependency.RecommendedVersion != "" {
			if !versionPattern.MatchString(dependency.RecommendedVersion) ||
				compareVersions(dependency.RecommendedVersion, dependency.MinimumVersion) < 0 ||
				!versionWithinMaximum(dependency.RecommendedVersion, dependency.MaximumVersion) {
				add(path+".recommended-version", "must be an orderable version inside the dependency range")
			}
		}
	}
	seenIncompatibilities := make(map[string]struct{}, len(manifest.Extensions.ProductIncompatibilities))
	for index, incompatibility := range manifest.Extensions.ProductIncompatibilities {
		path := fmt.Sprintf("extensions.product-incompatibilities[%d]", index)
		coordinate := incompatibility.ProductGroup + ":" + incompatibility.ProductName
		if !coordinatePattern.MatchString(coordinate) {
			add(path, "product-group and product-name must form a coordinate")
		}
		key := coordinate + ":" + incompatibility.ProductType
		if _, exists := seenIncompatibilities[key]; exists {
			add(path, "must not duplicate a Product incompatibility")
		}
		seenIncompatibilities[key] = struct{}{}
		validateApolloManifestRange(path, incompatibility.MinimumVersion, incompatibility.MaximumVersion, false, &errs)
	}
	validateApolloManifestServiceExtensions(manifest.Extensions, &errs)
	return errs
}

// validateApolloManifestServiceExtensions checks the extensions that describe a
// running workload rather than a packaging locator. They are validated for every
// Product type because a Helm Product that declares volumes-v2 means the same
// thing by it as a service Product does.
func validateApolloManifestServiceExtensions(extensions ApolloProductManifestExtensions, errs *[]ValidationError) {
	productmanifest.ValidateServiceExtensions(extensions, errs)
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

func validateApolloManifestRange(path, minimum, maximum string, required bool, errs *[]ValidationError) {
	if (required || minimum != "") && !versionPattern.MatchString(minimum) {
		*errs = append(*errs, ValidationError{Path: path + ".minimum-version", Message: "must be an Apollo orderable version"})
	}
	if (required || maximum != "") && !versionMatcherPattern.MatchString(maximum) {
		*errs = append(*errs, ValidationError{Path: path + ".maximum-version", Message: "must be an Apollo version matcher"})
	}
	if minimum != "" && maximum != "" && versionPattern.MatchString(minimum) &&
		versionMatcherPattern.MatchString(maximum) && !versionWithinMaximum(minimum, maximum) {
		*errs = append(*errs, ValidationError{Path: path, Message: "minimum-version must not exceed maximum-version"})
	}
}

package productmanifest

import (
	"fmt"
	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	validationdomain "github.com/jonathanagustin/conduit/internal/validation/domain"
	"regexp"
	"strings"
	"time"
)

var packagingCoordinatePattern = regexp.MustCompile(validationdomain.ProductCoordinateExpression)

// ValidatePackaging retains the stricter Helm manifest document checks.
// Catalog owns exact-reference grammar and Release values; execution admission remains native.
func ValidatePackaging(manifest Manifest) []validationdomain.Problem {
	errs := make([]validationdomain.Problem, 0)
	add := func(path, message string) {
		errs = append(errs, validationdomain.Problem{Path: path, Message: message})
	}
	if manifest.ManifestVersion != "1.0" {
		add("manifest-version", "must be 1.0")
	}
	if !packagingCoordinatePattern.MatchString(manifest.ProductGroup + ":" + manifest.ProductName) {
		add("product", "product-group and product-name must form a group:name coordinate")
	}
	if !catalog.VersionPattern.MatchString(manifest.ProductVersion) {
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
		if !catalog.VersionPattern.MatchString(helm.Version) {
			add("extensions.helm-chart.helm-chart-version", "must be an Apollo orderable version")
		}
		if !validRepositoryURL(helm.RepositoryURL) {
			add("extensions.helm-chart.helm-repository-url", "must use https:// or oci://")
		}
	}
	if strategy := manifest.Extensions.RolloutStrategy; strategy != "" &&
		strategy != catalog.RolloutManageRollout && strategy != catalog.RolloutApplyChangesNoWait {
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
		if !packagingCoordinatePattern.MatchString(coordinate) {
			add(path, "product-group and product-name must form a coordinate")
		}
		if _, exists := seenDependencies[coordinate]; exists {
			add(path, "must not duplicate a Product dependency")
		}
		seenDependencies[coordinate] = struct{}{}
		validateManifestRange(path, dependency.MinimumVersion, dependency.MaximumVersion, true, &errs)
		if dependency.RecommendedVersion != "" {
			if !catalog.VersionPattern.MatchString(dependency.RecommendedVersion) ||
				catalog.CompareVersions(dependency.RecommendedVersion, dependency.MinimumVersion) < 0 ||
				!catalog.VersionWithinMaximum(dependency.RecommendedVersion, dependency.MaximumVersion) {
				add(path+".recommended-version", "must be an orderable version inside the dependency range")
			}
		}
	}
	seenIncompatibilities := make(map[string]struct{}, len(manifest.Extensions.ProductIncompatibilities))
	for index, incompatibility := range manifest.Extensions.ProductIncompatibilities {
		path := fmt.Sprintf("extensions.product-incompatibilities[%d]", index)
		coordinate := incompatibility.ProductGroup + ":" + incompatibility.ProductName
		if !packagingCoordinatePattern.MatchString(coordinate) {
			add(path, "product-group and product-name must form a coordinate")
		}
		key := coordinate + ":" + incompatibility.ProductType
		if _, exists := seenIncompatibilities[key]; exists {
			add(path, "must not duplicate a Product incompatibility")
		}
		seenIncompatibilities[key] = struct{}{}
		validateManifestRange(path, incompatibility.MinimumVersion, incompatibility.MaximumVersion, false, &errs)
	}
	ValidateServiceExtensions(manifest.Extensions, &errs)
	return errs
}

func validateManifestRange(path, minimum, maximum string, required bool, errs *[]validationdomain.Problem) {
	if (required || minimum != "") && !catalog.VersionPattern.MatchString(minimum) {
		*errs = append(*errs, validationdomain.Problem{Path: path + ".minimum-version", Message: "must be an Apollo orderable version"})
	}
	if (required || maximum != "") && !catalog.VersionMatcherPattern.MatchString(maximum) {
		*errs = append(*errs, validationdomain.Problem{Path: path + ".maximum-version", Message: "must be an Apollo version matcher"})
	}
	if minimum != "" && maximum != "" && catalog.VersionPattern.MatchString(minimum) &&
		catalog.VersionMatcherPattern.MatchString(maximum) && !catalog.VersionWithinMaximum(minimum, maximum) {
		*errs = append(*errs, validationdomain.Problem{Path: path, Message: "minimum-version must not exceed maximum-version"})
	}
}

func validRepositoryURL(value string) bool {
	return strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "oci://")
}

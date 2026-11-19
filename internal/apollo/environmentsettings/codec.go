package environmentsettings

import (
	"fmt"
	"regexp"
	"strings"

	validationdomain "github.com/jonathanagustin/conduit/internal/validation/domain"
	"go.yaml.in/yaml/v4"
)

var (
	coordinatePattern      = regexp.MustCompile(validationdomain.ProductCoordinateExpression)
	dnsLabelPattern        = regexp.MustCompile(validationdomain.DNSLabelExpression)
	releaseChannelPattern  = regexp.MustCompile(validationdomain.ReleaseChannelExpression)
	dependencyGroupPattern = regexp.MustCompile(validationdomain.DependencyGroupExpression)
)

// Decode parses the settings document strictly. Unknown
// keys are preserved rather than rejected, because an operator's document may
// legitimately carry fields this adapter does not model yet, and silently dropping
// them on a round trip would destroy settings.
func Decode(contents string) (Settings, error) {
	var settings Settings
	if strings.TrimSpace(contents) == "" {
		return Settings{}, validationdomain.Problems{{
			Path: "settings", Message: "document is empty",
		}}
	}
	if err := yaml.Unmarshal([]byte(contents), &settings); err != nil {
		return Settings{}, validationdomain.Problems{{
			Path: "settings", Message: "decode settings document: " + err.Error(),
		}}
	}
	if errs := Validate(settings); len(errs) > 0 {
		return Settings{}, errs
	}
	return settings, nil
}

// Validate returns the existing ordered diagnostics for a settings document.
func Validate(settings Settings) validationdomain.Problems {
	errs := validationdomain.Problems{}
	if settings.ReleaseChannel != "" && !releaseChannelPattern.MatchString(settings.ReleaseChannel) {
		errs = append(errs, validationdomain.Problem{Path: SettingsFieldReleaseChannel, Message: "must be an uppercase Release Channel identifier"})
	}
	seen := make(map[string]struct{}, len(settings.ManagedHelmCharts))
	for index, chart := range settings.ManagedHelmCharts {
		path := fmt.Sprintf("%s[%d]", SettingsFieldManagedHelmCharts, index)
		name := strings.TrimSpace(chart.EntityLocator.Name)
		if name == "" {
			errs = append(errs, validationdomain.Problem{
				Path: path + "." + SettingsFieldEntityLocator + ".name", Message: "must not be empty",
			})
		}
		// Two entries locating the same installation make the document
		// self-contradictory: nothing decides which one wins.
		if _, duplicate := seen[name]; duplicate && name != "" {
			errs = append(errs, validationdomain.Problem{Path: path, Message: "duplicate installation " + name})
		}
		seen[name] = struct{}{}
		if chart.Product != "" && !coordinatePattern.MatchString(chart.Product) {
			errs = append(errs, validationdomain.Problem{
				Path: path + "." + SettingsFieldProduct, Message: "must be a group:name coordinate",
			})
		}
		if chart.KubernetesNamespace != "" && !dnsLabelPattern.MatchString(chart.KubernetesNamespace) {
			errs = append(errs, validationdomain.Problem{Path: path + "." + SettingsFieldKubernetesNamespace, Message: "must be a DNS label"})
		}
		if chart.ReleaseChannel != "" && !releaseChannelPattern.MatchString(chart.ReleaseChannel) {
			errs = append(errs, validationdomain.Problem{Path: path + "." + SettingsFieldReleaseChannel, Message: "must be an uppercase Release Channel identifier"})
		}
		if chart.HelmReleaseName != "" &&
			(!dnsLabelPattern.MatchString(chart.HelmReleaseName) || len(chart.HelmReleaseName) > 53) {
			errs = append(errs, validationdomain.Problem{Path: path + "." + SettingsFieldHelmReleaseName, Message: "must be a DNS label no longer than 53 characters"})
		}
		if chart.Stack != "" && !dependencyGroupPattern.MatchString(chart.Stack) {
			errs = append(errs, validationdomain.Problem{Path: path + "." + SettingsFieldStack, Message: "must be a stack identifier"})
		}
		for product, stack := range chart.DependencyStackOverrides {
			if !coordinatePattern.MatchString(product) || !dependencyGroupPattern.MatchString(stack) {
				errs = append(errs, validationdomain.Problem{Path: path + "." + SettingsFieldDependencyStacks, Message: "must map Product coordinates to stack identifiers"})
			}
		}
	}
	return errs
}

// Encode renders the settings document.
func Encode(settings Settings) (string, error) {
	encoded, err := yaml.Marshal(settings)
	if err != nil {
		return "", fmt.Errorf("encode settings document: %w", err)
	}
	return string(encoded), nil
}

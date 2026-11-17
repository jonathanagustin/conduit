package packagingtools

import (
	"fmt"
	"strings"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/udsschema"
	"gopkg.in/yaml.v3"
)

const (
	rulePackageCR          = "package-cr-schema"
	reasonPackageAPI       = "package cr: apiVersion must be uds.dev/v1alpha1"
	reasonPackageKind      = "package cr: kind must be Package"
	reasonPackageName      = "package cr: metadata.name is required"
	reasonPackageNamespace = "package cr: metadata.namespace is required"
	reasonPackageNetwork   = "package cr: spec.network is required"
	reasonPackageAllow     = "package cr: spec.network.allow must be a list"
	reasonPackageSSO       = "package cr: spec.sso must be a list when present"
)

// ValidatePackageCR checks a Package custom resource against the pinned UDS
// Core Package schema and against the rules packaging adds on top of it.
func (service *Service) ValidatePackageCR(raw string) domain.ManifestReport {
	return validatePackageCR(raw)
}

// validatePackageCR is two checks over the same document.
//
// The first is Conduit's own: the envelope, and that a network policy is
// stated at all, which the upstream schema does not require. The second is the
// upstream schema for the UDS Core version Conduit targets (see
// internal/udsschema). That schema closes every object, so a misspelled or
// unknown field anywhere under spec is a finding. Before it was consulted,
// only the handful of fields named below were looked at and everything else
// passed.
func validatePackageCR(raw string) domain.ManifestReport {
	if strings.TrimSpace(raw) == "" {
		return report(domain.ManifestFinding{Path: "uds-package.yaml", Rule: rulePackageCR, Severity: sevError, Message: "package cr is empty"})
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return report(domain.ManifestFinding{Path: "uds-package.yaml", Rule: rulePackageCR, Severity: sevError, Message: "package cr is not valid YAML"})
	}
	findings := packagePolicyFindings(doc)
	stated := make(map[string]struct{}, len(findings))
	for _, finding := range findings {
		stated[finding.Path] = struct{}{}
	}
	for _, violation := range udsschema.ValidatePackage([]byte(raw)) {
		path := violation.Path
		if path == "" {
			path = "uds-package.yaml"
		}
		// Where Conduit's own rule already names the path, its message is the
		// one operators and tests know; the schema's would only repeat it.
		if _, duplicate := stated[path]; duplicate {
			continue
		}
		findings = append(findings, domain.ManifestFinding{
			Path: path, Rule: rulePackageCR, Severity: sevError,
			Message: "package cr: " + violation.String() + " (UDS Core " + udsschema.CoreVersion + " Package schema)",
		})
	}
	return report(findings...)
}

func packagePolicyFindings(doc map[string]any) []domain.ManifestFinding {
	var findings []domain.ManifestFinding
	if fmt.Sprint(doc["apiVersion"]) != "uds.dev/v1alpha1" {
		findings = append(findings, domain.ManifestFinding{Path: "apiVersion", Rule: rulePackageCR, Severity: sevError, Message: reasonPackageAPI})
	}
	if fmt.Sprint(doc["kind"]) != "Package" {
		findings = append(findings, domain.ManifestFinding{Path: "kind", Rule: rulePackageCR, Severity: sevError, Message: reasonPackageKind})
	}
	meta, _ := asMap(doc["metadata"])
	if stringField(meta, "name") == "" {
		findings = append(findings, domain.ManifestFinding{Path: "metadata.name", Rule: rulePackageCR, Severity: sevError, Message: reasonPackageName})
	}
	if stringField(meta, "namespace") == "" {
		findings = append(findings, domain.ManifestFinding{Path: "metadata.namespace", Rule: rulePackageCR, Severity: sevError, Message: reasonPackageNamespace})
	}
	spec, ok := asMap(doc["spec"])
	if !ok {
		findings = append(findings, domain.ManifestFinding{Path: "spec.network", Rule: rulePackageCR, Severity: sevError, Message: reasonPackageNetwork})
		return findings
	}
	network, netOK := asMap(spec["network"])
	if !netOK {
		findings = append(findings, domain.ManifestFinding{Path: "spec.network", Rule: rulePackageCR, Severity: sevError, Message: reasonPackageNetwork})
	} else if _, exists := network["allow"]; exists {
		if _, listOK := asSlice(network["allow"]); !listOK {
			findings = append(findings, domain.ManifestFinding{Path: "spec.network.allow", Rule: rulePackageCR, Severity: sevError, Message: reasonPackageAllow})
		}
	}
	if _, exists := spec["sso"]; exists {
		if _, listOK := asSlice(spec["sso"]); !listOK {
			findings = append(findings, domain.ManifestFinding{Path: "spec.sso", Rule: rulePackageCR, Severity: sevError, Message: reasonPackageSSO})
		}
	}
	return findings
}

func stringField(obj map[string]any, key string) string {
	if obj == nil {
		return ""
	}
	value, ok := obj[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

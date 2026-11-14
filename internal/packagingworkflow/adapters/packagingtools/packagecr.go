package packagingtools

import (
	"fmt"
	"strings"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
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

// ValidatePackageCR checks the Package custom resource schema used by packaging.
func (service *Service) ValidatePackageCR(raw string) domain.ManifestReport {
	return validatePackageCR(raw)
}

func validatePackageCR(raw string) domain.ManifestReport {
	if strings.TrimSpace(raw) == "" {
		return report(domain.ManifestFinding{Path: "uds-package.yaml", Rule: rulePackageCR, Severity: sevError, Message: "package cr is empty"})
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return report(domain.ManifestFinding{Path: "uds-package.yaml", Rule: rulePackageCR, Severity: sevError, Message: "package cr is not valid YAML"})
	}
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
		return report(findings...)
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
	return report(findings...)
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

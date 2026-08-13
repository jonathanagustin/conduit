package controlplane

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

var namedDefaultHostnameTemplatePattern = regexp.MustCompile(`^\{\{\s*default_hostname\s*\(\s*organizationName\s*:\s*"([^"]+)"\s*\)\s*\}\}$`)
var environmentPreprocessPattern = regexp.MustCompile(`\{\{\s*preprocess\s+\.Values\.apollo\.environment\.([A-Za-z_][A-Za-z0-9_.]*)\s*\}\}`)
var secretPreprocessPattern = regexp.MustCompile(`\{\{\s*preprocess\s+\.Values\.apollo\.secrets\.([A-Za-z0-9][A-Za-z0-9-]*)\.k8sSecretName\s*\}\}`)

type versionedConfigBlock struct {
	Overrides map[string]any `yaml:"overrides"`
}

func resolveDeploymentHelmValues(environment Environment, entity Entity) (json.RawMessage, error) {
	values := map[string]any{}
	if len(entity.HelmValues) > 0 {
		if err := json.Unmarshal(entity.HelmValues, &values); err != nil {
			return nil, fmt.Errorf("decode Helm values: %w", err)
		}
	}
	overrides := map[string]any{}
	if strings.TrimSpace(entity.ConfigOverrides) != "" {
		preprocessed, err := preprocessDeploymentConfig(entity.ConfigOverrides, environment)
		if err != nil {
			return nil, err
		}
		blocks, err := decodeVersionedConfigOverrides(preprocessed)
		if err != nil {
			return nil, err
		}
		selected, found := selectVersionedConfigOverride(blocks, entity.Version)
		if !found {
			return nil, fmt.Errorf("no config override declares support for Product version %s", entity.Version)
		}
		overrides = selected
	}
	pullSecrets := make([]map[string]string, 0)
	secrets := make(map[string]any, len(environment.Secrets))
	defaultRegistry := ""
	for _, assignment := range environment.ArtifactRegistries {
		if defaultRegistry == "" {
			defaultRegistry = assignment.URL
		}
		if assignment.ImagePullSecretName != "" {
			pullSecrets = append(pullSecrets, map[string]string{"name": assignment.ImagePullSecretName})
		}
	}
	for _, assignment := range environment.Secrets {
		secrets[assignment.Name] = map[string]any{"k8sSecretName": assignment.KubernetesSecretName}
	}
	if len(environment.Config) == 0 && len(overrides) == 0 && len(pullSecrets) == 0 && defaultRegistry == "" && len(secrets) == 0 {
		return append(json.RawMessage(nil), entity.HelmValues...), nil
	}
	values = mergeStringMaps(values, cloneJSON(overrides))
	parts := strings.SplitN(entity.Product, ":", 2)
	entityID, _ := ApolloHelmChartEntityID(environment.ID, entity.Name)
	compatibilityValues := map[string]any{
		"entity":      map[string]any{"id": entityID},
		"product":     map[string]any{"group": parts[0], "name": parts[1], "version": entity.Version},
		"environment": mergeStringMaps(map[string]any{"id": environment.ID}, cloneJSON(environment.Config)),
	}
	if defaultRegistry != "" {
		compatibilityValues["defaultArtifactStoreURI"] = defaultRegistry
	}
	if len(pullSecrets) > 0 {
		compatibilityValues["imagePullSecrets"] = pullSecrets
	}
	if len(secrets) > 0 {
		compatibilityValues["secrets"] = secrets
	}
	// The key is part of the public Product chart compatibility boundary. Keep
	// the provider-specific wire name here rather than in generic domain types.
	values["apollo"] = compatibilityValues
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("encode deployment Helm values: %w", err)
	}
	return encoded, nil
}

func decodeVersionedConfigOverrides(contents string) (map[string]versionedConfigBlock, error) {
	decoder := yaml.NewDecoder(strings.NewReader(contents))
	decoder.KnownFields(true)
	blocks := map[string]versionedConfigBlock{}
	if err := decoder.Decode(&blocks); err != nil {
		return nil, fmt.Errorf("decode versioned config overrides: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("versioned config overrides must contain exactly one YAML document")
		}
		return nil, fmt.Errorf("decode trailing config override data: %w", err)
	}
	if len(blocks) == 0 {
		return nil, errors.New("versioned config overrides must contain at least one starting version")
	}
	for version, block := range blocks {
		if !versionPattern.MatchString(version) {
			return nil, fmt.Errorf("config override starting version %q is not orderable", version)
		}
		if block.Overrides == nil {
			return nil, fmt.Errorf("config override %s must contain an overrides object", version)
		}
	}
	return blocks, nil
}

func selectVersionedConfigOverride(blocks map[string]versionedConfigBlock, target string) (map[string]any, bool) {
	targetMajor := strings.SplitN(target, ".", 2)[0]
	selected := ""
	for start := range blocks {
		if strings.SplitN(start, ".", 2)[0] != targetMajor || compareVersions(start, target) > 0 {
			continue
		}
		if selected == "" || compareVersions(start, selected) > 0 {
			selected = start
		}
	}
	if selected == "" {
		return nil, false
	}
	return cloneJSON(blocks[selected].Overrides), true
}

func preprocessDeploymentConfig(contents string, environment Environment) (string, error) {
	var preprocessErr error
	resolved := environmentPreprocessPattern.ReplaceAllStringFunc(contents, func(match string) string {
		path := environmentPreprocessPattern.FindStringSubmatch(match)[1]
		value, found := lookupConfigPath(environment.Config, strings.Split(path, "."))
		if !found {
			preprocessErr = fmt.Errorf("Environment Config key %s does not exist", path)
			return match
		}
		encoded, err := yaml.Marshal(value)
		if err != nil {
			preprocessErr = fmt.Errorf("encode Environment Config key %s: %w", path, err)
			return match
		}
		return strings.TrimSpace(string(encoded))
	})
	resolved = secretPreprocessPattern.ReplaceAllStringFunc(resolved, func(match string) string {
		name := secretPreprocessPattern.FindStringSubmatch(match)[1]
		for _, assignment := range environment.Secrets {
			if assignment.Name == name {
				encoded, err := yaml.Marshal(assignment.KubernetesSecretName)
				if err != nil {
					preprocessErr = fmt.Errorf("encode deployment secret %s Kubernetes name: %w", name, err)
					return match
				}
				return strings.TrimSpace(string(encoded))
			}
		}
		preprocessErr = fmt.Errorf("deployment secret %s does not exist", name)
		return match
	})
	return resolved, preprocessErr
}

func lookupConfigPath(value any, path []string) (any, bool) {
	current := value
	for _, part := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func validateEnvironmentConfig(path string, config map[string]any, errs *[]ValidationError) {
	var visit func(string, any)
	visit = func(current string, value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, child := range typed {
				if strings.ContainsAny(key, "-. \t\r\n") || key == "" {
					*errs = append(*errs, ValidationError{Path: current, Message: fmt.Sprintf("key %q must not contain hyphens, periods, or whitespace", key)})
				}
				visit(current+"."+key, child)
			}
		case []any:
			for index, child := range typed {
				visit(fmt.Sprintf("%s[%d]", current, index), child)
			}
		}
	}
	visit(path, config)
}

func validateVersionedConfigOverrides(path, contents string, errs *[]ValidationError) {
	if strings.TrimSpace(contents) == "" {
		return
	}
	validationContents := environmentPreprocessPattern.ReplaceAllString(contents, "null")
	validationContents = secretPreprocessPattern.ReplaceAllString(validationContents, "null")
	if _, err := decodeVersionedConfigOverrides(validationContents); err != nil {
		*errs = append(*errs, ValidationError{Path: path, Message: err.Error()})
	}
}

func resolvedConfigurationDigest(config *ManagedInstallationConfiguration, namespace *NamespaceTarget) (string, error) {
	_, digest, err := resolvedExecutionConfiguration(config, nil, namespace)
	return digest, err
}

func resolvedExecutionConfiguration(
	config *ManagedInstallationConfiguration, helmValues json.RawMessage, namespace *NamespaceTarget,
) (json.RawMessage, string, error) {
	if config == nil {
		config = &ManagedInstallationConfiguration{}
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, "", fmt.Errorf("encode installation configuration: %w", err)
	}
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		return nil, "", fmt.Errorf("decode installation configuration: %w", err)
	}
	resolved, err := resolveValue(value, namespace)
	if err != nil {
		return nil, "", err
	}
	resolvedValues := any(map[string]any{})
	if len(helmValues) > 0 {
		if err := json.Unmarshal(helmValues, &resolvedValues); err != nil {
			return nil, "", fmt.Errorf("decode Helm values: %w", err)
		}
	}
	resolvedValues, err = resolveValue(resolvedValues, namespace)
	if err != nil {
		return nil, "", fmt.Errorf("resolve Helm values: %w", err)
	}
	valuesJSON, err := json.Marshal(resolvedValues)
	if err != nil {
		return nil, "", fmt.Errorf("encode resolved Helm values: %w", err)
	}
	digestInput, err := json.Marshal(struct {
		Installation any `json:"installation"`
		HelmValues   any `json:"helmValues"`
	}{Installation: resolved, HelmValues: resolvedValues})
	if err != nil {
		return nil, "", fmt.Errorf("encode resolved execution configuration: %w", err)
	}
	digest := sha256.Sum256(digestInput)
	return valuesJSON, "sha256:" + hex.EncodeToString(digest[:]), nil
}

func resolveValue(value any, namespace *NamespaceTarget) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		resolved := make(map[string]any, len(typed))
		for key, child := range typed {
			value, err := resolveValue(child, namespace)
			if err != nil {
				return nil, fmt.Errorf("resolve %s: %w", key, err)
			}
			resolved[key] = value
		}
		return resolved, nil
	case []any:
		resolved := make([]any, len(typed))
		for i, child := range typed {
			value, err := resolveValue(child, namespace)
			if err != nil {
				return nil, fmt.Errorf("resolve index %d: %w", i, err)
			}
			resolved[i] = value
		}
		return resolved, nil
	case string:
		return resolveTemplateString(typed, namespace)
	default:
		return value, nil
	}
}

func resolveTemplateString(value string, namespace *NamespaceTarget) (string, error) {
	templates := defaultHostnameTemplateFindPattern.FindAllString(value, -1)
	if len(templates) == 0 {
		return value, nil
	}
	if namespace == nil {
		return "", fmt.Errorf("enrollment template requires authoritative namespace metadata")
	}
	resolved := value
	for _, template := range templates {
		hostname, err := hostnameForTemplate(template, *namespace)
		if err != nil {
			return "", err
		}
		resolved = strings.Replace(resolved, template, hostname, 1)
	}
	return resolved, nil
}

func hostnameForTemplate(template string, namespace NamespaceTarget) (string, error) {
	if match := namedDefaultHostnameTemplatePattern.FindStringSubmatch(template); match != nil {
		for _, organization := range namespace.Organizations {
			if organization.DisplayName == match[1] {
				return organization.PrimaryHostname, nil
			}
		}
		return "", fmt.Errorf("namespace %s has no organization named %q", namespace.RID, match[1])
	}
	if len(namespace.Organizations) != 1 {
		return "", fmt.Errorf("namespace %s must have exactly one organization for default_hostname", namespace.RID)
	}
	return namespace.Organizations[0].PrimaryHostname, nil
}

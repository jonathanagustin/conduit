package conduitcli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"go.yaml.in/yaml/v4"
)

var (
	environmentIDPattern   = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,61}[a-z0-9])?$`)
	digestReferencePattern = regexp.MustCompile(`^[^\s/:]+(?:[.:][^\s/@]+|/[^\s/@]+)+(?:[:][^\s/@]+)?@sha256:[a-f0-9]{64}$`)
)

type environmentDescriptor struct {
	APIVersion  string                   `json:"apiVersion"`
	Kind        string                   `json:"kind"`
	ContactTeam string                   `json:"contactTeam"`
	Environment controlplane.Environment `json:"environment"`
}

func newEnvironmentDescriptor(name, contactTeam string) (environmentDescriptor, error) {
	name = strings.TrimSpace(name)
	contactTeam = strings.TrimSpace(contactTeam)
	if !environmentIDPattern.MatchString(name) {
		return environmentDescriptor{}, fmt.Errorf("Environment name must be a lowercase DNS-style ID no longer than 63 characters")
	}
	// An empty contact team is a first-class state, not a missing field. Apollo
	// publishes an Environment with no owning team (apollo-cli made --contact-team
	// optional to align with the API), and Conduit's own profile model already
	// permits it: ContactTeams returns the empty set and validateReferences
	// accepts it. The requirement lived only on this authoring path, so an
	// upstream ownerless Environment could not be expressed here. It can now.
	return environmentDescriptor{
		APIVersion: "platform.delivery/v1alpha1", Kind: "EnvironmentDescriptor", ContactTeam: contactTeam,
		Environment: controlplane.Environment{ID: name, Generation: "1", DeclaredStateSource: controlplane.SourceEditOnThisHub},
	}, nil
}

type spokeManifestInput struct {
	Environment         string
	Namespace           string
	Image               string
	HubURL              string
	AgentID             string
	ClientTLSSecret     string
	ExternalCAConfigMap string
	StorageClass        string
}

func generateSpokeManifest(input spokeManifestInput) (map[string]any, error) {
	input.Environment = strings.TrimSpace(input.Environment)
	input.Namespace = strings.TrimSpace(input.Namespace)
	input.Image = strings.TrimSpace(input.Image)
	input.HubURL = strings.TrimSpace(input.HubURL)
	input.AgentID = strings.TrimSpace(input.AgentID)
	input.ClientTLSSecret = strings.TrimSpace(input.ClientTLSSecret)
	input.ExternalCAConfigMap = strings.TrimSpace(input.ExternalCAConfigMap)
	if !environmentIDPattern.MatchString(input.Environment) || !environmentIDPattern.MatchString(input.Namespace) {
		return nil, fmt.Errorf("Environment and Namespace must be lowercase DNS-style IDs")
	}
	if !digestReferencePattern.MatchString(input.Image) {
		return nil, fmt.Errorf("--image must be an exact OCI reference ending in @sha256:<64 lowercase hex characters>")
	}
	if !strings.HasPrefix(input.HubURL, "https://") {
		return nil, fmt.Errorf("--control-plane-url must use https")
	}
	if input.ClientTLSSecret == "" || input.ExternalCAConfigMap == "" {
		return nil, fmt.Errorf("existing client TLS Secret and external CA ConfigMap names are required")
	}
	if input.AgentID == "" {
		input.AgentID = input.Environment
	}
	annotations := map[string]any{}
	if input.StorageClass != "" {
		annotations["delivery.platform/k8s-storage-class"] = input.StorageClass
	}
	labels := map[string]any{
		"app.kubernetes.io/name": "conduit-agent", "app.kubernetes.io/instance": input.Environment,
		"app.kubernetes.io/component": "spoke",
	}
	serviceAccount := map[string]any{
		"apiVersion": "v1", "kind": "ServiceAccount",
		"metadata":                     map[string]any{"name": "conduit-agent", "namespace": input.Namespace, "labels": labels},
		"automountServiceAccountToken": true,
	}
	podMetadata := map[string]any{"labels": labels}
	if len(annotations) > 0 {
		podMetadata["annotations"] = annotations
	}
	deployment := map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{"name": "conduit-agent", "namespace": input.Namespace, "labels": labels},
		"spec": map[string]any{
			"replicas": 1,
			"selector": map[string]any{"matchLabels": labels},
			"template": map[string]any{
				"metadata": podMetadata,
				"spec": map[string]any{
					"serviceAccountName": "conduit-agent", "automountServiceAccountToken": true,
					"securityContext": map[string]any{
						"runAsNonRoot": true, "runAsUser": 65532, "runAsGroup": 65532,
						"seccompProfile": map[string]any{"type": "RuntimeDefault"},
					},
					"containers": []any{map[string]any{
						"name": "spoke-agent", "image": input.Image, "imagePullPolicy": "IfNotPresent",
						"env": []any{
							map[string]any{"name": "CONTROL_PLANE_URL", "value": input.HubURL},
							map[string]any{"name": "CONTROL_PLANE_CA_FILE", "value": "/etc/ssl/platform-ca/ca.pem"},
							map[string]any{"name": "CONTROL_PLANE_CLIENT_CERT_FILE", "value": "/run/platform-tls/tls.crt"},
							map[string]any{"name": "CONTROL_PLANE_CLIENT_KEY_FILE", "value": "/run/platform-tls/tls.key"},
							map[string]any{"name": "ENVIRONMENT_ID", "value": input.Environment},
							map[string]any{"name": "AGENT_ID", "value": input.AgentID},
						},
						"securityContext": map[string]any{
							"allowPrivilegeEscalation": false, "readOnlyRootFilesystem": true,
							"capabilities": map[string]any{"drop": []any{"ALL"}},
						},
						"resources": map[string]any{
							"requests": map[string]any{"cpu": "50m", "memory": "64Mi"},
							"limits":   map[string]any{"cpu": "500m", "memory": "256Mi"},
						},
						"volumeMounts": []any{
							map[string]any{"name": "temporary", "mountPath": "/tmp"},
							map[string]any{"name": "client-tls", "mountPath": "/run/platform-tls", "readOnly": true},
							map[string]any{"name": "external-ca", "mountPath": "/etc/ssl/platform-ca", "readOnly": true},
						},
					}},
					"volumes": []any{
						map[string]any{"name": "temporary", "emptyDir": map[string]any{}},
						map[string]any{"name": "client-tls", "secret": map[string]any{"secretName": input.ClientTLSSecret, "optional": false}},
						map[string]any{"name": "external-ca", "configMap": map[string]any{
							"name": input.ExternalCAConfigMap, "items": []any{map[string]any{"key": "ca.pem", "path": "ca.pem"}},
						}},
					},
				},
			},
		},
	}
	return map[string]any{"apiVersion": "v1", "kind": "List", "items": []any{serviceAccount, deployment}}, nil
}

type helmChartInitOptions struct {
	RepositoryURL string
	Coordinate    string
	ChartPath     string
	Name          string
	Version       string
	ValuesPath    string
	SchemaPath    string
	OutputDir     string
	Artifacts     []string
	AsTGZ         bool
}

type helmChartInitResult struct {
	ManifestPath string   `json:"manifestPath,omitempty"`
	ValuesPath   string   `json:"valuesPath,omitempty"`
	SchemaPath   string   `json:"schemaPath,omitempty"`
	ArchivePath  string   `json:"archivePath,omitempty"`
	Artifacts    []string `json:"artifacts"`
}

func initializeHelmChartManifest(options helmChartInitOptions) (helmChartInitResult, error) {
	var err error
	options, err = resolveHelmChartInitSource(options)
	if err != nil {
		return helmChartInitResult{}, err
	}
	parts := strings.Split(options.Coordinate, ":")
	if len(parts) != 3 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" {
		return helmChartInitResult{}, fmt.Errorf("--maven-coordinate must be group:name:version")
	}
	values, err := readBoundedHelmInput(options.ValuesPath, controlplane.MaxHelmDefaultConfigBytes, "values")
	if err != nil {
		return helmChartInitResult{}, err
	}
	artifacts := append([]string(nil), options.Artifacts...)
	if len(artifacts) == 0 {
		var discovered struct {
			Image struct {
				Repository string `yaml:"repository"`
				Tag        string `yaml:"tag"`
				Digest     string `yaml:"digest"`
			} `yaml:"image"`
		}
		if err := yaml.Unmarshal(values, &discovered); err != nil {
			return helmChartInitResult{}, fmt.Errorf("decode values: %w", err)
		}
		if discovered.Image.Repository != "" && discovered.Image.Digest != "" {
			artifact := discovered.Image.Repository
			if discovered.Image.Tag != "" {
				artifact += ":" + discovered.Image.Tag
			}
			artifacts = append(artifacts, artifact+"@"+discovered.Image.Digest)
		}
	}
	if len(artifacts) == 0 {
		return helmChartInitResult{}, fmt.Errorf("at least one exact --artifact is required or must be discoverable from image.repository/tag/digest in values")
	}
	manifestArtifacts := make([]controlplane.ApolloManifestArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if !digestReferencePattern.MatchString(artifact) {
			return helmChartInitResult{}, fmt.Errorf("artifact %q is not an exact OCI sha256 digest reference", artifact)
		}
		manifestArtifacts = append(manifestArtifacts, controlplane.ApolloManifestArtifact{Type: "oci", URI: artifact})
	}
	manifest := controlplane.ApolloProductManifest{
		ManifestVersion: "1.0", ProductGroup: parts[0], ProductName: parts[1], ProductVersion: parts[2], ProductType: "helm-chart.v1",
		Extensions: controlplane.ApolloProductManifestExtensions{
			Artifacts: manifestArtifacts,
			HelmChart: &controlplane.ApolloManifestHelmChart{Name: options.Name, Version: options.Version, RepositoryURL: options.RepositoryURL},
		},
	}
	if errs := controlplane.ValidateApolloProductManifest(manifest); len(errs) > 0 {
		return helmChartInitResult{}, controlplane.ValidationErrors(errs)
	}
	if _, err := controlplane.ExecutableReleaseFromApolloManifest(manifest); err != nil {
		return helmChartInitResult{}, fmt.Errorf("Conduit executable Product manifest: %w", err)
	}
	encoded, err := yaml.Marshal(manifest)
	if err != nil {
		return helmChartInitResult{}, fmt.Errorf("encode manifest: %w", err)
	}
	if err := os.MkdirAll(options.OutputDir, 0o755); err != nil {
		return helmChartInitResult{}, fmt.Errorf("create output directory: %w", err)
	}
	var schema []byte
	if options.SchemaPath != "" {
		schema, err = readBoundedHelmInput(options.SchemaPath, controlplane.MaxHelmConfigSchemaBytes, "values schema")
		if err != nil {
			return helmChartInitResult{}, err
		}
	}
	bundle := controlplane.HelmProductReleaseBundle{Manifest: manifest, DefaultConfig: values, ConfigSchema: schema}
	if err := controlplane.ValidateHelmProductReleaseBundle(bundle); err != nil {
		return helmChartInitResult{}, err
	}
	if options.AsTGZ {
		archive, err := encodeHelmProductTGZ(bundle)
		if err != nil {
			return helmChartInitResult{}, err
		}
		archivePath := filepath.Join(options.OutputDir, manifest.ProductName+"-"+manifest.ProductVersion+".config.tgz")
		if err := writeNewFile(archivePath, archive); err != nil {
			return helmChartInitResult{}, err
		}
		return helmChartInitResult{ArchivePath: archivePath, Artifacts: artifacts}, nil
	}
	manifestPath := filepath.Join(options.OutputDir, "manifest.yml")
	valuesPath := filepath.Join(options.OutputDir, "values.yaml")
	if err := writeNewFile(manifestPath, encoded); err != nil {
		return helmChartInitResult{}, err
	}
	if filepath.Clean(options.ValuesPath) != filepath.Clean(valuesPath) {
		if err := writeNewFile(valuesPath, values); err != nil {
			_ = os.Remove(manifestPath)
			return helmChartInitResult{}, err
		}
	}
	result := helmChartInitResult{ManifestPath: manifestPath, ValuesPath: valuesPath, Artifacts: artifacts}
	if len(schema) > 0 {
		result.SchemaPath = filepath.Join(options.OutputDir, "values.schema.json")
		if filepath.Clean(options.SchemaPath) != filepath.Clean(result.SchemaPath) {
			if err := writeNewFile(result.SchemaPath, schema); err != nil {
				return helmChartInitResult{}, err
			}
		}
	}
	return result, nil
}

func resolveHelmChartInitSource(options helmChartInitOptions) (helmChartInitOptions, error) {
	if options.ChartPath == "" {
		missing := make([]string, 0, 3)
		for flag, value := range map[string]string{"--name": options.Name, "--version": options.Version, "--values": options.ValuesPath} {
			if strings.TrimSpace(value) == "" {
				missing = append(missing, flag)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			return helmChartInitOptions{}, fmt.Errorf("explicit chart initialization requires %s, or use --chart-path", strings.Join(missing, ", "))
		}
		return options, nil
	}
	if options.Name != "" || options.Version != "" || options.ValuesPath != "" || options.SchemaPath != "" {
		return helmChartInitOptions{}, fmt.Errorf("--chart-path cannot be combined with --name, --version, --values, or --config-schema")
	}
	info, err := os.Stat(options.ChartPath)
	if err != nil {
		return helmChartInitOptions{}, fmt.Errorf("inspect Helm chart path: %w", err)
	}
	if !info.IsDir() {
		return helmChartInitOptions{}, fmt.Errorf("--chart-path must name a directory")
	}
	chartFile := filepath.Join(options.ChartPath, "Chart.yaml")
	chartBytes, err := readBoundedHelmInput(chartFile, 1<<20, "Chart.yaml")
	if err != nil {
		return helmChartInitOptions{}, err
	}
	var metadata struct {
		APIVersion string `yaml:"apiVersion"`
		Name       string `yaml:"name"`
		Version    string `yaml:"version"`
		Type       string `yaml:"type"`
	}
	if err := yaml.Unmarshal(chartBytes, &metadata); err != nil {
		return helmChartInitOptions{}, fmt.Errorf("decode Chart.yaml: %w", err)
	}
	if metadata.APIVersion != "v2" {
		return helmChartInitOptions{}, fmt.Errorf("Chart.yaml apiVersion must be v2")
	}
	if metadata.Type == "library" {
		return helmChartInitOptions{}, fmt.Errorf("library Helm charts are not installable Product Releases")
	}
	if strings.TrimSpace(metadata.Name) == "" || strings.TrimSpace(metadata.Version) == "" {
		return helmChartInitOptions{}, fmt.Errorf("Chart.yaml requires name and version")
	}
	options.Name, options.Version = metadata.Name, metadata.Version
	options.ValuesPath = filepath.Join(options.ChartPath, "values.yaml")
	if _, err := os.Stat(options.ValuesPath); err != nil {
		return helmChartInitOptions{}, fmt.Errorf("inspect chart values.yaml: %w", err)
	}
	schema := filepath.Join(options.ChartPath, "values.schema.json")
	if _, err := os.Stat(schema); err == nil {
		options.SchemaPath = schema
	} else if !errors.Is(err, os.ErrNotExist) {
		return helmChartInitOptions{}, fmt.Errorf("inspect chart values.schema.json: %w", err)
	}
	return options, nil
}

func readBoundedHelmInput(path string, maximum int, label string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", label, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file", label)
	}
	if info.Size() > int64(maximum) {
		return nil, fmt.Errorf("%s exceeds %d bytes", label, maximum)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", label, err)
	}
	return contents, nil
}

func writeNewFile(path string, contents []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create %s without overwriting existing content: %w", path, err)
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

func readProductManifest(path string) (controlplane.ApolloProductManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return controlplane.ApolloProductManifest{}, fmt.Errorf("open Product manifest: %w", err)
	}
	defer file.Close()
	manifest, errs, err := controlplane.DecodeApolloProductManifest(file)
	if err != nil {
		return controlplane.ApolloProductManifest{}, err
	}
	if len(errs) > 0 {
		return controlplane.ApolloProductManifest{}, controlplane.ValidationErrors(errs)
	}
	return manifest, nil
}

func parseKeyValues(values []string) (map[string]string, error) {
	result := make(map[string]string, len(values))
	for _, value := range values {
		key, item, found := strings.Cut(value, "=")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			return nil, fmt.Errorf("variable %q must use key=value", value)
		}
		if _, duplicate := result[key]; duplicate {
			return nil, fmt.Errorf("variable %q is repeated", key)
		}
		result[key] = item
	}
	return result, nil
}

func splitOptionalVersion(coordinate string) (string, string, error) {
	parts := strings.Split(coordinate, ":")
	switch len(parts) {
	case 1:
		if parts[0] != "" {
			return parts[0], "", nil
		}
	case 2:
		if parts[0] == "module" && parts[1] != "" {
			return parts[1], "", nil
		}
		if parts[0] != "" && parts[1] != "" {
			return parts[0], parts[1], nil
		}
	case 3:
		if parts[0] == "module" && parts[1] != "" && parts[2] != "" {
			return parts[1], parts[2], nil
		}
	}
	return "", "", fmt.Errorf("Module must be name, name:version, module:name, or module:name:version")
}

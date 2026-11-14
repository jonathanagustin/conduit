package controlplane

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"go.yaml.in/yaml/v4"
)

// The Apollo Product Specification's Annotations page documents the Kubernetes
// label and annotations that identify a running workload back to the control
// plane. They are not written by a Product author into manifest.yml or
// configuration.yml: the deploying agent applies them, which is why they live
// here rather than in the Product Definition model.
//
// Platform already consumes one of these keys — the Agent's health monitor reads
// apollo.palantir.com/health.probe off a Pod to find its health endpoint — and
// already emitted the Entity ID in two places by hand-concatenating the string.
// This file is the single definition of the whole set, so a key cannot be
// spelled one way where it is produced and another where it is read.
//
// These are public documented key names. Applying them makes a workload legible
// to tooling that selects on them, including the NetworkPolicy pod selector the
// Health page documents. No private Apollo protocol is claimed.

// Apollo workload metadata keys.
const (
	// ApolloLivenessReadinessLabel marks a Pod whose liveness and readiness are
	// reported to the control plane. Its value is the empty string: presence is
	// the whole signal, which is what lets a NetworkPolicy select on it.
	ApolloLivenessReadinessLabel = "apollo.palantir.com/status.liveness_and_readiness"

	// ApolloHealthProbeAnnotation carries the JSON container-probe descriptor the
	// health monitor reads to find a replica's health endpoint.
	ApolloHealthProbeAnnotation = "apollo.palantir.com/health.probe"

	ApolloEnvironmentIDAnnotation  = "apollo.palantir.com/metadata.environment.id"
	ApolloEntityIDAnnotation       = "apollo.palantir.com/metadata.entity.id"
	ApolloProductNameAnnotation    = "apollo.palantir.com/metadata.product.name"
	ApolloProductGroupAnnotation   = "apollo.palantir.com/metadata.product.group"
	ApolloProductVersionAnnotation = "apollo.palantir.com/metadata.product.version"
)

// ApolloEntityIDMaximumLength bounds the Entity ID annotation. Kubernetes caps
// an annotation key at 253 characters but not its value; this bound is the
// documented one for this value, and exceeding it silently would produce an
// identifier that no longer round-trips.
const ApolloEntityIDMaximumLength = 255

// ApolloEntityTypeHelmChart is the Entity type Platform installs. It is a
// constant rather than a literal at the call sites because the type is part of
// the identifier's grammar, not a per-caller choice.
const ApolloEntityTypeHelmChart = "helm-chart"

// ApolloEntityID renders the documented Entity identifier,
// aeid:<environment>:<type>:<name>.
//
// It returns an error rather than a truncated string. A truncated identifier
// still looks like an Entity ID and would associate a workload with the wrong
// Entity, or with none, at whatever point something finally parsed it.
func ApolloEntityID(environment, entityType, name string) (string, error) {
	for label, value := range map[string]string{
		"environment": environment, "entity type": entityType, "entity name": name,
	} {
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("Apollo Entity ID requires a %s", label)
		}
		if strings.Contains(value, ":") {
			return "", fmt.Errorf("Apollo Entity ID %s must not contain a colon", label)
		}
	}
	id := "aeid:" + environment + ":" + entityType + ":" + name
	if len(id) > ApolloEntityIDMaximumLength {
		return "", fmt.Errorf("Apollo Entity ID %q exceeds %d characters", id, ApolloEntityIDMaximumLength)
	}
	return id, nil
}

// ApolloHelmChartEntityID is ApolloEntityID for the Entity type Platform
// installs.
func ApolloHelmChartEntityID(environment, name string) (string, error) {
	return ApolloEntityID(environment, ApolloEntityTypeHelmChart, name)
}

// ApolloWorkloadMetadata is the label and annotation set a deployed workload
// carries.
type ApolloWorkloadMetadata struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
}

// ApolloWorkloadMetadataFor builds the documented set for one installation.
//
// The Agent applies it through a Helm post-renderer rather than through chart
// values, so a Product author does not have to expose a labels hook, or know
// that Apollo metadata exists, for their workloads to be identifiable. The flag
// is spelled differently per Helm major version: an executable path on 3, a
// postrenderer/v1 plugin name on 4.
func ApolloWorkloadMetadataFor(environment, entityName, product, version string) (ApolloWorkloadMetadata, error) {
	entityID, err := ApolloHelmChartEntityID(environment, entityName)
	if err != nil {
		return ApolloWorkloadMetadata{}, err
	}
	group, name, found := strings.Cut(product, ":")
	if !found || strings.TrimSpace(group) == "" || strings.TrimSpace(name) == "" {
		return ApolloWorkloadMetadata{}, fmt.Errorf("product %q must be a group:name coordinate", product)
	}
	if strings.TrimSpace(version) == "" {
		return ApolloWorkloadMetadata{}, fmt.Errorf("installation %s requires a product version", entityName)
	}
	return ApolloWorkloadMetadata{
		Labels: map[string]string{ApolloLivenessReadinessLabel: ""},
		Annotations: map[string]string{
			ApolloEnvironmentIDAnnotation:  environment,
			ApolloEntityIDAnnotation:       entityID,
			ApolloProductGroupAnnotation:   group,
			ApolloProductNameAnnotation:    name,
			ApolloProductVersionAnnotation: version,
		},
	}, nil
}

// ApplyApolloWorkloadMetadata stamps the documented label and annotations onto
// rendered Kubernetes manifests.
//
// Every object carries the metadata annotations, so an operator looking at any
// resource can tell which Entity produced it. The liveness/readiness label goes
// on Pod templates as well, because that is what the documented NetworkPolicy
// selector and the status reporting path match on.
//
// A document this does not understand — a non-mapping, a comment-only chunk, an
// object with no kind — is passed through byte-for-byte rather than dropped. A
// post-renderer that silently discards part of a chart would be a far worse
// failure than one that under-annotates.
func ApplyApolloWorkloadMetadata(rendered []byte, metadata ApolloWorkloadMetadata) ([]byte, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(rendered))
	documents := make([]*yaml.Node, 0, 8)
	for {
		var document yaml.Node
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse rendered manifests: %w", err)
		}
		documents = append(documents, &document)
	}
	var out bytes.Buffer
	// One encoder for the whole stream: it is what writes the `---` separators
	// between documents. Encoding each document with its own encoder silently
	// concatenates them into a single invalid object.
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	for _, document := range documents {
		// A template that rendered to nothing leaves an empty document. It
		// carries no object to stamp and re-encoding it would fail, so it is
		// dropped -- which is what Helm itself does with an empty document.
		if len(document.Content) == 0 {
			continue
		}
		if len(document.Content) == 1 && document.Content[0].Kind == yaml.MappingNode {
			annotateApolloObject(document.Content[0], metadata)
		}
		if err := encoder.Encode(document); err != nil {
			return nil, fmt.Errorf("re-encode rendered manifests: %w", err)
		}
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("flush rendered manifests: %w", err)
	}
	return out.Bytes(), nil
}

// apolloPodTemplatePaths maps a workload kind to where its Pod template lives.
// Kinds outside this table still receive object annotations; they simply have no
// Pod template to label.
var apolloPodTemplatePaths = map[string][]string{
	"Deployment":            {"spec", "template"},
	"StatefulSet":           {"spec", "template"},
	"DaemonSet":             {"spec", "template"},
	"ReplicaSet":            {"spec", "template"},
	"ReplicationController": {"spec", "template"},
	"Job":                   {"spec", "template"},
	"CronJob":               {"spec", "jobTemplate", "spec", "template"},
}

func annotateApolloObject(object *yaml.Node, metadata ApolloWorkloadMetadata) {
	kind := apolloScalarAt(object, "kind")
	if kind == "" {
		return
	}
	objectMetadata := apolloEnsureMapping(object, "metadata")
	apolloMergeStrings(apolloEnsureMapping(objectMetadata, "annotations"), metadata.Annotations)
	// A bare Pod is its own template.
	if kind == "Pod" {
		apolloMergeStrings(apolloEnsureMapping(objectMetadata, "labels"), metadata.Labels)
		return
	}
	path, known := apolloPodTemplatePaths[kind]
	if !known {
		return
	}
	template := object
	for _, segment := range path {
		next := apolloMapValue(template, segment)
		if next == nil || next.Kind != yaml.MappingNode {
			return
		}
		template = next
	}
	templateMetadata := apolloEnsureMapping(template, "metadata")
	apolloMergeStrings(apolloEnsureMapping(templateMetadata, "labels"), metadata.Labels)
	apolloMergeStrings(apolloEnsureMapping(templateMetadata, "annotations"), metadata.Annotations)
}

func apolloMapValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	return nil
}

func apolloScalarAt(mapping *yaml.Node, key string) string {
	value := apolloMapValue(mapping, key)
	if value == nil || value.Kind != yaml.ScalarNode {
		return ""
	}
	return value.Value
}

// apolloEnsureMapping returns the mapping at key, creating it when absent. A
// key present but holding something other than a mapping is replaced, because a
// chart that wrote `annotations: null` still needs annotations.
func apolloEnsureMapping(parent *yaml.Node, key string) *yaml.Node {
	if existing := apolloMapValue(parent, key); existing != nil {
		if existing.Kind == yaml.MappingNode {
			return existing
		}
		existing.Kind = yaml.MappingNode
		existing.Tag = "!!map"
		existing.Value = ""
		existing.Content = nil
		return existing
	}
	created := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, created)
	return created
}

// apolloMergeStrings sets each pair, overwriting an existing key. Platform's
// values win over a chart's: these keys identify the installation, and a chart
// that hardcoded one would otherwise misreport which Entity it belongs to.
func apolloMergeStrings(mapping *yaml.Node, values map[string]string) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if existing := apolloMapValue(mapping, key); existing != nil {
			existing.Kind = yaml.ScalarNode
			existing.Tag = "!!str"
			existing.Style = 0
			existing.Content = nil
			existing.Value = values[key]
			continue
		}
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: values[key]})
	}
}

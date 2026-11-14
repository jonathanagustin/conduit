package controlplane

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func testWorkloadMetadata(t *testing.T) ApolloWorkloadMetadata {
	t.Helper()
	metadata, err := ApolloWorkloadMetadataFor("prod-eu", "catalog", "com.palantir:catalog", "1.2.3")
	if err != nil {
		t.Fatalf("workload metadata: %v", err)
	}
	return metadata
}

// decodeDocuments parses a stamped manifest stream back into generic maps so a
// test asserts on structure rather than on formatting.
func decodeDocuments(t *testing.T, rendered []byte) []map[string]any {
	t.Helper()
	decoder := yaml.NewDecoder(strings.NewReader(string(rendered)))
	documents := make([]map[string]any, 0, 4)
	for {
		var document map[string]any
		err := decoder.Decode(&document)
		if err != nil {
			break
		}
		documents = append(documents, document)
	}
	return documents
}

func nested(t *testing.T, document map[string]any, path ...string) map[string]any {
	t.Helper()
	current := document
	for _, segment := range path {
		next, ok := current[segment].(map[string]any)
		if !ok {
			t.Fatalf("path %s is absent or not a mapping in %v", strings.Join(path, "."), document)
		}
		current = next
	}
	return current
}

// TestWorkloadMetadataStampsObjectsAndPodTemplates is the core of the
// post-renderer: every object identifies its Entity, and Pod templates also
// carry the label the status path and NetworkPolicy selector match on.
func TestWorkloadMetadataStampsObjectsAndPodTemplates(t *testing.T) {
	stamped, err := ApplyApolloWorkloadMetadata([]byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: catalog
spec:
  template:
    metadata:
      labels:
        app: catalog
    spec:
      containers:
        - name: catalog
          image: registry.example/catalog:1.2.3
---
apiVersion: v1
kind: Service
metadata:
  name: catalog
spec:
  ports:
    - port: 8443
`), testWorkloadMetadata(t))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	documents := decodeDocuments(t, stamped)
	if len(documents) != 2 {
		t.Fatalf("expected 2 documents, got %d: %s", len(documents), stamped)
	}

	for _, document := range documents {
		annotations := nested(t, document, "metadata", "annotations")
		if annotations[ApolloEntityIDAnnotation] != "aeid:prod-eu:helm-chart:catalog" {
			t.Fatalf("object annotations = %v", annotations)
		}
		if annotations[ApolloProductVersionAnnotation] != "1.2.3" {
			t.Fatalf("version annotation = %v", annotations[ApolloProductVersionAnnotation])
		}
	}

	// The Pod template keeps what the chart wrote and gains the Apollo label.
	templateMetadata := nested(t, documents[0], "spec", "template", "metadata")
	labels, _ := templateMetadata["labels"].(map[string]any)
	if labels["app"] != "catalog" {
		t.Fatalf("the chart's own labels must survive, got %v", labels)
	}
	value, exists := labels[ApolloLivenessReadinessLabel]
	if !exists || value != "" {
		t.Fatalf("liveness/readiness label = %v exists=%v", value, exists)
	}
	templateAnnotations, _ := templateMetadata["annotations"].(map[string]any)
	if templateAnnotations[ApolloEnvironmentIDAnnotation] != "prod-eu" {
		t.Fatalf("template annotations = %v", templateAnnotations)
	}

	// A Service has no Pod template, so it must not gain the Pod label.
	serviceMetadata := nested(t, documents[1], "metadata")
	if serviceLabels, _ := serviceMetadata["labels"].(map[string]any); serviceLabels != nil {
		if _, exists := serviceLabels[ApolloLivenessReadinessLabel]; exists {
			t.Fatal("a Service must not carry the Pod liveness/readiness label")
		}
	}
}

// TestWorkloadMetadataPreservesContent is the property that matters most in a
// post-renderer: nothing a chart produced may be dropped.
func TestWorkloadMetadataPreservesContent(t *testing.T) {
	const chart = `apiVersion: v1
kind: ConfigMap
metadata:
  name: settings
data:
  nested.yml: |
    a: b
    c: d
  literal: "{{ not a substitution }}"
---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: sweeper
spec:
  schedule: "*/5 * * * *"
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: sweep
              image: registry.example/sweep:1.0.0
`
	stamped, err := ApplyApolloWorkloadMetadata([]byte(chart), testWorkloadMetadata(t))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	documents := decodeDocuments(t, stamped)
	if len(documents) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(documents))
	}
	data := nested(t, documents[0], "data")
	if data["nested.yml"] != "a: b\nc: d\n" {
		t.Fatalf("embedded document content changed: %q", data["nested.yml"])
	}
	if data["literal"] != "{{ not a substitution }}" {
		t.Fatalf("literal braces changed: %q", data["literal"])
	}
	// A CronJob's Pod template is nested one level deeper than a Deployment's.
	cronTemplate := nested(t, documents[1], "spec", "jobTemplate", "spec", "template", "metadata")
	labels, _ := cronTemplate["labels"].(map[string]any)
	if _, exists := labels[ApolloLivenessReadinessLabel]; !exists {
		t.Fatalf("CronJob Pod template labels = %v", labels)
	}
	if nested(t, documents[1], "spec")["schedule"] != "*/5 * * * *" {
		t.Fatal("the CronJob schedule must survive")
	}
}

// TestWorkloadMetadataHandlesAwkwardDocuments covers the shapes a real chart
// emits that a naive post-renderer corrupts or drops.
func TestWorkloadMetadataHandlesAwkwardDocuments(t *testing.T) {
	// A chart that emits `annotations:` with no value, plus an empty document
	// from a fully-disabled template, plus an object with no metadata at all.
	stamped, err := ApplyApolloWorkloadMetadata([]byte(`apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: store
  annotations:
spec:
  template:
    spec: {}
---
---
apiVersion: v1
kind: Namespace
`), testWorkloadMetadata(t))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	documents := decodeDocuments(t, stamped)
	if len(documents) == 0 {
		t.Fatal("documents were dropped")
	}
	annotations := nested(t, documents[0], "metadata", "annotations")
	if annotations[ApolloEntityIDAnnotation] != "aeid:prod-eu:helm-chart:catalog" {
		t.Fatalf("a null annotations block must be replaced, got %v", annotations)
	}
	// The StatefulSet's template had no metadata key at all; one must be created.
	templateLabels, _ := nested(t, documents[0], "spec", "template", "metadata")["labels"].(map[string]any)
	if _, exists := templateLabels[ApolloLivenessReadinessLabel]; !exists {
		t.Fatalf("template labels = %v", templateLabels)
	}
}

// TestWorkloadMetadataOverwritesConflictingValues holds the precedence rule: a
// chart that hardcoded one of these keys would otherwise misreport its Entity.
func TestWorkloadMetadataOverwritesConflictingValues(t *testing.T) {
	stamped, err := ApplyApolloWorkloadMetadata([]byte(`apiVersion: v1
kind: Pod
metadata:
  name: catalog
  annotations:
    apollo.palantir.com/metadata.entity.id: aeid:somewhere-else:helm-chart:other
    unrelated: keep-me
`), testWorkloadMetadata(t))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	annotations := nested(t, decodeDocuments(t, stamped)[0], "metadata", "annotations")
	if annotations[ApolloEntityIDAnnotation] != "aeid:prod-eu:helm-chart:catalog" {
		t.Fatalf("Platform's value must win, got %v", annotations[ApolloEntityIDAnnotation])
	}
	if annotations["unrelated"] != "keep-me" {
		t.Fatal("unrelated annotations must survive")
	}
	// A bare Pod is its own template and takes the label directly.
	labels, _ := nested(t, decodeDocuments(t, stamped)[0], "metadata")["labels"].(map[string]any)
	if _, exists := labels[ApolloLivenessReadinessLabel]; !exists {
		t.Fatalf("a Pod must carry the label directly, got %v", labels)
	}
}

// TestWorkloadMetadataRejectsUnparseableInput fails closed rather than emitting
// manifests it did not understand.
func TestWorkloadMetadataRejectsUnparseableInput(t *testing.T) {
	if _, err := ApplyApolloWorkloadMetadata(
		[]byte("kind: Deployment\n  bad: indentation\n"), testWorkloadMetadata(t)); err == nil {
		t.Fatal("unparseable manifests must be an error")
	}
}

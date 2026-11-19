package productmanifest

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Decode preserves the strict, single-document, size-limited structural boundary.
// It deliberately does not validate native release policy or resolve artifacts.
func Decode(reader io.Reader) (Manifest, error) {
	decoder := yaml.NewDecoder(io.LimitReader(reader, 1<<20))
	decoder.KnownFields(true)
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode Apollo Product manifest: %w", legacyDiagnostic(err))
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Manifest{}, errors.New("Apollo Product manifest must contain exactly one YAML document")
		}
		return Manifest{}, fmt.Errorf("decode trailing Apollo Product manifest data: %w", err)
	}
	return manifest, nil
}

// Moving a Go type must not change the existing YAML diagnostic contract.
// Rewrite only the terminal type label emitted by yaml, never user input.
// Keep the rich TypeError and all other wrapped errors available to errors.As/Is.
func legacyDiagnostic(err error) error {
	var typed *yaml.TypeError
	if !errors.As(err, &typed) {
		return err
	}
	result := &yaml.TypeError{Errors: append([]string(nil), typed.Errors...)}
	for i, message := range result.Errors {
		for current, legacy := range legacyTypeNames {
			for _, prefix := range []string{" in type ", " into "} {
				suffix := prefix + "productmanifest." + current
				if strings.HasSuffix(message, suffix) {
					result.Errors[i] = strings.TrimSuffix(message, suffix) + prefix + "controlplane." + legacy
				}
			}
		}
	}
	return result
}

var legacyTypeNames = map[string]string{
	"Manifest":           "ApolloProductManifest",
	"Extensions":         "ApolloProductManifestExtensions",
	"Volume":             "ApolloManifestVolume",
	"VolumeType":         "ApolloManifestVolumeType",
	"DurableVolume":      "ApolloDurableVolume",
	"ImagePrePullConfig": "ApolloImagePrePullConfig",
	"Artifact":           "ApolloManifestArtifact",
	"HelmChart":          "ApolloManifestHelmChart",
	"Dependency":         "ApolloManifestDependency",
	"Incompatibility":    "ApolloManifestIncompatibility",
}

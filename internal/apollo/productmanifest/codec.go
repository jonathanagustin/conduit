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
// Keep structured load errors and their underlying causes available to errors.As/Is.
func legacyDiagnostic(err error) error {
	var typed *yaml.LoadErrors
	if !errors.As(err, &typed) {
		return err
	}
	result := &yaml.LoadErrors{Errors: make([]*yaml.LoadError, len(typed.Errors))}
	for i, detail := range typed.Errors {
		copy := *detail
		result.Errors[i] = &copy
		message := detail.Message
		for current, legacy := range legacyTypeNames {
			for _, diagnostic := range []string{" in type ", " into "} {
				for _, wrapper := range []string{"", "*", "[]", "map[string]"} {
					prefix := diagnostic + wrapper
					suffix := prefix + "productmanifest." + current
					if strings.HasSuffix(message, suffix) {
						copy.Message = strings.TrimSuffix(message, suffix) + prefix + "controlplane." + legacy
					}
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

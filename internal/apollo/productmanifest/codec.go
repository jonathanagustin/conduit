package productmanifest

import (
	"errors"
	"fmt"
	"io"

	"github.com/jonathanagustin/conduit/internal/apollo/yamldiagnostics"
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

func legacyDiagnostic(err error) error {
	return yamldiagnostics.Rewrite(err, "productmanifest", "controlplane", legacyTypeNames)
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

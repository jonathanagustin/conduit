package productconfiguration

import (
	"errors"
	"fmt"
	"io"

	"github.com/jonathanagustin/conduit/internal/apollo/yamldiagnostics"
	"go.yaml.in/yaml/v4"
)

// Decode strictly decodes configuration.yml. Unknown
// top-level keys are preserved through the inline Other map for the same reason
// the Environment settings document preserves them: an unmodelled key must
// survive a round trip rather than be dropped by a validator that does not
// understand it yet.
func Decode(reader io.Reader) (Configuration, error) {
	if reader == nil {
		return Configuration{}, nil
	}
	decoder := yaml.NewDecoder(io.LimitReader(reader, 1<<20))
	decoder.KnownFields(true)
	var configuration Configuration
	if err := decoder.Decode(&configuration); err != nil {
		if errors.Is(err, io.EOF) {
			return Configuration{}, nil
		}
		return Configuration{}, fmt.Errorf("decode Apollo Product configuration: %w", yamldiagnostics.Rewrite(err, "productconfiguration", "controlplane", legacyTypeNames))
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Configuration{}, errors.New("Apollo Product configuration must contain exactly one YAML document")
		}
		return Configuration{}, fmt.Errorf("decode trailing Apollo Product configuration data: %w", err)
	}
	return configuration, nil
}

var legacyTypeNames = map[string]string{
	"Configuration":               "ApolloProductConfiguration",
	"ResourceRequirements":        "ApolloResourceRequirements",
	"ResourceQuantities":          "ApolloResourceQuantities",
	"Replication":                 "ApolloReplication",
	"VolumeConfiguration":         "ApolloVolumeConfiguration",
	"DurableStorageConfiguration": "ApolloDurableStorageConfiguration",
	"Endpoints":                   "ApolloEndpoints",
	"EndpointDefinition":          "ApolloEndpointDefinition",
	"ManagedFile":                 "ApolloManagedFile",
	"GeneratedSecret":             "ApolloGeneratedSecret",
	"Discovery":                   "ApolloDiscovery",
	"DiscoveryProduces":           "ApolloDiscoveryProduces",
	"DiscoveryConsumes":           "ApolloDiscoveryConsumes",
	"rawManagedFile":              "rawManagedFile",
}

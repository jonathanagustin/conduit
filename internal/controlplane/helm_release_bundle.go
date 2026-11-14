package controlplane

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"go.yaml.in/yaml/v4"
)

const (
	MaxHelmDefaultConfigBytes = 1 << 20
	MaxHelmConfigSchemaBytes  = 1 << 20
)

// HelmProductReleaseBundle is the Platform-native immutable publication input.
// It intentionally models semantic documents, not Apollo's private archive
// bytes. The Hub content-addresses the exact optional documents before binding
// their descriptors into the Product Release publication receipt.
type HelmProductReleaseBundle struct {
	Manifest      ApolloProductManifest `json:"manifest"`
	DefaultConfig []byte                `json:"defaultConfig,omitempty"`
	ConfigSchema  []byte                `json:"configSchema,omitempty"`
}

func ValidateHelmProductReleaseBundle(bundle HelmProductReleaseBundle) error {
	if _, err := ExecutableReleaseFromApolloManifest(bundle.Manifest); err != nil {
		return err
	}
	if len(bundle.DefaultConfig) > MaxHelmDefaultConfigBytes {
		return fmt.Errorf("default Helm configuration exceeds %d bytes", MaxHelmDefaultConfigBytes)
	}
	if len(bundle.ConfigSchema) > MaxHelmConfigSchemaBytes {
		return fmt.Errorf("Helm configuration schema exceeds %d bytes", MaxHelmConfigSchemaBytes)
	}
	if len(bundle.DefaultConfig) > 0 {
		decoder := yaml.NewDecoder(bytes.NewReader(bundle.DefaultConfig))
		var document map[string]any
		if err := decoder.Decode(&document); err != nil {
			return fmt.Errorf("decode default Helm configuration: %w", err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			if err == nil {
				return errors.New("default Helm configuration must contain exactly one YAML document")
			}
			return fmt.Errorf("decode trailing default Helm configuration: %w", err)
		}
	}
	if len(bundle.ConfigSchema) > 0 {
		var schema map[string]any
		decoder := json.NewDecoder(bytes.NewReader(bundle.ConfigSchema))
		if err := decoder.Decode(&schema); err != nil {
			return fmt.Errorf("decode Helm configuration schema: %w", err)
		}
		if decoder.Decode(&struct{}{}) != io.EOF {
			return errors.New("Helm configuration schema must contain exactly one JSON value")
		}
	}
	return nil
}

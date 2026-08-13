package foundryinterop

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

const (
	secretTypeEdgePipelineMagritteSource = "edgePipelineMagritteSource"
	secretTypeMarkings                   = "markings"
	secretTypeNamedCredential            = "namedCredential"
)

// ToNativeManaged maps the Platform-owned subset of the recovered Foundry
// installation contract into the delivery kernel. Product-specific resolved
// and preset shapes remain exact raw JSON.
func ToNativeManaged(source ApolloManagedInstallationConfigurationV0) (controlplane.ManagedInstallationConfiguration, error) {
	result := controlplane.ManagedInstallationConfiguration{
		CBACMarkings:       cloneSlice(source.CBACMarkings),
		InputMapping:       cloneRawMap[ResolvedBlockSetInputShape](source.InputMapping),
		PresetInputMapping: cloneRawMap[InputShapeResolver](source.PresetInputMapping),
		SecretInputMapping: make(map[string]controlplane.SecretResolvedInput, len(source.SecretInputMapping)),
	}
	for id, encoded := range source.SecretInputMapping {
		converted, err := toNativeSecret(json.RawMessage(encoded))
		if err != nil {
			return controlplane.ManagedInstallationConfiguration{}, fmt.Errorf("secretInputMapping[%s]: %w", id, err)
		}
		result.SecretInputMapping[id] = converted
	}
	return result, nil
}

func FromNativeManaged(source controlplane.ManagedInstallationConfiguration) (ApolloManagedInstallationConfigurationV0, error) {
	result := ApolloManagedInstallationConfigurationV0{
		CBACMarkings:       cloneSlice(source.CBACMarkings),
		InputMapping:       cloneToRecoveredMap[ResolvedBlockSetInputShape](source.InputMapping),
		PresetInputMapping: cloneToRecoveredMap[InputShapeResolver](source.PresetInputMapping),
		SecretInputMapping: make(map[string]SecretResolvedBlockSetInputShape, len(source.SecretInputMapping)),
	}
	for id, input := range source.SecretInputMapping {
		encoded, err := fromNativeSecret(input)
		if err != nil {
			return ApolloManagedInstallationConfigurationV0{}, fmt.Errorf("secretInputMapping[%s]: %w", id, err)
		}
		result.SecretInputMapping[id] = SecretResolvedBlockSetInputShape(encoded)
	}
	return result, nil
}

func ToNativeUniversal(source ApolloUniversalInstallationConfigurationV0) (controlplane.UniversalInstallationConfiguration, error) {
	managed, err := ToNativeManaged(source.ManagedConfig)
	if err != nil {
		return controlplane.UniversalInstallationConfiguration{}, err
	}
	return controlplane.UniversalInstallationConfiguration{
		EnrollmentAllowlist: cloneSlice(source.EnrollmentAllowlist),
		EnrollmentDenylist:  cloneSlice(source.EnrollmentDenylist),
		ManagedConfig:       managed,
		NamespaceDenylist:   cloneSlice(source.NamespaceDenylist),
	}, nil
}

func FromNativeUniversal(source controlplane.UniversalInstallationConfiguration) (ApolloUniversalInstallationConfigurationV0, error) {
	managed, err := FromNativeManaged(source.ManagedConfig)
	if err != nil {
		return ApolloUniversalInstallationConfigurationV0{}, err
	}
	return ApolloUniversalInstallationConfigurationV0{
		EnrollmentAllowlist: cloneSlice(source.EnrollmentAllowlist),
		EnrollmentDenylist:  cloneSlice(source.EnrollmentDenylist),
		ManagedConfig:       managed,
		NamespaceDenylist:   cloneSlice(source.NamespaceDenylist),
	}, nil
}

// ApplyManagedConfiguration makes the recovered configuration contract an
// executable input to Platform's native Entity model. The caller must still
// supply the immutable Product/Release coordinate and Environment-owned secret
// assignments; this adapter never invents either from opaque Foundry IDs.
func ApplyManagedConfiguration(
	entity *controlplane.Entity,
	source ApolloManagedInstallationConfigurationV0,
	requiredSecretKeys map[string][]string,
) error {
	if entity == nil {
		return errors.New("native Entity is required")
	}
	converted, err := ToNativeManaged(source)
	if err != nil {
		return err
	}
	entity.Configuration = &converted
	entity.Universal = nil
	return applySecretRequirements(entity, source.SecretInputMapping, requiredSecretKeys)
}

// ApplyUniversalConfiguration installs the recovered universal policy on a
// native Entity. Platform's normal desired-state validation and approval path
// remains authoritative after translation.
func ApplyUniversalConfiguration(
	entity *controlplane.Entity,
	source ApolloUniversalInstallationConfigurationV0,
	requiredSecretKeys map[string][]string,
) error {
	if entity == nil {
		return errors.New("native Entity is required")
	}
	converted, err := ToNativeUniversal(source)
	if err != nil {
		return err
	}
	entity.Configuration = nil
	entity.Universal = &converted
	return applySecretRequirements(entity, source.ManagedConfig.SecretInputMapping, requiredSecretKeys)
}

// AdaptEntityConfiguration implements controlplane.EntityConfigurationAdapter
// for the recovered Apollo V0 managed and universal JSON contracts.
func AdaptEntityConfiguration(
	mode string,
	payload json.RawMessage,
	requiredSecretKeys map[string][]string,
) (*controlplane.ManagedInstallationConfiguration, *controlplane.UniversalInstallationConfiguration, []controlplane.SecretRequirement, error) {
	entity := controlplane.Entity{}
	switch mode {
	case ManagedMode:
		var source ApolloManagedInstallationConfigurationV0
		if err := decodeExact(payload, &source); err != nil {
			return nil, nil, nil, err
		}
		if err := ApplyManagedConfiguration(&entity, source, requiredSecretKeys); err != nil {
			return nil, nil, nil, err
		}
	case UniversalMode:
		var source ApolloUniversalInstallationConfigurationV0
		if err := decodeExact(payload, &source); err != nil {
			return nil, nil, nil, err
		}
		if err := ApplyUniversalConfiguration(&entity, source, requiredSecretKeys); err != nil {
			return nil, nil, nil, err
		}
	default:
		return nil, nil, nil, fmt.Errorf("unsupported recovered configuration mode %q", mode)
	}
	return entity.Configuration, entity.Universal, cloneSlice(entity.SecretRequirements), nil
}

func decodeExact(payload json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode recovered configuration: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("recovered configuration must contain exactly one JSON document")
	}
	return nil
}

func applySecretRequirements(
	entity *controlplane.Entity,
	inputs map[string]SecretResolvedBlockSetInputShape,
	requiredSecretKeys map[string][]string,
) error {
	requirements := make([]controlplane.SecretRequirement, 0, len(inputs))
	for shapeID, encoded := range inputs {
		native, err := toNativeSecret(json.RawMessage(encoded))
		if err != nil {
			return fmt.Errorf("secretInputMapping[%s]: %w", shapeID, err)
		}
		secretName := ""
		switch native.Type {
		case secretTypeEdgePipelineMagritteSource:
			secretName = native.EdgePipelineMagritteSource.SecretName
		case secretTypeMarkings:
			secretName = native.Markings.SecretName
		case secretTypeNamedCredential:
			secretName = native.NamedCredential.SecretName
		}
		keys := cloneSlice(requiredSecretKeys[shapeID])
		if len(keys) == 0 {
			return fmt.Errorf("secretInputMapping[%s]: explicit required secret keys are required", shapeID)
		}
		requirements = append(requirements, controlplane.SecretRequirement{Name: shapeID, Secret: secretName, Keys: keys})
	}
	sort.Slice(requirements, func(i, j int) bool { return requirements[i].Name < requirements[j].Name })
	entity.SecretRequirements = requirements
	return nil
}

func toNativeSecret(encoded json.RawMessage) (controlplane.SecretResolvedInput, error) {
	var discriminator struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(encoded, &discriminator); err != nil {
		return controlplane.SecretResolvedInput{}, err
	}
	switch discriminator.Type {
	case secretTypeEdgePipelineMagritteSource:
		var value SecretResolvedBlockSetInputShapeEdgePipelineMagritteSource
		if err := json.Unmarshal(encoded, &value); err != nil {
			return controlplane.SecretResolvedInput{}, err
		}
		return controlplane.SecretResolvedInput{
			Type: secretTypeEdgePipelineMagritteSource,
			EdgePipelineMagritteSource: &controlplane.SecretResolvedEdgePipelineSourceInput{
				Config: cloneRaw(value.EdgePipelineMagritteSource.Config), SecretName: string(value.EdgePipelineMagritteSource.SecretName),
				TaskConfig: cloneRaw(value.EdgePipelineMagritteSource.TaskConfig),
			},
		}, nil
	case secretTypeMarkings:
		var value SecretResolvedBlockSetInputShapeMarkings
		if err := json.Unmarshal(encoded, &value); err != nil {
			return controlplane.SecretResolvedInput{}, err
		}
		return controlplane.SecretResolvedInput{Type: secretTypeMarkings, Markings: &controlplane.SecretAndKeyName{
			SecretName: string(value.Markings.SecretName), SecretKeyName: value.Markings.SecretKeyName,
		}}, nil
	case secretTypeNamedCredential:
		var value SecretResolvedBlockSetInputShapeNamedCredential
		if err := json.Unmarshal(encoded, &value); err != nil {
			return controlplane.SecretResolvedInput{}, err
		}
		return controlplane.SecretResolvedInput{Type: secretTypeNamedCredential, NamedCredential: &controlplane.SecretName{
			SecretName: string(value.NamedCredential.SecretName),
		}}, nil
	default:
		return controlplane.SecretResolvedInput{}, fmt.Errorf("unsupported recovered secret input type %q", discriminator.Type)
	}
}

func fromNativeSecret(source controlplane.SecretResolvedInput) (json.RawMessage, error) {
	var value any
	switch source.Type {
	case secretTypeEdgePipelineMagritteSource:
		if source.EdgePipelineMagritteSource == nil {
			return nil, errors.New("edgePipelineMagritteSource payload is required")
		}
		value = SecretResolvedBlockSetInputShapeEdgePipelineMagritteSource{
			Type: source.Type,
			EdgePipelineMagritteSource: SecretResolvedEdgePipelineMagritteSourceInputShape{
				Config: cloneRaw(source.EdgePipelineMagritteSource.Config), SecretName: ApolloSecretName(source.EdgePipelineMagritteSource.SecretName),
				TaskConfig: cloneRaw(source.EdgePipelineMagritteSource.TaskConfig),
			},
		}
	case secretTypeMarkings:
		if source.Markings == nil {
			return nil, errors.New("markings payload is required")
		}
		value = SecretResolvedBlockSetInputShapeMarkings{Type: source.Type, Markings: SecretResolvedMarkingsInputShape(ApolloSecretAndKeyName{
			SecretName: ApolloSecretName(source.Markings.SecretName), SecretKeyName: source.Markings.SecretKeyName,
		})}
	case secretTypeNamedCredential:
		if source.NamedCredential == nil {
			return nil, errors.New("namedCredential payload is required")
		}
		value = SecretResolvedBlockSetInputShapeNamedCredential{Type: source.Type, NamedCredential: SecretResolvedNamedCredentialInputShape{
			SecretName: ApolloSecretName(source.NamedCredential.SecretName),
		}}
	default:
		return nil, fmt.Errorf("unsupported native secret input type %q", source.Type)
	}
	return json.Marshal(value)
}

func cloneRaw(value json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), value...)
}

func cloneSlice[T any](source []T) []T {
	if source == nil {
		return nil
	}
	result := make([]T, len(source))
	copy(result, source)
	return result
}

func cloneRawMap[T ~[]byte](source map[string]T) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage, len(source))
	for key, value := range source {
		result[key] = append(json.RawMessage(nil), value...)
	}
	return result
}

func cloneToRecoveredMap[T ~[]byte](source map[string]json.RawMessage) map[string]T {
	result := make(map[string]T, len(source))
	for key, value := range source {
		result[key] = T(append(json.RawMessage(nil), value...))
	}
	return result
}

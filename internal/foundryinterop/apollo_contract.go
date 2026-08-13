// Package foundryinterop adapts a bounded, generated Foundry transport model
// to Platform's native delivery kernel. These aliases keep the adapter API
// stable while the source-derived wire types live in wire.
package foundryinterop

import (
	"encoding/json"

	"github.com/jonathanagustin/conduit/internal/foundryinterop/wire"
)

type ApolloEnvironmentID = wire.ApolloEnvironmentID
type ApolloSpaceID = wire.ApolloSpaceID
type ApolloFoundrySpaceIdentifier = wire.ApolloFoundrySpaceIdentifier
type ApolloEntityID = wire.ApolloEntityID
type ApolloArtifactURI = wire.ApolloArtifactURI
type ApolloHostURI = wire.ApolloHostURI
type ApolloPlanID = wire.ApolloPlanID
type ApolloPlanRID = wire.ApolloPlanRID
type ApolloPlanTaskRID = wire.ApolloPlanTaskRID
type ApolloSecretName = wire.ApolloSecretName

// ResolvedBlockSetInputShape and the resolver below are Product-specific
// external payloads, deliberately raw at the adapter boundary. Their bytes and
// future tagged variants remain intact.
type ResolvedBlockSetInputShape = json.RawMessage
type InputShapeResolver = json.RawMessage
type SecretResolvedBlockSetInputShape = wire.SecretResolvedBlockSetInputShape
type ApolloBlockSetPublishingStatus = wire.ApolloBlockSetPublishingStatus
type ApolloStackManagementConfig = wire.ApolloStackManagementConfig

type ApolloAgentSecretType = wire.ApolloAgentSecretType

const (
	ContractVersion                       = wire.ContractVersion
	AdapterID                             = wire.AdapterID
	ManagedMode                           = wire.ManagedMode
	UniversalMode                         = wire.UniversalMode
	ApolloAgentSecretTOFU                 = wire.ApolloAgentSecretTypeTOFUSECRET
	ApolloAgentSecretPrivateKeyPassphrase = wire.ApolloAgentSecretTypePRIVATEKEYPASSPHRASE
	ManagedStoreInstallEverything         = string(wire.ManagedStoreAccessLevelINSTALLEVERYTHING)
	ManagedStoreViewEverything            = string(wire.ManagedStoreAccessLevelVIEWEVERYTHING)
	ManagedStoreNone                      = string(wire.ManagedStoreAccessLevelNONE)
)

type ApolloAgentEntityInfo = wire.ApolloAgentEntityInfo
type ApolloAgentInfoUpdate = wire.ApolloAgentInfoUpdate
type ApolloBlockSetPublishingStatusFailure = wire.ApolloBlockSetPublishingStatusFailure
type ApolloBlockSetPublishingStatusInProgress = wire.ApolloBlockSetPublishingStatusInProgress
type ApolloBlockSetPublishingStatusSuccess = wire.ApolloBlockSetPublishingStatusSuccess
type BlockSetPublishingJobFailure = wire.BlockSetPublishingJobFailure
type BlockSetPublishingJobInProgress = wire.BlockSetPublishingJobInProgress
type BlockSetPublishingJobSuccess = wire.BlockSetPublishingJobSuccess
type ApolloCrossStackManifest = wire.ApolloCrossStackManifest
type ApolloEnvironment = wire.ApolloEnvironment
type ApolloGranularConfig = wire.ApolloGranularConfig
type ApolloManagedInstallationConfigurationV0 = wire.ApolloManagedInstallationConfigurationV0
type ApolloPublishError = wire.ApolloPublishError
type ApolloSecretAndKeyName = wire.ApolloSecretAndKeyName
type ApolloSpaceIdentifiers = wire.ApolloSpaceIdentifiers
type ApolloSpaceToPublishingStatus = wire.ApolloSpaceToPublishingStatus
type BatchGetApolloBlockSetVersionPublishingStatusResponse = wire.BatchGetApolloBlockSetVersionPublishingStatusResponse
type ApolloStackLevelConfig = wire.ApolloStackLevelConfig
type ApolloStackManagementConfigGranularConfig = wire.ApolloStackManagementConfigGranularConfig
type ApolloStackManagementConfigStackLevelConfig = wire.ApolloStackManagementConfigStackLevelConfig
type ApolloUniversalInstallationConfigurationV0 = wire.ApolloUniversalInstallationConfigurationV0
type SecretResolvedBlockSetInputShapeEdgePipelineMagritteSource = wire.SecretResolvedBlockSetInputShapeEdgePipelineMagritteSource
type SecretResolvedEdgePipelineMagritteSourceInputShape = wire.SecretResolvedEdgePipelineMagritteSourceInputShape
type SecretResolvedBlockSetInputShapeMarkings = wire.SecretResolvedBlockSetInputShapeMarkings
type SecretResolvedBlockSetInputShapeNamedCredential = wire.SecretResolvedBlockSetInputShapeNamedCredential
type SecretResolvedNamedCredentialInputShape = wire.SecretResolvedNamedCredentialInputShape
type SecretResolvedMarkingsInputShape = wire.SecretResolvedMarkingsInputShape
type CreateInstallationForBlockSetRequest = wire.CreateInstallationForBlockSetRequest
type FailedInstallationCreationRequest = wire.FailedInstallationCreationRequest
type FoundryProductsDownloaderStore = wire.FoundryProductsDownloaderStore
type ManageExistingInstallationRequest = wire.ManageExistingInstallationRequest
type ManagedInstallation = wire.ManagedInstallation
type ReleaseCreationErrorApolloPublishError = wire.ReleaseCreationErrorApolloPublishError
type UpdateInstallationForBlockSetRequest = wire.UpdateInstallationForBlockSetRequest

var StructuralTypeInventory = wire.StructuralTypeInventory

//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.12
// 	protoc        (unknown)
// source: conduit/environments/v1/environment_detail.proto

package environmentsv1

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	reflect "reflect"
	sync "sync"
	unsafe "unsafe"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type GetEnvironmentRequest struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// Literal native identity; do not replace it with a minted or inferred ID.
	Environment string `protobuf:"bytes,1,opt,name=environment,proto3" json:"environment,omitempty"`
	// Absence is unscoped. Present empty/invalid selection remains invalid.
	// Selection narrows existing resource access and grants none by itself.
	SpaceId       *string `protobuf:"bytes,2,opt,name=space_id,json=spaceId,proto3,oneof" json:"space_id,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *GetEnvironmentRequest) Reset() {
	*x = GetEnvironmentRequest{}
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *GetEnvironmentRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetEnvironmentRequest) ProtoMessage() {}

func (x *GetEnvironmentRequest) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetEnvironmentRequest.ProtoReflect.Descriptor instead.
func (*GetEnvironmentRequest) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_detail_proto_rawDescGZIP(), []int{0}
}

func (x *GetEnvironmentRequest) GetEnvironment() string {
	if x != nil {
		return x.Environment
	}
	return ""
}

func (x *GetEnvironmentRequest) GetSpaceId() string {
	if x != nil && x.SpaceId != nil {
		return *x.SpaceId
	}
	return ""
}

type GetEnvironmentResponse struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// Present on success; a missing Environment is a native not-found outcome.
	Record        *EnvironmentRecord `protobuf:"bytes,1,opt,name=record,proto3" json:"record,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *GetEnvironmentResponse) Reset() {
	*x = GetEnvironmentResponse{}
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *GetEnvironmentResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetEnvironmentResponse) ProtoMessage() {}

func (x *GetEnvironmentResponse) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetEnvironmentResponse.ProtoReflect.Descriptor instead.
func (*GetEnvironmentResponse) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_detail_proto_rawDescGZIP(), []int{1}
}

func (x *GetEnvironmentResponse) GetRecord() *EnvironmentRecord {
	if x != nil {
		return x.Record
	}
	return nil
}

// EnvironmentRecord carries the existing detail read, not a desired Entity list.
type EnvironmentRecord struct {
	state       protoimpl.MessageState `protogen:"open.v1"`
	Environment *Environment           `protobuf:"bytes,1,opt,name=environment,proto3" json:"environment,omitempty"`
	// Presence distinguishes no report from an observed empty report.
	Reported      *ReportedEnvironmentState `protobuf:"bytes,2,opt,name=reported,proto3" json:"reported,omitempty"`
	Spoke         *SpokeLiveness            `protobuf:"bytes,3,opt,name=spoke,proto3" json:"spoke,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *EnvironmentRecord) Reset() {
	*x = EnvironmentRecord{}
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *EnvironmentRecord) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*EnvironmentRecord) ProtoMessage() {}

func (x *EnvironmentRecord) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use EnvironmentRecord.ProtoReflect.Descriptor instead.
func (*EnvironmentRecord) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_detail_proto_rawDescGZIP(), []int{2}
}

func (x *EnvironmentRecord) GetEnvironment() *Environment {
	if x != nil {
		return x.Environment
	}
	return nil
}

func (x *EnvironmentRecord) GetReported() *ReportedEnvironmentState {
	if x != nil {
		return x.Reported
	}
	return nil
}

func (x *EnvironmentRecord) GetSpoke() *SpokeLiveness {
	if x != nil {
		return x.Spoke
	}
	return nil
}

// Environment is raw native configuration, not effective defaults or readiness.
type Environment struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	Id    string                 `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	// Opaque generation; never parse it as a numeric revision.
	Generation            string            `protobuf:"bytes,2,opt,name=generation,proto3" json:"generation,omitempty"`
	PackageArchitecture   string            `protobuf:"bytes,3,opt,name=package_architecture,json=packageArchitecture,proto3" json:"package_architecture,omitempty"`
	ConnectionSetting     string            `protobuf:"bytes,4,opt,name=connection_setting,json=connectionSetting,proto3" json:"connection_setting,omitempty"`
	DeclaredStateSource   string            `protobuf:"bytes,5,opt,name=declared_state_source,json=declaredStateSource,proto3" json:"declared_state_source,omitempty"`
	DefaultReleaseChannel string            `protobuf:"bytes,6,opt,name=default_release_channel,json=defaultReleaseChannel,proto3" json:"default_release_channel,omitempty"`
	Labels                map[string]string `protobuf:"bytes,7,rep,name=labels,proto3" json:"labels,omitempty" protobuf_key:"bytes,1,opt,name=key" protobuf_val:"bytes,2,opt,name=value"`
	// Exact JSON object bytes for the native arbitrary configuration map. Do not convert numbers through protobuf Struct or wrap the whole Environment as JSON.
	ConfigJson                 []byte                        `protobuf:"bytes,8,opt,name=config_json,json=configJson,proto3" json:"config_json,omitempty"`
	Namespaces                 []*NamespaceTarget            `protobuf:"bytes,9,rep,name=namespaces,proto3" json:"namespaces,omitempty"`
	ArtifactSecurityPolicy     *ArtifactSecurityPolicy       `protobuf:"bytes,10,opt,name=artifact_security_policy,json=artifactSecurityPolicy,proto3" json:"artifact_security_policy,omitempty"`
	ArtifactAvailabilityPolicy *ArtifactAvailabilityPolicy   `protobuf:"bytes,11,opt,name=artifact_availability_policy,json=artifactAvailabilityPolicy,proto3" json:"artifact_availability_policy,omitempty"`
	ArtifactRegistries         []*ArtifactRegistryAssignment `protobuf:"bytes,12,rep,name=artifact_registries,json=artifactRegistries,proto3" json:"artifact_registries,omitempty"`
	Secrets                    []*ExternalSecretAssignment   `protobuf:"bytes,13,rep,name=secrets,proto3" json:"secrets,omitempty"`
	AutomaticSecurityRecall    bool                          `protobuf:"varint,14,opt,name=automatic_security_recall,json=automaticSecurityRecall,proto3" json:"automatic_security_recall,omitempty"`
	VulnerabilityUpgrades      *VulnerabilityUpgradePolicy   `protobuf:"bytes,15,opt,name=vulnerability_upgrades,json=vulnerabilityUpgrades,proto3" json:"vulnerability_upgrades,omitempty"`
	AutoSuppressionThreshold   int64                         `protobuf:"varint,16,opt,name=auto_suppression_threshold,json=autoSuppressionThreshold,proto3" json:"auto_suppression_threshold,omitempty"`
	Maintenance                *MaintenancePolicy            `protobuf:"bytes,17,opt,name=maintenance,proto3" json:"maintenance,omitempty"`
	HelmExecution              *HelmExecutionDefaults        `protobuf:"bytes,18,opt,name=helm_execution,json=helmExecution,proto3" json:"helm_execution,omitempty"`
	unknownFields              protoimpl.UnknownFields
	sizeCache                  protoimpl.SizeCache
}

func (x *Environment) Reset() {
	*x = Environment{}
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[3]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Environment) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Environment) ProtoMessage() {}

func (x *Environment) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[3]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Environment.ProtoReflect.Descriptor instead.
func (*Environment) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_detail_proto_rawDescGZIP(), []int{3}
}

func (x *Environment) GetId() string {
	if x != nil {
		return x.Id
	}
	return ""
}

func (x *Environment) GetGeneration() string {
	if x != nil {
		return x.Generation
	}
	return ""
}

func (x *Environment) GetPackageArchitecture() string {
	if x != nil {
		return x.PackageArchitecture
	}
	return ""
}

func (x *Environment) GetConnectionSetting() string {
	if x != nil {
		return x.ConnectionSetting
	}
	return ""
}

func (x *Environment) GetDeclaredStateSource() string {
	if x != nil {
		return x.DeclaredStateSource
	}
	return ""
}

func (x *Environment) GetDefaultReleaseChannel() string {
	if x != nil {
		return x.DefaultReleaseChannel
	}
	return ""
}

func (x *Environment) GetLabels() map[string]string {
	if x != nil {
		return x.Labels
	}
	return nil
}

func (x *Environment) GetConfigJson() []byte {
	if x != nil {
		return x.ConfigJson
	}
	return nil
}

func (x *Environment) GetNamespaces() []*NamespaceTarget {
	if x != nil {
		return x.Namespaces
	}
	return nil
}

func (x *Environment) GetArtifactSecurityPolicy() *ArtifactSecurityPolicy {
	if x != nil {
		return x.ArtifactSecurityPolicy
	}
	return nil
}

func (x *Environment) GetArtifactAvailabilityPolicy() *ArtifactAvailabilityPolicy {
	if x != nil {
		return x.ArtifactAvailabilityPolicy
	}
	return nil
}

func (x *Environment) GetArtifactRegistries() []*ArtifactRegistryAssignment {
	if x != nil {
		return x.ArtifactRegistries
	}
	return nil
}

func (x *Environment) GetSecrets() []*ExternalSecretAssignment {
	if x != nil {
		return x.Secrets
	}
	return nil
}

func (x *Environment) GetAutomaticSecurityRecall() bool {
	if x != nil {
		return x.AutomaticSecurityRecall
	}
	return false
}

func (x *Environment) GetVulnerabilityUpgrades() *VulnerabilityUpgradePolicy {
	if x != nil {
		return x.VulnerabilityUpgrades
	}
	return nil
}

func (x *Environment) GetAutoSuppressionThreshold() int64 {
	if x != nil {
		return x.AutoSuppressionThreshold
	}
	return 0
}

func (x *Environment) GetMaintenance() *MaintenancePolicy {
	if x != nil {
		return x.Maintenance
	}
	return nil
}

func (x *Environment) GetHelmExecution() *HelmExecutionDefaults {
	if x != nil {
		return x.HelmExecution
	}
	return nil
}

// MaintenancePolicy retains configured windows and recorded overrides.
type MaintenancePolicy struct {
	state         protoimpl.MessageState   `protogen:"open.v1"`
	Downtime      *WeeklyMaintenanceWindow `protobuf:"bytes,1,opt,name=downtime,proto3" json:"downtime,omitempty"`
	NoDowntime    *WeeklyMaintenanceWindow `protobuf:"bytes,2,opt,name=no_downtime,json=noDowntime,proto3" json:"no_downtime,omitempty"`
	Overrides     []*MaintenanceOverride   `protobuf:"bytes,3,rep,name=overrides,proto3" json:"overrides,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *MaintenancePolicy) Reset() {
	*x = MaintenancePolicy{}
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[4]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *MaintenancePolicy) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*MaintenancePolicy) ProtoMessage() {}

func (x *MaintenancePolicy) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[4]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use MaintenancePolicy.ProtoReflect.Descriptor instead.
func (*MaintenancePolicy) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_detail_proto_rawDescGZIP(), []int{4}
}

func (x *MaintenancePolicy) GetDowntime() *WeeklyMaintenanceWindow {
	if x != nil {
		return x.Downtime
	}
	return nil
}

func (x *MaintenancePolicy) GetNoDowntime() *WeeklyMaintenanceWindow {
	if x != nil {
		return x.NoDowntime
	}
	return nil
}

func (x *MaintenancePolicy) GetOverrides() []*MaintenanceOverride {
	if x != nil {
		return x.Overrides
	}
	return nil
}

// ImagePrePullPolicy retains the native default when absent; present false is an explicit opt-out.
type ImagePrePullPolicy struct {
	state          protoimpl.MessageState `protogen:"open.v1"`
	Enabled        bool                   `protobuf:"varint,1,opt,name=enabled,proto3" json:"enabled,omitempty"`
	TimeoutSeconds int64                  `protobuf:"varint,2,opt,name=timeout_seconds,json=timeoutSeconds,proto3" json:"timeout_seconds,omitempty"`
	unknownFields  protoimpl.UnknownFields
	sizeCache      protoimpl.SizeCache
}

func (x *ImagePrePullPolicy) Reset() {
	*x = ImagePrePullPolicy{}
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[5]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ImagePrePullPolicy) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ImagePrePullPolicy) ProtoMessage() {}

func (x *ImagePrePullPolicy) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[5]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ImagePrePullPolicy.ProtoReflect.Descriptor instead.
func (*ImagePrePullPolicy) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_detail_proto_rawDescGZIP(), []int{5}
}

func (x *ImagePrePullPolicy) GetEnabled() bool {
	if x != nil {
		return x.Enabled
	}
	return false
}

func (x *ImagePrePullPolicy) GetTimeoutSeconds() int64 {
	if x != nil {
		return x.TimeoutSeconds
	}
	return 0
}

type HelmExecutionDefaults struct {
	state                                  protoimpl.MessageState `protogen:"open.v1"`
	DefaultRolloutStrategy                 string                 `protobuf:"bytes,1,opt,name=default_rollout_strategy,json=defaultRolloutStrategy,proto3" json:"default_rollout_strategy,omitempty"`
	InstallResourceReadinessTimeoutSeconds int64                  `protobuf:"varint,2,opt,name=install_resource_readiness_timeout_seconds,json=installResourceReadinessTimeoutSeconds,proto3" json:"install_resource_readiness_timeout_seconds,omitempty"`
	UpgradeResourceReadinessTimeoutSeconds int64                  `protobuf:"varint,3,opt,name=upgrade_resource_readiness_timeout_seconds,json=upgradeResourceReadinessTimeoutSeconds,proto3" json:"upgrade_resource_readiness_timeout_seconds,omitempty"`
	ImagePrePull                           *ImagePrePullPolicy    `protobuf:"bytes,4,opt,name=image_pre_pull,json=imagePrePull,proto3" json:"image_pre_pull,omitempty"`
	unknownFields                          protoimpl.UnknownFields
	sizeCache                              protoimpl.SizeCache
}

func (x *HelmExecutionDefaults) Reset() {
	*x = HelmExecutionDefaults{}
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[6]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *HelmExecutionDefaults) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*HelmExecutionDefaults) ProtoMessage() {}

func (x *HelmExecutionDefaults) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[6]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use HelmExecutionDefaults.ProtoReflect.Descriptor instead.
func (*HelmExecutionDefaults) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_detail_proto_rawDescGZIP(), []int{6}
}

func (x *HelmExecutionDefaults) GetDefaultRolloutStrategy() string {
	if x != nil {
		return x.DefaultRolloutStrategy
	}
	return ""
}

func (x *HelmExecutionDefaults) GetInstallResourceReadinessTimeoutSeconds() int64 {
	if x != nil {
		return x.InstallResourceReadinessTimeoutSeconds
	}
	return 0
}

func (x *HelmExecutionDefaults) GetUpgradeResourceReadinessTimeoutSeconds() int64 {
	if x != nil {
		return x.UpgradeResourceReadinessTimeoutSeconds
	}
	return 0
}

func (x *HelmExecutionDefaults) GetImagePrePull() *ImagePrePullPolicy {
	if x != nil {
		return x.ImagePrePull
	}
	return nil
}

// ArtifactSecurityPolicy retains raw scan policy values without resolving freshness defaults.
type ArtifactSecurityPolicy struct {
	state                 protoimpl.MessageState `protogen:"open.v1"`
	RequiredOutcome       string                 `protobuf:"bytes,1,opt,name=required_outcome,json=requiredOutcome,proto3" json:"required_outcome,omitempty"`
	MaximumScanAgeSeconds int64                  `protobuf:"varint,2,opt,name=maximum_scan_age_seconds,json=maximumScanAgeSeconds,proto3" json:"maximum_scan_age_seconds,omitempty"`
	unknownFields         protoimpl.UnknownFields
	sizeCache             protoimpl.SizeCache
}

func (x *ArtifactSecurityPolicy) Reset() {
	*x = ArtifactSecurityPolicy{}
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[7]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ArtifactSecurityPolicy) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ArtifactSecurityPolicy) ProtoMessage() {}

func (x *ArtifactSecurityPolicy) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[7]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ArtifactSecurityPolicy.ProtoReflect.Descriptor instead.
func (*ArtifactSecurityPolicy) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_detail_proto_rawDescGZIP(), []int{7}
}

func (x *ArtifactSecurityPolicy) GetRequiredOutcome() string {
	if x != nil {
		return x.RequiredOutcome
	}
	return ""
}

func (x *ArtifactSecurityPolicy) GetMaximumScanAgeSeconds() int64 {
	if x != nil {
		return x.MaximumScanAgeSeconds
	}
	return 0
}

type ArtifactAvailabilityPolicy struct {
	state           protoimpl.MessageState `protogen:"open.v1"`
	RequiredOutcome string                 `protobuf:"bytes,1,opt,name=required_outcome,json=requiredOutcome,proto3" json:"required_outcome,omitempty"`
	unknownFields   protoimpl.UnknownFields
	sizeCache       protoimpl.SizeCache
}

func (x *ArtifactAvailabilityPolicy) Reset() {
	*x = ArtifactAvailabilityPolicy{}
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[8]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ArtifactAvailabilityPolicy) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ArtifactAvailabilityPolicy) ProtoMessage() {}

func (x *ArtifactAvailabilityPolicy) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[8]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ArtifactAvailabilityPolicy.ProtoReflect.Descriptor instead.
func (*ArtifactAvailabilityPolicy) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_detail_proto_rawDescGZIP(), []int{8}
}

func (x *ArtifactAvailabilityPolicy) GetRequiredOutcome() string {
	if x != nil {
		return x.RequiredOutcome
	}
	return ""
}

type VulnerabilityUpgradePolicy struct {
	state                           protoimpl.MessageState `protogen:"open.v1"`
	AllowRecalledForVulnerabilities bool                   `protobuf:"varint,1,opt,name=allow_recalled_for_vulnerabilities,json=allowRecalledForVulnerabilities,proto3" json:"allow_recalled_for_vulnerabilities,omitempty"`
	RescanSlaSeconds                int64                  `protobuf:"varint,2,opt,name=rescan_sla_seconds,json=rescanSlaSeconds,proto3" json:"rescan_sla_seconds,omitempty"`
	unknownFields                   protoimpl.UnknownFields
	sizeCache                       protoimpl.SizeCache
}

func (x *VulnerabilityUpgradePolicy) Reset() {
	*x = VulnerabilityUpgradePolicy{}
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[9]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *VulnerabilityUpgradePolicy) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*VulnerabilityUpgradePolicy) ProtoMessage() {}

func (x *VulnerabilityUpgradePolicy) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[9]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use VulnerabilityUpgradePolicy.ProtoReflect.Descriptor instead.
func (*VulnerabilityUpgradePolicy) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_detail_proto_rawDescGZIP(), []int{9}
}

func (x *VulnerabilityUpgradePolicy) GetAllowRecalledForVulnerabilities() bool {
	if x != nil {
		return x.AllowRecalledForVulnerabilities
	}
	return false
}

func (x *VulnerabilityUpgradePolicy) GetRescanSlaSeconds() int64 {
	if x != nil {
		return x.RescanSlaSeconds
	}
	return 0
}

// ArtifactRegistryAssignment carries references and classification, not credential material.
type ArtifactRegistryAssignment struct {
	state               protoimpl.MessageState `protogen:"open.v1"`
	Id                  string                 `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	Url                 string                 `protobuf:"bytes,2,opt,name=url,proto3" json:"url,omitempty"`
	Type                string                 `protobuf:"bytes,3,opt,name=type,proto3" json:"type,omitempty"`
	Revision            int64                  `protobuf:"varint,4,opt,name=revision,proto3" json:"revision,omitempty"`
	Mirrors             []string               `protobuf:"bytes,5,rep,name=mirrors,proto3" json:"mirrors,omitempty"`
	Authentication      string                 `protobuf:"bytes,6,opt,name=authentication,proto3" json:"authentication,omitempty"`
	CredentialRef       string                 `protobuf:"bytes,7,opt,name=credential_ref,json=credentialRef,proto3" json:"credential_ref,omitempty"`
	ImagePullSecretName string                 `protobuf:"bytes,8,opt,name=image_pull_secret_name,json=imagePullSecretName,proto3" json:"image_pull_secret_name,omitempty"`
	unknownFields       protoimpl.UnknownFields
	sizeCache           protoimpl.SizeCache
}

func (x *ArtifactRegistryAssignment) Reset() {
	*x = ArtifactRegistryAssignment{}
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[10]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ArtifactRegistryAssignment) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ArtifactRegistryAssignment) ProtoMessage() {}

func (x *ArtifactRegistryAssignment) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[10]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ArtifactRegistryAssignment.ProtoReflect.Descriptor instead.
func (*ArtifactRegistryAssignment) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_detail_proto_rawDescGZIP(), []int{10}
}

func (x *ArtifactRegistryAssignment) GetId() string {
	if x != nil {
		return x.Id
	}
	return ""
}

func (x *ArtifactRegistryAssignment) GetUrl() string {
	if x != nil {
		return x.Url
	}
	return ""
}

func (x *ArtifactRegistryAssignment) GetType() string {
	if x != nil {
		return x.Type
	}
	return ""
}

func (x *ArtifactRegistryAssignment) GetRevision() int64 {
	if x != nil {
		return x.Revision
	}
	return 0
}

func (x *ArtifactRegistryAssignment) GetMirrors() []string {
	if x != nil {
		return x.Mirrors
	}
	return nil
}

func (x *ArtifactRegistryAssignment) GetAuthentication() string {
	if x != nil {
		return x.Authentication
	}
	return ""
}

func (x *ArtifactRegistryAssignment) GetCredentialRef() string {
	if x != nil {
		return x.CredentialRef
	}
	return ""
}

func (x *ArtifactRegistryAssignment) GetImagePullSecretName() string {
	if x != nil {
		return x.ImagePullSecretName
	}
	return ""
}

// ExternalSecretAssignment identifies Environment-side material; the Hub does not dereference it.
type ExternalSecretAssignment struct {
	state                protoimpl.MessageState `protogen:"open.v1"`
	Name                 string                 `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Revision             int64                  `protobuf:"varint,2,opt,name=revision,proto3" json:"revision,omitempty"`
	ProviderRef          string                 `protobuf:"bytes,3,opt,name=provider_ref,json=providerRef,proto3" json:"provider_ref,omitempty"`
	KubernetesSecretName string                 `protobuf:"bytes,4,opt,name=kubernetes_secret_name,json=kubernetesSecretName,proto3" json:"kubernetes_secret_name,omitempty"`
	Keys                 []string               `protobuf:"bytes,5,rep,name=keys,proto3" json:"keys,omitempty"`
	unknownFields        protoimpl.UnknownFields
	sizeCache            protoimpl.SizeCache
}

func (x *ExternalSecretAssignment) Reset() {
	*x = ExternalSecretAssignment{}
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[11]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ExternalSecretAssignment) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ExternalSecretAssignment) ProtoMessage() {}

func (x *ExternalSecretAssignment) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[11]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ExternalSecretAssignment.ProtoReflect.Descriptor instead.
func (*ExternalSecretAssignment) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_detail_proto_rawDescGZIP(), []int{11}
}

func (x *ExternalSecretAssignment) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

func (x *ExternalSecretAssignment) GetRevision() int64 {
	if x != nil {
		return x.Revision
	}
	return 0
}

func (x *ExternalSecretAssignment) GetProviderRef() string {
	if x != nil {
		return x.ProviderRef
	}
	return ""
}

func (x *ExternalSecretAssignment) GetKubernetesSecretName() string {
	if x != nil {
		return x.KubernetesSecretName
	}
	return ""
}

func (x *ExternalSecretAssignment) GetKeys() []string {
	if x != nil {
		return x.Keys
	}
	return nil
}

// NamespaceTarget retains native flags and installed version/configuration observations.
type NamespaceTarget struct {
	state       protoimpl.MessageState `protogen:"open.v1"`
	Rid         string                 `protobuf:"bytes,1,opt,name=rid,proto3" json:"rid,omitempty"`
	Name        string                 `protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
	Description string                 `protobuf:"bytes,3,opt,name=description,proto3" json:"description,omitempty"`
	// Presence retains the distinction between absent and explicit false.
	DeletionProtected  *bool                       `protobuf:"varint,4,opt,name=deletion_protected,json=deletionProtected,proto3,oneof" json:"deletion_protected,omitempty"`
	EnrollmentRid      string                      `protobuf:"bytes,5,opt,name=enrollment_rid,json=enrollmentRid,proto3" json:"enrollment_rid,omitempty"`
	Organizations      []*Organization             `protobuf:"bytes,6,rep,name=organizations,proto3" json:"organizations,omitempty"`
	Trashed            bool                        `protobuf:"varint,7,opt,name=trashed,proto3" json:"trashed,omitempty"`
	OntologyCountValid bool                        `protobuf:"varint,8,opt,name=ontology_count_valid,json=ontologyCountValid,proto3" json:"ontology_count_valid,omitempty"`
	Installed          map[string]*InstalledTarget `protobuf:"bytes,9,rep,name=installed,proto3" json:"installed,omitempty" protobuf_key:"bytes,1,opt,name=key" protobuf_val:"bytes,2,opt,name=value"`
	unknownFields      protoimpl.UnknownFields
	sizeCache          protoimpl.SizeCache
}

func (x *NamespaceTarget) Reset() {
	*x = NamespaceTarget{}
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[12]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *NamespaceTarget) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*NamespaceTarget) ProtoMessage() {}

func (x *NamespaceTarget) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[12]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use NamespaceTarget.ProtoReflect.Descriptor instead.
func (*NamespaceTarget) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_detail_proto_rawDescGZIP(), []int{12}
}

func (x *NamespaceTarget) GetRid() string {
	if x != nil {
		return x.Rid
	}
	return ""
}

func (x *NamespaceTarget) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

func (x *NamespaceTarget) GetDescription() string {
	if x != nil {
		return x.Description
	}
	return ""
}

func (x *NamespaceTarget) GetDeletionProtected() bool {
	if x != nil && x.DeletionProtected != nil {
		return *x.DeletionProtected
	}
	return false
}

func (x *NamespaceTarget) GetEnrollmentRid() string {
	if x != nil {
		return x.EnrollmentRid
	}
	return ""
}

func (x *NamespaceTarget) GetOrganizations() []*Organization {
	if x != nil {
		return x.Organizations
	}
	return nil
}

func (x *NamespaceTarget) GetTrashed() bool {
	if x != nil {
		return x.Trashed
	}
	return false
}

func (x *NamespaceTarget) GetOntologyCountValid() bool {
	if x != nil {
		return x.OntologyCountValid
	}
	return false
}

func (x *NamespaceTarget) GetInstalled() map[string]*InstalledTarget {
	if x != nil {
		return x.Installed
	}
	return nil
}

type Organization struct {
	state           protoimpl.MessageState `protogen:"open.v1"`
	DisplayName     string                 `protobuf:"bytes,1,opt,name=display_name,json=displayName,proto3" json:"display_name,omitempty"`
	PrimaryHostname string                 `protobuf:"bytes,2,opt,name=primary_hostname,json=primaryHostname,proto3" json:"primary_hostname,omitempty"`
	unknownFields   protoimpl.UnknownFields
	sizeCache       protoimpl.SizeCache
}

func (x *Organization) Reset() {
	*x = Organization{}
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[13]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Organization) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Organization) ProtoMessage() {}

func (x *Organization) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[13]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Organization.ProtoReflect.Descriptor instead.
func (*Organization) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_detail_proto_rawDescGZIP(), []int{13}
}

func (x *Organization) GetDisplayName() string {
	if x != nil {
		return x.DisplayName
	}
	return ""
}

func (x *Organization) GetPrimaryHostname() string {
	if x != nil {
		return x.PrimaryHostname
	}
	return ""
}

type InstalledTarget struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Version       string                 `protobuf:"bytes,1,opt,name=version,proto3" json:"version,omitempty"`
	ConfigDigest  string                 `protobuf:"bytes,2,opt,name=config_digest,json=configDigest,proto3" json:"config_digest,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *InstalledTarget) Reset() {
	*x = InstalledTarget{}
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[14]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *InstalledTarget) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*InstalledTarget) ProtoMessage() {}

func (x *InstalledTarget) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[14]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use InstalledTarget.ProtoReflect.Descriptor instead.
func (*InstalledTarget) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_detail_proto_rawDescGZIP(), []int{14}
}

func (x *InstalledTarget) GetVersion() string {
	if x != nil {
		return x.Version
	}
	return ""
}

func (x *InstalledTarget) GetConfigDigest() string {
	if x != nil {
		return x.ConfigDigest
	}
	return ""
}

type WeeklyMaintenanceWindow struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	TimeZone      string                 `protobuf:"bytes,1,opt,name=time_zone,json=timeZone,proto3" json:"time_zone,omitempty"`
	Intervals     []string               `protobuf:"bytes,2,rep,name=intervals,proto3" json:"intervals,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *WeeklyMaintenanceWindow) Reset() {
	*x = WeeklyMaintenanceWindow{}
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[15]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *WeeklyMaintenanceWindow) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*WeeklyMaintenanceWindow) ProtoMessage() {}

func (x *WeeklyMaintenanceWindow) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[15]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use WeeklyMaintenanceWindow.ProtoReflect.Descriptor instead.
func (*WeeklyMaintenanceWindow) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_detail_proto_rawDescGZIP(), []int{15}
}

func (x *WeeklyMaintenanceWindow) GetTimeZone() string {
	if x != nil {
		return x.TimeZone
	}
	return ""
}

func (x *WeeklyMaintenanceWindow) GetIntervals() []string {
	if x != nil {
		return x.Intervals
	}
	return nil
}

// MaintenanceOverride carries recorded review metadata, not approval authority.
type MaintenanceOverride struct {
	state          protoimpl.MessageState `protogen:"open.v1"`
	InstallationId string                 `protobuf:"bytes,1,opt,name=installation_id,json=installationId,proto3" json:"installation_id,omitempty"`
	Type           string                 `protobuf:"bytes,2,opt,name=type,proto3" json:"type,omitempty"`
	StartsAt       *timestamppb.Timestamp `protobuf:"bytes,3,opt,name=starts_at,json=startsAt,proto3" json:"starts_at,omitempty"`
	EndsAt         *timestamppb.Timestamp `protobuf:"bytes,4,opt,name=ends_at,json=endsAt,proto3" json:"ends_at,omitempty"`
	Reason         string                 `protobuf:"bytes,5,opt,name=reason,proto3" json:"reason,omitempty"`
	// Existing recorded reviewer metadata; never use it as the request principal.
	ApprovedBy    string `protobuf:"bytes,6,opt,name=approved_by,json=approvedBy,proto3" json:"approved_by,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *MaintenanceOverride) Reset() {
	*x = MaintenanceOverride{}
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[16]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *MaintenanceOverride) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*MaintenanceOverride) ProtoMessage() {}

func (x *MaintenanceOverride) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_detail_proto_msgTypes[16]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use MaintenanceOverride.ProtoReflect.Descriptor instead.
func (*MaintenanceOverride) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_detail_proto_rawDescGZIP(), []int{16}
}

func (x *MaintenanceOverride) GetInstallationId() string {
	if x != nil {
		return x.InstallationId
	}
	return ""
}

func (x *MaintenanceOverride) GetType() string {
	if x != nil {
		return x.Type
	}
	return ""
}

func (x *MaintenanceOverride) GetStartsAt() *timestamppb.Timestamp {
	if x != nil {
		return x.StartsAt
	}
	return nil
}

func (x *MaintenanceOverride) GetEndsAt() *timestamppb.Timestamp {
	if x != nil {
		return x.EndsAt
	}
	return nil
}

func (x *MaintenanceOverride) GetReason() string {
	if x != nil {
		return x.Reason
	}
	return ""
}

func (x *MaintenanceOverride) GetApprovedBy() string {
	if x != nil {
		return x.ApprovedBy
	}
	return ""
}

var File_conduit_environments_v1_environment_detail_proto protoreflect.FileDescriptor

const file_conduit_environments_v1_environment_detail_proto_rawDesc = "" +
	"\n" +
	"0conduit/environments/v1/environment_detail.proto\x12\x17conduit.environments.v1\x1a5conduit/environments/v1/environment_observation.proto\x1a1conduit/environments/v1/environment_summary.proto\x1a\x1fgoogle/protobuf/timestamp.proto\"f\n" +
	"\x15GetEnvironmentRequest\x12 \n" +
	"\venvironment\x18\x01 \x01(\tR\venvironment\x12\x1e\n" +
	"\bspace_id\x18\x02 \x01(\tH\x00R\aspaceId\x88\x01\x01B\v\n" +
	"\t_space_id\"\\\n" +
	"\x16GetEnvironmentResponse\x12B\n" +
	"\x06record\x18\x01 \x01(\v2*.conduit.environments.v1.EnvironmentRecordR\x06record\"\xe8\x01\n" +
	"\x11EnvironmentRecord\x12F\n" +
	"\venvironment\x18\x01 \x01(\v2$.conduit.environments.v1.EnvironmentR\venvironment\x12M\n" +
	"\breported\x18\x02 \x01(\v21.conduit.environments.v1.ReportedEnvironmentStateR\breported\x12<\n" +
	"\x05spoke\x18\x03 \x01(\v2&.conduit.environments.v1.SpokeLivenessR\x05spoke\"\x9b\n" +
	"\n" +
	"\vEnvironment\x12\x0e\n" +
	"\x02id\x18\x01 \x01(\tR\x02id\x12\x1e\n" +
	"\n" +
	"generation\x18\x02 \x01(\tR\n" +
	"generation\x121\n" +
	"\x14package_architecture\x18\x03 \x01(\tR\x13packageArchitecture\x12-\n" +
	"\x12connection_setting\x18\x04 \x01(\tR\x11connectionSetting\x122\n" +
	"\x15declared_state_source\x18\x05 \x01(\tR\x13declaredStateSource\x126\n" +
	"\x17default_release_channel\x18\x06 \x01(\tR\x15defaultReleaseChannel\x12H\n" +
	"\x06labels\x18\a \x03(\v20.conduit.environments.v1.Environment.LabelsEntryR\x06labels\x12\x1f\n" +
	"\vconfig_json\x18\b \x01(\fR\n" +
	"configJson\x12H\n" +
	"\n" +
	"namespaces\x18\t \x03(\v2(.conduit.environments.v1.NamespaceTargetR\n" +
	"namespaces\x12i\n" +
	"\x18artifact_security_policy\x18\n" +
	" \x01(\v2/.conduit.environments.v1.ArtifactSecurityPolicyR\x16artifactSecurityPolicy\x12u\n" +
	"\x1cartifact_availability_policy\x18\v \x01(\v23.conduit.environments.v1.ArtifactAvailabilityPolicyR\x1aartifactAvailabilityPolicy\x12d\n" +
	"\x13artifact_registries\x18\f \x03(\v23.conduit.environments.v1.ArtifactRegistryAssignmentR\x12artifactRegistries\x12K\n" +
	"\asecrets\x18\r \x03(\v21.conduit.environments.v1.ExternalSecretAssignmentR\asecrets\x12:\n" +
	"\x19automatic_security_recall\x18\x0e \x01(\bR\x17automaticSecurityRecall\x12j\n" +
	"\x16vulnerability_upgrades\x18\x0f \x01(\v23.conduit.environments.v1.VulnerabilityUpgradePolicyR\x15vulnerabilityUpgrades\x12<\n" +
	"\x1aauto_suppression_threshold\x18\x10 \x01(\x03R\x18autoSuppressionThreshold\x12L\n" +
	"\vmaintenance\x18\x11 \x01(\v2*.conduit.environments.v1.MaintenancePolicyR\vmaintenance\x12U\n" +
	"\x0ehelm_execution\x18\x12 \x01(\v2..conduit.environments.v1.HelmExecutionDefaultsR\rhelmExecution\x1a9\n" +
	"\vLabelsEntry\x12\x10\n" +
	"\x03key\x18\x01 \x01(\tR\x03key\x12\x14\n" +
	"\x05value\x18\x02 \x01(\tR\x05value:\x028\x01\"\x80\x02\n" +
	"\x11MaintenancePolicy\x12L\n" +
	"\bdowntime\x18\x01 \x01(\v20.conduit.environments.v1.WeeklyMaintenanceWindowR\bdowntime\x12Q\n" +
	"\vno_downtime\x18\x02 \x01(\v20.conduit.environments.v1.WeeklyMaintenanceWindowR\n" +
	"noDowntime\x12J\n" +
	"\toverrides\x18\x03 \x03(\v2,.conduit.environments.v1.MaintenanceOverrideR\toverrides\"W\n" +
	"\x12ImagePrePullPolicy\x12\x18\n" +
	"\aenabled\x18\x01 \x01(\bR\aenabled\x12'\n" +
	"\x0ftimeout_seconds\x18\x02 \x01(\x03R\x0etimeoutSeconds\"\xdc\x02\n" +
	"\x15HelmExecutionDefaults\x128\n" +
	"\x18default_rollout_strategy\x18\x01 \x01(\tR\x16defaultRolloutStrategy\x12Z\n" +
	"*install_resource_readiness_timeout_seconds\x18\x02 \x01(\x03R&installResourceReadinessTimeoutSeconds\x12Z\n" +
	"*upgrade_resource_readiness_timeout_seconds\x18\x03 \x01(\x03R&upgradeResourceReadinessTimeoutSeconds\x12Q\n" +
	"\x0eimage_pre_pull\x18\x04 \x01(\v2+.conduit.environments.v1.ImagePrePullPolicyR\fimagePrePull\"|\n" +
	"\x16ArtifactSecurityPolicy\x12)\n" +
	"\x10required_outcome\x18\x01 \x01(\tR\x0frequiredOutcome\x127\n" +
	"\x18maximum_scan_age_seconds\x18\x02 \x01(\x03R\x15maximumScanAgeSeconds\"G\n" +
	"\x1aArtifactAvailabilityPolicy\x12)\n" +
	"\x10required_outcome\x18\x01 \x01(\tR\x0frequiredOutcome\"\x97\x01\n" +
	"\x1aVulnerabilityUpgradePolicy\x12K\n" +
	"\"allow_recalled_for_vulnerabilities\x18\x01 \x01(\bR\x1fallowRecalledForVulnerabilities\x12,\n" +
	"\x12rescan_sla_seconds\x18\x02 \x01(\x03R\x10rescanSlaSeconds\"\x8c\x02\n" +
	"\x1aArtifactRegistryAssignment\x12\x0e\n" +
	"\x02id\x18\x01 \x01(\tR\x02id\x12\x10\n" +
	"\x03url\x18\x02 \x01(\tR\x03url\x12\x12\n" +
	"\x04type\x18\x03 \x01(\tR\x04type\x12\x1a\n" +
	"\brevision\x18\x04 \x01(\x03R\brevision\x12\x18\n" +
	"\amirrors\x18\x05 \x03(\tR\amirrors\x12&\n" +
	"\x0eauthentication\x18\x06 \x01(\tR\x0eauthentication\x12%\n" +
	"\x0ecredential_ref\x18\a \x01(\tR\rcredentialRef\x123\n" +
	"\x16image_pull_secret_name\x18\b \x01(\tR\x13imagePullSecretName\"\xb7\x01\n" +
	"\x18ExternalSecretAssignment\x12\x12\n" +
	"\x04name\x18\x01 \x01(\tR\x04name\x12\x1a\n" +
	"\brevision\x18\x02 \x01(\x03R\brevision\x12!\n" +
	"\fprovider_ref\x18\x03 \x01(\tR\vproviderRef\x124\n" +
	"\x16kubernetes_secret_name\x18\x04 \x01(\tR\x14kubernetesSecretName\x12\x12\n" +
	"\x04keys\x18\x05 \x03(\tR\x04keys\"\xa3\x04\n" +
	"\x0fNamespaceTarget\x12\x10\n" +
	"\x03rid\x18\x01 \x01(\tR\x03rid\x12\x12\n" +
	"\x04name\x18\x02 \x01(\tR\x04name\x12 \n" +
	"\vdescription\x18\x03 \x01(\tR\vdescription\x122\n" +
	"\x12deletion_protected\x18\x04 \x01(\bH\x00R\x11deletionProtected\x88\x01\x01\x12%\n" +
	"\x0eenrollment_rid\x18\x05 \x01(\tR\renrollmentRid\x12K\n" +
	"\rorganizations\x18\x06 \x03(\v2%.conduit.environments.v1.OrganizationR\rorganizations\x12\x18\n" +
	"\atrashed\x18\a \x01(\bR\atrashed\x120\n" +
	"\x14ontology_count_valid\x18\b \x01(\bR\x12ontologyCountValid\x12U\n" +
	"\tinstalled\x18\t \x03(\v27.conduit.environments.v1.NamespaceTarget.InstalledEntryR\tinstalled\x1af\n" +
	"\x0eInstalledEntry\x12\x10\n" +
	"\x03key\x18\x01 \x01(\tR\x03key\x12>\n" +
	"\x05value\x18\x02 \x01(\v2(.conduit.environments.v1.InstalledTargetR\x05value:\x028\x01B\x15\n" +
	"\x13_deletion_protected\"\\\n" +
	"\fOrganization\x12!\n" +
	"\fdisplay_name\x18\x01 \x01(\tR\vdisplayName\x12)\n" +
	"\x10primary_hostname\x18\x02 \x01(\tR\x0fprimaryHostname\"P\n" +
	"\x0fInstalledTarget\x12\x18\n" +
	"\aversion\x18\x01 \x01(\tR\aversion\x12#\n" +
	"\rconfig_digest\x18\x02 \x01(\tR\fconfigDigest\"T\n" +
	"\x17WeeklyMaintenanceWindow\x12\x1b\n" +
	"\ttime_zone\x18\x01 \x01(\tR\btimeZone\x12\x1c\n" +
	"\tintervals\x18\x02 \x03(\tR\tintervals\"\xf9\x01\n" +
	"\x13MaintenanceOverride\x12'\n" +
	"\x0finstallation_id\x18\x01 \x01(\tR\x0einstallationId\x12\x12\n" +
	"\x04type\x18\x02 \x01(\tR\x04type\x127\n" +
	"\tstarts_at\x18\x03 \x01(\v2\x1a.google.protobuf.TimestampR\bstartsAt\x123\n" +
	"\aends_at\x18\x04 \x01(\v2\x1a.google.protobuf.TimestampR\x06endsAt\x12\x16\n" +
	"\x06reason\x18\x05 \x01(\tR\x06reason\x12\x1f\n" +
	"\vapproved_by\x18\x06 \x01(\tR\n" +
	"approvedBy2\x92\x01\n" +
	"\x18EnvironmentDetailService\x12v\n" +
	"\x0eGetEnvironment\x12..conduit.environments.v1.GetEnvironmentRequest\x1a/.conduit.environments.v1.GetEnvironmentResponse\"\x03\x90\x02\x01BRZPgithub.com/jonathanagustin/conduit/gen/go/conduit/environments/v1;environmentsv1b\x06proto3"

var (
	file_conduit_environments_v1_environment_detail_proto_rawDescOnce sync.Once
	file_conduit_environments_v1_environment_detail_proto_rawDescData []byte
)

func file_conduit_environments_v1_environment_detail_proto_rawDescGZIP() []byte {
	file_conduit_environments_v1_environment_detail_proto_rawDescOnce.Do(func() {
		file_conduit_environments_v1_environment_detail_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_conduit_environments_v1_environment_detail_proto_rawDesc), len(file_conduit_environments_v1_environment_detail_proto_rawDesc)))
	})
	return file_conduit_environments_v1_environment_detail_proto_rawDescData
}

var file_conduit_environments_v1_environment_detail_proto_msgTypes = make([]protoimpl.MessageInfo, 19)
var file_conduit_environments_v1_environment_detail_proto_goTypes = []any{
	(*GetEnvironmentRequest)(nil),      // 0: conduit.environments.v1.GetEnvironmentRequest
	(*GetEnvironmentResponse)(nil),     // 1: conduit.environments.v1.GetEnvironmentResponse
	(*EnvironmentRecord)(nil),          // 2: conduit.environments.v1.EnvironmentRecord
	(*Environment)(nil),                // 3: conduit.environments.v1.Environment
	(*MaintenancePolicy)(nil),          // 4: conduit.environments.v1.MaintenancePolicy
	(*ImagePrePullPolicy)(nil),         // 5: conduit.environments.v1.ImagePrePullPolicy
	(*HelmExecutionDefaults)(nil),      // 6: conduit.environments.v1.HelmExecutionDefaults
	(*ArtifactSecurityPolicy)(nil),     // 7: conduit.environments.v1.ArtifactSecurityPolicy
	(*ArtifactAvailabilityPolicy)(nil), // 8: conduit.environments.v1.ArtifactAvailabilityPolicy
	(*VulnerabilityUpgradePolicy)(nil), // 9: conduit.environments.v1.VulnerabilityUpgradePolicy
	(*ArtifactRegistryAssignment)(nil), // 10: conduit.environments.v1.ArtifactRegistryAssignment
	(*ExternalSecretAssignment)(nil),   // 11: conduit.environments.v1.ExternalSecretAssignment
	(*NamespaceTarget)(nil),            // 12: conduit.environments.v1.NamespaceTarget
	(*Organization)(nil),               // 13: conduit.environments.v1.Organization
	(*InstalledTarget)(nil),            // 14: conduit.environments.v1.InstalledTarget
	(*WeeklyMaintenanceWindow)(nil),    // 15: conduit.environments.v1.WeeklyMaintenanceWindow
	(*MaintenanceOverride)(nil),        // 16: conduit.environments.v1.MaintenanceOverride
	nil,                                // 17: conduit.environments.v1.Environment.LabelsEntry
	nil,                                // 18: conduit.environments.v1.NamespaceTarget.InstalledEntry
	(*ReportedEnvironmentState)(nil),   // 19: conduit.environments.v1.ReportedEnvironmentState
	(*SpokeLiveness)(nil),              // 20: conduit.environments.v1.SpokeLiveness
	(*timestamppb.Timestamp)(nil),      // 21: google.protobuf.Timestamp
}
var file_conduit_environments_v1_environment_detail_proto_depIdxs = []int32{
	2,  // 0: conduit.environments.v1.GetEnvironmentResponse.record:type_name -> conduit.environments.v1.EnvironmentRecord
	3,  // 1: conduit.environments.v1.EnvironmentRecord.environment:type_name -> conduit.environments.v1.Environment
	19, // 2: conduit.environments.v1.EnvironmentRecord.reported:type_name -> conduit.environments.v1.ReportedEnvironmentState
	20, // 3: conduit.environments.v1.EnvironmentRecord.spoke:type_name -> conduit.environments.v1.SpokeLiveness
	17, // 4: conduit.environments.v1.Environment.labels:type_name -> conduit.environments.v1.Environment.LabelsEntry
	12, // 5: conduit.environments.v1.Environment.namespaces:type_name -> conduit.environments.v1.NamespaceTarget
	7,  // 6: conduit.environments.v1.Environment.artifact_security_policy:type_name -> conduit.environments.v1.ArtifactSecurityPolicy
	8,  // 7: conduit.environments.v1.Environment.artifact_availability_policy:type_name -> conduit.environments.v1.ArtifactAvailabilityPolicy
	10, // 8: conduit.environments.v1.Environment.artifact_registries:type_name -> conduit.environments.v1.ArtifactRegistryAssignment
	11, // 9: conduit.environments.v1.Environment.secrets:type_name -> conduit.environments.v1.ExternalSecretAssignment
	9,  // 10: conduit.environments.v1.Environment.vulnerability_upgrades:type_name -> conduit.environments.v1.VulnerabilityUpgradePolicy
	4,  // 11: conduit.environments.v1.Environment.maintenance:type_name -> conduit.environments.v1.MaintenancePolicy
	6,  // 12: conduit.environments.v1.Environment.helm_execution:type_name -> conduit.environments.v1.HelmExecutionDefaults
	15, // 13: conduit.environments.v1.MaintenancePolicy.downtime:type_name -> conduit.environments.v1.WeeklyMaintenanceWindow
	15, // 14: conduit.environments.v1.MaintenancePolicy.no_downtime:type_name -> conduit.environments.v1.WeeklyMaintenanceWindow
	16, // 15: conduit.environments.v1.MaintenancePolicy.overrides:type_name -> conduit.environments.v1.MaintenanceOverride
	5,  // 16: conduit.environments.v1.HelmExecutionDefaults.image_pre_pull:type_name -> conduit.environments.v1.ImagePrePullPolicy
	13, // 17: conduit.environments.v1.NamespaceTarget.organizations:type_name -> conduit.environments.v1.Organization
	18, // 18: conduit.environments.v1.NamespaceTarget.installed:type_name -> conduit.environments.v1.NamespaceTarget.InstalledEntry
	21, // 19: conduit.environments.v1.MaintenanceOverride.starts_at:type_name -> google.protobuf.Timestamp
	21, // 20: conduit.environments.v1.MaintenanceOverride.ends_at:type_name -> google.protobuf.Timestamp
	14, // 21: conduit.environments.v1.NamespaceTarget.InstalledEntry.value:type_name -> conduit.environments.v1.InstalledTarget
	0,  // 22: conduit.environments.v1.EnvironmentDetailService.GetEnvironment:input_type -> conduit.environments.v1.GetEnvironmentRequest
	1,  // 23: conduit.environments.v1.EnvironmentDetailService.GetEnvironment:output_type -> conduit.environments.v1.GetEnvironmentResponse
	23, // [23:24] is the sub-list for method output_type
	22, // [22:23] is the sub-list for method input_type
	22, // [22:22] is the sub-list for extension type_name
	22, // [22:22] is the sub-list for extension extendee
	0,  // [0:22] is the sub-list for field type_name
}

func init() { file_conduit_environments_v1_environment_detail_proto_init() }
func file_conduit_environments_v1_environment_detail_proto_init() {
	if File_conduit_environments_v1_environment_detail_proto != nil {
		return
	}
	file_conduit_environments_v1_environment_observation_proto_init()
	file_conduit_environments_v1_environment_summary_proto_init()
	file_conduit_environments_v1_environment_detail_proto_msgTypes[0].OneofWrappers = []any{}
	file_conduit_environments_v1_environment_detail_proto_msgTypes[12].OneofWrappers = []any{}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_conduit_environments_v1_environment_detail_proto_rawDesc), len(file_conduit_environments_v1_environment_detail_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   19,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_conduit_environments_v1_environment_detail_proto_goTypes,
		DependencyIndexes: file_conduit_environments_v1_environment_detail_proto_depIdxs,
		MessageInfos:      file_conduit_environments_v1_environment_detail_proto_msgTypes,
	}.Build()
	File_conduit_environments_v1_environment_detail_proto = out.File
	file_conduit_environments_v1_environment_detail_proto_goTypes = nil
	file_conduit_environments_v1_environment_detail_proto_depIdxs = nil
}

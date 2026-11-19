//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.12
// 	protoc        (unknown)
// source: conduit/environments/v1/environment_observation.proto

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

type GetEnvironmentReportedStateRequest struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Environment   string                 `protobuf:"bytes,1,opt,name=environment,proto3" json:"environment,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *GetEnvironmentReportedStateRequest) Reset() {
	*x = GetEnvironmentReportedStateRequest{}
	mi := &file_conduit_environments_v1_environment_observation_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *GetEnvironmentReportedStateRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetEnvironmentReportedStateRequest) ProtoMessage() {}

func (x *GetEnvironmentReportedStateRequest) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_observation_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetEnvironmentReportedStateRequest.ProtoReflect.Descriptor instead.
func (*GetEnvironmentReportedStateRequest) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_observation_proto_rawDescGZIP(), []int{0}
}

func (x *GetEnvironmentReportedStateRequest) GetEnvironment() string {
	if x != nil {
		return x.Environment
	}
	return ""
}

// ReportedEnvironmentState retains observed evidence. An absent timestamp means
// native zero/unknown time; no
// current timestamp is invented. Open status strings retain native vocabulary.
type ReportedEnvironmentState struct {
	state         protoimpl.MessageState     `protogen:"open.v1"`
	Environment   string                     `protobuf:"bytes,1,opt,name=environment,proto3" json:"environment,omitempty"`
	AgentId       string                     `protobuf:"bytes,2,opt,name=agent_id,json=agentId,proto3" json:"agent_id,omitempty"`
	PlanId        string                     `protobuf:"bytes,3,opt,name=plan_id,json=planId,proto3" json:"plan_id,omitempty"`
	ObservedAt    *timestamppb.Timestamp     `protobuf:"bytes,4,opt,name=observed_at,json=observedAt,proto3" json:"observed_at,omitempty"`
	Entities      []*ReportedEntity          `protobuf:"bytes,5,rep,name=entities,proto3" json:"entities,omitempty"`
	Secrets       []*ReportedSecretReference `protobuf:"bytes,6,rep,name=secrets,proto3" json:"secrets,omitempty"`
	Revision      uint64                     `protobuf:"varint,7,opt,name=revision,proto3" json:"revision,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ReportedEnvironmentState) Reset() {
	*x = ReportedEnvironmentState{}
	mi := &file_conduit_environments_v1_environment_observation_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ReportedEnvironmentState) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ReportedEnvironmentState) ProtoMessage() {}

func (x *ReportedEnvironmentState) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_observation_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ReportedEnvironmentState.ProtoReflect.Descriptor instead.
func (*ReportedEnvironmentState) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_observation_proto_rawDescGZIP(), []int{1}
}

func (x *ReportedEnvironmentState) GetEnvironment() string {
	if x != nil {
		return x.Environment
	}
	return ""
}

func (x *ReportedEnvironmentState) GetAgentId() string {
	if x != nil {
		return x.AgentId
	}
	return ""
}

func (x *ReportedEnvironmentState) GetPlanId() string {
	if x != nil {
		return x.PlanId
	}
	return ""
}

func (x *ReportedEnvironmentState) GetObservedAt() *timestamppb.Timestamp {
	if x != nil {
		return x.ObservedAt
	}
	return nil
}

func (x *ReportedEnvironmentState) GetEntities() []*ReportedEntity {
	if x != nil {
		return x.Entities
	}
	return nil
}

func (x *ReportedEnvironmentState) GetSecrets() []*ReportedSecretReference {
	if x != nil {
		return x.Secrets
	}
	return nil
}

func (x *ReportedEnvironmentState) GetRevision() uint64 {
	if x != nil {
		return x.Revision
	}
	return 0
}

type ReportedEntity struct {
	state          protoimpl.MessageState `protogen:"open.v1"`
	InstallationId string                 `protobuf:"bytes,1,opt,name=installation_id,json=installationId,proto3" json:"installation_id,omitempty"`
	Product        string                 `protobuf:"bytes,2,opt,name=product,proto3" json:"product,omitempty"`
	Version        string                 `protobuf:"bytes,3,opt,name=version,proto3" json:"version,omitempty"`
	ReleaseDigest  string                 `protobuf:"bytes,4,opt,name=release_digest,json=releaseDigest,proto3" json:"release_digest,omitempty"`
	Namespace      string                 `protobuf:"bytes,5,opt,name=namespace,proto3" json:"namespace,omitempty"`
	ConfigDigest   string                 `protobuf:"bytes,6,opt,name=config_digest,json=configDigest,proto3" json:"config_digest,omitempty"`
	Health         string                 `protobuf:"bytes,7,opt,name=health,proto3" json:"health,omitempty"`
	RolloutStatus  string                 `protobuf:"bytes,8,opt,name=rollout_status,json=rolloutStatus,proto3" json:"rollout_status,omitempty"`
	ConfigStatus   string                 `protobuf:"bytes,9,opt,name=config_status,json=configStatus,proto3" json:"config_status,omitempty"`
	Lifecycle      string                 `protobuf:"bytes,10,opt,name=lifecycle,proto3" json:"lifecycle,omitempty"`
	ObservedAt     *timestamppb.Timestamp `protobuf:"bytes,11,opt,name=observed_at,json=observedAt,proto3" json:"observed_at,omitempty"`
	// Presence distinguishes no health detail from an observed empty detail.
	HealthDetails *EntityHealthDetails `protobuf:"bytes,12,opt,name=health_details,json=healthDetails,proto3" json:"health_details,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ReportedEntity) Reset() {
	*x = ReportedEntity{}
	mi := &file_conduit_environments_v1_environment_observation_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ReportedEntity) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ReportedEntity) ProtoMessage() {}

func (x *ReportedEntity) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_observation_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ReportedEntity.ProtoReflect.Descriptor instead.
func (*ReportedEntity) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_observation_proto_rawDescGZIP(), []int{2}
}

func (x *ReportedEntity) GetInstallationId() string {
	if x != nil {
		return x.InstallationId
	}
	return ""
}

func (x *ReportedEntity) GetProduct() string {
	if x != nil {
		return x.Product
	}
	return ""
}

func (x *ReportedEntity) GetVersion() string {
	if x != nil {
		return x.Version
	}
	return ""
}

func (x *ReportedEntity) GetReleaseDigest() string {
	if x != nil {
		return x.ReleaseDigest
	}
	return ""
}

func (x *ReportedEntity) GetNamespace() string {
	if x != nil {
		return x.Namespace
	}
	return ""
}

func (x *ReportedEntity) GetConfigDigest() string {
	if x != nil {
		return x.ConfigDigest
	}
	return ""
}

func (x *ReportedEntity) GetHealth() string {
	if x != nil {
		return x.Health
	}
	return ""
}

func (x *ReportedEntity) GetRolloutStatus() string {
	if x != nil {
		return x.RolloutStatus
	}
	return ""
}

func (x *ReportedEntity) GetConfigStatus() string {
	if x != nil {
		return x.ConfigStatus
	}
	return ""
}

func (x *ReportedEntity) GetLifecycle() string {
	if x != nil {
		return x.Lifecycle
	}
	return ""
}

func (x *ReportedEntity) GetObservedAt() *timestamppb.Timestamp {
	if x != nil {
		return x.ObservedAt
	}
	return nil
}

func (x *ReportedEntity) GetHealthDetails() *EntityHealthDetails {
	if x != nil {
		return x.HealthDetails
	}
	return nil
}

type EntityHealthDetails struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Quorum        string                 `protobuf:"bytes,1,opt,name=quorum,proto3" json:"quorum,omitempty"`
	Nodes         []*EntityNodeHealth    `protobuf:"bytes,2,rep,name=nodes,proto3" json:"nodes,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *EntityHealthDetails) Reset() {
	*x = EntityHealthDetails{}
	mi := &file_conduit_environments_v1_environment_observation_proto_msgTypes[3]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *EntityHealthDetails) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*EntityHealthDetails) ProtoMessage() {}

func (x *EntityHealthDetails) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_observation_proto_msgTypes[3]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use EntityHealthDetails.ProtoReflect.Descriptor instead.
func (*EntityHealthDetails) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_observation_proto_rawDescGZIP(), []int{3}
}

func (x *EntityHealthDetails) GetQuorum() string {
	if x != nil {
		return x.Quorum
	}
	return ""
}

func (x *EntityHealthDetails) GetNodes() []*EntityNodeHealth {
	if x != nil {
		return x.Nodes
	}
	return nil
}

type EntityNodeHealth struct {
	state          protoimpl.MessageState   `protogen:"open.v1"`
	NodeId         string                   `protobuf:"bytes,1,opt,name=node_id,json=nodeId,proto3" json:"node_id,omitempty"`
	ProductVersion string                   `protobuf:"bytes,2,opt,name=product_version,json=productVersion,proto3" json:"product_version,omitempty"`
	Result         string                   `protobuf:"bytes,3,opt,name=result,proto3" json:"result,omitempty"`
	Containers     []*EntityContainerHealth `protobuf:"bytes,4,rep,name=containers,proto3" json:"containers,omitempty"`
	unknownFields  protoimpl.UnknownFields
	sizeCache      protoimpl.SizeCache
}

func (x *EntityNodeHealth) Reset() {
	*x = EntityNodeHealth{}
	mi := &file_conduit_environments_v1_environment_observation_proto_msgTypes[4]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *EntityNodeHealth) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*EntityNodeHealth) ProtoMessage() {}

func (x *EntityNodeHealth) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_observation_proto_msgTypes[4]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use EntityNodeHealth.ProtoReflect.Descriptor instead.
func (*EntityNodeHealth) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_observation_proto_rawDescGZIP(), []int{4}
}

func (x *EntityNodeHealth) GetNodeId() string {
	if x != nil {
		return x.NodeId
	}
	return ""
}

func (x *EntityNodeHealth) GetProductVersion() string {
	if x != nil {
		return x.ProductVersion
	}
	return ""
}

func (x *EntityNodeHealth) GetResult() string {
	if x != nil {
		return x.Result
	}
	return ""
}

func (x *EntityNodeHealth) GetContainers() []*EntityContainerHealth {
	if x != nil {
		return x.Containers
	}
	return nil
}

type EntityContainerHealth struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	ContainerId   string                 `protobuf:"bytes,1,opt,name=container_id,json=containerId,proto3" json:"container_id,omitempty"`
	Liveness      bool                   `protobuf:"varint,2,opt,name=liveness,proto3" json:"liveness,omitempty"`
	Readiness     bool                   `protobuf:"varint,3,opt,name=readiness,proto3" json:"readiness,omitempty"`
	Result        string                 `protobuf:"bytes,4,opt,name=result,proto3" json:"result,omitempty"`
	Checks        []*HealthCheck         `protobuf:"bytes,5,rep,name=checks,proto3" json:"checks,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *EntityContainerHealth) Reset() {
	*x = EntityContainerHealth{}
	mi := &file_conduit_environments_v1_environment_observation_proto_msgTypes[5]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *EntityContainerHealth) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*EntityContainerHealth) ProtoMessage() {}

func (x *EntityContainerHealth) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_observation_proto_msgTypes[5]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use EntityContainerHealth.ProtoReflect.Descriptor instead.
func (*EntityContainerHealth) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_observation_proto_rawDescGZIP(), []int{5}
}

func (x *EntityContainerHealth) GetContainerId() string {
	if x != nil {
		return x.ContainerId
	}
	return ""
}

func (x *EntityContainerHealth) GetLiveness() bool {
	if x != nil {
		return x.Liveness
	}
	return false
}

func (x *EntityContainerHealth) GetReadiness() bool {
	if x != nil {
		return x.Readiness
	}
	return false
}

func (x *EntityContainerHealth) GetResult() string {
	if x != nil {
		return x.Result
	}
	return ""
}

func (x *EntityContainerHealth) GetChecks() []*HealthCheck {
	if x != nil {
		return x.Checks
	}
	return nil
}

type HealthCheck struct {
	state   protoimpl.MessageState `protogen:"open.v1"`
	Id      string                 `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	Message string                 `protobuf:"bytes,2,opt,name=message,proto3" json:"message,omitempty"`
	Result  string                 `protobuf:"bytes,3,opt,name=result,proto3" json:"result,omitempty"`
	// JSON encoding of the native arbitrary params map, preserving numbers without
	// a protobuf Struct/double conversion. Native nil encodes as JSON null.
	ParamsJson    []byte `protobuf:"bytes,4,opt,name=params_json,json=paramsJson,proto3" json:"params_json,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *HealthCheck) Reset() {
	*x = HealthCheck{}
	mi := &file_conduit_environments_v1_environment_observation_proto_msgTypes[6]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *HealthCheck) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*HealthCheck) ProtoMessage() {}

func (x *HealthCheck) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_observation_proto_msgTypes[6]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use HealthCheck.ProtoReflect.Descriptor instead.
func (*HealthCheck) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_observation_proto_rawDescGZIP(), []int{6}
}

func (x *HealthCheck) GetId() string {
	if x != nil {
		return x.Id
	}
	return ""
}

func (x *HealthCheck) GetMessage() string {
	if x != nil {
		return x.Message
	}
	return ""
}

func (x *HealthCheck) GetResult() string {
	if x != nil {
		return x.Result
	}
	return ""
}

func (x *HealthCheck) GetParamsJson() []byte {
	if x != nil {
		return x.ParamsJson
	}
	return nil
}

// ReportedSecretReference carries metadata only: provider references and key names,
// not secret values. This is still private Environment inventory.
type ReportedSecretReference struct {
	state                protoimpl.MessageState `protogen:"open.v1"`
	Assignment           string                 `protobuf:"bytes,1,opt,name=assignment,proto3" json:"assignment,omitempty"`
	Namespace            string                 `protobuf:"bytes,2,opt,name=namespace,proto3" json:"namespace,omitempty"`
	Revision             int64                  `protobuf:"varint,3,opt,name=revision,proto3" json:"revision,omitempty"`
	ProviderRef          string                 `protobuf:"bytes,4,opt,name=provider_ref,json=providerRef,proto3" json:"provider_ref,omitempty"`
	KubernetesSecretName string                 `protobuf:"bytes,5,opt,name=kubernetes_secret_name,json=kubernetesSecretName,proto3" json:"kubernetes_secret_name,omitempty"`
	Keys                 []string               `protobuf:"bytes,6,rep,name=keys,proto3" json:"keys,omitempty"`
	unknownFields        protoimpl.UnknownFields
	sizeCache            protoimpl.SizeCache
}

func (x *ReportedSecretReference) Reset() {
	*x = ReportedSecretReference{}
	mi := &file_conduit_environments_v1_environment_observation_proto_msgTypes[7]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ReportedSecretReference) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ReportedSecretReference) ProtoMessage() {}

func (x *ReportedSecretReference) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_observation_proto_msgTypes[7]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ReportedSecretReference.ProtoReflect.Descriptor instead.
func (*ReportedSecretReference) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_observation_proto_rawDescGZIP(), []int{7}
}

func (x *ReportedSecretReference) GetAssignment() string {
	if x != nil {
		return x.Assignment
	}
	return ""
}

func (x *ReportedSecretReference) GetNamespace() string {
	if x != nil {
		return x.Namespace
	}
	return ""
}

func (x *ReportedSecretReference) GetRevision() int64 {
	if x != nil {
		return x.Revision
	}
	return 0
}

func (x *ReportedSecretReference) GetProviderRef() string {
	if x != nil {
		return x.ProviderRef
	}
	return ""
}

func (x *ReportedSecretReference) GetKubernetesSecretName() string {
	if x != nil {
		return x.KubernetesSecretName
	}
	return ""
}

func (x *ReportedSecretReference) GetKeys() []string {
	if x != nil {
		return x.Keys
	}
	return nil
}

type GetEnvironmentReportedStateResponse struct {
	state         protoimpl.MessageState    `protogen:"open.v1"`
	Reported      *ReportedEnvironmentState `protobuf:"bytes,1,opt,name=reported,proto3" json:"reported,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *GetEnvironmentReportedStateResponse) Reset() {
	*x = GetEnvironmentReportedStateResponse{}
	mi := &file_conduit_environments_v1_environment_observation_proto_msgTypes[8]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *GetEnvironmentReportedStateResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetEnvironmentReportedStateResponse) ProtoMessage() {}

func (x *GetEnvironmentReportedStateResponse) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_observation_proto_msgTypes[8]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetEnvironmentReportedStateResponse.ProtoReflect.Descriptor instead.
func (*GetEnvironmentReportedStateResponse) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_observation_proto_rawDescGZIP(), []int{8}
}

func (x *GetEnvironmentReportedStateResponse) GetReported() *ReportedEnvironmentState {
	if x != nil {
		return x.Reported
	}
	return nil
}

var File_conduit_environments_v1_environment_observation_proto protoreflect.FileDescriptor

const file_conduit_environments_v1_environment_observation_proto_rawDesc = "" +
	"\n" +
	"5conduit/environments/v1/environment_observation.proto\x12\x17conduit.environments.v1\x1a\x1fgoogle/protobuf/timestamp.proto\"F\n" +
	"\"GetEnvironmentReportedStateRequest\x12 \n" +
	"\venvironment\x18\x01 \x01(\tR\venvironment\"\xda\x02\n" +
	"\x18ReportedEnvironmentState\x12 \n" +
	"\venvironment\x18\x01 \x01(\tR\venvironment\x12\x19\n" +
	"\bagent_id\x18\x02 \x01(\tR\aagentId\x12\x17\n" +
	"\aplan_id\x18\x03 \x01(\tR\x06planId\x12;\n" +
	"\vobserved_at\x18\x04 \x01(\v2\x1a.google.protobuf.TimestampR\n" +
	"observedAt\x12C\n" +
	"\bentities\x18\x05 \x03(\v2'.conduit.environments.v1.ReportedEntityR\bentities\x12J\n" +
	"\asecrets\x18\x06 \x03(\v20.conduit.environments.v1.ReportedSecretReferenceR\asecrets\x12\x1a\n" +
	"\brevision\x18\a \x01(\x04R\brevision\"\xeb\x03\n" +
	"\x0eReportedEntity\x12'\n" +
	"\x0finstallation_id\x18\x01 \x01(\tR\x0einstallationId\x12\x18\n" +
	"\aproduct\x18\x02 \x01(\tR\aproduct\x12\x18\n" +
	"\aversion\x18\x03 \x01(\tR\aversion\x12%\n" +
	"\x0erelease_digest\x18\x04 \x01(\tR\rreleaseDigest\x12\x1c\n" +
	"\tnamespace\x18\x05 \x01(\tR\tnamespace\x12#\n" +
	"\rconfig_digest\x18\x06 \x01(\tR\fconfigDigest\x12\x16\n" +
	"\x06health\x18\a \x01(\tR\x06health\x12%\n" +
	"\x0erollout_status\x18\b \x01(\tR\rrolloutStatus\x12#\n" +
	"\rconfig_status\x18\t \x01(\tR\fconfigStatus\x12\x1c\n" +
	"\tlifecycle\x18\n" +
	" \x01(\tR\tlifecycle\x12;\n" +
	"\vobserved_at\x18\v \x01(\v2\x1a.google.protobuf.TimestampR\n" +
	"observedAt\x12S\n" +
	"\x0ehealth_details\x18\f \x01(\v2,.conduit.environments.v1.EntityHealthDetailsR\rhealthDetails\"n\n" +
	"\x13EntityHealthDetails\x12\x16\n" +
	"\x06quorum\x18\x01 \x01(\tR\x06quorum\x12?\n" +
	"\x05nodes\x18\x02 \x03(\v2).conduit.environments.v1.EntityNodeHealthR\x05nodes\"\xbc\x01\n" +
	"\x10EntityNodeHealth\x12\x17\n" +
	"\anode_id\x18\x01 \x01(\tR\x06nodeId\x12'\n" +
	"\x0fproduct_version\x18\x02 \x01(\tR\x0eproductVersion\x12\x16\n" +
	"\x06result\x18\x03 \x01(\tR\x06result\x12N\n" +
	"\n" +
	"containers\x18\x04 \x03(\v2..conduit.environments.v1.EntityContainerHealthR\n" +
	"containers\"\xca\x01\n" +
	"\x15EntityContainerHealth\x12!\n" +
	"\fcontainer_id\x18\x01 \x01(\tR\vcontainerId\x12\x1a\n" +
	"\bliveness\x18\x02 \x01(\bR\bliveness\x12\x1c\n" +
	"\treadiness\x18\x03 \x01(\bR\treadiness\x12\x16\n" +
	"\x06result\x18\x04 \x01(\tR\x06result\x12<\n" +
	"\x06checks\x18\x05 \x03(\v2$.conduit.environments.v1.HealthCheckR\x06checks\"p\n" +
	"\vHealthCheck\x12\x0e\n" +
	"\x02id\x18\x01 \x01(\tR\x02id\x12\x18\n" +
	"\amessage\x18\x02 \x01(\tR\amessage\x12\x16\n" +
	"\x06result\x18\x03 \x01(\tR\x06result\x12\x1f\n" +
	"\vparams_json\x18\x04 \x01(\fR\n" +
	"paramsJson\"\xe0\x01\n" +
	"\x17ReportedSecretReference\x12\x1e\n" +
	"\n" +
	"assignment\x18\x01 \x01(\tR\n" +
	"assignment\x12\x1c\n" +
	"\tnamespace\x18\x02 \x01(\tR\tnamespace\x12\x1a\n" +
	"\brevision\x18\x03 \x01(\x03R\brevision\x12!\n" +
	"\fprovider_ref\x18\x04 \x01(\tR\vproviderRef\x124\n" +
	"\x16kubernetes_secret_name\x18\x05 \x01(\tR\x14kubernetesSecretName\x12\x12\n" +
	"\x04keys\x18\x06 \x03(\tR\x04keys\"t\n" +
	"#GetEnvironmentReportedStateResponse\x12M\n" +
	"\breported\x18\x01 \x01(\v21.conduit.environments.v1.ReportedEnvironmentStateR\breported2\xbf\x01\n" +
	"\x1dEnvironmentObservationService\x12\x9d\x01\n" +
	"\x1bGetEnvironmentReportedState\x12;.conduit.environments.v1.GetEnvironmentReportedStateRequest\x1a<.conduit.environments.v1.GetEnvironmentReportedStateResponse\"\x03\x90\x02\x01BRZPgithub.com/jonathanagustin/conduit/gen/go/conduit/environments/v1;environmentsv1b\x06proto3"

var (
	file_conduit_environments_v1_environment_observation_proto_rawDescOnce sync.Once
	file_conduit_environments_v1_environment_observation_proto_rawDescData []byte
)

func file_conduit_environments_v1_environment_observation_proto_rawDescGZIP() []byte {
	file_conduit_environments_v1_environment_observation_proto_rawDescOnce.Do(func() {
		file_conduit_environments_v1_environment_observation_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_conduit_environments_v1_environment_observation_proto_rawDesc), len(file_conduit_environments_v1_environment_observation_proto_rawDesc)))
	})
	return file_conduit_environments_v1_environment_observation_proto_rawDescData
}

var file_conduit_environments_v1_environment_observation_proto_msgTypes = make([]protoimpl.MessageInfo, 9)
var file_conduit_environments_v1_environment_observation_proto_goTypes = []any{
	(*GetEnvironmentReportedStateRequest)(nil),  // 0: conduit.environments.v1.GetEnvironmentReportedStateRequest
	(*ReportedEnvironmentState)(nil),            // 1: conduit.environments.v1.ReportedEnvironmentState
	(*ReportedEntity)(nil),                      // 2: conduit.environments.v1.ReportedEntity
	(*EntityHealthDetails)(nil),                 // 3: conduit.environments.v1.EntityHealthDetails
	(*EntityNodeHealth)(nil),                    // 4: conduit.environments.v1.EntityNodeHealth
	(*EntityContainerHealth)(nil),               // 5: conduit.environments.v1.EntityContainerHealth
	(*HealthCheck)(nil),                         // 6: conduit.environments.v1.HealthCheck
	(*ReportedSecretReference)(nil),             // 7: conduit.environments.v1.ReportedSecretReference
	(*GetEnvironmentReportedStateResponse)(nil), // 8: conduit.environments.v1.GetEnvironmentReportedStateResponse
	(*timestamppb.Timestamp)(nil),               // 9: google.protobuf.Timestamp
}
var file_conduit_environments_v1_environment_observation_proto_depIdxs = []int32{
	9,  // 0: conduit.environments.v1.ReportedEnvironmentState.observed_at:type_name -> google.protobuf.Timestamp
	2,  // 1: conduit.environments.v1.ReportedEnvironmentState.entities:type_name -> conduit.environments.v1.ReportedEntity
	7,  // 2: conduit.environments.v1.ReportedEnvironmentState.secrets:type_name -> conduit.environments.v1.ReportedSecretReference
	9,  // 3: conduit.environments.v1.ReportedEntity.observed_at:type_name -> google.protobuf.Timestamp
	3,  // 4: conduit.environments.v1.ReportedEntity.health_details:type_name -> conduit.environments.v1.EntityHealthDetails
	4,  // 5: conduit.environments.v1.EntityHealthDetails.nodes:type_name -> conduit.environments.v1.EntityNodeHealth
	5,  // 6: conduit.environments.v1.EntityNodeHealth.containers:type_name -> conduit.environments.v1.EntityContainerHealth
	6,  // 7: conduit.environments.v1.EntityContainerHealth.checks:type_name -> conduit.environments.v1.HealthCheck
	1,  // 8: conduit.environments.v1.GetEnvironmentReportedStateResponse.reported:type_name -> conduit.environments.v1.ReportedEnvironmentState
	0,  // 9: conduit.environments.v1.EnvironmentObservationService.GetEnvironmentReportedState:input_type -> conduit.environments.v1.GetEnvironmentReportedStateRequest
	8,  // 10: conduit.environments.v1.EnvironmentObservationService.GetEnvironmentReportedState:output_type -> conduit.environments.v1.GetEnvironmentReportedStateResponse
	10, // [10:11] is the sub-list for method output_type
	9,  // [9:10] is the sub-list for method input_type
	9,  // [9:9] is the sub-list for extension type_name
	9,  // [9:9] is the sub-list for extension extendee
	0,  // [0:9] is the sub-list for field type_name
}

func init() { file_conduit_environments_v1_environment_observation_proto_init() }
func file_conduit_environments_v1_environment_observation_proto_init() {
	if File_conduit_environments_v1_environment_observation_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_conduit_environments_v1_environment_observation_proto_rawDesc), len(file_conduit_environments_v1_environment_observation_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   9,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_conduit_environments_v1_environment_observation_proto_goTypes,
		DependencyIndexes: file_conduit_environments_v1_environment_observation_proto_depIdxs,
		MessageInfos:      file_conduit_environments_v1_environment_observation_proto_msgTypes,
	}.Build()
	File_conduit_environments_v1_environment_observation_proto = out.File
	file_conduit_environments_v1_environment_observation_proto_goTypes = nil
	file_conduit_environments_v1_environment_observation_proto_depIdxs = nil
}

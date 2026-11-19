//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.12
// 	protoc        (unknown)
// source: conduit/fleet/v1/fleet.proto

package fleetv1

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

type ListFleetInstallationsRequest struct {
	state          protoimpl.MessageState `protogen:"open.v1"`
	Environment    string                 `protobuf:"bytes,1,opt,name=environment,proto3" json:"environment,omitempty"`
	InstallationId string                 `protobuf:"bytes,2,opt,name=installation_id,json=installationId,proto3" json:"installation_id,omitempty"`
	Product        string                 `protobuf:"bytes,3,opt,name=product,proto3" json:"product,omitempty"`
	Health         string                 `protobuf:"bytes,4,opt,name=health,proto3" json:"health,omitempty"`
	RolloutStatus  string                 `protobuf:"bytes,5,opt,name=rollout_status,json=rolloutStatus,proto3" json:"rollout_status,omitempty"`
	ConfigStatus   string                 `protobuf:"bytes,6,opt,name=config_status,json=configStatus,proto3" json:"config_status,omitempty"`
	Lifecycle      string                 `protobuf:"bytes,7,opt,name=lifecycle,proto3" json:"lifecycle,omitempty"`
	Search         string                 `protobuf:"bytes,8,opt,name=search,proto3" json:"search,omitempty"`
	Cursor         string                 `protobuf:"bytes,9,opt,name=cursor,proto3" json:"cursor,omitempty"`
	// Absent uses 100; explicit limits must be 1..200.
	Limit *int32 `protobuf:"varint,10,opt,name=limit,proto3,oneof" json:"limit,omitempty"`
	// Absent uses native reporting policy; explicit values must be 1..86400.
	MaxAgeSeconds *int32 `protobuf:"varint,11,opt,name=max_age_seconds,json=maxAgeSeconds,proto3,oneof" json:"max_age_seconds,omitempty"`
	// Readable membership narrows current Environment authority, never grants it.
	SpaceId       string `protobuf:"bytes,12,opt,name=space_id,json=spaceId,proto3" json:"space_id,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ListFleetInstallationsRequest) Reset() {
	*x = ListFleetInstallationsRequest{}
	mi := &file_conduit_fleet_v1_fleet_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListFleetInstallationsRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListFleetInstallationsRequest) ProtoMessage() {}

func (x *ListFleetInstallationsRequest) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_fleet_v1_fleet_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListFleetInstallationsRequest.ProtoReflect.Descriptor instead.
func (*ListFleetInstallationsRequest) Descriptor() ([]byte, []int) {
	return file_conduit_fleet_v1_fleet_proto_rawDescGZIP(), []int{0}
}

func (x *ListFleetInstallationsRequest) GetEnvironment() string {
	if x != nil {
		return x.Environment
	}
	return ""
}

func (x *ListFleetInstallationsRequest) GetInstallationId() string {
	if x != nil {
		return x.InstallationId
	}
	return ""
}

func (x *ListFleetInstallationsRequest) GetProduct() string {
	if x != nil {
		return x.Product
	}
	return ""
}

func (x *ListFleetInstallationsRequest) GetHealth() string {
	if x != nil {
		return x.Health
	}
	return ""
}

func (x *ListFleetInstallationsRequest) GetRolloutStatus() string {
	if x != nil {
		return x.RolloutStatus
	}
	return ""
}

func (x *ListFleetInstallationsRequest) GetConfigStatus() string {
	if x != nil {
		return x.ConfigStatus
	}
	return ""
}

func (x *ListFleetInstallationsRequest) GetLifecycle() string {
	if x != nil {
		return x.Lifecycle
	}
	return ""
}

func (x *ListFleetInstallationsRequest) GetSearch() string {
	if x != nil {
		return x.Search
	}
	return ""
}

func (x *ListFleetInstallationsRequest) GetCursor() string {
	if x != nil {
		return x.Cursor
	}
	return ""
}

func (x *ListFleetInstallationsRequest) GetLimit() int32 {
	if x != nil && x.Limit != nil {
		return *x.Limit
	}
	return 0
}

func (x *ListFleetInstallationsRequest) GetMaxAgeSeconds() int32 {
	if x != nil && x.MaxAgeSeconds != nil {
		return *x.MaxAgeSeconds
	}
	return 0
}

func (x *ListFleetInstallationsRequest) GetSpaceId() string {
	if x != nil {
		return x.SpaceId
	}
	return ""
}

// FleetInstallation is an observation/projection, never execution authority.
type FleetInstallation struct {
	state                 protoimpl.MessageState `protogen:"open.v1"`
	Environment           string                 `protobuf:"bytes,1,opt,name=environment,proto3" json:"environment,omitempty"`
	InstallationId        string                 `protobuf:"bytes,2,opt,name=installation_id,json=installationId,proto3" json:"installation_id,omitempty"`
	Product               string                 `protobuf:"bytes,3,opt,name=product,proto3" json:"product,omitempty"`
	Namespace             string                 `protobuf:"bytes,4,opt,name=namespace,proto3" json:"namespace,omitempty"`
	DesiredVersion        string                 `protobuf:"bytes,5,opt,name=desired_version,json=desiredVersion,proto3" json:"desired_version,omitempty"`
	ObservedVersion       string                 `protobuf:"bytes,6,opt,name=observed_version,json=observedVersion,proto3" json:"observed_version,omitempty"`
	DesiredReleaseDigest  string                 `protobuf:"bytes,7,opt,name=desired_release_digest,json=desiredReleaseDigest,proto3" json:"desired_release_digest,omitempty"`
	ObservedReleaseDigest string                 `protobuf:"bytes,8,opt,name=observed_release_digest,json=observedReleaseDigest,proto3" json:"observed_release_digest,omitempty"`
	DesiredConfigDigest   string                 `protobuf:"bytes,9,opt,name=desired_config_digest,json=desiredConfigDigest,proto3" json:"desired_config_digest,omitempty"`
	ObservedConfigDigest  string                 `protobuf:"bytes,10,opt,name=observed_config_digest,json=observedConfigDigest,proto3" json:"observed_config_digest,omitempty"`
	Health                string                 `protobuf:"bytes,11,opt,name=health,proto3" json:"health,omitempty"`
	RolloutStatus         string                 `protobuf:"bytes,12,opt,name=rollout_status,json=rolloutStatus,proto3" json:"rollout_status,omitempty"`
	ConfigStatus          string                 `protobuf:"bytes,13,opt,name=config_status,json=configStatus,proto3" json:"config_status,omitempty"`
	Lifecycle             string                 `protobuf:"bytes,14,opt,name=lifecycle,proto3" json:"lifecycle,omitempty"`
	// Missing means no recorded observation, never a fabricated current time.
	ObservedAt            *timestamppb.Timestamp `protobuf:"bytes,15,opt,name=observed_at,json=observedAt,proto3" json:"observed_at,omitempty"`
	ObservationAgeSeconds int64                  `protobuf:"varint,16,opt,name=observation_age_seconds,json=observationAgeSeconds,proto3" json:"observation_age_seconds,omitempty"`
	Stale                 bool                   `protobuf:"varint,17,opt,name=stale,proto3" json:"stale,omitempty"`
	unknownFields         protoimpl.UnknownFields
	sizeCache             protoimpl.SizeCache
}

func (x *FleetInstallation) Reset() {
	*x = FleetInstallation{}
	mi := &file_conduit_fleet_v1_fleet_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *FleetInstallation) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*FleetInstallation) ProtoMessage() {}

func (x *FleetInstallation) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_fleet_v1_fleet_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use FleetInstallation.ProtoReflect.Descriptor instead.
func (*FleetInstallation) Descriptor() ([]byte, []int) {
	return file_conduit_fleet_v1_fleet_proto_rawDescGZIP(), []int{1}
}

func (x *FleetInstallation) GetEnvironment() string {
	if x != nil {
		return x.Environment
	}
	return ""
}

func (x *FleetInstallation) GetInstallationId() string {
	if x != nil {
		return x.InstallationId
	}
	return ""
}

func (x *FleetInstallation) GetProduct() string {
	if x != nil {
		return x.Product
	}
	return ""
}

func (x *FleetInstallation) GetNamespace() string {
	if x != nil {
		return x.Namespace
	}
	return ""
}

func (x *FleetInstallation) GetDesiredVersion() string {
	if x != nil {
		return x.DesiredVersion
	}
	return ""
}

func (x *FleetInstallation) GetObservedVersion() string {
	if x != nil {
		return x.ObservedVersion
	}
	return ""
}

func (x *FleetInstallation) GetDesiredReleaseDigest() string {
	if x != nil {
		return x.DesiredReleaseDigest
	}
	return ""
}

func (x *FleetInstallation) GetObservedReleaseDigest() string {
	if x != nil {
		return x.ObservedReleaseDigest
	}
	return ""
}

func (x *FleetInstallation) GetDesiredConfigDigest() string {
	if x != nil {
		return x.DesiredConfigDigest
	}
	return ""
}

func (x *FleetInstallation) GetObservedConfigDigest() string {
	if x != nil {
		return x.ObservedConfigDigest
	}
	return ""
}

func (x *FleetInstallation) GetHealth() string {
	if x != nil {
		return x.Health
	}
	return ""
}

func (x *FleetInstallation) GetRolloutStatus() string {
	if x != nil {
		return x.RolloutStatus
	}
	return ""
}

func (x *FleetInstallation) GetConfigStatus() string {
	if x != nil {
		return x.ConfigStatus
	}
	return ""
}

func (x *FleetInstallation) GetLifecycle() string {
	if x != nil {
		return x.Lifecycle
	}
	return ""
}

func (x *FleetInstallation) GetObservedAt() *timestamppb.Timestamp {
	if x != nil {
		return x.ObservedAt
	}
	return nil
}

func (x *FleetInstallation) GetObservationAgeSeconds() int64 {
	if x != nil {
		return x.ObservationAgeSeconds
	}
	return 0
}

func (x *FleetInstallation) GetStale() bool {
	if x != nil {
		return x.Stale
	}
	return false
}

// FleetSummary counts all matching visible rows before pagination.
type FleetSummary struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Total         int64                  `protobuf:"varint,1,opt,name=total,proto3" json:"total,omitempty"`
	Health        map[string]int64       `protobuf:"bytes,2,rep,name=health,proto3" json:"health,omitempty" protobuf_key:"bytes,1,opt,name=key" protobuf_val:"varint,2,opt,name=value"`
	Rollout       map[string]int64       `protobuf:"bytes,3,rep,name=rollout,proto3" json:"rollout,omitempty" protobuf_key:"bytes,1,opt,name=key" protobuf_val:"varint,2,opt,name=value"`
	Config        map[string]int64       `protobuf:"bytes,4,rep,name=config,proto3" json:"config,omitempty" protobuf_key:"bytes,1,opt,name=key" protobuf_val:"varint,2,opt,name=value"`
	Lifecycle     map[string]int64       `protobuf:"bytes,5,rep,name=lifecycle,proto3" json:"lifecycle,omitempty" protobuf_key:"bytes,1,opt,name=key" protobuf_val:"varint,2,opt,name=value"`
	Stale         int64                  `protobuf:"varint,6,opt,name=stale,proto3" json:"stale,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *FleetSummary) Reset() {
	*x = FleetSummary{}
	mi := &file_conduit_fleet_v1_fleet_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *FleetSummary) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*FleetSummary) ProtoMessage() {}

func (x *FleetSummary) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_fleet_v1_fleet_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use FleetSummary.ProtoReflect.Descriptor instead.
func (*FleetSummary) Descriptor() ([]byte, []int) {
	return file_conduit_fleet_v1_fleet_proto_rawDescGZIP(), []int{2}
}

func (x *FleetSummary) GetTotal() int64 {
	if x != nil {
		return x.Total
	}
	return 0
}

func (x *FleetSummary) GetHealth() map[string]int64 {
	if x != nil {
		return x.Health
	}
	return nil
}

func (x *FleetSummary) GetRollout() map[string]int64 {
	if x != nil {
		return x.Rollout
	}
	return nil
}

func (x *FleetSummary) GetConfig() map[string]int64 {
	if x != nil {
		return x.Config
	}
	return nil
}

func (x *FleetSummary) GetLifecycle() map[string]int64 {
	if x != nil {
		return x.Lifecycle
	}
	return nil
}

func (x *FleetSummary) GetStale() int64 {
	if x != nil {
		return x.Stale
	}
	return 0
}

// FleetFacets retains native counterfactual counts: a dimension excludes its
// own filter, but never excludes authority or selected Space membership.
type FleetFacets struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Environment   map[string]int64       `protobuf:"bytes,1,rep,name=environment,proto3" json:"environment,omitempty" protobuf_key:"bytes,1,opt,name=key" protobuf_val:"varint,2,opt,name=value"`
	Product       map[string]int64       `protobuf:"bytes,2,rep,name=product,proto3" json:"product,omitempty" protobuf_key:"bytes,1,opt,name=key" protobuf_val:"varint,2,opt,name=value"`
	Health        map[string]int64       `protobuf:"bytes,3,rep,name=health,proto3" json:"health,omitempty" protobuf_key:"bytes,1,opt,name=key" protobuf_val:"varint,2,opt,name=value"`
	RolloutStatus map[string]int64       `protobuf:"bytes,4,rep,name=rollout_status,json=rolloutStatus,proto3" json:"rollout_status,omitempty" protobuf_key:"bytes,1,opt,name=key" protobuf_val:"varint,2,opt,name=value"`
	ConfigStatus  map[string]int64       `protobuf:"bytes,5,rep,name=config_status,json=configStatus,proto3" json:"config_status,omitempty" protobuf_key:"bytes,1,opt,name=key" protobuf_val:"varint,2,opt,name=value"`
	Lifecycle     map[string]int64       `protobuf:"bytes,6,rep,name=lifecycle,proto3" json:"lifecycle,omitempty" protobuf_key:"bytes,1,opt,name=key" protobuf_val:"varint,2,opt,name=value"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *FleetFacets) Reset() {
	*x = FleetFacets{}
	mi := &file_conduit_fleet_v1_fleet_proto_msgTypes[3]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *FleetFacets) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*FleetFacets) ProtoMessage() {}

func (x *FleetFacets) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_fleet_v1_fleet_proto_msgTypes[3]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use FleetFacets.ProtoReflect.Descriptor instead.
func (*FleetFacets) Descriptor() ([]byte, []int) {
	return file_conduit_fleet_v1_fleet_proto_rawDescGZIP(), []int{3}
}

func (x *FleetFacets) GetEnvironment() map[string]int64 {
	if x != nil {
		return x.Environment
	}
	return nil
}

func (x *FleetFacets) GetProduct() map[string]int64 {
	if x != nil {
		return x.Product
	}
	return nil
}

func (x *FleetFacets) GetHealth() map[string]int64 {
	if x != nil {
		return x.Health
	}
	return nil
}

func (x *FleetFacets) GetRolloutStatus() map[string]int64 {
	if x != nil {
		return x.RolloutStatus
	}
	return nil
}

func (x *FleetFacets) GetConfigStatus() map[string]int64 {
	if x != nil {
		return x.ConfigStatus
	}
	return nil
}

func (x *FleetFacets) GetLifecycle() map[string]int64 {
	if x != nil {
		return x.Lifecycle
	}
	return nil
}

// ListFleetInstallationsResponse maps only REST-visible native fields. Internal
// facet-contribution scaffolding is not exposed. Empty protobuf collections do
// not retain Go nil-versus-empty distinctions.
type ListFleetInstallationsResponse struct {
	state               protoimpl.MessageState `protogen:"open.v1"`
	GeneratedAt         *timestamppb.Timestamp `protobuf:"bytes,1,opt,name=generated_at,json=generatedAt,proto3" json:"generated_at,omitempty"`
	MaxResultAgeSeconds int64                  `protobuf:"varint,2,opt,name=max_result_age_seconds,json=maxResultAgeSeconds,proto3" json:"max_result_age_seconds,omitempty"`
	Installations       []*FleetInstallation   `protobuf:"bytes,3,rep,name=installations,proto3" json:"installations,omitempty"`
	Summary             *FleetSummary          `protobuf:"bytes,4,opt,name=summary,proto3" json:"summary,omitempty"`
	Facets              *FleetFacets           `protobuf:"bytes,5,opt,name=facets,proto3" json:"facets,omitempty"`
	PageSize            int32                  `protobuf:"varint,6,opt,name=page_size,json=pageSize,proto3" json:"page_size,omitempty"`
	Limit               int32                  `protobuf:"varint,7,opt,name=limit,proto3" json:"limit,omitempty"`
	NextCursor          string                 `protobuf:"bytes,8,opt,name=next_cursor,json=nextCursor,proto3" json:"next_cursor,omitempty"`
	PreviousCursor      string                 `protobuf:"bytes,9,opt,name=previous_cursor,json=previousCursor,proto3" json:"previous_cursor,omitempty"`
	HasPrevious         bool                   `protobuf:"varint,10,opt,name=has_previous,json=hasPrevious,proto3" json:"has_previous,omitempty"`
	unknownFields       protoimpl.UnknownFields
	sizeCache           protoimpl.SizeCache
}

func (x *ListFleetInstallationsResponse) Reset() {
	*x = ListFleetInstallationsResponse{}
	mi := &file_conduit_fleet_v1_fleet_proto_msgTypes[4]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListFleetInstallationsResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListFleetInstallationsResponse) ProtoMessage() {}

func (x *ListFleetInstallationsResponse) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_fleet_v1_fleet_proto_msgTypes[4]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListFleetInstallationsResponse.ProtoReflect.Descriptor instead.
func (*ListFleetInstallationsResponse) Descriptor() ([]byte, []int) {
	return file_conduit_fleet_v1_fleet_proto_rawDescGZIP(), []int{4}
}

func (x *ListFleetInstallationsResponse) GetGeneratedAt() *timestamppb.Timestamp {
	if x != nil {
		return x.GeneratedAt
	}
	return nil
}

func (x *ListFleetInstallationsResponse) GetMaxResultAgeSeconds() int64 {
	if x != nil {
		return x.MaxResultAgeSeconds
	}
	return 0
}

func (x *ListFleetInstallationsResponse) GetInstallations() []*FleetInstallation {
	if x != nil {
		return x.Installations
	}
	return nil
}

func (x *ListFleetInstallationsResponse) GetSummary() *FleetSummary {
	if x != nil {
		return x.Summary
	}
	return nil
}

func (x *ListFleetInstallationsResponse) GetFacets() *FleetFacets {
	if x != nil {
		return x.Facets
	}
	return nil
}

func (x *ListFleetInstallationsResponse) GetPageSize() int32 {
	if x != nil {
		return x.PageSize
	}
	return 0
}

func (x *ListFleetInstallationsResponse) GetLimit() int32 {
	if x != nil {
		return x.Limit
	}
	return 0
}

func (x *ListFleetInstallationsResponse) GetNextCursor() string {
	if x != nil {
		return x.NextCursor
	}
	return ""
}

func (x *ListFleetInstallationsResponse) GetPreviousCursor() string {
	if x != nil {
		return x.PreviousCursor
	}
	return ""
}

func (x *ListFleetInstallationsResponse) GetHasPrevious() bool {
	if x != nil {
		return x.HasPrevious
	}
	return false
}

var File_conduit_fleet_v1_fleet_proto protoreflect.FileDescriptor

const file_conduit_fleet_v1_fleet_proto_rawDesc = "" +
	"\n" +
	"\x1cconduit/fleet/v1/fleet.proto\x12\x10conduit.fleet.v1\x1a\x1fgoogle/protobuf/timestamp.proto\"\xb7\x03\n" +
	"\x1dListFleetInstallationsRequest\x12 \n" +
	"\venvironment\x18\x01 \x01(\tR\venvironment\x12'\n" +
	"\x0finstallation_id\x18\x02 \x01(\tR\x0einstallationId\x12\x18\n" +
	"\aproduct\x18\x03 \x01(\tR\aproduct\x12\x16\n" +
	"\x06health\x18\x04 \x01(\tR\x06health\x12%\n" +
	"\x0erollout_status\x18\x05 \x01(\tR\rrolloutStatus\x12#\n" +
	"\rconfig_status\x18\x06 \x01(\tR\fconfigStatus\x12\x1c\n" +
	"\tlifecycle\x18\a \x01(\tR\tlifecycle\x12\x16\n" +
	"\x06search\x18\b \x01(\tR\x06search\x12\x16\n" +
	"\x06cursor\x18\t \x01(\tR\x06cursor\x12\x19\n" +
	"\x05limit\x18\n" +
	" \x01(\x05H\x00R\x05limit\x88\x01\x01\x12+\n" +
	"\x0fmax_age_seconds\x18\v \x01(\x05H\x01R\rmaxAgeSeconds\x88\x01\x01\x12\x19\n" +
	"\bspace_id\x18\f \x01(\tR\aspaceIdB\b\n" +
	"\x06_limitB\x12\n" +
	"\x10_max_age_seconds\"\xcf\x05\n" +
	"\x11FleetInstallation\x12 \n" +
	"\venvironment\x18\x01 \x01(\tR\venvironment\x12'\n" +
	"\x0finstallation_id\x18\x02 \x01(\tR\x0einstallationId\x12\x18\n" +
	"\aproduct\x18\x03 \x01(\tR\aproduct\x12\x1c\n" +
	"\tnamespace\x18\x04 \x01(\tR\tnamespace\x12'\n" +
	"\x0fdesired_version\x18\x05 \x01(\tR\x0edesiredVersion\x12)\n" +
	"\x10observed_version\x18\x06 \x01(\tR\x0fobservedVersion\x124\n" +
	"\x16desired_release_digest\x18\a \x01(\tR\x14desiredReleaseDigest\x126\n" +
	"\x17observed_release_digest\x18\b \x01(\tR\x15observedReleaseDigest\x122\n" +
	"\x15desired_config_digest\x18\t \x01(\tR\x13desiredConfigDigest\x124\n" +
	"\x16observed_config_digest\x18\n" +
	" \x01(\tR\x14observedConfigDigest\x12\x16\n" +
	"\x06health\x18\v \x01(\tR\x06health\x12%\n" +
	"\x0erollout_status\x18\f \x01(\tR\rrolloutStatus\x12#\n" +
	"\rconfig_status\x18\r \x01(\tR\fconfigStatus\x12\x1c\n" +
	"\tlifecycle\x18\x0e \x01(\tR\tlifecycle\x12;\n" +
	"\vobserved_at\x18\x0f \x01(\v2\x1a.google.protobuf.TimestampR\n" +
	"observedAt\x126\n" +
	"\x17observation_age_seconds\x18\x10 \x01(\x03R\x15observationAgeSeconds\x12\x14\n" +
	"\x05stale\x18\x11 \x01(\bR\x05stale\"\xc6\x04\n" +
	"\fFleetSummary\x12\x14\n" +
	"\x05total\x18\x01 \x01(\x03R\x05total\x12B\n" +
	"\x06health\x18\x02 \x03(\v2*.conduit.fleet.v1.FleetSummary.HealthEntryR\x06health\x12E\n" +
	"\arollout\x18\x03 \x03(\v2+.conduit.fleet.v1.FleetSummary.RolloutEntryR\arollout\x12B\n" +
	"\x06config\x18\x04 \x03(\v2*.conduit.fleet.v1.FleetSummary.ConfigEntryR\x06config\x12K\n" +
	"\tlifecycle\x18\x05 \x03(\v2-.conduit.fleet.v1.FleetSummary.LifecycleEntryR\tlifecycle\x12\x14\n" +
	"\x05stale\x18\x06 \x01(\x03R\x05stale\x1a9\n" +
	"\vHealthEntry\x12\x10\n" +
	"\x03key\x18\x01 \x01(\tR\x03key\x12\x14\n" +
	"\x05value\x18\x02 \x01(\x03R\x05value:\x028\x01\x1a:\n" +
	"\fRolloutEntry\x12\x10\n" +
	"\x03key\x18\x01 \x01(\tR\x03key\x12\x14\n" +
	"\x05value\x18\x02 \x01(\x03R\x05value:\x028\x01\x1a9\n" +
	"\vConfigEntry\x12\x10\n" +
	"\x03key\x18\x01 \x01(\tR\x03key\x12\x14\n" +
	"\x05value\x18\x02 \x01(\x03R\x05value:\x028\x01\x1a<\n" +
	"\x0eLifecycleEntry\x12\x10\n" +
	"\x03key\x18\x01 \x01(\tR\x03key\x12\x14\n" +
	"\x05value\x18\x02 \x01(\x03R\x05value:\x028\x01\"\xdb\x06\n" +
	"\vFleetFacets\x12P\n" +
	"\venvironment\x18\x01 \x03(\v2..conduit.fleet.v1.FleetFacets.EnvironmentEntryR\venvironment\x12D\n" +
	"\aproduct\x18\x02 \x03(\v2*.conduit.fleet.v1.FleetFacets.ProductEntryR\aproduct\x12A\n" +
	"\x06health\x18\x03 \x03(\v2).conduit.fleet.v1.FleetFacets.HealthEntryR\x06health\x12W\n" +
	"\x0erollout_status\x18\x04 \x03(\v20.conduit.fleet.v1.FleetFacets.RolloutStatusEntryR\rrolloutStatus\x12T\n" +
	"\rconfig_status\x18\x05 \x03(\v2/.conduit.fleet.v1.FleetFacets.ConfigStatusEntryR\fconfigStatus\x12J\n" +
	"\tlifecycle\x18\x06 \x03(\v2,.conduit.fleet.v1.FleetFacets.LifecycleEntryR\tlifecycle\x1a>\n" +
	"\x10EnvironmentEntry\x12\x10\n" +
	"\x03key\x18\x01 \x01(\tR\x03key\x12\x14\n" +
	"\x05value\x18\x02 \x01(\x03R\x05value:\x028\x01\x1a:\n" +
	"\fProductEntry\x12\x10\n" +
	"\x03key\x18\x01 \x01(\tR\x03key\x12\x14\n" +
	"\x05value\x18\x02 \x01(\x03R\x05value:\x028\x01\x1a9\n" +
	"\vHealthEntry\x12\x10\n" +
	"\x03key\x18\x01 \x01(\tR\x03key\x12\x14\n" +
	"\x05value\x18\x02 \x01(\x03R\x05value:\x028\x01\x1a@\n" +
	"\x12RolloutStatusEntry\x12\x10\n" +
	"\x03key\x18\x01 \x01(\tR\x03key\x12\x14\n" +
	"\x05value\x18\x02 \x01(\x03R\x05value:\x028\x01\x1a?\n" +
	"\x11ConfigStatusEntry\x12\x10\n" +
	"\x03key\x18\x01 \x01(\tR\x03key\x12\x14\n" +
	"\x05value\x18\x02 \x01(\x03R\x05value:\x028\x01\x1a<\n" +
	"\x0eLifecycleEntry\x12\x10\n" +
	"\x03key\x18\x01 \x01(\tR\x03key\x12\x14\n" +
	"\x05value\x18\x02 \x01(\x03R\x05value:\x028\x01\"\xf0\x03\n" +
	"\x1eListFleetInstallationsResponse\x12=\n" +
	"\fgenerated_at\x18\x01 \x01(\v2\x1a.google.protobuf.TimestampR\vgeneratedAt\x123\n" +
	"\x16max_result_age_seconds\x18\x02 \x01(\x03R\x13maxResultAgeSeconds\x12I\n" +
	"\rinstallations\x18\x03 \x03(\v2#.conduit.fleet.v1.FleetInstallationR\rinstallations\x128\n" +
	"\asummary\x18\x04 \x01(\v2\x1e.conduit.fleet.v1.FleetSummaryR\asummary\x125\n" +
	"\x06facets\x18\x05 \x01(\v2\x1d.conduit.fleet.v1.FleetFacetsR\x06facets\x12\x1b\n" +
	"\tpage_size\x18\x06 \x01(\x05R\bpageSize\x12\x14\n" +
	"\x05limit\x18\a \x01(\x05R\x05limit\x12\x1f\n" +
	"\vnext_cursor\x18\b \x01(\tR\n" +
	"nextCursor\x12'\n" +
	"\x0fprevious_cursor\x18\t \x01(\tR\x0epreviousCursor\x12!\n" +
	"\fhas_previous\x18\n" +
	" \x01(\bR\vhasPrevious2\x95\x01\n" +
	"\x10FleetReadService\x12\x80\x01\n" +
	"\x16ListFleetInstallations\x12/.conduit.fleet.v1.ListFleetInstallationsRequest\x1a0.conduit.fleet.v1.ListFleetInstallationsResponse\"\x03\x90\x02\x01BDZBgithub.com/jonathanagustin/conduit/gen/go/conduit/fleet/v1;fleetv1b\x06proto3"

var (
	file_conduit_fleet_v1_fleet_proto_rawDescOnce sync.Once
	file_conduit_fleet_v1_fleet_proto_rawDescData []byte
)

func file_conduit_fleet_v1_fleet_proto_rawDescGZIP() []byte {
	file_conduit_fleet_v1_fleet_proto_rawDescOnce.Do(func() {
		file_conduit_fleet_v1_fleet_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_conduit_fleet_v1_fleet_proto_rawDesc), len(file_conduit_fleet_v1_fleet_proto_rawDesc)))
	})
	return file_conduit_fleet_v1_fleet_proto_rawDescData
}

var file_conduit_fleet_v1_fleet_proto_msgTypes = make([]protoimpl.MessageInfo, 15)
var file_conduit_fleet_v1_fleet_proto_goTypes = []any{
	(*ListFleetInstallationsRequest)(nil),  // 0: conduit.fleet.v1.ListFleetInstallationsRequest
	(*FleetInstallation)(nil),              // 1: conduit.fleet.v1.FleetInstallation
	(*FleetSummary)(nil),                   // 2: conduit.fleet.v1.FleetSummary
	(*FleetFacets)(nil),                    // 3: conduit.fleet.v1.FleetFacets
	(*ListFleetInstallationsResponse)(nil), // 4: conduit.fleet.v1.ListFleetInstallationsResponse
	nil,                                    // 5: conduit.fleet.v1.FleetSummary.HealthEntry
	nil,                                    // 6: conduit.fleet.v1.FleetSummary.RolloutEntry
	nil,                                    // 7: conduit.fleet.v1.FleetSummary.ConfigEntry
	nil,                                    // 8: conduit.fleet.v1.FleetSummary.LifecycleEntry
	nil,                                    // 9: conduit.fleet.v1.FleetFacets.EnvironmentEntry
	nil,                                    // 10: conduit.fleet.v1.FleetFacets.ProductEntry
	nil,                                    // 11: conduit.fleet.v1.FleetFacets.HealthEntry
	nil,                                    // 12: conduit.fleet.v1.FleetFacets.RolloutStatusEntry
	nil,                                    // 13: conduit.fleet.v1.FleetFacets.ConfigStatusEntry
	nil,                                    // 14: conduit.fleet.v1.FleetFacets.LifecycleEntry
	(*timestamppb.Timestamp)(nil),          // 15: google.protobuf.Timestamp
}
var file_conduit_fleet_v1_fleet_proto_depIdxs = []int32{
	15, // 0: conduit.fleet.v1.FleetInstallation.observed_at:type_name -> google.protobuf.Timestamp
	5,  // 1: conduit.fleet.v1.FleetSummary.health:type_name -> conduit.fleet.v1.FleetSummary.HealthEntry
	6,  // 2: conduit.fleet.v1.FleetSummary.rollout:type_name -> conduit.fleet.v1.FleetSummary.RolloutEntry
	7,  // 3: conduit.fleet.v1.FleetSummary.config:type_name -> conduit.fleet.v1.FleetSummary.ConfigEntry
	8,  // 4: conduit.fleet.v1.FleetSummary.lifecycle:type_name -> conduit.fleet.v1.FleetSummary.LifecycleEntry
	9,  // 5: conduit.fleet.v1.FleetFacets.environment:type_name -> conduit.fleet.v1.FleetFacets.EnvironmentEntry
	10, // 6: conduit.fleet.v1.FleetFacets.product:type_name -> conduit.fleet.v1.FleetFacets.ProductEntry
	11, // 7: conduit.fleet.v1.FleetFacets.health:type_name -> conduit.fleet.v1.FleetFacets.HealthEntry
	12, // 8: conduit.fleet.v1.FleetFacets.rollout_status:type_name -> conduit.fleet.v1.FleetFacets.RolloutStatusEntry
	13, // 9: conduit.fleet.v1.FleetFacets.config_status:type_name -> conduit.fleet.v1.FleetFacets.ConfigStatusEntry
	14, // 10: conduit.fleet.v1.FleetFacets.lifecycle:type_name -> conduit.fleet.v1.FleetFacets.LifecycleEntry
	15, // 11: conduit.fleet.v1.ListFleetInstallationsResponse.generated_at:type_name -> google.protobuf.Timestamp
	1,  // 12: conduit.fleet.v1.ListFleetInstallationsResponse.installations:type_name -> conduit.fleet.v1.FleetInstallation
	2,  // 13: conduit.fleet.v1.ListFleetInstallationsResponse.summary:type_name -> conduit.fleet.v1.FleetSummary
	3,  // 14: conduit.fleet.v1.ListFleetInstallationsResponse.facets:type_name -> conduit.fleet.v1.FleetFacets
	0,  // 15: conduit.fleet.v1.FleetReadService.ListFleetInstallations:input_type -> conduit.fleet.v1.ListFleetInstallationsRequest
	4,  // 16: conduit.fleet.v1.FleetReadService.ListFleetInstallations:output_type -> conduit.fleet.v1.ListFleetInstallationsResponse
	16, // [16:17] is the sub-list for method output_type
	15, // [15:16] is the sub-list for method input_type
	15, // [15:15] is the sub-list for extension type_name
	15, // [15:15] is the sub-list for extension extendee
	0,  // [0:15] is the sub-list for field type_name
}

func init() { file_conduit_fleet_v1_fleet_proto_init() }
func file_conduit_fleet_v1_fleet_proto_init() {
	if File_conduit_fleet_v1_fleet_proto != nil {
		return
	}
	file_conduit_fleet_v1_fleet_proto_msgTypes[0].OneofWrappers = []any{}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_conduit_fleet_v1_fleet_proto_rawDesc), len(file_conduit_fleet_v1_fleet_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   15,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_conduit_fleet_v1_fleet_proto_goTypes,
		DependencyIndexes: file_conduit_fleet_v1_fleet_proto_depIdxs,
		MessageInfos:      file_conduit_fleet_v1_fleet_proto_msgTypes,
	}.Build()
	File_conduit_fleet_v1_fleet_proto = out.File
	file_conduit_fleet_v1_fleet_proto_goTypes = nil
	file_conduit_fleet_v1_fleet_proto_depIdxs = nil
}

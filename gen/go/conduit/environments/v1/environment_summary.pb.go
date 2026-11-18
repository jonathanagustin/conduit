//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.12
// 	protoc        (unknown)
// source: conduit/environments/v1/environment_summary.proto

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

type ListEnvironmentSummariesRequest struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// Absent uses native summary default 100; explicit values must be 1..200.
	Limit *int32 `protobuf:"varint,1,opt,name=limit,proto3,oneof" json:"limit,omitempty"`
	// Native opaque continuation, bound to normalized search and selected Space.
	Cursor string `protobuf:"bytes,2,opt,name=cursor,proto3" json:"cursor,omitempty"`
	// Native case-insensitive Environment ID substring search.
	Search string `protobuf:"bytes,3,opt,name=search,proto3" json:"search,omitempty"`
	// Empty means no Space selection, never an authorization bypass.
	SpaceId       string `protobuf:"bytes,4,opt,name=space_id,json=spaceId,proto3" json:"space_id,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ListEnvironmentSummariesRequest) Reset() {
	*x = ListEnvironmentSummariesRequest{}
	mi := &file_conduit_environments_v1_environment_summary_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListEnvironmentSummariesRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListEnvironmentSummariesRequest) ProtoMessage() {}

func (x *ListEnvironmentSummariesRequest) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_summary_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListEnvironmentSummariesRequest.ProtoReflect.Descriptor instead.
func (*ListEnvironmentSummariesRequest) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_summary_proto_rawDescGZIP(), []int{0}
}

func (x *ListEnvironmentSummariesRequest) GetLimit() int32 {
	if x != nil && x.Limit != nil {
		return *x.Limit
	}
	return 0
}

func (x *ListEnvironmentSummariesRequest) GetCursor() string {
	if x != nil {
		return x.Cursor
	}
	return ""
}

func (x *ListEnvironmentSummariesRequest) GetSearch() string {
	if x != nil {
		return x.Search
	}
	return ""
}

func (x *ListEnvironmentSummariesRequest) GetSpaceId() string {
	if x != nil {
		return x.SpaceId
	}
	return ""
}

// EnvironmentIdentity contains existing native identity, not configuration.
// Generation is an opaque string and must not be parsed as a numeric revision.
type EnvironmentIdentity struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Id            string                 `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	Generation    string                 `protobuf:"bytes,2,opt,name=generation,proto3" json:"generation,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *EnvironmentIdentity) Reset() {
	*x = EnvironmentIdentity{}
	mi := &file_conduit_environments_v1_environment_summary_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *EnvironmentIdentity) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*EnvironmentIdentity) ProtoMessage() {}

func (x *EnvironmentIdentity) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_summary_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use EnvironmentIdentity.ProtoReflect.Descriptor instead.
func (*EnvironmentIdentity) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_summary_proto_rawDescGZIP(), []int{1}
}

func (x *EnvironmentIdentity) GetId() string {
	if x != nil {
		return x.Id
	}
	return ""
}

func (x *EnvironmentIdentity) GetGeneration() string {
	if x != nil {
		return x.Generation
	}
	return ""
}

// EnvironmentReportSummary is reported evidence, never desired state.
type EnvironmentReportSummary struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Environment   string                 `protobuf:"bytes,1,opt,name=environment,proto3" json:"environment,omitempty"`
	AgentId       string                 `protobuf:"bytes,2,opt,name=agent_id,json=agentId,proto3" json:"agent_id,omitempty"`
	ObservedAt    *timestamppb.Timestamp `protobuf:"bytes,3,opt,name=observed_at,json=observedAt,proto3" json:"observed_at,omitempty"`
	EntityCount   int64                  `protobuf:"varint,4,opt,name=entity_count,json=entityCount,proto3" json:"entity_count,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *EnvironmentReportSummary) Reset() {
	*x = EnvironmentReportSummary{}
	mi := &file_conduit_environments_v1_environment_summary_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *EnvironmentReportSummary) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*EnvironmentReportSummary) ProtoMessage() {}

func (x *EnvironmentReportSummary) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_summary_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use EnvironmentReportSummary.ProtoReflect.Descriptor instead.
func (*EnvironmentReportSummary) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_summary_proto_rawDescGZIP(), []int{2}
}

func (x *EnvironmentReportSummary) GetEnvironment() string {
	if x != nil {
		return x.Environment
	}
	return ""
}

func (x *EnvironmentReportSummary) GetAgentId() string {
	if x != nil {
		return x.AgentId
	}
	return ""
}

func (x *EnvironmentReportSummary) GetObservedAt() *timestamppb.Timestamp {
	if x != nil {
		return x.ObservedAt
	}
	return nil
}

func (x *EnvironmentReportSummary) GetEntityCount() int64 {
	if x != nil {
		return x.EntityCount
	}
	return 0
}

// SpokeLiveness preserves open native state vocabulary and observation time.
// Absence of an observation must not be replaced by the current time.
type SpokeLiveness struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	AgentId       string                 `protobuf:"bytes,1,opt,name=agent_id,json=agentId,proto3" json:"agent_id,omitempty"`
	State         string                 `protobuf:"bytes,2,opt,name=state,proto3" json:"state,omitempty"`
	ObservedAt    *timestamppb.Timestamp `protobuf:"bytes,3,opt,name=observed_at,json=observedAt,proto3" json:"observed_at,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *SpokeLiveness) Reset() {
	*x = SpokeLiveness{}
	mi := &file_conduit_environments_v1_environment_summary_proto_msgTypes[3]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *SpokeLiveness) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*SpokeLiveness) ProtoMessage() {}

func (x *SpokeLiveness) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_summary_proto_msgTypes[3]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use SpokeLiveness.ProtoReflect.Descriptor instead.
func (*SpokeLiveness) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_summary_proto_rawDescGZIP(), []int{3}
}

func (x *SpokeLiveness) GetAgentId() string {
	if x != nil {
		return x.AgentId
	}
	return ""
}

func (x *SpokeLiveness) GetState() string {
	if x != nil {
		return x.State
	}
	return ""
}

func (x *SpokeLiveness) GetObservedAt() *timestamppb.Timestamp {
	if x != nil {
		return x.ObservedAt
	}
	return nil
}

type EnvironmentSummary struct {
	state       protoimpl.MessageState `protogen:"open.v1"`
	Environment *EnvironmentIdentity   `protobuf:"bytes,1,opt,name=environment,proto3" json:"environment,omitempty"`
	// Message presence distinguishes no report from an observed empty report.
	Reported      *EnvironmentReportSummary `protobuf:"bytes,2,opt,name=reported,proto3" json:"reported,omitempty"`
	Spoke         *SpokeLiveness            `protobuf:"bytes,3,opt,name=spoke,proto3" json:"spoke,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *EnvironmentSummary) Reset() {
	*x = EnvironmentSummary{}
	mi := &file_conduit_environments_v1_environment_summary_proto_msgTypes[4]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *EnvironmentSummary) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*EnvironmentSummary) ProtoMessage() {}

func (x *EnvironmentSummary) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_summary_proto_msgTypes[4]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use EnvironmentSummary.ProtoReflect.Descriptor instead.
func (*EnvironmentSummary) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_summary_proto_rawDescGZIP(), []int{4}
}

func (x *EnvironmentSummary) GetEnvironment() *EnvironmentIdentity {
	if x != nil {
		return x.Environment
	}
	return nil
}

func (x *EnvironmentSummary) GetReported() *EnvironmentReportSummary {
	if x != nil {
		return x.Reported
	}
	return nil
}

func (x *EnvironmentSummary) GetSpoke() *SpokeLiveness {
	if x != nil {
		return x.Spoke
	}
	return nil
}

type ListEnvironmentSummariesResponse struct {
	state        protoimpl.MessageState `protogen:"open.v1"`
	Environments []*EnvironmentSummary  `protobuf:"bytes,1,rep,name=environments,proto3" json:"environments,omitempty"`
	// Total counts only authorized, Space-selected and search-filtered records.
	Total          int64  `protobuf:"varint,2,opt,name=total,proto3" json:"total,omitempty"`
	PageSize       int32  `protobuf:"varint,3,opt,name=page_size,json=pageSize,proto3" json:"page_size,omitempty"`
	Limit          int32  `protobuf:"varint,4,opt,name=limit,proto3" json:"limit,omitempty"`
	NextCursor     string `protobuf:"bytes,5,opt,name=next_cursor,json=nextCursor,proto3" json:"next_cursor,omitempty"`
	PreviousCursor string `protobuf:"bytes,6,opt,name=previous_cursor,json=previousCursor,proto3" json:"previous_cursor,omitempty"`
	HasPrevious    bool   `protobuf:"varint,7,opt,name=has_previous,json=hasPrevious,proto3" json:"has_previous,omitempty"`
	unknownFields  protoimpl.UnknownFields
	sizeCache      protoimpl.SizeCache
}

func (x *ListEnvironmentSummariesResponse) Reset() {
	*x = ListEnvironmentSummariesResponse{}
	mi := &file_conduit_environments_v1_environment_summary_proto_msgTypes[5]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListEnvironmentSummariesResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListEnvironmentSummariesResponse) ProtoMessage() {}

func (x *ListEnvironmentSummariesResponse) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_summary_proto_msgTypes[5]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListEnvironmentSummariesResponse.ProtoReflect.Descriptor instead.
func (*ListEnvironmentSummariesResponse) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_summary_proto_rawDescGZIP(), []int{5}
}

func (x *ListEnvironmentSummariesResponse) GetEnvironments() []*EnvironmentSummary {
	if x != nil {
		return x.Environments
	}
	return nil
}

func (x *ListEnvironmentSummariesResponse) GetTotal() int64 {
	if x != nil {
		return x.Total
	}
	return 0
}

func (x *ListEnvironmentSummariesResponse) GetPageSize() int32 {
	if x != nil {
		return x.PageSize
	}
	return 0
}

func (x *ListEnvironmentSummariesResponse) GetLimit() int32 {
	if x != nil {
		return x.Limit
	}
	return 0
}

func (x *ListEnvironmentSummariesResponse) GetNextCursor() string {
	if x != nil {
		return x.NextCursor
	}
	return ""
}

func (x *ListEnvironmentSummariesResponse) GetPreviousCursor() string {
	if x != nil {
		return x.PreviousCursor
	}
	return ""
}

func (x *ListEnvironmentSummariesResponse) GetHasPrevious() bool {
	if x != nil {
		return x.HasPrevious
	}
	return false
}

var File_conduit_environments_v1_environment_summary_proto protoreflect.FileDescriptor

const file_conduit_environments_v1_environment_summary_proto_rawDesc = "" +
	"\n" +
	"1conduit/environments/v1/environment_summary.proto\x12\x17conduit.environments.v1\x1a\x1fgoogle/protobuf/timestamp.proto\"\x91\x01\n" +
	"\x1fListEnvironmentSummariesRequest\x12\x19\n" +
	"\x05limit\x18\x01 \x01(\x05H\x00R\x05limit\x88\x01\x01\x12\x16\n" +
	"\x06cursor\x18\x02 \x01(\tR\x06cursor\x12\x16\n" +
	"\x06search\x18\x03 \x01(\tR\x06search\x12\x19\n" +
	"\bspace_id\x18\x04 \x01(\tR\aspaceIdB\b\n" +
	"\x06_limit\"E\n" +
	"\x13EnvironmentIdentity\x12\x0e\n" +
	"\x02id\x18\x01 \x01(\tR\x02id\x12\x1e\n" +
	"\n" +
	"generation\x18\x02 \x01(\tR\n" +
	"generation\"\xb7\x01\n" +
	"\x18EnvironmentReportSummary\x12 \n" +
	"\venvironment\x18\x01 \x01(\tR\venvironment\x12\x19\n" +
	"\bagent_id\x18\x02 \x01(\tR\aagentId\x12;\n" +
	"\vobserved_at\x18\x03 \x01(\v2\x1a.google.protobuf.TimestampR\n" +
	"observedAt\x12!\n" +
	"\fentity_count\x18\x04 \x01(\x03R\ventityCount\"}\n" +
	"\rSpokeLiveness\x12\x19\n" +
	"\bagent_id\x18\x01 \x01(\tR\aagentId\x12\x14\n" +
	"\x05state\x18\x02 \x01(\tR\x05state\x12;\n" +
	"\vobserved_at\x18\x03 \x01(\v2\x1a.google.protobuf.TimestampR\n" +
	"observedAt\"\xf1\x01\n" +
	"\x12EnvironmentSummary\x12N\n" +
	"\venvironment\x18\x01 \x01(\v2,.conduit.environments.v1.EnvironmentIdentityR\venvironment\x12M\n" +
	"\breported\x18\x02 \x01(\v21.conduit.environments.v1.EnvironmentReportSummaryR\breported\x12<\n" +
	"\x05spoke\x18\x03 \x01(\v2&.conduit.environments.v1.SpokeLivenessR\x05spoke\"\xa9\x02\n" +
	" ListEnvironmentSummariesResponse\x12O\n" +
	"\fenvironments\x18\x01 \x03(\v2+.conduit.environments.v1.EnvironmentSummaryR\fenvironments\x12\x14\n" +
	"\x05total\x18\x02 \x01(\x03R\x05total\x12\x1b\n" +
	"\tpage_size\x18\x03 \x01(\x05R\bpageSize\x12\x14\n" +
	"\x05limit\x18\x04 \x01(\x05R\x05limit\x12\x1f\n" +
	"\vnext_cursor\x18\x05 \x01(\tR\n" +
	"nextCursor\x12'\n" +
	"\x0fprevious_cursor\x18\x06 \x01(\tR\x0epreviousCursor\x12!\n" +
	"\fhas_previous\x18\a \x01(\bR\vhasPrevious2\xaf\x01\n" +
	"\x16EnvironmentReadService\x12\x94\x01\n" +
	"\x18ListEnvironmentSummaries\x128.conduit.environments.v1.ListEnvironmentSummariesRequest\x1a9.conduit.environments.v1.ListEnvironmentSummariesResponse\"\x03\x90\x02\x01BRZPgithub.com/jonathanagustin/conduit/gen/go/conduit/environments/v1;environmentsv1b\x06proto3"

var (
	file_conduit_environments_v1_environment_summary_proto_rawDescOnce sync.Once
	file_conduit_environments_v1_environment_summary_proto_rawDescData []byte
)

func file_conduit_environments_v1_environment_summary_proto_rawDescGZIP() []byte {
	file_conduit_environments_v1_environment_summary_proto_rawDescOnce.Do(func() {
		file_conduit_environments_v1_environment_summary_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_conduit_environments_v1_environment_summary_proto_rawDesc), len(file_conduit_environments_v1_environment_summary_proto_rawDesc)))
	})
	return file_conduit_environments_v1_environment_summary_proto_rawDescData
}

var file_conduit_environments_v1_environment_summary_proto_msgTypes = make([]protoimpl.MessageInfo, 6)
var file_conduit_environments_v1_environment_summary_proto_goTypes = []any{
	(*ListEnvironmentSummariesRequest)(nil),  // 0: conduit.environments.v1.ListEnvironmentSummariesRequest
	(*EnvironmentIdentity)(nil),              // 1: conduit.environments.v1.EnvironmentIdentity
	(*EnvironmentReportSummary)(nil),         // 2: conduit.environments.v1.EnvironmentReportSummary
	(*SpokeLiveness)(nil),                    // 3: conduit.environments.v1.SpokeLiveness
	(*EnvironmentSummary)(nil),               // 4: conduit.environments.v1.EnvironmentSummary
	(*ListEnvironmentSummariesResponse)(nil), // 5: conduit.environments.v1.ListEnvironmentSummariesResponse
	(*timestamppb.Timestamp)(nil),            // 6: google.protobuf.Timestamp
}
var file_conduit_environments_v1_environment_summary_proto_depIdxs = []int32{
	6, // 0: conduit.environments.v1.EnvironmentReportSummary.observed_at:type_name -> google.protobuf.Timestamp
	6, // 1: conduit.environments.v1.SpokeLiveness.observed_at:type_name -> google.protobuf.Timestamp
	1, // 2: conduit.environments.v1.EnvironmentSummary.environment:type_name -> conduit.environments.v1.EnvironmentIdentity
	2, // 3: conduit.environments.v1.EnvironmentSummary.reported:type_name -> conduit.environments.v1.EnvironmentReportSummary
	3, // 4: conduit.environments.v1.EnvironmentSummary.spoke:type_name -> conduit.environments.v1.SpokeLiveness
	4, // 5: conduit.environments.v1.ListEnvironmentSummariesResponse.environments:type_name -> conduit.environments.v1.EnvironmentSummary
	0, // 6: conduit.environments.v1.EnvironmentReadService.ListEnvironmentSummaries:input_type -> conduit.environments.v1.ListEnvironmentSummariesRequest
	5, // 7: conduit.environments.v1.EnvironmentReadService.ListEnvironmentSummaries:output_type -> conduit.environments.v1.ListEnvironmentSummariesResponse
	7, // [7:8] is the sub-list for method output_type
	6, // [6:7] is the sub-list for method input_type
	6, // [6:6] is the sub-list for extension type_name
	6, // [6:6] is the sub-list for extension extendee
	0, // [0:6] is the sub-list for field type_name
}

func init() { file_conduit_environments_v1_environment_summary_proto_init() }
func file_conduit_environments_v1_environment_summary_proto_init() {
	if File_conduit_environments_v1_environment_summary_proto != nil {
		return
	}
	file_conduit_environments_v1_environment_summary_proto_msgTypes[0].OneofWrappers = []any{}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_conduit_environments_v1_environment_summary_proto_rawDesc), len(file_conduit_environments_v1_environment_summary_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   6,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_conduit_environments_v1_environment_summary_proto_goTypes,
		DependencyIndexes: file_conduit_environments_v1_environment_summary_proto_depIdxs,
		MessageInfos:      file_conduit_environments_v1_environment_summary_proto_msgTypes,
	}.Build()
	File_conduit_environments_v1_environment_summary_proto = out.File
	file_conduit_environments_v1_environment_summary_proto_goTypes = nil
	file_conduit_environments_v1_environment_summary_proto_depIdxs = nil
}

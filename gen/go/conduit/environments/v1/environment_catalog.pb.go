//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.12
// 	protoc        (unknown)
// source: conduit/environments/v1/environment_catalog.proto

package environmentsv1

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
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

// ListEnvironmentsRequest selects the full projection, not summary view.
type ListEnvironmentsRequest struct {
	state  protoimpl.MessageState `protogen:"open.v1"`
	Search string                 `protobuf:"bytes,1,opt,name=search,proto3" json:"search,omitempty"`
	Cursor string                 `protobuf:"bytes,2,opt,name=cursor,proto3" json:"cursor,omitempty"`
	// Absent uses the native full-list default of 20; present must be 1 through 20.
	// The summary index's 100/200 limits do not apply to this full-record method.
	Limit *int32 `protobuf:"varint,3,opt,name=limit,proto3,oneof" json:"limit,omitempty"`
	// Absence is unscoped; present empty/invalid selection remains invalid.
	SpaceId       *string `protobuf:"bytes,4,opt,name=space_id,json=spaceId,proto3,oneof" json:"space_id,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ListEnvironmentsRequest) Reset() {
	*x = ListEnvironmentsRequest{}
	mi := &file_conduit_environments_v1_environment_catalog_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListEnvironmentsRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListEnvironmentsRequest) ProtoMessage() {}

func (x *ListEnvironmentsRequest) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_catalog_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListEnvironmentsRequest.ProtoReflect.Descriptor instead.
func (*ListEnvironmentsRequest) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_catalog_proto_rawDescGZIP(), []int{0}
}

func (x *ListEnvironmentsRequest) GetSearch() string {
	if x != nil {
		return x.Search
	}
	return ""
}

func (x *ListEnvironmentsRequest) GetCursor() string {
	if x != nil {
		return x.Cursor
	}
	return ""
}

func (x *ListEnvironmentsRequest) GetLimit() int32 {
	if x != nil && x.Limit != nil {
		return *x.Limit
	}
	return 0
}

func (x *ListEnvironmentsRequest) GetSpaceId() string {
	if x != nil && x.SpaceId != nil {
		return *x.SpaceId
	}
	return ""
}

// ListEnvironmentsResponse retains native full-record pagination metadata.
type ListEnvironmentsResponse struct {
	state        protoimpl.MessageState `protogen:"open.v1"`
	Environments []*EnvironmentRecord   `protobuf:"bytes,1,rep,name=environments,proto3" json:"environments,omitempty"`
	// Counts only readable records matching search within the selected grouping.
	Total          int64  `protobuf:"varint,2,opt,name=total,proto3" json:"total,omitempty"`
	PageSize       int32  `protobuf:"varint,3,opt,name=page_size,json=pageSize,proto3" json:"page_size,omitempty"`
	Limit          int32  `protobuf:"varint,4,opt,name=limit,proto3" json:"limit,omitempty"`
	NextCursor     string `protobuf:"bytes,5,opt,name=next_cursor,json=nextCursor,proto3" json:"next_cursor,omitempty"`
	PreviousCursor string `protobuf:"bytes,6,opt,name=previous_cursor,json=previousCursor,proto3" json:"previous_cursor,omitempty"`
	HasPrevious    bool   `protobuf:"varint,7,opt,name=has_previous,json=hasPrevious,proto3" json:"has_previous,omitempty"`
	unknownFields  protoimpl.UnknownFields
	sizeCache      protoimpl.SizeCache
}

func (x *ListEnvironmentsResponse) Reset() {
	*x = ListEnvironmentsResponse{}
	mi := &file_conduit_environments_v1_environment_catalog_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListEnvironmentsResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListEnvironmentsResponse) ProtoMessage() {}

func (x *ListEnvironmentsResponse) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_environments_v1_environment_catalog_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListEnvironmentsResponse.ProtoReflect.Descriptor instead.
func (*ListEnvironmentsResponse) Descriptor() ([]byte, []int) {
	return file_conduit_environments_v1_environment_catalog_proto_rawDescGZIP(), []int{1}
}

func (x *ListEnvironmentsResponse) GetEnvironments() []*EnvironmentRecord {
	if x != nil {
		return x.Environments
	}
	return nil
}

func (x *ListEnvironmentsResponse) GetTotal() int64 {
	if x != nil {
		return x.Total
	}
	return 0
}

func (x *ListEnvironmentsResponse) GetPageSize() int32 {
	if x != nil {
		return x.PageSize
	}
	return 0
}

func (x *ListEnvironmentsResponse) GetLimit() int32 {
	if x != nil {
		return x.Limit
	}
	return 0
}

func (x *ListEnvironmentsResponse) GetNextCursor() string {
	if x != nil {
		return x.NextCursor
	}
	return ""
}

func (x *ListEnvironmentsResponse) GetPreviousCursor() string {
	if x != nil {
		return x.PreviousCursor
	}
	return ""
}

func (x *ListEnvironmentsResponse) GetHasPrevious() bool {
	if x != nil {
		return x.HasPrevious
	}
	return false
}

var File_conduit_environments_v1_environment_catalog_proto protoreflect.FileDescriptor

const file_conduit_environments_v1_environment_catalog_proto_rawDesc = "" +
	"\n" +
	"1conduit/environments/v1/environment_catalog.proto\x12\x17conduit.environments.v1\x1a0conduit/environments/v1/environment_detail.proto\"\x9b\x01\n" +
	"\x17ListEnvironmentsRequest\x12\x16\n" +
	"\x06search\x18\x01 \x01(\tR\x06search\x12\x16\n" +
	"\x06cursor\x18\x02 \x01(\tR\x06cursor\x12\x19\n" +
	"\x05limit\x18\x03 \x01(\x05H\x00R\x05limit\x88\x01\x01\x12\x1e\n" +
	"\bspace_id\x18\x04 \x01(\tH\x01R\aspaceId\x88\x01\x01B\b\n" +
	"\x06_limitB\v\n" +
	"\t_space_id\"\xa0\x02\n" +
	"\x18ListEnvironmentsResponse\x12N\n" +
	"\fenvironments\x18\x01 \x03(\v2*.conduit.environments.v1.EnvironmentRecordR\fenvironments\x12\x14\n" +
	"\x05total\x18\x02 \x01(\x03R\x05total\x12\x1b\n" +
	"\tpage_size\x18\x03 \x01(\x05R\bpageSize\x12\x14\n" +
	"\x05limit\x18\x04 \x01(\x05R\x05limit\x12\x1f\n" +
	"\vnext_cursor\x18\x05 \x01(\tR\n" +
	"nextCursor\x12'\n" +
	"\x0fprevious_cursor\x18\x06 \x01(\tR\x0epreviousCursor\x12!\n" +
	"\fhas_previous\x18\a \x01(\bR\vhasPrevious2\x99\x01\n" +
	"\x19EnvironmentCatalogService\x12|\n" +
	"\x10ListEnvironments\x120.conduit.environments.v1.ListEnvironmentsRequest\x1a1.conduit.environments.v1.ListEnvironmentsResponse\"\x03\x90\x02\x01BRZPgithub.com/jonathanagustin/conduit/gen/go/conduit/environments/v1;environmentsv1b\x06proto3"

var (
	file_conduit_environments_v1_environment_catalog_proto_rawDescOnce sync.Once
	file_conduit_environments_v1_environment_catalog_proto_rawDescData []byte
)

func file_conduit_environments_v1_environment_catalog_proto_rawDescGZIP() []byte {
	file_conduit_environments_v1_environment_catalog_proto_rawDescOnce.Do(func() {
		file_conduit_environments_v1_environment_catalog_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_conduit_environments_v1_environment_catalog_proto_rawDesc), len(file_conduit_environments_v1_environment_catalog_proto_rawDesc)))
	})
	return file_conduit_environments_v1_environment_catalog_proto_rawDescData
}

var file_conduit_environments_v1_environment_catalog_proto_msgTypes = make([]protoimpl.MessageInfo, 2)
var file_conduit_environments_v1_environment_catalog_proto_goTypes = []any{
	(*ListEnvironmentsRequest)(nil),  // 0: conduit.environments.v1.ListEnvironmentsRequest
	(*ListEnvironmentsResponse)(nil), // 1: conduit.environments.v1.ListEnvironmentsResponse
	(*EnvironmentRecord)(nil),        // 2: conduit.environments.v1.EnvironmentRecord
}
var file_conduit_environments_v1_environment_catalog_proto_depIdxs = []int32{
	2, // 0: conduit.environments.v1.ListEnvironmentsResponse.environments:type_name -> conduit.environments.v1.EnvironmentRecord
	0, // 1: conduit.environments.v1.EnvironmentCatalogService.ListEnvironments:input_type -> conduit.environments.v1.ListEnvironmentsRequest
	1, // 2: conduit.environments.v1.EnvironmentCatalogService.ListEnvironments:output_type -> conduit.environments.v1.ListEnvironmentsResponse
	2, // [2:3] is the sub-list for method output_type
	1, // [1:2] is the sub-list for method input_type
	1, // [1:1] is the sub-list for extension type_name
	1, // [1:1] is the sub-list for extension extendee
	0, // [0:1] is the sub-list for field type_name
}

func init() { file_conduit_environments_v1_environment_catalog_proto_init() }
func file_conduit_environments_v1_environment_catalog_proto_init() {
	if File_conduit_environments_v1_environment_catalog_proto != nil {
		return
	}
	file_conduit_environments_v1_environment_detail_proto_init()
	file_conduit_environments_v1_environment_catalog_proto_msgTypes[0].OneofWrappers = []any{}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_conduit_environments_v1_environment_catalog_proto_rawDesc), len(file_conduit_environments_v1_environment_catalog_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   2,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_conduit_environments_v1_environment_catalog_proto_goTypes,
		DependencyIndexes: file_conduit_environments_v1_environment_catalog_proto_depIdxs,
		MessageInfos:      file_conduit_environments_v1_environment_catalog_proto_msgTypes,
	}.Build()
	File_conduit_environments_v1_environment_catalog_proto = out.File
	file_conduit_environments_v1_environment_catalog_proto_goTypes = nil
	file_conduit_environments_v1_environment_catalog_proto_depIdxs = nil
}

//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.12
// 	protoc        (unknown)
// source: conduit/plans/v1/plan_scoped_summary.proto

package plansv1

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

type ListPlanSummariesInSpaceRequest struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// Required native identity; absent/empty/invalid values are input failures.
	SpaceId string `protobuf:"bytes,1,opt,name=space_id,json=spaceId,proto3" json:"space_id,omitempty"`
	// Absent uses the native summary default 100; present must be 1 through 200.
	Limit *int32 `protobuf:"varint,2,opt,name=limit,proto3,oneof" json:"limit,omitempty"`
	// Existing opaque cursor, bound to filters and this Space; not a snapshot.
	Cursor         string `protobuf:"bytes,3,opt,name=cursor,proto3" json:"cursor,omitempty"`
	Environment    string `protobuf:"bytes,4,opt,name=environment,proto3" json:"environment,omitempty"`
	Status         string `protobuf:"bytes,5,opt,name=status,proto3" json:"status,omitempty"`
	InstallationId string `protobuf:"bytes,6,opt,name=installation_id,json=installationId,proto3" json:"installation_id,omitempty"`
	Search         string `protobuf:"bytes,7,opt,name=search,proto3" json:"search,omitempty"`
	unknownFields  protoimpl.UnknownFields
	sizeCache      protoimpl.SizeCache
}

func (x *ListPlanSummariesInSpaceRequest) Reset() {
	*x = ListPlanSummariesInSpaceRequest{}
	mi := &file_conduit_plans_v1_plan_scoped_summary_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListPlanSummariesInSpaceRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListPlanSummariesInSpaceRequest) ProtoMessage() {}

func (x *ListPlanSummariesInSpaceRequest) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_plans_v1_plan_scoped_summary_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListPlanSummariesInSpaceRequest.ProtoReflect.Descriptor instead.
func (*ListPlanSummariesInSpaceRequest) Descriptor() ([]byte, []int) {
	return file_conduit_plans_v1_plan_scoped_summary_proto_rawDescGZIP(), []int{0}
}

func (x *ListPlanSummariesInSpaceRequest) GetSpaceId() string {
	if x != nil {
		return x.SpaceId
	}
	return ""
}

func (x *ListPlanSummariesInSpaceRequest) GetLimit() int32 {
	if x != nil && x.Limit != nil {
		return *x.Limit
	}
	return 0
}

func (x *ListPlanSummariesInSpaceRequest) GetCursor() string {
	if x != nil {
		return x.Cursor
	}
	return ""
}

func (x *ListPlanSummariesInSpaceRequest) GetEnvironment() string {
	if x != nil {
		return x.Environment
	}
	return ""
}

func (x *ListPlanSummariesInSpaceRequest) GetStatus() string {
	if x != nil {
		return x.Status
	}
	return ""
}

func (x *ListPlanSummariesInSpaceRequest) GetInstallationId() string {
	if x != nil {
		return x.InstallationId
	}
	return ""
}

func (x *ListPlanSummariesInSpaceRequest) GetSearch() string {
	if x != nil {
		return x.Search
	}
	return ""
}

type ListPlanSummariesInSpaceResponse struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// Echo the validated selection. Clients bind it before accepting any rows;
	// this is a grouping coordinate, not a grant or a delegated identity.
	SpaceId string `protobuf:"bytes,1,opt,name=space_id,json=spaceId,proto3" json:"space_id,omitempty"`
	// Existing summary fields and native current-visibility pagination metadata.
	Page          *ListPlanSummariesResponse `protobuf:"bytes,2,opt,name=page,proto3" json:"page,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ListPlanSummariesInSpaceResponse) Reset() {
	*x = ListPlanSummariesInSpaceResponse{}
	mi := &file_conduit_plans_v1_plan_scoped_summary_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListPlanSummariesInSpaceResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListPlanSummariesInSpaceResponse) ProtoMessage() {}

func (x *ListPlanSummariesInSpaceResponse) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_plans_v1_plan_scoped_summary_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListPlanSummariesInSpaceResponse.ProtoReflect.Descriptor instead.
func (*ListPlanSummariesInSpaceResponse) Descriptor() ([]byte, []int) {
	return file_conduit_plans_v1_plan_scoped_summary_proto_rawDescGZIP(), []int{1}
}

func (x *ListPlanSummariesInSpaceResponse) GetSpaceId() string {
	if x != nil {
		return x.SpaceId
	}
	return ""
}

func (x *ListPlanSummariesInSpaceResponse) GetPage() *ListPlanSummariesResponse {
	if x != nil {
		return x.Page
	}
	return nil
}

var File_conduit_plans_v1_plan_scoped_summary_proto protoreflect.FileDescriptor

const file_conduit_plans_v1_plan_scoped_summary_proto_rawDesc = "" +
	"\n" +
	"*conduit/plans/v1/plan_scoped_summary.proto\x12\x10conduit.plans.v1\x1a#conduit/plans/v1/plan_summary.proto\"\xf4\x01\n" +
	"\x1fListPlanSummariesInSpaceRequest\x12\x19\n" +
	"\bspace_id\x18\x01 \x01(\tR\aspaceId\x12\x19\n" +
	"\x05limit\x18\x02 \x01(\x05H\x00R\x05limit\x88\x01\x01\x12\x16\n" +
	"\x06cursor\x18\x03 \x01(\tR\x06cursor\x12 \n" +
	"\venvironment\x18\x04 \x01(\tR\venvironment\x12\x16\n" +
	"\x06status\x18\x05 \x01(\tR\x06status\x12'\n" +
	"\x0finstallation_id\x18\x06 \x01(\tR\x0einstallationId\x12\x16\n" +
	"\x06search\x18\a \x01(\tR\x06searchB\b\n" +
	"\x06_limit\"~\n" +
	" ListPlanSummariesInSpaceResponse\x12\x19\n" +
	"\bspace_id\x18\x01 \x01(\tR\aspaceId\x12?\n" +
	"\x04page\x18\x02 \x01(\v2+.conduit.plans.v1.ListPlanSummariesResponseR\x04page2\xa0\x01\n" +
	"\x15PlanScopedReadService\x12\x86\x01\n" +
	"\x18ListPlanSummariesInSpace\x121.conduit.plans.v1.ListPlanSummariesInSpaceRequest\x1a2.conduit.plans.v1.ListPlanSummariesInSpaceResponse\"\x03\x90\x02\x01BDZBgithub.com/jonathanagustin/conduit/gen/go/conduit/plans/v1;plansv1b\x06proto3"

var (
	file_conduit_plans_v1_plan_scoped_summary_proto_rawDescOnce sync.Once
	file_conduit_plans_v1_plan_scoped_summary_proto_rawDescData []byte
)

func file_conduit_plans_v1_plan_scoped_summary_proto_rawDescGZIP() []byte {
	file_conduit_plans_v1_plan_scoped_summary_proto_rawDescOnce.Do(func() {
		file_conduit_plans_v1_plan_scoped_summary_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_conduit_plans_v1_plan_scoped_summary_proto_rawDesc), len(file_conduit_plans_v1_plan_scoped_summary_proto_rawDesc)))
	})
	return file_conduit_plans_v1_plan_scoped_summary_proto_rawDescData
}

var file_conduit_plans_v1_plan_scoped_summary_proto_msgTypes = make([]protoimpl.MessageInfo, 2)
var file_conduit_plans_v1_plan_scoped_summary_proto_goTypes = []any{
	(*ListPlanSummariesInSpaceRequest)(nil),  // 0: conduit.plans.v1.ListPlanSummariesInSpaceRequest
	(*ListPlanSummariesInSpaceResponse)(nil), // 1: conduit.plans.v1.ListPlanSummariesInSpaceResponse
	(*ListPlanSummariesResponse)(nil),        // 2: conduit.plans.v1.ListPlanSummariesResponse
}
var file_conduit_plans_v1_plan_scoped_summary_proto_depIdxs = []int32{
	2, // 0: conduit.plans.v1.ListPlanSummariesInSpaceResponse.page:type_name -> conduit.plans.v1.ListPlanSummariesResponse
	0, // 1: conduit.plans.v1.PlanScopedReadService.ListPlanSummariesInSpace:input_type -> conduit.plans.v1.ListPlanSummariesInSpaceRequest
	1, // 2: conduit.plans.v1.PlanScopedReadService.ListPlanSummariesInSpace:output_type -> conduit.plans.v1.ListPlanSummariesInSpaceResponse
	2, // [2:3] is the sub-list for method output_type
	1, // [1:2] is the sub-list for method input_type
	1, // [1:1] is the sub-list for extension type_name
	1, // [1:1] is the sub-list for extension extendee
	0, // [0:1] is the sub-list for field type_name
}

func init() { file_conduit_plans_v1_plan_scoped_summary_proto_init() }
func file_conduit_plans_v1_plan_scoped_summary_proto_init() {
	if File_conduit_plans_v1_plan_scoped_summary_proto != nil {
		return
	}
	file_conduit_plans_v1_plan_summary_proto_init()
	file_conduit_plans_v1_plan_scoped_summary_proto_msgTypes[0].OneofWrappers = []any{}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_conduit_plans_v1_plan_scoped_summary_proto_rawDesc), len(file_conduit_plans_v1_plan_scoped_summary_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   2,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_conduit_plans_v1_plan_scoped_summary_proto_goTypes,
		DependencyIndexes: file_conduit_plans_v1_plan_scoped_summary_proto_depIdxs,
		MessageInfos:      file_conduit_plans_v1_plan_scoped_summary_proto_msgTypes,
	}.Build()
	File_conduit_plans_v1_plan_scoped_summary_proto = out.File
	file_conduit_plans_v1_plan_scoped_summary_proto_goTypes = nil
	file_conduit_plans_v1_plan_scoped_summary_proto_depIdxs = nil
}

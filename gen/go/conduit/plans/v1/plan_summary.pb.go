//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.12
// 	protoc        (unknown)
// source: conduit/plans/v1/plan_summary.proto

package plansv1

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

type ListPlanSummariesRequest struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// limit is absent for the native default 100; valid explicit values are 1..200.
	Limit *int32 `protobuf:"varint,1,opt,name=limit,proto3,oneof" json:"limit,omitempty"`
	// cursor is the native opaque continuation, bound to the active filters.
	Cursor         string `protobuf:"bytes,2,opt,name=cursor,proto3" json:"cursor,omitempty"`
	Environment    string `protobuf:"bytes,3,opt,name=environment,proto3" json:"environment,omitempty"`
	Status         string `protobuf:"bytes,4,opt,name=status,proto3" json:"status,omitempty"`
	InstallationId string `protobuf:"bytes,5,opt,name=installation_id,json=installationId,proto3" json:"installation_id,omitempty"`
	Search         string `protobuf:"bytes,6,opt,name=search,proto3" json:"search,omitempty"`
	unknownFields  protoimpl.UnknownFields
	sizeCache      protoimpl.SizeCache
}

func (x *ListPlanSummariesRequest) Reset() {
	*x = ListPlanSummariesRequest{}
	mi := &file_conduit_plans_v1_plan_summary_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListPlanSummariesRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListPlanSummariesRequest) ProtoMessage() {}

func (x *ListPlanSummariesRequest) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_plans_v1_plan_summary_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListPlanSummariesRequest.ProtoReflect.Descriptor instead.
func (*ListPlanSummariesRequest) Descriptor() ([]byte, []int) {
	return file_conduit_plans_v1_plan_summary_proto_rawDescGZIP(), []int{0}
}

func (x *ListPlanSummariesRequest) GetLimit() int32 {
	if x != nil && x.Limit != nil {
		return *x.Limit
	}
	return 0
}

func (x *ListPlanSummariesRequest) GetCursor() string {
	if x != nil {
		return x.Cursor
	}
	return ""
}

func (x *ListPlanSummariesRequest) GetEnvironment() string {
	if x != nil {
		return x.Environment
	}
	return ""
}

func (x *ListPlanSummariesRequest) GetStatus() string {
	if x != nil {
		return x.Status
	}
	return ""
}

func (x *ListPlanSummariesRequest) GetInstallationId() string {
	if x != nil {
		return x.InstallationId
	}
	return ""
}

func (x *ListPlanSummariesRequest) GetSearch() string {
	if x != nil {
		return x.Search
	}
	return ""
}

// PlanSummary is the existing summary projection, not a full Plan or approval.
type PlanSummary struct {
	state       protoimpl.MessageState `protogen:"open.v1"`
	Id          string                 `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	Environment string                 `protobuf:"bytes,2,opt,name=environment,proto3" json:"environment,omitempty"`
	// status is open native vocabulary, including future statuses.
	Status            string                 `protobuf:"bytes,3,opt,name=status,proto3" json:"status,omitempty"`
	StepCount         int64                  `protobuf:"varint,4,opt,name=step_count,json=stepCount,proto3" json:"step_count,omitempty"`
	ApprovalCount     int64                  `protobuf:"varint,5,opt,name=approval_count,json=approvalCount,proto3" json:"approval_count,omitempty"`
	RequiredApprovals int64                  `protobuf:"varint,6,opt,name=required_approvals,json=requiredApprovals,proto3" json:"required_approvals,omitempty"`
	CreatedAt         *timestamppb.Timestamp `protobuf:"bytes,7,opt,name=created_at,json=createdAt,proto3" json:"created_at,omitempty"`
	UpdatedAt         *timestamppb.Timestamp `protobuf:"bytes,8,opt,name=updated_at,json=updatedAt,proto3" json:"updated_at,omitempty"`
	Revision          uint64                 `protobuf:"varint,9,opt,name=revision,proto3" json:"revision,omitempty"`
	unknownFields     protoimpl.UnknownFields
	sizeCache         protoimpl.SizeCache
}

func (x *PlanSummary) Reset() {
	*x = PlanSummary{}
	mi := &file_conduit_plans_v1_plan_summary_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *PlanSummary) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*PlanSummary) ProtoMessage() {}

func (x *PlanSummary) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_plans_v1_plan_summary_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use PlanSummary.ProtoReflect.Descriptor instead.
func (*PlanSummary) Descriptor() ([]byte, []int) {
	return file_conduit_plans_v1_plan_summary_proto_rawDescGZIP(), []int{1}
}

func (x *PlanSummary) GetId() string {
	if x != nil {
		return x.Id
	}
	return ""
}

func (x *PlanSummary) GetEnvironment() string {
	if x != nil {
		return x.Environment
	}
	return ""
}

func (x *PlanSummary) GetStatus() string {
	if x != nil {
		return x.Status
	}
	return ""
}

func (x *PlanSummary) GetStepCount() int64 {
	if x != nil {
		return x.StepCount
	}
	return 0
}

func (x *PlanSummary) GetApprovalCount() int64 {
	if x != nil {
		return x.ApprovalCount
	}
	return 0
}

func (x *PlanSummary) GetRequiredApprovals() int64 {
	if x != nil {
		return x.RequiredApprovals
	}
	return 0
}

func (x *PlanSummary) GetCreatedAt() *timestamppb.Timestamp {
	if x != nil {
		return x.CreatedAt
	}
	return nil
}

func (x *PlanSummary) GetUpdatedAt() *timestamppb.Timestamp {
	if x != nil {
		return x.UpdatedAt
	}
	return nil
}

func (x *PlanSummary) GetRevision() uint64 {
	if x != nil {
		return x.Revision
	}
	return 0
}

type ListPlanSummariesResponse struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	Plans []*PlanSummary         `protobuf:"bytes,1,rep,name=plans,proto3" json:"plans,omitempty"`
	// total counts only authorized, filtered matches.
	Total          int64  `protobuf:"varint,2,opt,name=total,proto3" json:"total,omitempty"`
	PageSize       int32  `protobuf:"varint,3,opt,name=page_size,json=pageSize,proto3" json:"page_size,omitempty"`
	Limit          int32  `protobuf:"varint,4,opt,name=limit,proto3" json:"limit,omitempty"`
	NextCursor     string `protobuf:"bytes,5,opt,name=next_cursor,json=nextCursor,proto3" json:"next_cursor,omitempty"`
	PreviousCursor string `protobuf:"bytes,6,opt,name=previous_cursor,json=previousCursor,proto3" json:"previous_cursor,omitempty"`
	HasPrevious    bool   `protobuf:"varint,7,opt,name=has_previous,json=hasPrevious,proto3" json:"has_previous,omitempty"`
	unknownFields  protoimpl.UnknownFields
	sizeCache      protoimpl.SizeCache
}

func (x *ListPlanSummariesResponse) Reset() {
	*x = ListPlanSummariesResponse{}
	mi := &file_conduit_plans_v1_plan_summary_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListPlanSummariesResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListPlanSummariesResponse) ProtoMessage() {}

func (x *ListPlanSummariesResponse) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_plans_v1_plan_summary_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListPlanSummariesResponse.ProtoReflect.Descriptor instead.
func (*ListPlanSummariesResponse) Descriptor() ([]byte, []int) {
	return file_conduit_plans_v1_plan_summary_proto_rawDescGZIP(), []int{2}
}

func (x *ListPlanSummariesResponse) GetPlans() []*PlanSummary {
	if x != nil {
		return x.Plans
	}
	return nil
}

func (x *ListPlanSummariesResponse) GetTotal() int64 {
	if x != nil {
		return x.Total
	}
	return 0
}

func (x *ListPlanSummariesResponse) GetPageSize() int32 {
	if x != nil {
		return x.PageSize
	}
	return 0
}

func (x *ListPlanSummariesResponse) GetLimit() int32 {
	if x != nil {
		return x.Limit
	}
	return 0
}

func (x *ListPlanSummariesResponse) GetNextCursor() string {
	if x != nil {
		return x.NextCursor
	}
	return ""
}

func (x *ListPlanSummariesResponse) GetPreviousCursor() string {
	if x != nil {
		return x.PreviousCursor
	}
	return ""
}

func (x *ListPlanSummariesResponse) GetHasPrevious() bool {
	if x != nil {
		return x.HasPrevious
	}
	return false
}

var File_conduit_plans_v1_plan_summary_proto protoreflect.FileDescriptor

const file_conduit_plans_v1_plan_summary_proto_rawDesc = "" +
	"\n" +
	"#conduit/plans/v1/plan_summary.proto\x12\x10conduit.plans.v1\x1a\x1fgoogle/protobuf/timestamp.proto\"\xd2\x01\n" +
	"\x18ListPlanSummariesRequest\x12\x19\n" +
	"\x05limit\x18\x01 \x01(\x05H\x00R\x05limit\x88\x01\x01\x12\x16\n" +
	"\x06cursor\x18\x02 \x01(\tR\x06cursor\x12 \n" +
	"\venvironment\x18\x03 \x01(\tR\venvironment\x12\x16\n" +
	"\x06status\x18\x04 \x01(\tR\x06status\x12'\n" +
	"\x0finstallation_id\x18\x05 \x01(\tR\x0einstallationId\x12\x16\n" +
	"\x06search\x18\x06 \x01(\tR\x06searchB\b\n" +
	"\x06_limit\"\xde\x02\n" +
	"\vPlanSummary\x12\x0e\n" +
	"\x02id\x18\x01 \x01(\tR\x02id\x12 \n" +
	"\venvironment\x18\x02 \x01(\tR\venvironment\x12\x16\n" +
	"\x06status\x18\x03 \x01(\tR\x06status\x12\x1d\n" +
	"\n" +
	"step_count\x18\x04 \x01(\x03R\tstepCount\x12%\n" +
	"\x0eapproval_count\x18\x05 \x01(\x03R\rapprovalCount\x12-\n" +
	"\x12required_approvals\x18\x06 \x01(\x03R\x11requiredApprovals\x129\n" +
	"\n" +
	"created_at\x18\a \x01(\v2\x1a.google.protobuf.TimestampR\tcreatedAt\x129\n" +
	"\n" +
	"updated_at\x18\b \x01(\v2\x1a.google.protobuf.TimestampR\tupdatedAt\x12\x1a\n" +
	"\brevision\x18\t \x01(\x04R\brevision\"\x86\x02\n" +
	"\x19ListPlanSummariesResponse\x123\n" +
	"\x05plans\x18\x01 \x03(\v2\x1d.conduit.plans.v1.PlanSummaryR\x05plans\x12\x14\n" +
	"\x05total\x18\x02 \x01(\x03R\x05total\x12\x1b\n" +
	"\tpage_size\x18\x03 \x01(\x05R\bpageSize\x12\x14\n" +
	"\x05limit\x18\x04 \x01(\x05R\x05limit\x12\x1f\n" +
	"\vnext_cursor\x18\x05 \x01(\tR\n" +
	"nextCursor\x12'\n" +
	"\x0fprevious_cursor\x18\x06 \x01(\tR\x0epreviousCursor\x12!\n" +
	"\fhas_previous\x18\a \x01(\bR\vhasPrevious2\x84\x01\n" +
	"\x0fPlanReadService\x12q\n" +
	"\x11ListPlanSummaries\x12*.conduit.plans.v1.ListPlanSummariesRequest\x1a+.conduit.plans.v1.ListPlanSummariesResponse\"\x03\x90\x02\x01BDZBgithub.com/jonathanagustin/conduit/gen/go/conduit/plans/v1;plansv1b\x06proto3"

var (
	file_conduit_plans_v1_plan_summary_proto_rawDescOnce sync.Once
	file_conduit_plans_v1_plan_summary_proto_rawDescData []byte
)

func file_conduit_plans_v1_plan_summary_proto_rawDescGZIP() []byte {
	file_conduit_plans_v1_plan_summary_proto_rawDescOnce.Do(func() {
		file_conduit_plans_v1_plan_summary_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_conduit_plans_v1_plan_summary_proto_rawDesc), len(file_conduit_plans_v1_plan_summary_proto_rawDesc)))
	})
	return file_conduit_plans_v1_plan_summary_proto_rawDescData
}

var file_conduit_plans_v1_plan_summary_proto_msgTypes = make([]protoimpl.MessageInfo, 3)
var file_conduit_plans_v1_plan_summary_proto_goTypes = []any{
	(*ListPlanSummariesRequest)(nil),  // 0: conduit.plans.v1.ListPlanSummariesRequest
	(*PlanSummary)(nil),               // 1: conduit.plans.v1.PlanSummary
	(*ListPlanSummariesResponse)(nil), // 2: conduit.plans.v1.ListPlanSummariesResponse
	(*timestamppb.Timestamp)(nil),     // 3: google.protobuf.Timestamp
}
var file_conduit_plans_v1_plan_summary_proto_depIdxs = []int32{
	3, // 0: conduit.plans.v1.PlanSummary.created_at:type_name -> google.protobuf.Timestamp
	3, // 1: conduit.plans.v1.PlanSummary.updated_at:type_name -> google.protobuf.Timestamp
	1, // 2: conduit.plans.v1.ListPlanSummariesResponse.plans:type_name -> conduit.plans.v1.PlanSummary
	0, // 3: conduit.plans.v1.PlanReadService.ListPlanSummaries:input_type -> conduit.plans.v1.ListPlanSummariesRequest
	2, // 4: conduit.plans.v1.PlanReadService.ListPlanSummaries:output_type -> conduit.plans.v1.ListPlanSummariesResponse
	4, // [4:5] is the sub-list for method output_type
	3, // [3:4] is the sub-list for method input_type
	3, // [3:3] is the sub-list for extension type_name
	3, // [3:3] is the sub-list for extension extendee
	0, // [0:3] is the sub-list for field type_name
}

func init() { file_conduit_plans_v1_plan_summary_proto_init() }
func file_conduit_plans_v1_plan_summary_proto_init() {
	if File_conduit_plans_v1_plan_summary_proto != nil {
		return
	}
	file_conduit_plans_v1_plan_summary_proto_msgTypes[0].OneofWrappers = []any{}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_conduit_plans_v1_plan_summary_proto_rawDesc), len(file_conduit_plans_v1_plan_summary_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   3,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_conduit_plans_v1_plan_summary_proto_goTypes,
		DependencyIndexes: file_conduit_plans_v1_plan_summary_proto_depIdxs,
		MessageInfos:      file_conduit_plans_v1_plan_summary_proto_msgTypes,
	}.Build()
	File_conduit_plans_v1_plan_summary_proto = out.File
	file_conduit_plans_v1_plan_summary_proto_goTypes = nil
	file_conduit_plans_v1_plan_summary_proto_depIdxs = nil
}

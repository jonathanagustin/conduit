//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.12
// 	protoc        (unknown)
// source: conduit/plans/v1/plan_approval.proto

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

type ApprovePlanRequest struct {
	state  protoimpl.MessageState `protogen:"open.v1"`
	PlanId string                 `protobuf:"bytes,1,opt,name=plan_id,json=planId,proto3" json:"plan_id,omitempty"`
	// rationale is the native required review explanation, not an identity.
	Rationale     string `protobuf:"bytes,2,opt,name=rationale,proto3" json:"rationale,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ApprovePlanRequest) Reset() {
	*x = ApprovePlanRequest{}
	mi := &file_conduit_plans_v1_plan_approval_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ApprovePlanRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ApprovePlanRequest) ProtoMessage() {}

func (x *ApprovePlanRequest) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_plans_v1_plan_approval_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ApprovePlanRequest.ProtoReflect.Descriptor instead.
func (*ApprovePlanRequest) Descriptor() ([]byte, []int) {
	return file_conduit_plans_v1_plan_approval_proto_rawDescGZIP(), []int{0}
}

func (x *ApprovePlanRequest) GetPlanId() string {
	if x != nil {
		return x.PlanId
	}
	return ""
}

func (x *ApprovePlanRequest) GetRationale() string {
	if x != nil {
		return x.Rationale
	}
	return ""
}

type PlanApproval struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Subject       string                 `protobuf:"bytes,1,opt,name=subject,proto3" json:"subject,omitempty"`
	Rationale     string                 `protobuf:"bytes,2,opt,name=rationale,proto3" json:"rationale,omitempty"`
	CreatedAt     *timestamppb.Timestamp `protobuf:"bytes,3,opt,name=created_at,json=createdAt,proto3" json:"created_at,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *PlanApproval) Reset() {
	*x = PlanApproval{}
	mi := &file_conduit_plans_v1_plan_approval_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *PlanApproval) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*PlanApproval) ProtoMessage() {}

func (x *PlanApproval) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_plans_v1_plan_approval_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use PlanApproval.ProtoReflect.Descriptor instead.
func (*PlanApproval) Descriptor() ([]byte, []int) {
	return file_conduit_plans_v1_plan_approval_proto_rawDescGZIP(), []int{1}
}

func (x *PlanApproval) GetSubject() string {
	if x != nil {
		return x.Subject
	}
	return ""
}

func (x *PlanApproval) GetRationale() string {
	if x != nil {
		return x.Rationale
	}
	return ""
}

func (x *PlanApproval) GetCreatedAt() *timestamppb.Timestamp {
	if x != nil {
		return x.CreatedAt
	}
	return nil
}

// ApprovePlanResponse projects the native result without execution credentials.
type ApprovePlanResponse struct {
	state           protoimpl.MessageState `protogen:"open.v1"`
	Plan            *PlanSummary           `protobuf:"bytes,1,opt,name=plan,proto3" json:"plan,omitempty"`
	Approvals       []*PlanApproval        `protobuf:"bytes,2,rep,name=approvals,proto3" json:"approvals,omitempty"`
	ChangeRequestId string                 `protobuf:"bytes,3,opt,name=change_request_id,json=changeRequestId,proto3" json:"change_request_id,omitempty"`
	unknownFields   protoimpl.UnknownFields
	sizeCache       protoimpl.SizeCache
}

func (x *ApprovePlanResponse) Reset() {
	*x = ApprovePlanResponse{}
	mi := &file_conduit_plans_v1_plan_approval_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ApprovePlanResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ApprovePlanResponse) ProtoMessage() {}

func (x *ApprovePlanResponse) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_plans_v1_plan_approval_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ApprovePlanResponse.ProtoReflect.Descriptor instead.
func (*ApprovePlanResponse) Descriptor() ([]byte, []int) {
	return file_conduit_plans_v1_plan_approval_proto_rawDescGZIP(), []int{2}
}

func (x *ApprovePlanResponse) GetPlan() *PlanSummary {
	if x != nil {
		return x.Plan
	}
	return nil
}

func (x *ApprovePlanResponse) GetApprovals() []*PlanApproval {
	if x != nil {
		return x.Approvals
	}
	return nil
}

func (x *ApprovePlanResponse) GetChangeRequestId() string {
	if x != nil {
		return x.ChangeRequestId
	}
	return ""
}

// PlanApprovalOutcome is an error detail, not a request or an approval permit.
// Absent details mean an unknown outcome, never proof that no write occurred.
type PlanApprovalOutcome struct {
	state            protoimpl.MessageState `protogen:"open.v1"`
	PlanId           string                 `protobuf:"bytes,1,opt,name=plan_id,json=planId,proto3" json:"plan_id,omitempty"`
	ApprovalRecorded bool                   `protobuf:"varint,2,opt,name=approval_recorded,json=approvalRecorded,proto3" json:"approval_recorded,omitempty"`
	HandoffFailed    bool                   `protobuf:"varint,3,opt,name=handoff_failed,json=handoffFailed,proto3" json:"handoff_failed,omitempty"`
	unknownFields    protoimpl.UnknownFields
	sizeCache        protoimpl.SizeCache
}

func (x *PlanApprovalOutcome) Reset() {
	*x = PlanApprovalOutcome{}
	mi := &file_conduit_plans_v1_plan_approval_proto_msgTypes[3]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *PlanApprovalOutcome) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*PlanApprovalOutcome) ProtoMessage() {}

func (x *PlanApprovalOutcome) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_plans_v1_plan_approval_proto_msgTypes[3]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use PlanApprovalOutcome.ProtoReflect.Descriptor instead.
func (*PlanApprovalOutcome) Descriptor() ([]byte, []int) {
	return file_conduit_plans_v1_plan_approval_proto_rawDescGZIP(), []int{3}
}

func (x *PlanApprovalOutcome) GetPlanId() string {
	if x != nil {
		return x.PlanId
	}
	return ""
}

func (x *PlanApprovalOutcome) GetApprovalRecorded() bool {
	if x != nil {
		return x.ApprovalRecorded
	}
	return false
}

func (x *PlanApprovalOutcome) GetHandoffFailed() bool {
	if x != nil {
		return x.HandoffFailed
	}
	return false
}

var File_conduit_plans_v1_plan_approval_proto protoreflect.FileDescriptor

const file_conduit_plans_v1_plan_approval_proto_rawDesc = "" +
	"\n" +
	"$conduit/plans/v1/plan_approval.proto\x12\x10conduit.plans.v1\x1a#conduit/plans/v1/plan_summary.proto\x1a\x1fgoogle/protobuf/timestamp.proto\"K\n" +
	"\x12ApprovePlanRequest\x12\x17\n" +
	"\aplan_id\x18\x01 \x01(\tR\x06planId\x12\x1c\n" +
	"\trationale\x18\x02 \x01(\tR\trationale\"\x81\x01\n" +
	"\fPlanApproval\x12\x18\n" +
	"\asubject\x18\x01 \x01(\tR\asubject\x12\x1c\n" +
	"\trationale\x18\x02 \x01(\tR\trationale\x129\n" +
	"\n" +
	"created_at\x18\x03 \x01(\v2\x1a.google.protobuf.TimestampR\tcreatedAt\"\xb2\x01\n" +
	"\x13ApprovePlanResponse\x121\n" +
	"\x04plan\x18\x01 \x01(\v2\x1d.conduit.plans.v1.PlanSummaryR\x04plan\x12<\n" +
	"\tapprovals\x18\x02 \x03(\v2\x1e.conduit.plans.v1.PlanApprovalR\tapprovals\x12*\n" +
	"\x11change_request_id\x18\x03 \x01(\tR\x0fchangeRequestId\"\x82\x01\n" +
	"\x13PlanApprovalOutcome\x12\x17\n" +
	"\aplan_id\x18\x01 \x01(\tR\x06planId\x12+\n" +
	"\x11approval_recorded\x18\x02 \x01(\bR\x10approvalRecorded\x12%\n" +
	"\x0ehandoff_failed\x18\x03 \x01(\bR\rhandoffFailed2q\n" +
	"\x13PlanApprovalService\x12Z\n" +
	"\vApprovePlan\x12$.conduit.plans.v1.ApprovePlanRequest\x1a%.conduit.plans.v1.ApprovePlanResponseBDZBgithub.com/jonathanagustin/conduit/gen/go/conduit/plans/v1;plansv1b\x06proto3"

var (
	file_conduit_plans_v1_plan_approval_proto_rawDescOnce sync.Once
	file_conduit_plans_v1_plan_approval_proto_rawDescData []byte
)

func file_conduit_plans_v1_plan_approval_proto_rawDescGZIP() []byte {
	file_conduit_plans_v1_plan_approval_proto_rawDescOnce.Do(func() {
		file_conduit_plans_v1_plan_approval_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_conduit_plans_v1_plan_approval_proto_rawDesc), len(file_conduit_plans_v1_plan_approval_proto_rawDesc)))
	})
	return file_conduit_plans_v1_plan_approval_proto_rawDescData
}

var file_conduit_plans_v1_plan_approval_proto_msgTypes = make([]protoimpl.MessageInfo, 4)
var file_conduit_plans_v1_plan_approval_proto_goTypes = []any{
	(*ApprovePlanRequest)(nil),    // 0: conduit.plans.v1.ApprovePlanRequest
	(*PlanApproval)(nil),          // 1: conduit.plans.v1.PlanApproval
	(*ApprovePlanResponse)(nil),   // 2: conduit.plans.v1.ApprovePlanResponse
	(*PlanApprovalOutcome)(nil),   // 3: conduit.plans.v1.PlanApprovalOutcome
	(*timestamppb.Timestamp)(nil), // 4: google.protobuf.Timestamp
	(*PlanSummary)(nil),           // 5: conduit.plans.v1.PlanSummary
}
var file_conduit_plans_v1_plan_approval_proto_depIdxs = []int32{
	4, // 0: conduit.plans.v1.PlanApproval.created_at:type_name -> google.protobuf.Timestamp
	5, // 1: conduit.plans.v1.ApprovePlanResponse.plan:type_name -> conduit.plans.v1.PlanSummary
	1, // 2: conduit.plans.v1.ApprovePlanResponse.approvals:type_name -> conduit.plans.v1.PlanApproval
	0, // 3: conduit.plans.v1.PlanApprovalService.ApprovePlan:input_type -> conduit.plans.v1.ApprovePlanRequest
	2, // 4: conduit.plans.v1.PlanApprovalService.ApprovePlan:output_type -> conduit.plans.v1.ApprovePlanResponse
	4, // [4:5] is the sub-list for method output_type
	3, // [3:4] is the sub-list for method input_type
	3, // [3:3] is the sub-list for extension type_name
	3, // [3:3] is the sub-list for extension extendee
	0, // [0:3] is the sub-list for field type_name
}

func init() { file_conduit_plans_v1_plan_approval_proto_init() }
func file_conduit_plans_v1_plan_approval_proto_init() {
	if File_conduit_plans_v1_plan_approval_proto != nil {
		return
	}
	file_conduit_plans_v1_plan_summary_proto_init()
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_conduit_plans_v1_plan_approval_proto_rawDesc), len(file_conduit_plans_v1_plan_approval_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   4,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_conduit_plans_v1_plan_approval_proto_goTypes,
		DependencyIndexes: file_conduit_plans_v1_plan_approval_proto_depIdxs,
		MessageInfos:      file_conduit_plans_v1_plan_approval_proto_msgTypes,
	}.Build()
	File_conduit_plans_v1_plan_approval_proto = out.File
	file_conduit_plans_v1_plan_approval_proto_goTypes = nil
	file_conduit_plans_v1_plan_approval_proto_depIdxs = nil
}

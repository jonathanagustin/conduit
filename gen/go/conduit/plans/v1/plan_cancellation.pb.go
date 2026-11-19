//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.12
// 	protoc        (unknown)
// source: conduit/plans/v1/plan_cancellation.proto

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

type CancelPlanRequest struct {
	state  protoimpl.MessageState `protogen:"open.v1"`
	PlanId string                 `protobuf:"bytes,1,opt,name=plan_id,json=planId,proto3" json:"plan_id,omitempty"`
	Reason string                 `protobuf:"bytes,2,opt,name=reason,proto3" json:"reason,omitempty"`
	// Suppression additionally requires Operate on the Plan's Environment.
	CreateSuppression bool `protobuf:"varint,3,opt,name=create_suppression,json=createSuppression,proto3" json:"create_suppression,omitempty"`
	// Zero uses the existing 30-minute default when suppression is requested.
	SuppressionDurationSeconds int64  `protobuf:"varint,4,opt,name=suppression_duration_seconds,json=suppressionDurationSeconds,proto3" json:"suppression_duration_seconds,omitempty"`
	InstallationId             string `protobuf:"bytes,5,opt,name=installation_id,json=installationId,proto3" json:"installation_id,omitempty"`
	unknownFields              protoimpl.UnknownFields
	sizeCache                  protoimpl.SizeCache
}

func (x *CancelPlanRequest) Reset() {
	*x = CancelPlanRequest{}
	mi := &file_conduit_plans_v1_plan_cancellation_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *CancelPlanRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*CancelPlanRequest) ProtoMessage() {}

func (x *CancelPlanRequest) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_plans_v1_plan_cancellation_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use CancelPlanRequest.ProtoReflect.Descriptor instead.
func (*CancelPlanRequest) Descriptor() ([]byte, []int) {
	return file_conduit_plans_v1_plan_cancellation_proto_rawDescGZIP(), []int{0}
}

func (x *CancelPlanRequest) GetPlanId() string {
	if x != nil {
		return x.PlanId
	}
	return ""
}

func (x *CancelPlanRequest) GetReason() string {
	if x != nil {
		return x.Reason
	}
	return ""
}

func (x *CancelPlanRequest) GetCreateSuppression() bool {
	if x != nil {
		return x.CreateSuppression
	}
	return false
}

func (x *CancelPlanRequest) GetSuppressionDurationSeconds() int64 {
	if x != nil {
		return x.SuppressionDurationSeconds
	}
	return 0
}

func (x *CancelPlanRequest) GetInstallationId() string {
	if x != nil {
		return x.InstallationId
	}
	return ""
}

// RecordedPlanCancellation describes the native cancellation, not an approval.
type RecordedPlanCancellation struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Subject       string                 `protobuf:"bytes,1,opt,name=subject,proto3" json:"subject,omitempty"`
	Reason        string                 `protobuf:"bytes,2,opt,name=reason,proto3" json:"reason,omitempty"`
	CreatedAt     *timestamppb.Timestamp `protobuf:"bytes,3,opt,name=created_at,json=createdAt,proto3" json:"created_at,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *RecordedPlanCancellation) Reset() {
	*x = RecordedPlanCancellation{}
	mi := &file_conduit_plans_v1_plan_cancellation_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *RecordedPlanCancellation) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*RecordedPlanCancellation) ProtoMessage() {}

func (x *RecordedPlanCancellation) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_plans_v1_plan_cancellation_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use RecordedPlanCancellation.ProtoReflect.Descriptor instead.
func (*RecordedPlanCancellation) Descriptor() ([]byte, []int) {
	return file_conduit_plans_v1_plan_cancellation_proto_rawDescGZIP(), []int{1}
}

func (x *RecordedPlanCancellation) GetSubject() string {
	if x != nil {
		return x.Subject
	}
	return ""
}

func (x *RecordedPlanCancellation) GetReason() string {
	if x != nil {
		return x.Reason
	}
	return ""
}

func (x *RecordedPlanCancellation) GetCreatedAt() *timestamppb.Timestamp {
	if x != nil {
		return x.CreatedAt
	}
	return nil
}

// CancelPlanResponse is a scoped command result, not the complete REST Plan.
// Success follows all requested native effects, including suppression creation.
// Cancellation, Change Request cancellation and suppression are sequential.
type CancelPlanResponse struct {
	state           protoimpl.MessageState    `protogen:"open.v1"`
	Plan            *PlanSummary              `protobuf:"bytes,1,opt,name=plan,proto3" json:"plan,omitempty"`
	Cancellation    *RecordedPlanCancellation `protobuf:"bytes,2,opt,name=cancellation,proto3" json:"cancellation,omitempty"`
	ChangeRequestId string                    `protobuf:"bytes,3,opt,name=change_request_id,json=changeRequestId,proto3" json:"change_request_id,omitempty"`
	unknownFields   protoimpl.UnknownFields
	sizeCache       protoimpl.SizeCache
}

func (x *CancelPlanResponse) Reset() {
	*x = CancelPlanResponse{}
	mi := &file_conduit_plans_v1_plan_cancellation_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *CancelPlanResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*CancelPlanResponse) ProtoMessage() {}

func (x *CancelPlanResponse) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_plans_v1_plan_cancellation_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use CancelPlanResponse.ProtoReflect.Descriptor instead.
func (*CancelPlanResponse) Descriptor() ([]byte, []int) {
	return file_conduit_plans_v1_plan_cancellation_proto_rawDescGZIP(), []int{2}
}

func (x *CancelPlanResponse) GetPlan() *PlanSummary {
	if x != nil {
		return x.Plan
	}
	return nil
}

func (x *CancelPlanResponse) GetCancellation() *RecordedPlanCancellation {
	if x != nil {
		return x.Cancellation
	}
	return nil
}

func (x *CancelPlanResponse) GetChangeRequestId() string {
	if x != nil {
		return x.ChangeRequestId
	}
	return ""
}

// PlanCancellationOutcome is error evidence, never a retry permit. It is emitted
// only after the native command completed but its response could not be mapped.
// Absent details mean an unknown outcome, never proof that no write occurred:
// native command errors may follow a persisted cancellation or other effect.
type PlanCancellationOutcome struct {
	state            protoimpl.MessageState `protogen:"open.v1"`
	PlanId           string                 `protobuf:"bytes,1,opt,name=plan_id,json=planId,proto3" json:"plan_id,omitempty"`
	CommandCompleted bool                   `protobuf:"varint,2,opt,name=command_completed,json=commandCompleted,proto3" json:"command_completed,omitempty"`
	unknownFields    protoimpl.UnknownFields
	sizeCache        protoimpl.SizeCache
}

func (x *PlanCancellationOutcome) Reset() {
	*x = PlanCancellationOutcome{}
	mi := &file_conduit_plans_v1_plan_cancellation_proto_msgTypes[3]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *PlanCancellationOutcome) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*PlanCancellationOutcome) ProtoMessage() {}

func (x *PlanCancellationOutcome) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_plans_v1_plan_cancellation_proto_msgTypes[3]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use PlanCancellationOutcome.ProtoReflect.Descriptor instead.
func (*PlanCancellationOutcome) Descriptor() ([]byte, []int) {
	return file_conduit_plans_v1_plan_cancellation_proto_rawDescGZIP(), []int{3}
}

func (x *PlanCancellationOutcome) GetPlanId() string {
	if x != nil {
		return x.PlanId
	}
	return ""
}

func (x *PlanCancellationOutcome) GetCommandCompleted() bool {
	if x != nil {
		return x.CommandCompleted
	}
	return false
}

var File_conduit_plans_v1_plan_cancellation_proto protoreflect.FileDescriptor

const file_conduit_plans_v1_plan_cancellation_proto_rawDesc = "" +
	"\n" +
	"(conduit/plans/v1/plan_cancellation.proto\x12\x10conduit.plans.v1\x1a#conduit/plans/v1/plan_summary.proto\x1a\x1fgoogle/protobuf/timestamp.proto\"\xde\x01\n" +
	"\x11CancelPlanRequest\x12\x17\n" +
	"\aplan_id\x18\x01 \x01(\tR\x06planId\x12\x16\n" +
	"\x06reason\x18\x02 \x01(\tR\x06reason\x12-\n" +
	"\x12create_suppression\x18\x03 \x01(\bR\x11createSuppression\x12@\n" +
	"\x1csuppression_duration_seconds\x18\x04 \x01(\x03R\x1asuppressionDurationSeconds\x12'\n" +
	"\x0finstallation_id\x18\x05 \x01(\tR\x0einstallationId\"\x87\x01\n" +
	"\x18RecordedPlanCancellation\x12\x18\n" +
	"\asubject\x18\x01 \x01(\tR\asubject\x12\x16\n" +
	"\x06reason\x18\x02 \x01(\tR\x06reason\x129\n" +
	"\n" +
	"created_at\x18\x03 \x01(\v2\x1a.google.protobuf.TimestampR\tcreatedAt\"\xc3\x01\n" +
	"\x12CancelPlanResponse\x121\n" +
	"\x04plan\x18\x01 \x01(\v2\x1d.conduit.plans.v1.PlanSummaryR\x04plan\x12N\n" +
	"\fcancellation\x18\x02 \x01(\v2*.conduit.plans.v1.RecordedPlanCancellationR\fcancellation\x12*\n" +
	"\x11change_request_id\x18\x03 \x01(\tR\x0fchangeRequestId\"_\n" +
	"\x17PlanCancellationOutcome\x12\x17\n" +
	"\aplan_id\x18\x01 \x01(\tR\x06planId\x12+\n" +
	"\x11command_completed\x18\x02 \x01(\bR\x10commandCompleted2r\n" +
	"\x17PlanCancellationService\x12W\n" +
	"\n" +
	"CancelPlan\x12#.conduit.plans.v1.CancelPlanRequest\x1a$.conduit.plans.v1.CancelPlanResponseBDZBgithub.com/jonathanagustin/conduit/gen/go/conduit/plans/v1;plansv1b\x06proto3"

var (
	file_conduit_plans_v1_plan_cancellation_proto_rawDescOnce sync.Once
	file_conduit_plans_v1_plan_cancellation_proto_rawDescData []byte
)

func file_conduit_plans_v1_plan_cancellation_proto_rawDescGZIP() []byte {
	file_conduit_plans_v1_plan_cancellation_proto_rawDescOnce.Do(func() {
		file_conduit_plans_v1_plan_cancellation_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_conduit_plans_v1_plan_cancellation_proto_rawDesc), len(file_conduit_plans_v1_plan_cancellation_proto_rawDesc)))
	})
	return file_conduit_plans_v1_plan_cancellation_proto_rawDescData
}

var file_conduit_plans_v1_plan_cancellation_proto_msgTypes = make([]protoimpl.MessageInfo, 4)
var file_conduit_plans_v1_plan_cancellation_proto_goTypes = []any{
	(*CancelPlanRequest)(nil),        // 0: conduit.plans.v1.CancelPlanRequest
	(*RecordedPlanCancellation)(nil), // 1: conduit.plans.v1.RecordedPlanCancellation
	(*CancelPlanResponse)(nil),       // 2: conduit.plans.v1.CancelPlanResponse
	(*PlanCancellationOutcome)(nil),  // 3: conduit.plans.v1.PlanCancellationOutcome
	(*timestamppb.Timestamp)(nil),    // 4: google.protobuf.Timestamp
	(*PlanSummary)(nil),              // 5: conduit.plans.v1.PlanSummary
}
var file_conduit_plans_v1_plan_cancellation_proto_depIdxs = []int32{
	4, // 0: conduit.plans.v1.RecordedPlanCancellation.created_at:type_name -> google.protobuf.Timestamp
	5, // 1: conduit.plans.v1.CancelPlanResponse.plan:type_name -> conduit.plans.v1.PlanSummary
	1, // 2: conduit.plans.v1.CancelPlanResponse.cancellation:type_name -> conduit.plans.v1.RecordedPlanCancellation
	0, // 3: conduit.plans.v1.PlanCancellationService.CancelPlan:input_type -> conduit.plans.v1.CancelPlanRequest
	2, // 4: conduit.plans.v1.PlanCancellationService.CancelPlan:output_type -> conduit.plans.v1.CancelPlanResponse
	4, // [4:5] is the sub-list for method output_type
	3, // [3:4] is the sub-list for method input_type
	3, // [3:3] is the sub-list for extension type_name
	3, // [3:3] is the sub-list for extension extendee
	0, // [0:3] is the sub-list for field type_name
}

func init() { file_conduit_plans_v1_plan_cancellation_proto_init() }
func file_conduit_plans_v1_plan_cancellation_proto_init() {
	if File_conduit_plans_v1_plan_cancellation_proto != nil {
		return
	}
	file_conduit_plans_v1_plan_summary_proto_init()
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_conduit_plans_v1_plan_cancellation_proto_rawDesc), len(file_conduit_plans_v1_plan_cancellation_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   4,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_conduit_plans_v1_plan_cancellation_proto_goTypes,
		DependencyIndexes: file_conduit_plans_v1_plan_cancellation_proto_depIdxs,
		MessageInfos:      file_conduit_plans_v1_plan_cancellation_proto_msgTypes,
	}.Build()
	File_conduit_plans_v1_plan_cancellation_proto = out.File
	file_conduit_plans_v1_plan_cancellation_proto_goTypes = nil
	file_conduit_plans_v1_plan_cancellation_proto_depIdxs = nil
}

//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.12
// 	protoc        (unknown)
// source: conduit/reporting/v1/overview.proto

// Package conduit.reporting.v1 describes the existing operator-filtered overview.

package reportingv1

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
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

// GetOverviewRequest preserves the native Reporting overview shape.
type GetOverviewRequest struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *GetOverviewRequest) Reset() {
	*x = GetOverviewRequest{}
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *GetOverviewRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetOverviewRequest) ProtoMessage() {}

func (x *GetOverviewRequest) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetOverviewRequest.ProtoReflect.Descriptor instead.
func (*GetOverviewRequest) Descriptor() ([]byte, []int) {
	return file_conduit_reporting_v1_overview_proto_rawDescGZIP(), []int{0}
}

// GetOverviewResponse preserves the native Reporting overview shape.
type GetOverviewResponse struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// generated_at maps the corresponding native JSON field.
	GeneratedAt *timestamppb.Timestamp `protobuf:"bytes,1,opt,name=generated_at,json=generatedAt,proto3" json:"generated_at,omitempty"`
	// stale_after_seconds maps the corresponding native JSON field.
	StaleAfterSeconds int64 `protobuf:"varint,2,opt,name=stale_after_seconds,json=staleAfterSeconds,proto3" json:"stale_after_seconds,omitempty"`
	// awaiting_approvals maps the corresponding native JSON field.
	AwaitingApprovals *ChangeRequestPanel `protobuf:"bytes,3,opt,name=awaiting_approvals,json=awaitingApprovals,proto3" json:"awaiting_approvals,omitempty"`
	// failed_plans maps the corresponding native JSON field.
	FailedPlans *PlanPanel `protobuf:"bytes,4,opt,name=failed_plans,json=failedPlans,proto3" json:"failed_plans,omitempty"`
	// running_plans maps the corresponding native JSON field.
	RunningPlans *PlanPanel `protobuf:"bytes,5,opt,name=running_plans,json=runningPlans,proto3" json:"running_plans,omitempty"`
	// unhealthy_entities maps the corresponding native JSON field.
	UnhealthyEntities *InstallationPanel `protobuf:"bytes,6,opt,name=unhealthy_entities,json=unhealthyEntities,proto3" json:"unhealthy_entities,omitempty"`
	// stale_reports maps the corresponding native JSON field.
	StaleReports *StaleReportPanel `protobuf:"bytes,7,opt,name=stale_reports,json=staleReports,proto3" json:"stale_reports,omitempty"`
	// blocked_promotions maps the corresponding native JSON field.
	BlockedPromotions *PromotionPanel `protobuf:"bytes,8,opt,name=blocked_promotions,json=blockedPromotions,proto3" json:"blocked_promotions,omitempty"`
	unknownFields     protoimpl.UnknownFields
	sizeCache         protoimpl.SizeCache
}

func (x *GetOverviewResponse) Reset() {
	*x = GetOverviewResponse{}
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *GetOverviewResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetOverviewResponse) ProtoMessage() {}

func (x *GetOverviewResponse) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetOverviewResponse.ProtoReflect.Descriptor instead.
func (*GetOverviewResponse) Descriptor() ([]byte, []int) {
	return file_conduit_reporting_v1_overview_proto_rawDescGZIP(), []int{1}
}

func (x *GetOverviewResponse) GetGeneratedAt() *timestamppb.Timestamp {
	if x != nil {
		return x.GeneratedAt
	}
	return nil
}

func (x *GetOverviewResponse) GetStaleAfterSeconds() int64 {
	if x != nil {
		return x.StaleAfterSeconds
	}
	return 0
}

func (x *GetOverviewResponse) GetAwaitingApprovals() *ChangeRequestPanel {
	if x != nil {
		return x.AwaitingApprovals
	}
	return nil
}

func (x *GetOverviewResponse) GetFailedPlans() *PlanPanel {
	if x != nil {
		return x.FailedPlans
	}
	return nil
}

func (x *GetOverviewResponse) GetRunningPlans() *PlanPanel {
	if x != nil {
		return x.RunningPlans
	}
	return nil
}

func (x *GetOverviewResponse) GetUnhealthyEntities() *InstallationPanel {
	if x != nil {
		return x.UnhealthyEntities
	}
	return nil
}

func (x *GetOverviewResponse) GetStaleReports() *StaleReportPanel {
	if x != nil {
		return x.StaleReports
	}
	return nil
}

func (x *GetOverviewResponse) GetBlockedPromotions() *PromotionPanel {
	if x != nil {
		return x.BlockedPromotions
	}
	return nil
}

// OverviewChangeRequest preserves the native Reporting overview shape.
type OverviewChangeRequest struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// id maps the corresponding native JSON field.
	Id string `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	// title maps the corresponding native JSON field.
	Title string `protobuf:"bytes,2,opt,name=title,proto3" json:"title,omitempty"`
	// environment maps the corresponding native JSON field.
	Environment   string `protobuf:"bytes,3,opt,name=environment,proto3" json:"environment,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *OverviewChangeRequest) Reset() {
	*x = OverviewChangeRequest{}
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *OverviewChangeRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*OverviewChangeRequest) ProtoMessage() {}

func (x *OverviewChangeRequest) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use OverviewChangeRequest.ProtoReflect.Descriptor instead.
func (*OverviewChangeRequest) Descriptor() ([]byte, []int) {
	return file_conduit_reporting_v1_overview_proto_rawDescGZIP(), []int{2}
}

func (x *OverviewChangeRequest) GetId() string {
	if x != nil {
		return x.Id
	}
	return ""
}

func (x *OverviewChangeRequest) GetTitle() string {
	if x != nil {
		return x.Title
	}
	return ""
}

func (x *OverviewChangeRequest) GetEnvironment() string {
	if x != nil {
		return x.Environment
	}
	return ""
}

// OverviewPlan preserves the native Reporting overview shape.
type OverviewPlan struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// id maps the corresponding native JSON field.
	Id string `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	// environment maps the corresponding native JSON field.
	Environment   string `protobuf:"bytes,2,opt,name=environment,proto3" json:"environment,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *OverviewPlan) Reset() {
	*x = OverviewPlan{}
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[3]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *OverviewPlan) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*OverviewPlan) ProtoMessage() {}

func (x *OverviewPlan) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[3]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use OverviewPlan.ProtoReflect.Descriptor instead.
func (*OverviewPlan) Descriptor() ([]byte, []int) {
	return file_conduit_reporting_v1_overview_proto_rawDescGZIP(), []int{3}
}

func (x *OverviewPlan) GetId() string {
	if x != nil {
		return x.Id
	}
	return ""
}

func (x *OverviewPlan) GetEnvironment() string {
	if x != nil {
		return x.Environment
	}
	return ""
}

// OverviewInstallation preserves the native Reporting overview shape.
type OverviewInstallation struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// environment maps the corresponding native JSON field.
	Environment string `protobuf:"bytes,1,opt,name=environment,proto3" json:"environment,omitempty"`
	// installation_id maps the corresponding native JSON field.
	InstallationId string `protobuf:"bytes,2,opt,name=installation_id,json=installationId,proto3" json:"installation_id,omitempty"`
	// health maps the corresponding native JSON field.
	Health        string `protobuf:"bytes,3,opt,name=health,proto3" json:"health,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *OverviewInstallation) Reset() {
	*x = OverviewInstallation{}
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[4]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *OverviewInstallation) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*OverviewInstallation) ProtoMessage() {}

func (x *OverviewInstallation) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[4]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use OverviewInstallation.ProtoReflect.Descriptor instead.
func (*OverviewInstallation) Descriptor() ([]byte, []int) {
	return file_conduit_reporting_v1_overview_proto_rawDescGZIP(), []int{4}
}

func (x *OverviewInstallation) GetEnvironment() string {
	if x != nil {
		return x.Environment
	}
	return ""
}

func (x *OverviewInstallation) GetInstallationId() string {
	if x != nil {
		return x.InstallationId
	}
	return ""
}

func (x *OverviewInstallation) GetHealth() string {
	if x != nil {
		return x.Health
	}
	return ""
}

// OverviewPromotion preserves the native Reporting overview shape.
type OverviewPromotion struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// id maps the corresponding native JSON field.
	Id string `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	// product maps the corresponding native JSON field.
	Product string `protobuf:"bytes,2,opt,name=product,proto3" json:"product,omitempty"`
	// version maps the corresponding native JSON field.
	Version string `protobuf:"bytes,3,opt,name=version,proto3" json:"version,omitempty"`
	// from_channel maps the corresponding native JSON field.
	FromChannel string `protobuf:"bytes,4,opt,name=from_channel,json=fromChannel,proto3" json:"from_channel,omitempty"`
	// to_channel maps the corresponding native JSON field.
	ToChannel     string `protobuf:"bytes,5,opt,name=to_channel,json=toChannel,proto3" json:"to_channel,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *OverviewPromotion) Reset() {
	*x = OverviewPromotion{}
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[5]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *OverviewPromotion) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*OverviewPromotion) ProtoMessage() {}

func (x *OverviewPromotion) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[5]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use OverviewPromotion.ProtoReflect.Descriptor instead.
func (*OverviewPromotion) Descriptor() ([]byte, []int) {
	return file_conduit_reporting_v1_overview_proto_rawDescGZIP(), []int{5}
}

func (x *OverviewPromotion) GetId() string {
	if x != nil {
		return x.Id
	}
	return ""
}

func (x *OverviewPromotion) GetProduct() string {
	if x != nil {
		return x.Product
	}
	return ""
}

func (x *OverviewPromotion) GetVersion() string {
	if x != nil {
		return x.Version
	}
	return ""
}

func (x *OverviewPromotion) GetFromChannel() string {
	if x != nil {
		return x.FromChannel
	}
	return ""
}

func (x *OverviewPromotion) GetToChannel() string {
	if x != nil {
		return x.ToChannel
	}
	return ""
}

// ChangeRequestPanel preserves the native Reporting overview shape.
type ChangeRequestPanel struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// status is CURRENT or UNAVAILABLE; unavailable is not an empty successful panel.
	Status string `protobuf:"bytes,1,opt,name=status,proto3" json:"status,omitempty"`
	// count maps the corresponding native JSON field.
	Count int64 `protobuf:"varint,2,opt,name=count,proto3" json:"count,omitempty"`
	// items maps the corresponding native JSON field.
	Items         []*OverviewChangeRequest `protobuf:"bytes,3,rep,name=items,proto3" json:"items,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ChangeRequestPanel) Reset() {
	*x = ChangeRequestPanel{}
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[6]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ChangeRequestPanel) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ChangeRequestPanel) ProtoMessage() {}

func (x *ChangeRequestPanel) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[6]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ChangeRequestPanel.ProtoReflect.Descriptor instead.
func (*ChangeRequestPanel) Descriptor() ([]byte, []int) {
	return file_conduit_reporting_v1_overview_proto_rawDescGZIP(), []int{6}
}

func (x *ChangeRequestPanel) GetStatus() string {
	if x != nil {
		return x.Status
	}
	return ""
}

func (x *ChangeRequestPanel) GetCount() int64 {
	if x != nil {
		return x.Count
	}
	return 0
}

func (x *ChangeRequestPanel) GetItems() []*OverviewChangeRequest {
	if x != nil {
		return x.Items
	}
	return nil
}

// PlanPanel preserves the native Reporting overview shape.
type PlanPanel struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// status is CURRENT or UNAVAILABLE; unavailable is not an empty successful panel.
	Status string `protobuf:"bytes,1,opt,name=status,proto3" json:"status,omitempty"`
	// count maps the corresponding native JSON field.
	Count int64 `protobuf:"varint,2,opt,name=count,proto3" json:"count,omitempty"`
	// items maps the corresponding native JSON field.
	Items         []*OverviewPlan `protobuf:"bytes,3,rep,name=items,proto3" json:"items,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *PlanPanel) Reset() {
	*x = PlanPanel{}
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[7]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *PlanPanel) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*PlanPanel) ProtoMessage() {}

func (x *PlanPanel) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[7]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use PlanPanel.ProtoReflect.Descriptor instead.
func (*PlanPanel) Descriptor() ([]byte, []int) {
	return file_conduit_reporting_v1_overview_proto_rawDescGZIP(), []int{7}
}

func (x *PlanPanel) GetStatus() string {
	if x != nil {
		return x.Status
	}
	return ""
}

func (x *PlanPanel) GetCount() int64 {
	if x != nil {
		return x.Count
	}
	return 0
}

func (x *PlanPanel) GetItems() []*OverviewPlan {
	if x != nil {
		return x.Items
	}
	return nil
}

// InstallationPanel preserves the native Reporting overview shape.
type InstallationPanel struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// status is CURRENT or UNAVAILABLE; unavailable is not an empty successful panel.
	Status string `protobuf:"bytes,1,opt,name=status,proto3" json:"status,omitempty"`
	// count maps the corresponding native JSON field.
	Count int64 `protobuf:"varint,2,opt,name=count,proto3" json:"count,omitempty"`
	// items maps the corresponding native JSON field.
	Items         []*OverviewInstallation `protobuf:"bytes,3,rep,name=items,proto3" json:"items,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *InstallationPanel) Reset() {
	*x = InstallationPanel{}
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[8]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *InstallationPanel) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*InstallationPanel) ProtoMessage() {}

func (x *InstallationPanel) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[8]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use InstallationPanel.ProtoReflect.Descriptor instead.
func (*InstallationPanel) Descriptor() ([]byte, []int) {
	return file_conduit_reporting_v1_overview_proto_rawDescGZIP(), []int{8}
}

func (x *InstallationPanel) GetStatus() string {
	if x != nil {
		return x.Status
	}
	return ""
}

func (x *InstallationPanel) GetCount() int64 {
	if x != nil {
		return x.Count
	}
	return 0
}

func (x *InstallationPanel) GetItems() []*OverviewInstallation {
	if x != nil {
		return x.Items
	}
	return nil
}

// StaleReportPanel preserves the native Reporting overview shape.
type StaleReportPanel struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// status is CURRENT or UNAVAILABLE; unavailable is not an empty successful panel.
	Status string `protobuf:"bytes,1,opt,name=status,proto3" json:"status,omitempty"`
	// count maps the corresponding native JSON field.
	Count int64 `protobuf:"varint,2,opt,name=count,proto3" json:"count,omitempty"`
	// items maps the corresponding native JSON field.
	Items         []*emptypb.Empty `protobuf:"bytes,3,rep,name=items,proto3" json:"items,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *StaleReportPanel) Reset() {
	*x = StaleReportPanel{}
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[9]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *StaleReportPanel) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*StaleReportPanel) ProtoMessage() {}

func (x *StaleReportPanel) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[9]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use StaleReportPanel.ProtoReflect.Descriptor instead.
func (*StaleReportPanel) Descriptor() ([]byte, []int) {
	return file_conduit_reporting_v1_overview_proto_rawDescGZIP(), []int{9}
}

func (x *StaleReportPanel) GetStatus() string {
	if x != nil {
		return x.Status
	}
	return ""
}

func (x *StaleReportPanel) GetCount() int64 {
	if x != nil {
		return x.Count
	}
	return 0
}

func (x *StaleReportPanel) GetItems() []*emptypb.Empty {
	if x != nil {
		return x.Items
	}
	return nil
}

// PromotionPanel preserves the native Reporting overview shape.
type PromotionPanel struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// status is CURRENT or UNAVAILABLE; unavailable is not an empty successful panel.
	Status string `protobuf:"bytes,1,opt,name=status,proto3" json:"status,omitempty"`
	// count maps the corresponding native JSON field.
	Count int64 `protobuf:"varint,2,opt,name=count,proto3" json:"count,omitempty"`
	// items maps the corresponding native JSON field.
	Items         []*OverviewPromotion `protobuf:"bytes,3,rep,name=items,proto3" json:"items,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *PromotionPanel) Reset() {
	*x = PromotionPanel{}
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[10]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *PromotionPanel) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*PromotionPanel) ProtoMessage() {}

func (x *PromotionPanel) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_reporting_v1_overview_proto_msgTypes[10]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use PromotionPanel.ProtoReflect.Descriptor instead.
func (*PromotionPanel) Descriptor() ([]byte, []int) {
	return file_conduit_reporting_v1_overview_proto_rawDescGZIP(), []int{10}
}

func (x *PromotionPanel) GetStatus() string {
	if x != nil {
		return x.Status
	}
	return ""
}

func (x *PromotionPanel) GetCount() int64 {
	if x != nil {
		return x.Count
	}
	return 0
}

func (x *PromotionPanel) GetItems() []*OverviewPromotion {
	if x != nil {
		return x.Items
	}
	return nil
}

var File_conduit_reporting_v1_overview_proto protoreflect.FileDescriptor

const file_conduit_reporting_v1_overview_proto_rawDesc = "" +
	"\n" +
	"#conduit/reporting/v1/overview.proto\x12\x14conduit.reporting.v1\x1a\x1bgoogle/protobuf/empty.proto\x1a\x1fgoogle/protobuf/timestamp.proto\"\x14\n" +
	"\x12GetOverviewRequest\"\xe1\x04\n" +
	"\x13GetOverviewResponse\x12=\n" +
	"\fgenerated_at\x18\x01 \x01(\v2\x1a.google.protobuf.TimestampR\vgeneratedAt\x12.\n" +
	"\x13stale_after_seconds\x18\x02 \x01(\x03R\x11staleAfterSeconds\x12W\n" +
	"\x12awaiting_approvals\x18\x03 \x01(\v2(.conduit.reporting.v1.ChangeRequestPanelR\x11awaitingApprovals\x12B\n" +
	"\ffailed_plans\x18\x04 \x01(\v2\x1f.conduit.reporting.v1.PlanPanelR\vfailedPlans\x12D\n" +
	"\rrunning_plans\x18\x05 \x01(\v2\x1f.conduit.reporting.v1.PlanPanelR\frunningPlans\x12V\n" +
	"\x12unhealthy_entities\x18\x06 \x01(\v2'.conduit.reporting.v1.InstallationPanelR\x11unhealthyEntities\x12K\n" +
	"\rstale_reports\x18\a \x01(\v2&.conduit.reporting.v1.StaleReportPanelR\fstaleReports\x12S\n" +
	"\x12blocked_promotions\x18\b \x01(\v2$.conduit.reporting.v1.PromotionPanelR\x11blockedPromotions\"_\n" +
	"\x15OverviewChangeRequest\x12\x0e\n" +
	"\x02id\x18\x01 \x01(\tR\x02id\x12\x14\n" +
	"\x05title\x18\x02 \x01(\tR\x05title\x12 \n" +
	"\venvironment\x18\x03 \x01(\tR\venvironment\"@\n" +
	"\fOverviewPlan\x12\x0e\n" +
	"\x02id\x18\x01 \x01(\tR\x02id\x12 \n" +
	"\venvironment\x18\x02 \x01(\tR\venvironment\"y\n" +
	"\x14OverviewInstallation\x12 \n" +
	"\venvironment\x18\x01 \x01(\tR\venvironment\x12'\n" +
	"\x0finstallation_id\x18\x02 \x01(\tR\x0einstallationId\x12\x16\n" +
	"\x06health\x18\x03 \x01(\tR\x06health\"\x99\x01\n" +
	"\x11OverviewPromotion\x12\x0e\n" +
	"\x02id\x18\x01 \x01(\tR\x02id\x12\x18\n" +
	"\aproduct\x18\x02 \x01(\tR\aproduct\x12\x18\n" +
	"\aversion\x18\x03 \x01(\tR\aversion\x12!\n" +
	"\ffrom_channel\x18\x04 \x01(\tR\vfromChannel\x12\x1d\n" +
	"\n" +
	"to_channel\x18\x05 \x01(\tR\ttoChannel\"\x85\x01\n" +
	"\x12ChangeRequestPanel\x12\x16\n" +
	"\x06status\x18\x01 \x01(\tR\x06status\x12\x14\n" +
	"\x05count\x18\x02 \x01(\x03R\x05count\x12A\n" +
	"\x05items\x18\x03 \x03(\v2+.conduit.reporting.v1.OverviewChangeRequestR\x05items\"s\n" +
	"\tPlanPanel\x12\x16\n" +
	"\x06status\x18\x01 \x01(\tR\x06status\x12\x14\n" +
	"\x05count\x18\x02 \x01(\x03R\x05count\x128\n" +
	"\x05items\x18\x03 \x03(\v2\".conduit.reporting.v1.OverviewPlanR\x05items\"\x83\x01\n" +
	"\x11InstallationPanel\x12\x16\n" +
	"\x06status\x18\x01 \x01(\tR\x06status\x12\x14\n" +
	"\x05count\x18\x02 \x01(\x03R\x05count\x12@\n" +
	"\x05items\x18\x03 \x03(\v2*.conduit.reporting.v1.OverviewInstallationR\x05items\"n\n" +
	"\x10StaleReportPanel\x12\x16\n" +
	"\x06status\x18\x01 \x01(\tR\x06status\x12\x14\n" +
	"\x05count\x18\x02 \x01(\x03R\x05count\x12,\n" +
	"\x05items\x18\x03 \x03(\v2\x16.google.protobuf.EmptyR\x05items\"}\n" +
	"\x0ePromotionPanel\x12\x16\n" +
	"\x06status\x18\x01 \x01(\tR\x06status\x12\x14\n" +
	"\x05count\x18\x02 \x01(\x03R\x05count\x12=\n" +
	"\x05items\x18\x03 \x03(\v2'.conduit.reporting.v1.OverviewPromotionR\x05items2{\n" +
	"\x10ReportingService\x12g\n" +
	"\vGetOverview\x12(.conduit.reporting.v1.GetOverviewRequest\x1a).conduit.reporting.v1.GetOverviewResponse\"\x03\x90\x02\x01BLZJgithub.com/jonathanagustin/conduit/gen/go/conduit/reporting/v1;reportingv1b\x06proto3"

var (
	file_conduit_reporting_v1_overview_proto_rawDescOnce sync.Once
	file_conduit_reporting_v1_overview_proto_rawDescData []byte
)

func file_conduit_reporting_v1_overview_proto_rawDescGZIP() []byte {
	file_conduit_reporting_v1_overview_proto_rawDescOnce.Do(func() {
		file_conduit_reporting_v1_overview_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_conduit_reporting_v1_overview_proto_rawDesc), len(file_conduit_reporting_v1_overview_proto_rawDesc)))
	})
	return file_conduit_reporting_v1_overview_proto_rawDescData
}

var file_conduit_reporting_v1_overview_proto_msgTypes = make([]protoimpl.MessageInfo, 11)
var file_conduit_reporting_v1_overview_proto_goTypes = []any{
	(*GetOverviewRequest)(nil),    // 0: conduit.reporting.v1.GetOverviewRequest
	(*GetOverviewResponse)(nil),   // 1: conduit.reporting.v1.GetOverviewResponse
	(*OverviewChangeRequest)(nil), // 2: conduit.reporting.v1.OverviewChangeRequest
	(*OverviewPlan)(nil),          // 3: conduit.reporting.v1.OverviewPlan
	(*OverviewInstallation)(nil),  // 4: conduit.reporting.v1.OverviewInstallation
	(*OverviewPromotion)(nil),     // 5: conduit.reporting.v1.OverviewPromotion
	(*ChangeRequestPanel)(nil),    // 6: conduit.reporting.v1.ChangeRequestPanel
	(*PlanPanel)(nil),             // 7: conduit.reporting.v1.PlanPanel
	(*InstallationPanel)(nil),     // 8: conduit.reporting.v1.InstallationPanel
	(*StaleReportPanel)(nil),      // 9: conduit.reporting.v1.StaleReportPanel
	(*PromotionPanel)(nil),        // 10: conduit.reporting.v1.PromotionPanel
	(*timestamppb.Timestamp)(nil), // 11: google.protobuf.Timestamp
	(*emptypb.Empty)(nil),         // 12: google.protobuf.Empty
}
var file_conduit_reporting_v1_overview_proto_depIdxs = []int32{
	11, // 0: conduit.reporting.v1.GetOverviewResponse.generated_at:type_name -> google.protobuf.Timestamp
	6,  // 1: conduit.reporting.v1.GetOverviewResponse.awaiting_approvals:type_name -> conduit.reporting.v1.ChangeRequestPanel
	7,  // 2: conduit.reporting.v1.GetOverviewResponse.failed_plans:type_name -> conduit.reporting.v1.PlanPanel
	7,  // 3: conduit.reporting.v1.GetOverviewResponse.running_plans:type_name -> conduit.reporting.v1.PlanPanel
	8,  // 4: conduit.reporting.v1.GetOverviewResponse.unhealthy_entities:type_name -> conduit.reporting.v1.InstallationPanel
	9,  // 5: conduit.reporting.v1.GetOverviewResponse.stale_reports:type_name -> conduit.reporting.v1.StaleReportPanel
	10, // 6: conduit.reporting.v1.GetOverviewResponse.blocked_promotions:type_name -> conduit.reporting.v1.PromotionPanel
	2,  // 7: conduit.reporting.v1.ChangeRequestPanel.items:type_name -> conduit.reporting.v1.OverviewChangeRequest
	3,  // 8: conduit.reporting.v1.PlanPanel.items:type_name -> conduit.reporting.v1.OverviewPlan
	4,  // 9: conduit.reporting.v1.InstallationPanel.items:type_name -> conduit.reporting.v1.OverviewInstallation
	12, // 10: conduit.reporting.v1.StaleReportPanel.items:type_name -> google.protobuf.Empty
	5,  // 11: conduit.reporting.v1.PromotionPanel.items:type_name -> conduit.reporting.v1.OverviewPromotion
	0,  // 12: conduit.reporting.v1.ReportingService.GetOverview:input_type -> conduit.reporting.v1.GetOverviewRequest
	1,  // 13: conduit.reporting.v1.ReportingService.GetOverview:output_type -> conduit.reporting.v1.GetOverviewResponse
	13, // [13:14] is the sub-list for method output_type
	12, // [12:13] is the sub-list for method input_type
	12, // [12:12] is the sub-list for extension type_name
	12, // [12:12] is the sub-list for extension extendee
	0,  // [0:12] is the sub-list for field type_name
}

func init() { file_conduit_reporting_v1_overview_proto_init() }
func file_conduit_reporting_v1_overview_proto_init() {
	if File_conduit_reporting_v1_overview_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_conduit_reporting_v1_overview_proto_rawDesc), len(file_conduit_reporting_v1_overview_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   11,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_conduit_reporting_v1_overview_proto_goTypes,
		DependencyIndexes: file_conduit_reporting_v1_overview_proto_depIdxs,
		MessageInfos:      file_conduit_reporting_v1_overview_proto_msgTypes,
	}.Build()
	File_conduit_reporting_v1_overview_proto = out.File
	file_conduit_reporting_v1_overview_proto_goTypes = nil
	file_conduit_reporting_v1_overview_proto_depIdxs = nil
}

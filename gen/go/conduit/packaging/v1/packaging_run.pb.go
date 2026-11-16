//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.12
// 	protoc        (unknown)
// source: conduit/packaging/v1/packaging_run.proto

// Package conduit.packaging.v1 contains packaging-run read contracts.
// Other Hub domains use the same proto/conduit/<domain>/v1 layout.

package packagingv1

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

// GetPackagingRunRequest names the run to read.
type GetPackagingRunRequest struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// run_id is the path value of GET /v1/packaging-runs/{run_id}.
	RunId         string `protobuf:"bytes,1,opt,name=run_id,json=runId,proto3" json:"run_id,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *GetPackagingRunRequest) Reset() {
	*x = GetPackagingRunRequest{}
	mi := &file_conduit_packaging_v1_packaging_run_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *GetPackagingRunRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetPackagingRunRequest) ProtoMessage() {}

func (x *GetPackagingRunRequest) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_packaging_v1_packaging_run_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetPackagingRunRequest.ProtoReflect.Descriptor instead.
func (*GetPackagingRunRequest) Descriptor() ([]byte, []int) {
	return file_conduit_packaging_v1_packaging_run_proto_rawDescGZIP(), []int{0}
}

func (x *GetPackagingRunRequest) GetRunId() string {
	if x != nil {
		return x.RunId
	}
	return ""
}

// GetPackagingRunResponse is the REST body of GET /v1/packaging-runs/{run_id}.
type GetPackagingRunResponse struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// tier is the REST field tier.
	Tier string `protobuf:"bytes,1,opt,name=tier,proto3" json:"tier,omitempty"`
	// configured is the REST field configured.
	Configured bool `protobuf:"varint,2,opt,name=configured,proto3" json:"configured,omitempty"`
	// authority is the REST field authority.
	Authority string `protobuf:"bytes,3,opt,name=authority,proto3" json:"authority,omitempty"`
	// plan_authority is the REST field planAuthority.
	PlanAuthority string `protobuf:"bytes,4,opt,name=plan_authority,json=planAuthority,proto3" json:"plan_authority,omitempty"`
	// approval_authority is the REST field approvalAuthority.
	ApprovalAuthority string `protobuf:"bytes,5,opt,name=approval_authority,json=approvalAuthority,proto3" json:"approval_authority,omitempty"`
	// run_id is the REST field runId.
	RunId string `protobuf:"bytes,6,opt,name=run_id,json=runId,proto3" json:"run_id,omitempty"`
	// phase is the REST field phase.
	Phase string `protobuf:"bytes,7,opt,name=phase,proto3" json:"phase,omitempty"`
	// outcome is the REST field outcome.
	Outcome string `protobuf:"bytes,8,opt,name=outcome,proto3" json:"outcome,omitempty"`
	// reason is the REST field reason.
	Reason string `protobuf:"bytes,9,opt,name=reason,proto3" json:"reason,omitempty"`
	// source_digest is the REST field sourceDigest.
	SourceDigest string `protobuf:"bytes,10,opt,name=source_digest,json=sourceDigest,proto3" json:"source_digest,omitempty"`
	// artifact_digest is the REST field artifactDigest.
	ArtifactDigest string `protobuf:"bytes,11,opt,name=artifact_digest,json=artifactDigest,proto3" json:"artifact_digest,omitempty"`
	// proposal_revision is the REST field proposalRevision.
	ProposalRevision string `protobuf:"bytes,12,opt,name=proposal_revision,json=proposalRevision,proto3" json:"proposal_revision,omitempty"`
	// target_plan_ref is the REST field targetPlanRef.
	TargetPlanRef string `protobuf:"bytes,13,opt,name=target_plan_ref,json=targetPlanRef,proto3" json:"target_plan_ref,omitempty"`
	// plan_id is the REST field planId.
	PlanId string `protobuf:"bytes,14,opt,name=plan_id,json=planId,proto3" json:"plan_id,omitempty"`
	// handoff_reconciled is the REST field handoffReconciled.
	HandoffReconciled bool `protobuf:"varint,15,opt,name=handoff_reconciled,json=handoffReconciled,proto3" json:"handoff_reconciled,omitempty"`
	// last_review is the REST field lastReview.
	LastReview string `protobuf:"bytes,16,opt,name=last_review,json=lastReview,proto3" json:"last_review,omitempty"`
	// duplicate_reviews is the REST field duplicateReviews.
	DuplicateReviews int32 `protobuf:"varint,17,opt,name=duplicate_reviews,json=duplicateReviews,proto3" json:"duplicate_reviews,omitempty"`
	// template_version is the REST field templateVersion.
	TemplateVersion string `protobuf:"bytes,18,opt,name=template_version,json=templateVersion,proto3" json:"template_version,omitempty"`
	// tool_versions is the REST field toolVersions.
	ToolVersions  string `protobuf:"bytes,19,opt,name=tool_versions,json=toolVersions,proto3" json:"tool_versions,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *GetPackagingRunResponse) Reset() {
	*x = GetPackagingRunResponse{}
	mi := &file_conduit_packaging_v1_packaging_run_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *GetPackagingRunResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetPackagingRunResponse) ProtoMessage() {}

func (x *GetPackagingRunResponse) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_packaging_v1_packaging_run_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetPackagingRunResponse.ProtoReflect.Descriptor instead.
func (*GetPackagingRunResponse) Descriptor() ([]byte, []int) {
	return file_conduit_packaging_v1_packaging_run_proto_rawDescGZIP(), []int{1}
}

func (x *GetPackagingRunResponse) GetTier() string {
	if x != nil {
		return x.Tier
	}
	return ""
}

func (x *GetPackagingRunResponse) GetConfigured() bool {
	if x != nil {
		return x.Configured
	}
	return false
}

func (x *GetPackagingRunResponse) GetAuthority() string {
	if x != nil {
		return x.Authority
	}
	return ""
}

func (x *GetPackagingRunResponse) GetPlanAuthority() string {
	if x != nil {
		return x.PlanAuthority
	}
	return ""
}

func (x *GetPackagingRunResponse) GetApprovalAuthority() string {
	if x != nil {
		return x.ApprovalAuthority
	}
	return ""
}

func (x *GetPackagingRunResponse) GetRunId() string {
	if x != nil {
		return x.RunId
	}
	return ""
}

func (x *GetPackagingRunResponse) GetPhase() string {
	if x != nil {
		return x.Phase
	}
	return ""
}

func (x *GetPackagingRunResponse) GetOutcome() string {
	if x != nil {
		return x.Outcome
	}
	return ""
}

func (x *GetPackagingRunResponse) GetReason() string {
	if x != nil {
		return x.Reason
	}
	return ""
}

func (x *GetPackagingRunResponse) GetSourceDigest() string {
	if x != nil {
		return x.SourceDigest
	}
	return ""
}

func (x *GetPackagingRunResponse) GetArtifactDigest() string {
	if x != nil {
		return x.ArtifactDigest
	}
	return ""
}

func (x *GetPackagingRunResponse) GetProposalRevision() string {
	if x != nil {
		return x.ProposalRevision
	}
	return ""
}

func (x *GetPackagingRunResponse) GetTargetPlanRef() string {
	if x != nil {
		return x.TargetPlanRef
	}
	return ""
}

func (x *GetPackagingRunResponse) GetPlanId() string {
	if x != nil {
		return x.PlanId
	}
	return ""
}

func (x *GetPackagingRunResponse) GetHandoffReconciled() bool {
	if x != nil {
		return x.HandoffReconciled
	}
	return false
}

func (x *GetPackagingRunResponse) GetLastReview() string {
	if x != nil {
		return x.LastReview
	}
	return ""
}

func (x *GetPackagingRunResponse) GetDuplicateReviews() int32 {
	if x != nil {
		return x.DuplicateReviews
	}
	return 0
}

func (x *GetPackagingRunResponse) GetTemplateVersion() string {
	if x != nil {
		return x.TemplateVersion
	}
	return ""
}

func (x *GetPackagingRunResponse) GetToolVersions() string {
	if x != nil {
		return x.ToolVersions
	}
	return ""
}

// ListPackagingRunsRequest uses the native index filters and opaque cursor.
type ListPackagingRunsRequest struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// view is full (default, max 20) or summary (default 100, max 200).
	View string `protobuf:"bytes,1,opt,name=view,proto3" json:"view,omitempty"`
	// limit is optional; explicitly zero or negative limits are invalid.
	Limit *int32 `protobuf:"varint,2,opt,name=limit,proto3,oneof" json:"limit,omitempty"`
	// cursor must match these filters and is never interpreted by the client.
	Cursor string `protobuf:"bytes,3,opt,name=cursor,proto3" json:"cursor,omitempty"`
	// phase selects the native phase.
	Phase string `protobuf:"bytes,4,opt,name=phase,proto3" json:"phase,omitempty"`
	// outcome selects the native outcome.
	Outcome string `protobuf:"bytes,5,opt,name=outcome,proto3" json:"outcome,omitempty"`
	// plan_id selects the referenced Plan, without granting Plan authority.
	PlanId string `protobuf:"bytes,6,opt,name=plan_id,json=planId,proto3" json:"plan_id,omitempty"`
	// search is the native case-insensitive search string.
	Search        string `protobuf:"bytes,7,opt,name=search,proto3" json:"search,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ListPackagingRunsRequest) Reset() {
	*x = ListPackagingRunsRequest{}
	mi := &file_conduit_packaging_v1_packaging_run_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListPackagingRunsRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListPackagingRunsRequest) ProtoMessage() {}

func (x *ListPackagingRunsRequest) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_packaging_v1_packaging_run_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListPackagingRunsRequest.ProtoReflect.Descriptor instead.
func (*ListPackagingRunsRequest) Descriptor() ([]byte, []int) {
	return file_conduit_packaging_v1_packaging_run_proto_rawDescGZIP(), []int{2}
}

func (x *ListPackagingRunsRequest) GetView() string {
	if x != nil {
		return x.View
	}
	return ""
}

func (x *ListPackagingRunsRequest) GetLimit() int32 {
	if x != nil && x.Limit != nil {
		return *x.Limit
	}
	return 0
}

func (x *ListPackagingRunsRequest) GetCursor() string {
	if x != nil {
		return x.Cursor
	}
	return ""
}

func (x *ListPackagingRunsRequest) GetPhase() string {
	if x != nil {
		return x.Phase
	}
	return ""
}

func (x *ListPackagingRunsRequest) GetOutcome() string {
	if x != nil {
		return x.Outcome
	}
	return ""
}

func (x *ListPackagingRunsRequest) GetPlanId() string {
	if x != nil {
		return x.PlanId
	}
	return ""
}

func (x *ListPackagingRunsRequest) GetSearch() string {
	if x != nil {
		return x.Search
	}
	return ""
}

// ListPackagingRunsResponse counts only visible, filtered runs.
type ListPackagingRunsResponse struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// tier marks the native read model.
	Tier string `protobuf:"bytes,1,opt,name=tier,proto3" json:"tier,omitempty"`
	// configured reports whether a packaging reader is configured.
	Configured bool `protobuf:"varint,2,opt,name=configured,proto3" json:"configured,omitempty"`
	// authority is packaging-run.
	Authority string `protobuf:"bytes,3,opt,name=authority,proto3" json:"authority,omitempty"`
	// plan_authority remains conduit-plan.
	PlanAuthority string `protobuf:"bytes,4,opt,name=plan_authority,json=planAuthority,proto3" json:"plan_authority,omitempty"`
	// approval_authority remains conduit-governance.
	ApprovalAuthority string `protobuf:"bytes,5,opt,name=approval_authority,json=approvalAuthority,proto3" json:"approval_authority,omitempty"`
	// items are redacted rows, ordered by run_id.
	Items []*GetPackagingRunResponse `protobuf:"bytes,6,rep,name=items,proto3" json:"items,omitempty"`
	// page_size is the number of returned rows.
	PageSize int32 `protobuf:"varint,7,opt,name=page_size,json=pageSize,proto3" json:"page_size,omitempty"`
	// limit is the effective page limit.
	Limit int32 `protobuf:"varint,8,opt,name=limit,proto3" json:"limit,omitempty"`
	// has_previous indicates a previous visible page.
	HasPrevious bool `protobuf:"varint,9,opt,name=has_previous,json=hasPrevious,proto3" json:"has_previous,omitempty"`
	// next_cursor is absent at the end of the filtered index.
	NextCursor string `protobuf:"bytes,10,opt,name=next_cursor,json=nextCursor,proto3" json:"next_cursor,omitempty"`
	// previous_cursor is absent on the first page.
	PreviousCursor string `protobuf:"bytes,11,opt,name=previous_cursor,json=previousCursor,proto3" json:"previous_cursor,omitempty"`
	// total counts visible matches, not all stored runs.
	Total         int64 `protobuf:"varint,12,opt,name=total,proto3" json:"total,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ListPackagingRunsResponse) Reset() {
	*x = ListPackagingRunsResponse{}
	mi := &file_conduit_packaging_v1_packaging_run_proto_msgTypes[3]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListPackagingRunsResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListPackagingRunsResponse) ProtoMessage() {}

func (x *ListPackagingRunsResponse) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_packaging_v1_packaging_run_proto_msgTypes[3]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListPackagingRunsResponse.ProtoReflect.Descriptor instead.
func (*ListPackagingRunsResponse) Descriptor() ([]byte, []int) {
	return file_conduit_packaging_v1_packaging_run_proto_rawDescGZIP(), []int{3}
}

func (x *ListPackagingRunsResponse) GetTier() string {
	if x != nil {
		return x.Tier
	}
	return ""
}

func (x *ListPackagingRunsResponse) GetConfigured() bool {
	if x != nil {
		return x.Configured
	}
	return false
}

func (x *ListPackagingRunsResponse) GetAuthority() string {
	if x != nil {
		return x.Authority
	}
	return ""
}

func (x *ListPackagingRunsResponse) GetPlanAuthority() string {
	if x != nil {
		return x.PlanAuthority
	}
	return ""
}

func (x *ListPackagingRunsResponse) GetApprovalAuthority() string {
	if x != nil {
		return x.ApprovalAuthority
	}
	return ""
}

func (x *ListPackagingRunsResponse) GetItems() []*GetPackagingRunResponse {
	if x != nil {
		return x.Items
	}
	return nil
}

func (x *ListPackagingRunsResponse) GetPageSize() int32 {
	if x != nil {
		return x.PageSize
	}
	return 0
}

func (x *ListPackagingRunsResponse) GetLimit() int32 {
	if x != nil {
		return x.Limit
	}
	return 0
}

func (x *ListPackagingRunsResponse) GetHasPrevious() bool {
	if x != nil {
		return x.HasPrevious
	}
	return false
}

func (x *ListPackagingRunsResponse) GetNextCursor() string {
	if x != nil {
		return x.NextCursor
	}
	return ""
}

func (x *ListPackagingRunsResponse) GetPreviousCursor() string {
	if x != nil {
		return x.PreviousCursor
	}
	return ""
}

func (x *ListPackagingRunsResponse) GetTotal() int64 {
	if x != nil {
		return x.Total
	}
	return 0
}

var File_conduit_packaging_v1_packaging_run_proto protoreflect.FileDescriptor

const file_conduit_packaging_v1_packaging_run_proto_rawDesc = "" +
	"\n" +
	"(conduit/packaging/v1/packaging_run.proto\x12\x14conduit.packaging.v1\"/\n" +
	"\x16GetPackagingRunRequest\x12\x15\n" +
	"\x06run_id\x18\x01 \x01(\tR\x05runId\"\xa9\x05\n" +
	"\x17GetPackagingRunResponse\x12\x12\n" +
	"\x04tier\x18\x01 \x01(\tR\x04tier\x12\x1e\n" +
	"\n" +
	"configured\x18\x02 \x01(\bR\n" +
	"configured\x12\x1c\n" +
	"\tauthority\x18\x03 \x01(\tR\tauthority\x12%\n" +
	"\x0eplan_authority\x18\x04 \x01(\tR\rplanAuthority\x12-\n" +
	"\x12approval_authority\x18\x05 \x01(\tR\x11approvalAuthority\x12\x15\n" +
	"\x06run_id\x18\x06 \x01(\tR\x05runId\x12\x14\n" +
	"\x05phase\x18\a \x01(\tR\x05phase\x12\x18\n" +
	"\aoutcome\x18\b \x01(\tR\aoutcome\x12\x16\n" +
	"\x06reason\x18\t \x01(\tR\x06reason\x12#\n" +
	"\rsource_digest\x18\n" +
	" \x01(\tR\fsourceDigest\x12'\n" +
	"\x0fartifact_digest\x18\v \x01(\tR\x0eartifactDigest\x12+\n" +
	"\x11proposal_revision\x18\f \x01(\tR\x10proposalRevision\x12&\n" +
	"\x0ftarget_plan_ref\x18\r \x01(\tR\rtargetPlanRef\x12\x17\n" +
	"\aplan_id\x18\x0e \x01(\tR\x06planId\x12-\n" +
	"\x12handoff_reconciled\x18\x0f \x01(\bR\x11handoffReconciled\x12\x1f\n" +
	"\vlast_review\x18\x10 \x01(\tR\n" +
	"lastReview\x12+\n" +
	"\x11duplicate_reviews\x18\x11 \x01(\x05R\x10duplicateReviews\x12)\n" +
	"\x10template_version\x18\x12 \x01(\tR\x0ftemplateVersion\x12#\n" +
	"\rtool_versions\x18\x13 \x01(\tR\ftoolVersions\"\xcc\x01\n" +
	"\x18ListPackagingRunsRequest\x12\x12\n" +
	"\x04view\x18\x01 \x01(\tR\x04view\x12\x19\n" +
	"\x05limit\x18\x02 \x01(\x05H\x00R\x05limit\x88\x01\x01\x12\x16\n" +
	"\x06cursor\x18\x03 \x01(\tR\x06cursor\x12\x14\n" +
	"\x05phase\x18\x04 \x01(\tR\x05phase\x12\x18\n" +
	"\aoutcome\x18\x05 \x01(\tR\aoutcome\x12\x17\n" +
	"\aplan_id\x18\x06 \x01(\tR\x06planId\x12\x16\n" +
	"\x06search\x18\a \x01(\tR\x06searchB\b\n" +
	"\x06_limit\"\xbe\x03\n" +
	"\x19ListPackagingRunsResponse\x12\x12\n" +
	"\x04tier\x18\x01 \x01(\tR\x04tier\x12\x1e\n" +
	"\n" +
	"configured\x18\x02 \x01(\bR\n" +
	"configured\x12\x1c\n" +
	"\tauthority\x18\x03 \x01(\tR\tauthority\x12%\n" +
	"\x0eplan_authority\x18\x04 \x01(\tR\rplanAuthority\x12-\n" +
	"\x12approval_authority\x18\x05 \x01(\tR\x11approvalAuthority\x12C\n" +
	"\x05items\x18\x06 \x03(\v2-.conduit.packaging.v1.GetPackagingRunResponseR\x05items\x12\x1b\n" +
	"\tpage_size\x18\a \x01(\x05R\bpageSize\x12\x14\n" +
	"\x05limit\x18\b \x01(\x05R\x05limit\x12!\n" +
	"\fhas_previous\x18\t \x01(\bR\vhasPrevious\x12\x1f\n" +
	"\vnext_cursor\x18\n" +
	" \x01(\tR\n" +
	"nextCursor\x12'\n" +
	"\x0fprevious_cursor\x18\v \x01(\tR\x0epreviousCursor\x12\x14\n" +
	"\x05total\x18\f \x01(\x03R\x05total2\x85\x02\n" +
	"\x13PackagingRunService\x12y\n" +
	"\x11ListPackagingRuns\x12..conduit.packaging.v1.ListPackagingRunsRequest\x1a/.conduit.packaging.v1.ListPackagingRunsResponse\"\x03\x90\x02\x01\x12s\n" +
	"\x0fGetPackagingRun\x12,.conduit.packaging.v1.GetPackagingRunRequest\x1a-.conduit.packaging.v1.GetPackagingRunResponse\"\x03\x90\x02\x01BLZJgithub.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1;packagingv1b\x06proto3"

var (
	file_conduit_packaging_v1_packaging_run_proto_rawDescOnce sync.Once
	file_conduit_packaging_v1_packaging_run_proto_rawDescData []byte
)

func file_conduit_packaging_v1_packaging_run_proto_rawDescGZIP() []byte {
	file_conduit_packaging_v1_packaging_run_proto_rawDescOnce.Do(func() {
		file_conduit_packaging_v1_packaging_run_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_conduit_packaging_v1_packaging_run_proto_rawDesc), len(file_conduit_packaging_v1_packaging_run_proto_rawDesc)))
	})
	return file_conduit_packaging_v1_packaging_run_proto_rawDescData
}

var file_conduit_packaging_v1_packaging_run_proto_msgTypes = make([]protoimpl.MessageInfo, 4)
var file_conduit_packaging_v1_packaging_run_proto_goTypes = []any{
	(*GetPackagingRunRequest)(nil),    // 0: conduit.packaging.v1.GetPackagingRunRequest
	(*GetPackagingRunResponse)(nil),   // 1: conduit.packaging.v1.GetPackagingRunResponse
	(*ListPackagingRunsRequest)(nil),  // 2: conduit.packaging.v1.ListPackagingRunsRequest
	(*ListPackagingRunsResponse)(nil), // 3: conduit.packaging.v1.ListPackagingRunsResponse
}
var file_conduit_packaging_v1_packaging_run_proto_depIdxs = []int32{
	1, // 0: conduit.packaging.v1.ListPackagingRunsResponse.items:type_name -> conduit.packaging.v1.GetPackagingRunResponse
	2, // 1: conduit.packaging.v1.PackagingRunService.ListPackagingRuns:input_type -> conduit.packaging.v1.ListPackagingRunsRequest
	0, // 2: conduit.packaging.v1.PackagingRunService.GetPackagingRun:input_type -> conduit.packaging.v1.GetPackagingRunRequest
	3, // 3: conduit.packaging.v1.PackagingRunService.ListPackagingRuns:output_type -> conduit.packaging.v1.ListPackagingRunsResponse
	1, // 4: conduit.packaging.v1.PackagingRunService.GetPackagingRun:output_type -> conduit.packaging.v1.GetPackagingRunResponse
	3, // [3:5] is the sub-list for method output_type
	1, // [1:3] is the sub-list for method input_type
	1, // [1:1] is the sub-list for extension type_name
	1, // [1:1] is the sub-list for extension extendee
	0, // [0:1] is the sub-list for field type_name
}

func init() { file_conduit_packaging_v1_packaging_run_proto_init() }
func file_conduit_packaging_v1_packaging_run_proto_init() {
	if File_conduit_packaging_v1_packaging_run_proto != nil {
		return
	}
	file_conduit_packaging_v1_packaging_run_proto_msgTypes[2].OneofWrappers = []any{}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_conduit_packaging_v1_packaging_run_proto_rawDesc), len(file_conduit_packaging_v1_packaging_run_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   4,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_conduit_packaging_v1_packaging_run_proto_goTypes,
		DependencyIndexes: file_conduit_packaging_v1_packaging_run_proto_depIdxs,
		MessageInfos:      file_conduit_packaging_v1_packaging_run_proto_msgTypes,
	}.Build()
	File_conduit_packaging_v1_packaging_run_proto = out.File
	file_conduit_packaging_v1_packaging_run_proto_goTypes = nil
	file_conduit_packaging_v1_packaging_run_proto_depIdxs = nil
}

//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.12
// 	protoc        (unknown)
// source: conduit/governance/v1/authorization_audit.proto

package governancev1

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

type ListAuthorizationAuditEventsRequest struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// Absent uses 100; explicit limits must be 1..200.
	Limit *int32 `protobuf:"varint,1,opt,name=limit,proto3,oneof" json:"limit,omitempty"`
	// Native opaque continuation bound to resource, time bounds and order.
	Cursor      string `protobuf:"bytes,2,opt,name=cursor,proto3" json:"cursor,omitempty"`
	ResourceRid string `protobuf:"bytes,3,opt,name=resource_rid,json=resourceRid,proto3" json:"resource_rid,omitempty"`
	// Inclusive lower and exclusive upper bound; absent leaves a side open.
	Since         *timestamppb.Timestamp `protobuf:"bytes,4,opt,name=since,proto3" json:"since,omitempty"`
	Until         *timestamppb.Timestamp `protobuf:"bytes,5,opt,name=until,proto3" json:"until,omitempty"`
	Descending    bool                   `protobuf:"varint,6,opt,name=descending,proto3" json:"descending,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ListAuthorizationAuditEventsRequest) Reset() {
	*x = ListAuthorizationAuditEventsRequest{}
	mi := &file_conduit_governance_v1_authorization_audit_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListAuthorizationAuditEventsRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListAuthorizationAuditEventsRequest) ProtoMessage() {}

func (x *ListAuthorizationAuditEventsRequest) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_governance_v1_authorization_audit_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListAuthorizationAuditEventsRequest.ProtoReflect.Descriptor instead.
func (*ListAuthorizationAuditEventsRequest) Descriptor() ([]byte, []int) {
	return file_conduit_governance_v1_authorization_audit_proto_rawDescGZIP(), []int{0}
}

func (x *ListAuthorizationAuditEventsRequest) GetLimit() int32 {
	if x != nil && x.Limit != nil {
		return *x.Limit
	}
	return 0
}

func (x *ListAuthorizationAuditEventsRequest) GetCursor() string {
	if x != nil {
		return x.Cursor
	}
	return ""
}

func (x *ListAuthorizationAuditEventsRequest) GetResourceRid() string {
	if x != nil {
		return x.ResourceRid
	}
	return ""
}

func (x *ListAuthorizationAuditEventsRequest) GetSince() *timestamppb.Timestamp {
	if x != nil {
		return x.Since
	}
	return nil
}

func (x *ListAuthorizationAuditEventsRequest) GetUntil() *timestamppb.Timestamp {
	if x != nil {
		return x.Until
	}
	return nil
}

func (x *ListAuthorizationAuditEventsRequest) GetDescending() bool {
	if x != nil {
		return x.Descending
	}
	return false
}

type AuthorizationAuditEvent struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Id            string                 `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	SchemaVersion string                 `protobuf:"bytes,2,opt,name=schema_version,json=schemaVersion,proto3" json:"schema_version,omitempty"`
	Categories    []string               `protobuf:"bytes,3,rep,name=categories,proto3" json:"categories,omitempty"`
	TraceId       string                 `protobuf:"bytes,4,opt,name=trace_id,json=traceId,proto3" json:"trace_id,omitempty"`
	Subject       string                 `protobuf:"bytes,5,opt,name=subject,proto3" json:"subject,omitempty"`
	Action        string                 `protobuf:"bytes,6,opt,name=action,proto3" json:"action,omitempty"`
	ResourceRid   string                 `protobuf:"bytes,7,opt,name=resource_rid,json=resourceRid,proto3" json:"resource_rid,omitempty"`
	Decision      string                 `protobuf:"bytes,8,opt,name=decision,proto3" json:"decision,omitempty"`
	Reason        string                 `protobuf:"bytes,9,opt,name=reason,proto3" json:"reason,omitempty"`
	// Native JSON encoded as UTF-8 bytes, not protobuf Struct: metadata numbers
	// must not be rounded through protobuf's floating-point Value representation.
	RequestFieldsJson []byte `protobuf:"bytes,10,opt,name=request_fields_json,json=requestFieldsJson,proto3" json:"request_fields_json,omitempty"`
	ResultFieldsJson  []byte `protobuf:"bytes,11,opt,name=result_fields_json,json=resultFieldsJson,proto3" json:"result_fields_json,omitempty"`
	// Empty means the native optional context was absent/empty.
	ContextJson []byte `protobuf:"bytes,12,opt,name=context_json,json=contextJson,proto3" json:"context_json,omitempty"`
	// Missing means no recorded timestamp; never filled with the current time.
	CreatedAt     *timestamppb.Timestamp `protobuf:"bytes,13,opt,name=created_at,json=createdAt,proto3" json:"created_at,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *AuthorizationAuditEvent) Reset() {
	*x = AuthorizationAuditEvent{}
	mi := &file_conduit_governance_v1_authorization_audit_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *AuthorizationAuditEvent) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*AuthorizationAuditEvent) ProtoMessage() {}

func (x *AuthorizationAuditEvent) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_governance_v1_authorization_audit_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use AuthorizationAuditEvent.ProtoReflect.Descriptor instead.
func (*AuthorizationAuditEvent) Descriptor() ([]byte, []int) {
	return file_conduit_governance_v1_authorization_audit_proto_rawDescGZIP(), []int{1}
}

func (x *AuthorizationAuditEvent) GetId() string {
	if x != nil {
		return x.Id
	}
	return ""
}

func (x *AuthorizationAuditEvent) GetSchemaVersion() string {
	if x != nil {
		return x.SchemaVersion
	}
	return ""
}

func (x *AuthorizationAuditEvent) GetCategories() []string {
	if x != nil {
		return x.Categories
	}
	return nil
}

func (x *AuthorizationAuditEvent) GetTraceId() string {
	if x != nil {
		return x.TraceId
	}
	return ""
}

func (x *AuthorizationAuditEvent) GetSubject() string {
	if x != nil {
		return x.Subject
	}
	return ""
}

func (x *AuthorizationAuditEvent) GetAction() string {
	if x != nil {
		return x.Action
	}
	return ""
}

func (x *AuthorizationAuditEvent) GetResourceRid() string {
	if x != nil {
		return x.ResourceRid
	}
	return ""
}

func (x *AuthorizationAuditEvent) GetDecision() string {
	if x != nil {
		return x.Decision
	}
	return ""
}

func (x *AuthorizationAuditEvent) GetReason() string {
	if x != nil {
		return x.Reason
	}
	return ""
}

func (x *AuthorizationAuditEvent) GetRequestFieldsJson() []byte {
	if x != nil {
		return x.RequestFieldsJson
	}
	return nil
}

func (x *AuthorizationAuditEvent) GetResultFieldsJson() []byte {
	if x != nil {
		return x.ResultFieldsJson
	}
	return nil
}

func (x *AuthorizationAuditEvent) GetContextJson() []byte {
	if x != nil {
		return x.ContextJson
	}
	return nil
}

func (x *AuthorizationAuditEvent) GetCreatedAt() *timestamppb.Timestamp {
	if x != nil {
		return x.CreatedAt
	}
	return nil
}

type ListAuthorizationAuditEventsResponse struct {
	state      protoimpl.MessageState     `protogen:"open.v1"`
	Events     []*AuthorizationAuditEvent `protobuf:"bytes,1,rep,name=events,proto3" json:"events,omitempty"`
	Total      int64                      `protobuf:"varint,2,opt,name=total,proto3" json:"total,omitempty"`
	PageSize   int32                      `protobuf:"varint,3,opt,name=page_size,json=pageSize,proto3" json:"page_size,omitempty"`
	Limit      int32                      `protobuf:"varint,4,opt,name=limit,proto3" json:"limit,omitempty"`
	NextCursor string                     `protobuf:"bytes,5,opt,name=next_cursor,json=nextCursor,proto3" json:"next_cursor,omitempty"`
	// Forward seek only. No previous cursor is issued.
	HasPrevious   bool `protobuf:"varint,6,opt,name=has_previous,json=hasPrevious,proto3" json:"has_previous,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ListAuthorizationAuditEventsResponse) Reset() {
	*x = ListAuthorizationAuditEventsResponse{}
	mi := &file_conduit_governance_v1_authorization_audit_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListAuthorizationAuditEventsResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListAuthorizationAuditEventsResponse) ProtoMessage() {}

func (x *ListAuthorizationAuditEventsResponse) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_governance_v1_authorization_audit_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListAuthorizationAuditEventsResponse.ProtoReflect.Descriptor instead.
func (*ListAuthorizationAuditEventsResponse) Descriptor() ([]byte, []int) {
	return file_conduit_governance_v1_authorization_audit_proto_rawDescGZIP(), []int{2}
}

func (x *ListAuthorizationAuditEventsResponse) GetEvents() []*AuthorizationAuditEvent {
	if x != nil {
		return x.Events
	}
	return nil
}

func (x *ListAuthorizationAuditEventsResponse) GetTotal() int64 {
	if x != nil {
		return x.Total
	}
	return 0
}

func (x *ListAuthorizationAuditEventsResponse) GetPageSize() int32 {
	if x != nil {
		return x.PageSize
	}
	return 0
}

func (x *ListAuthorizationAuditEventsResponse) GetLimit() int32 {
	if x != nil {
		return x.Limit
	}
	return 0
}

func (x *ListAuthorizationAuditEventsResponse) GetNextCursor() string {
	if x != nil {
		return x.NextCursor
	}
	return ""
}

func (x *ListAuthorizationAuditEventsResponse) GetHasPrevious() bool {
	if x != nil {
		return x.HasPrevious
	}
	return false
}

var File_conduit_governance_v1_authorization_audit_proto protoreflect.FileDescriptor

const file_conduit_governance_v1_authorization_audit_proto_rawDesc = "" +
	"\n" +
	"/conduit/governance/v1/authorization_audit.proto\x12\x15conduit.governance.v1\x1a\x1fgoogle/protobuf/timestamp.proto\"\x89\x02\n" +
	"#ListAuthorizationAuditEventsRequest\x12\x19\n" +
	"\x05limit\x18\x01 \x01(\x05H\x00R\x05limit\x88\x01\x01\x12\x16\n" +
	"\x06cursor\x18\x02 \x01(\tR\x06cursor\x12!\n" +
	"\fresource_rid\x18\x03 \x01(\tR\vresourceRid\x120\n" +
	"\x05since\x18\x04 \x01(\v2\x1a.google.protobuf.TimestampR\x05since\x120\n" +
	"\x05until\x18\x05 \x01(\v2\x1a.google.protobuf.TimestampR\x05until\x12\x1e\n" +
	"\n" +
	"descending\x18\x06 \x01(\bR\n" +
	"descendingB\b\n" +
	"\x06_limit\"\xd0\x03\n" +
	"\x17AuthorizationAuditEvent\x12\x0e\n" +
	"\x02id\x18\x01 \x01(\tR\x02id\x12%\n" +
	"\x0eschema_version\x18\x02 \x01(\tR\rschemaVersion\x12\x1e\n" +
	"\n" +
	"categories\x18\x03 \x03(\tR\n" +
	"categories\x12\x19\n" +
	"\btrace_id\x18\x04 \x01(\tR\atraceId\x12\x18\n" +
	"\asubject\x18\x05 \x01(\tR\asubject\x12\x16\n" +
	"\x06action\x18\x06 \x01(\tR\x06action\x12!\n" +
	"\fresource_rid\x18\a \x01(\tR\vresourceRid\x12\x1a\n" +
	"\bdecision\x18\b \x01(\tR\bdecision\x12\x16\n" +
	"\x06reason\x18\t \x01(\tR\x06reason\x12.\n" +
	"\x13request_fields_json\x18\n" +
	" \x01(\fR\x11requestFieldsJson\x12,\n" +
	"\x12result_fields_json\x18\v \x01(\fR\x10resultFieldsJson\x12!\n" +
	"\fcontext_json\x18\f \x01(\fR\vcontextJson\x129\n" +
	"\n" +
	"created_at\x18\r \x01(\v2\x1a.google.protobuf.TimestampR\tcreatedAt\"\xfb\x01\n" +
	"$ListAuthorizationAuditEventsResponse\x12F\n" +
	"\x06events\x18\x01 \x03(\v2..conduit.governance.v1.AuthorizationAuditEventR\x06events\x12\x14\n" +
	"\x05total\x18\x02 \x01(\x03R\x05total\x12\x1b\n" +
	"\tpage_size\x18\x03 \x01(\x05R\bpageSize\x12\x14\n" +
	"\x05limit\x18\x04 \x01(\x05R\x05limit\x12\x1f\n" +
	"\vnext_cursor\x18\x05 \x01(\tR\n" +
	"nextCursor\x12!\n" +
	"\fhas_previous\x18\x06 \x01(\bR\vhasPrevious2\xbe\x01\n" +
	"\x1dAuthorizationAuditReadService\x12\x9c\x01\n" +
	"\x1cListAuthorizationAuditEvents\x12:.conduit.governance.v1.ListAuthorizationAuditEventsRequest\x1a;.conduit.governance.v1.ListAuthorizationAuditEventsResponse\"\x03\x90\x02\x01BNZLgithub.com/jonathanagustin/conduit/gen/go/conduit/governance/v1;governancev1b\x06proto3"

var (
	file_conduit_governance_v1_authorization_audit_proto_rawDescOnce sync.Once
	file_conduit_governance_v1_authorization_audit_proto_rawDescData []byte
)

func file_conduit_governance_v1_authorization_audit_proto_rawDescGZIP() []byte {
	file_conduit_governance_v1_authorization_audit_proto_rawDescOnce.Do(func() {
		file_conduit_governance_v1_authorization_audit_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_conduit_governance_v1_authorization_audit_proto_rawDesc), len(file_conduit_governance_v1_authorization_audit_proto_rawDesc)))
	})
	return file_conduit_governance_v1_authorization_audit_proto_rawDescData
}

var file_conduit_governance_v1_authorization_audit_proto_msgTypes = make([]protoimpl.MessageInfo, 3)
var file_conduit_governance_v1_authorization_audit_proto_goTypes = []any{
	(*ListAuthorizationAuditEventsRequest)(nil),  // 0: conduit.governance.v1.ListAuthorizationAuditEventsRequest
	(*AuthorizationAuditEvent)(nil),              // 1: conduit.governance.v1.AuthorizationAuditEvent
	(*ListAuthorizationAuditEventsResponse)(nil), // 2: conduit.governance.v1.ListAuthorizationAuditEventsResponse
	(*timestamppb.Timestamp)(nil),                // 3: google.protobuf.Timestamp
}
var file_conduit_governance_v1_authorization_audit_proto_depIdxs = []int32{
	3, // 0: conduit.governance.v1.ListAuthorizationAuditEventsRequest.since:type_name -> google.protobuf.Timestamp
	3, // 1: conduit.governance.v1.ListAuthorizationAuditEventsRequest.until:type_name -> google.protobuf.Timestamp
	3, // 2: conduit.governance.v1.AuthorizationAuditEvent.created_at:type_name -> google.protobuf.Timestamp
	1, // 3: conduit.governance.v1.ListAuthorizationAuditEventsResponse.events:type_name -> conduit.governance.v1.AuthorizationAuditEvent
	0, // 4: conduit.governance.v1.AuthorizationAuditReadService.ListAuthorizationAuditEvents:input_type -> conduit.governance.v1.ListAuthorizationAuditEventsRequest
	2, // 5: conduit.governance.v1.AuthorizationAuditReadService.ListAuthorizationAuditEvents:output_type -> conduit.governance.v1.ListAuthorizationAuditEventsResponse
	5, // [5:6] is the sub-list for method output_type
	4, // [4:5] is the sub-list for method input_type
	4, // [4:4] is the sub-list for extension type_name
	4, // [4:4] is the sub-list for extension extendee
	0, // [0:4] is the sub-list for field type_name
}

func init() { file_conduit_governance_v1_authorization_audit_proto_init() }
func file_conduit_governance_v1_authorization_audit_proto_init() {
	if File_conduit_governance_v1_authorization_audit_proto != nil {
		return
	}
	file_conduit_governance_v1_authorization_audit_proto_msgTypes[0].OneofWrappers = []any{}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_conduit_governance_v1_authorization_audit_proto_rawDesc), len(file_conduit_governance_v1_authorization_audit_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   3,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_conduit_governance_v1_authorization_audit_proto_goTypes,
		DependencyIndexes: file_conduit_governance_v1_authorization_audit_proto_depIdxs,
		MessageInfos:      file_conduit_governance_v1_authorization_audit_proto_msgTypes,
	}.Build()
	File_conduit_governance_v1_authorization_audit_proto = out.File
	file_conduit_governance_v1_authorization_audit_proto_goTypes = nil
	file_conduit_governance_v1_authorization_audit_proto_depIdxs = nil
}

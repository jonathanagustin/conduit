//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.12
// 	protoc        (unknown)
// source: conduit/notifications/v1/notification_event.proto

// Package conduit.notifications.v1 describes resource-authorized native events.

package notificationsv1

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

// ListNotificationEventsRequest names the existing authorized resource window.
type ListNotificationEventsRequest struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// resource_rid is the required resourceRid query value, trimmed by the Hub.
	ResourceRid string `protobuf:"bytes,1,opt,name=resource_rid,json=resourceRid,proto3" json:"resource_rid,omitempty"`
	// limit is optional: absent selects the native ceiling; explicit zero is invalid.
	Limit         *int64 `protobuf:"varint,2,opt,name=limit,proto3,oneof" json:"limit,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ListNotificationEventsRequest) Reset() {
	*x = ListNotificationEventsRequest{}
	mi := &file_conduit_notifications_v1_notification_event_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListNotificationEventsRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListNotificationEventsRequest) ProtoMessage() {}

func (x *ListNotificationEventsRequest) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_notifications_v1_notification_event_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListNotificationEventsRequest.ProtoReflect.Descriptor instead.
func (*ListNotificationEventsRequest) Descriptor() ([]byte, []int) {
	return file_conduit_notifications_v1_notification_event_proto_rawDescGZIP(), []int{0}
}

func (x *ListNotificationEventsRequest) GetResourceRid() string {
	if x != nil {
		return x.ResourceRid
	}
	return ""
}

func (x *ListNotificationEventsRequest) GetLimit() int64 {
	if x != nil && x.Limit != nil {
		return *x.Limit
	}
	return 0
}

// ListNotificationEventsResponse retains the native event read envelope.
type ListNotificationEventsResponse struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// resource_rid is the resource actually authorized and read.
	ResourceRid string `protobuf:"bytes,1,opt,name=resource_rid,json=resourceRid,proto3" json:"resource_rid,omitempty"`
	// limit is the bounded limit applied by the native Reader.
	Limit int32 `protobuf:"varint,2,opt,name=limit,proto3" json:"limit,omitempty"`
	// events are the returned ordered window, not a total or a resume stream.
	Events        []*NotificationEvent `protobuf:"bytes,3,rep,name=events,proto3" json:"events,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ListNotificationEventsResponse) Reset() {
	*x = ListNotificationEventsResponse{}
	mi := &file_conduit_notifications_v1_notification_event_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListNotificationEventsResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListNotificationEventsResponse) ProtoMessage() {}

func (x *ListNotificationEventsResponse) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_notifications_v1_notification_event_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListNotificationEventsResponse.ProtoReflect.Descriptor instead.
func (*ListNotificationEventsResponse) Descriptor() ([]byte, []int) {
	return file_conduit_notifications_v1_notification_event_proto_rawDescGZIP(), []int{1}
}

func (x *ListNotificationEventsResponse) GetResourceRid() string {
	if x != nil {
		return x.ResourceRid
	}
	return ""
}

func (x *ListNotificationEventsResponse) GetLimit() int32 {
	if x != nil {
		return x.Limit
	}
	return 0
}

func (x *ListNotificationEventsResponse) GetEvents() []*NotificationEvent {
	if x != nil {
		return x.Events
	}
	return nil
}

// NotificationEvent is the native notifications/domain.Event projection.
type NotificationEvent struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// id is the stable native event identifier.
	Id string `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	// type preserves the native open event vocabulary.
	Type string `protobuf:"bytes,2,opt,name=type,proto3" json:"type,omitempty"`
	// resource_rid is the resource the event is about.
	ResourceRid string `protobuf:"bytes,3,opt,name=resource_rid,json=resourceRid,proto3" json:"resource_rid,omitempty"`
	// environment is optional native environment context, not additional authority.
	Environment string `protobuf:"bytes,4,opt,name=environment,proto3" json:"environment,omitempty"`
	// summary is the stored human-readable sentence.
	Summary string `protobuf:"bytes,5,opt,name=summary,proto3" json:"summary,omitempty"`
	// actor is empty for system-generated events.
	Actor string `protobuf:"bytes,6,opt,name=actor,proto3" json:"actor,omitempty"`
	// occurred_at is the native event timestamp.
	OccurredAt    *timestamppb.Timestamp `protobuf:"bytes,7,opt,name=occurred_at,json=occurredAt,proto3" json:"occurred_at,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *NotificationEvent) Reset() {
	*x = NotificationEvent{}
	mi := &file_conduit_notifications_v1_notification_event_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *NotificationEvent) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*NotificationEvent) ProtoMessage() {}

func (x *NotificationEvent) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_notifications_v1_notification_event_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use NotificationEvent.ProtoReflect.Descriptor instead.
func (*NotificationEvent) Descriptor() ([]byte, []int) {
	return file_conduit_notifications_v1_notification_event_proto_rawDescGZIP(), []int{2}
}

func (x *NotificationEvent) GetId() string {
	if x != nil {
		return x.Id
	}
	return ""
}

func (x *NotificationEvent) GetType() string {
	if x != nil {
		return x.Type
	}
	return ""
}

func (x *NotificationEvent) GetResourceRid() string {
	if x != nil {
		return x.ResourceRid
	}
	return ""
}

func (x *NotificationEvent) GetEnvironment() string {
	if x != nil {
		return x.Environment
	}
	return ""
}

func (x *NotificationEvent) GetSummary() string {
	if x != nil {
		return x.Summary
	}
	return ""
}

func (x *NotificationEvent) GetActor() string {
	if x != nil {
		return x.Actor
	}
	return ""
}

func (x *NotificationEvent) GetOccurredAt() *timestamppb.Timestamp {
	if x != nil {
		return x.OccurredAt
	}
	return nil
}

var File_conduit_notifications_v1_notification_event_proto protoreflect.FileDescriptor

const file_conduit_notifications_v1_notification_event_proto_rawDesc = "" +
	"\n" +
	"1conduit/notifications/v1/notification_event.proto\x12\x18conduit.notifications.v1\x1a\x1fgoogle/protobuf/timestamp.proto\"g\n" +
	"\x1dListNotificationEventsRequest\x12!\n" +
	"\fresource_rid\x18\x01 \x01(\tR\vresourceRid\x12\x19\n" +
	"\x05limit\x18\x02 \x01(\x03H\x00R\x05limit\x88\x01\x01B\b\n" +
	"\x06_limit\"\x9e\x01\n" +
	"\x1eListNotificationEventsResponse\x12!\n" +
	"\fresource_rid\x18\x01 \x01(\tR\vresourceRid\x12\x14\n" +
	"\x05limit\x18\x02 \x01(\x05R\x05limit\x12C\n" +
	"\x06events\x18\x03 \x03(\v2+.conduit.notifications.v1.NotificationEventR\x06events\"\xe9\x01\n" +
	"\x11NotificationEvent\x12\x0e\n" +
	"\x02id\x18\x01 \x01(\tR\x02id\x12\x12\n" +
	"\x04type\x18\x02 \x01(\tR\x04type\x12!\n" +
	"\fresource_rid\x18\x03 \x01(\tR\vresourceRid\x12 \n" +
	"\venvironment\x18\x04 \x01(\tR\venvironment\x12\x18\n" +
	"\asummary\x18\x05 \x01(\tR\asummary\x12\x14\n" +
	"\x05actor\x18\x06 \x01(\tR\x05actor\x12;\n" +
	"\voccurred_at\x18\a \x01(\v2\x1a.google.protobuf.TimestampR\n" +
	"occurredAt2\xa8\x01\n" +
	"\x13NotificationService\x12\x90\x01\n" +
	"\x16ListNotificationEvents\x127.conduit.notifications.v1.ListNotificationEventsRequest\x1a8.conduit.notifications.v1.ListNotificationEventsResponse\"\x03\x90\x02\x01BTZRgithub.com/jonathanagustin/conduit/gen/go/conduit/notifications/v1;notificationsv1b\x06proto3"

var (
	file_conduit_notifications_v1_notification_event_proto_rawDescOnce sync.Once
	file_conduit_notifications_v1_notification_event_proto_rawDescData []byte
)

func file_conduit_notifications_v1_notification_event_proto_rawDescGZIP() []byte {
	file_conduit_notifications_v1_notification_event_proto_rawDescOnce.Do(func() {
		file_conduit_notifications_v1_notification_event_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_conduit_notifications_v1_notification_event_proto_rawDesc), len(file_conduit_notifications_v1_notification_event_proto_rawDesc)))
	})
	return file_conduit_notifications_v1_notification_event_proto_rawDescData
}

var file_conduit_notifications_v1_notification_event_proto_msgTypes = make([]protoimpl.MessageInfo, 3)
var file_conduit_notifications_v1_notification_event_proto_goTypes = []any{
	(*ListNotificationEventsRequest)(nil),  // 0: conduit.notifications.v1.ListNotificationEventsRequest
	(*ListNotificationEventsResponse)(nil), // 1: conduit.notifications.v1.ListNotificationEventsResponse
	(*NotificationEvent)(nil),              // 2: conduit.notifications.v1.NotificationEvent
	(*timestamppb.Timestamp)(nil),          // 3: google.protobuf.Timestamp
}
var file_conduit_notifications_v1_notification_event_proto_depIdxs = []int32{
	2, // 0: conduit.notifications.v1.ListNotificationEventsResponse.events:type_name -> conduit.notifications.v1.NotificationEvent
	3, // 1: conduit.notifications.v1.NotificationEvent.occurred_at:type_name -> google.protobuf.Timestamp
	0, // 2: conduit.notifications.v1.NotificationService.ListNotificationEvents:input_type -> conduit.notifications.v1.ListNotificationEventsRequest
	1, // 3: conduit.notifications.v1.NotificationService.ListNotificationEvents:output_type -> conduit.notifications.v1.ListNotificationEventsResponse
	3, // [3:4] is the sub-list for method output_type
	2, // [2:3] is the sub-list for method input_type
	2, // [2:2] is the sub-list for extension type_name
	2, // [2:2] is the sub-list for extension extendee
	0, // [0:2] is the sub-list for field type_name
}

func init() { file_conduit_notifications_v1_notification_event_proto_init() }
func file_conduit_notifications_v1_notification_event_proto_init() {
	if File_conduit_notifications_v1_notification_event_proto != nil {
		return
	}
	file_conduit_notifications_v1_notification_event_proto_msgTypes[0].OneofWrappers = []any{}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_conduit_notifications_v1_notification_event_proto_rawDesc), len(file_conduit_notifications_v1_notification_event_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   3,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_conduit_notifications_v1_notification_event_proto_goTypes,
		DependencyIndexes: file_conduit_notifications_v1_notification_event_proto_depIdxs,
		MessageInfos:      file_conduit_notifications_v1_notification_event_proto_msgTypes,
	}.Build()
	File_conduit_notifications_v1_notification_event_proto = out.File
	file_conduit_notifications_v1_notification_event_proto_goTypes = nil
	file_conduit_notifications_v1_notification_event_proto_depIdxs = nil
}

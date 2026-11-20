//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.12
// 	protoc        (unknown)
// source: conduit/changeactivity/v1/change_request_discussion.proto

package changeactivityv1

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

type ListChangeRequestCommentsRequest struct {
	state           protoimpl.MessageState `protogen:"open.v1"`
	ChangeRequestId string                 `protobuf:"bytes,1,opt,name=change_request_id,json=changeRequestId,proto3" json:"change_request_id,omitempty"`
	// Native opaque cursor for this request; not a stable history snapshot.
	Cursor        string `protobuf:"bytes,2,opt,name=cursor,proto3" json:"cursor,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ListChangeRequestCommentsRequest) Reset() {
	*x = ListChangeRequestCommentsRequest{}
	mi := &file_conduit_changeactivity_v1_change_request_discussion_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListChangeRequestCommentsRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListChangeRequestCommentsRequest) ProtoMessage() {}

func (x *ListChangeRequestCommentsRequest) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_changeactivity_v1_change_request_discussion_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListChangeRequestCommentsRequest.ProtoReflect.Descriptor instead.
func (*ListChangeRequestCommentsRequest) Descriptor() ([]byte, []int) {
	return file_conduit_changeactivity_v1_change_request_discussion_proto_rawDescGZIP(), []int{0}
}

func (x *ListChangeRequestCommentsRequest) GetChangeRequestId() string {
	if x != nil {
		return x.ChangeRequestId
	}
	return ""
}

func (x *ListChangeRequestCommentsRequest) GetCursor() string {
	if x != nil {
		return x.Cursor
	}
	return ""
}

type ChangeRequestComment struct {
	state           protoimpl.MessageState `protogen:"open.v1"`
	Id              string                 `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	ChangeRequestId string                 `protobuf:"bytes,2,opt,name=change_request_id,json=changeRequestId,proto3" json:"change_request_id,omitempty"`
	Author          string                 `protobuf:"bytes,3,opt,name=author,proto3" json:"author,omitempty"`
	// The original carrier, distinct from the represented author.
	Actor         string                 `protobuf:"bytes,4,opt,name=actor,proto3" json:"actor,omitempty"`
	CreatedAt     *timestamppb.Timestamp `protobuf:"bytes,5,opt,name=created_at,json=createdAt,proto3" json:"created_at,omitempty"`
	Text          string                 `protobuf:"bytes,6,opt,name=text,proto3" json:"text,omitempty"`
	ValueTier     string                 `protobuf:"bytes,7,opt,name=value_tier,json=valueTier,proto3" json:"value_tier,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ChangeRequestComment) Reset() {
	*x = ChangeRequestComment{}
	mi := &file_conduit_changeactivity_v1_change_request_discussion_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ChangeRequestComment) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ChangeRequestComment) ProtoMessage() {}

func (x *ChangeRequestComment) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_changeactivity_v1_change_request_discussion_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ChangeRequestComment.ProtoReflect.Descriptor instead.
func (*ChangeRequestComment) Descriptor() ([]byte, []int) {
	return file_conduit_changeactivity_v1_change_request_discussion_proto_rawDescGZIP(), []int{1}
}

func (x *ChangeRequestComment) GetId() string {
	if x != nil {
		return x.Id
	}
	return ""
}

func (x *ChangeRequestComment) GetChangeRequestId() string {
	if x != nil {
		return x.ChangeRequestId
	}
	return ""
}

func (x *ChangeRequestComment) GetAuthor() string {
	if x != nil {
		return x.Author
	}
	return ""
}

func (x *ChangeRequestComment) GetActor() string {
	if x != nil {
		return x.Actor
	}
	return ""
}

func (x *ChangeRequestComment) GetCreatedAt() *timestamppb.Timestamp {
	if x != nil {
		return x.CreatedAt
	}
	return nil
}

func (x *ChangeRequestComment) GetText() string {
	if x != nil {
		return x.Text
	}
	return ""
}

func (x *ChangeRequestComment) GetValueTier() string {
	if x != nil {
		return x.ValueTier
	}
	return ""
}

type ListChangeRequestCommentsResponse struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// At most eight native comments, ordered by time then ID.
	Comments      []*ChangeRequestComment `protobuf:"bytes,1,rep,name=comments,proto3" json:"comments,omitempty"`
	NextCursor    string                  `protobuf:"bytes,2,opt,name=next_cursor,json=nextCursor,proto3" json:"next_cursor,omitempty"`
	ValueTier     string                  `protobuf:"bytes,3,opt,name=value_tier,json=valueTier,proto3" json:"value_tier,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ListChangeRequestCommentsResponse) Reset() {
	*x = ListChangeRequestCommentsResponse{}
	mi := &file_conduit_changeactivity_v1_change_request_discussion_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListChangeRequestCommentsResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListChangeRequestCommentsResponse) ProtoMessage() {}

func (x *ListChangeRequestCommentsResponse) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_changeactivity_v1_change_request_discussion_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListChangeRequestCommentsResponse.ProtoReflect.Descriptor instead.
func (*ListChangeRequestCommentsResponse) Descriptor() ([]byte, []int) {
	return file_conduit_changeactivity_v1_change_request_discussion_proto_rawDescGZIP(), []int{2}
}

func (x *ListChangeRequestCommentsResponse) GetComments() []*ChangeRequestComment {
	if x != nil {
		return x.Comments
	}
	return nil
}

func (x *ListChangeRequestCommentsResponse) GetNextCursor() string {
	if x != nil {
		return x.NextCursor
	}
	return ""
}

func (x *ListChangeRequestCommentsResponse) GetValueTier() string {
	if x != nil {
		return x.ValueTier
	}
	return ""
}

var File_conduit_changeactivity_v1_change_request_discussion_proto protoreflect.FileDescriptor

const file_conduit_changeactivity_v1_change_request_discussion_proto_rawDesc = "" +
	"\n" +
	"9conduit/changeactivity/v1/change_request_discussion.proto\x12\x19conduit.changeactivity.v1\x1a\x1fgoogle/protobuf/timestamp.proto\"f\n" +
	" ListChangeRequestCommentsRequest\x12*\n" +
	"\x11change_request_id\x18\x01 \x01(\tR\x0fchangeRequestId\x12\x16\n" +
	"\x06cursor\x18\x02 \x01(\tR\x06cursor\"\xee\x01\n" +
	"\x14ChangeRequestComment\x12\x0e\n" +
	"\x02id\x18\x01 \x01(\tR\x02id\x12*\n" +
	"\x11change_request_id\x18\x02 \x01(\tR\x0fchangeRequestId\x12\x16\n" +
	"\x06author\x18\x03 \x01(\tR\x06author\x12\x14\n" +
	"\x05actor\x18\x04 \x01(\tR\x05actor\x129\n" +
	"\n" +
	"created_at\x18\x05 \x01(\v2\x1a.google.protobuf.TimestampR\tcreatedAt\x12\x12\n" +
	"\x04text\x18\x06 \x01(\tR\x04text\x12\x1d\n" +
	"\n" +
	"value_tier\x18\a \x01(\tR\tvalueTier\"\xb0\x01\n" +
	"!ListChangeRequestCommentsResponse\x12K\n" +
	"\bcomments\x18\x01 \x03(\v2/.conduit.changeactivity.v1.ChangeRequestCommentR\bcomments\x12\x1f\n" +
	"\vnext_cursor\x18\x02 \x01(\tR\n" +
	"nextCursor\x12\x1d\n" +
	"\n" +
	"value_tier\x18\x03 \x01(\tR\tvalueTier2\xc2\x01\n" +
	"\"ChangeRequestDiscussionReadService\x12\x9b\x01\n" +
	"\x19ListChangeRequestComments\x12;.conduit.changeactivity.v1.ListChangeRequestCommentsRequest\x1a<.conduit.changeactivity.v1.ListChangeRequestCommentsResponse\"\x03\x90\x02\x01BVZTgithub.com/jonathanagustin/conduit/gen/go/conduit/changeactivity/v1;changeactivityv1b\x06proto3"

var (
	file_conduit_changeactivity_v1_change_request_discussion_proto_rawDescOnce sync.Once
	file_conduit_changeactivity_v1_change_request_discussion_proto_rawDescData []byte
)

func file_conduit_changeactivity_v1_change_request_discussion_proto_rawDescGZIP() []byte {
	file_conduit_changeactivity_v1_change_request_discussion_proto_rawDescOnce.Do(func() {
		file_conduit_changeactivity_v1_change_request_discussion_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_conduit_changeactivity_v1_change_request_discussion_proto_rawDesc), len(file_conduit_changeactivity_v1_change_request_discussion_proto_rawDesc)))
	})
	return file_conduit_changeactivity_v1_change_request_discussion_proto_rawDescData
}

var file_conduit_changeactivity_v1_change_request_discussion_proto_msgTypes = make([]protoimpl.MessageInfo, 3)
var file_conduit_changeactivity_v1_change_request_discussion_proto_goTypes = []any{
	(*ListChangeRequestCommentsRequest)(nil),  // 0: conduit.changeactivity.v1.ListChangeRequestCommentsRequest
	(*ChangeRequestComment)(nil),              // 1: conduit.changeactivity.v1.ChangeRequestComment
	(*ListChangeRequestCommentsResponse)(nil), // 2: conduit.changeactivity.v1.ListChangeRequestCommentsResponse
	(*timestamppb.Timestamp)(nil),             // 3: google.protobuf.Timestamp
}
var file_conduit_changeactivity_v1_change_request_discussion_proto_depIdxs = []int32{
	3, // 0: conduit.changeactivity.v1.ChangeRequestComment.created_at:type_name -> google.protobuf.Timestamp
	1, // 1: conduit.changeactivity.v1.ListChangeRequestCommentsResponse.comments:type_name -> conduit.changeactivity.v1.ChangeRequestComment
	0, // 2: conduit.changeactivity.v1.ChangeRequestDiscussionReadService.ListChangeRequestComments:input_type -> conduit.changeactivity.v1.ListChangeRequestCommentsRequest
	2, // 3: conduit.changeactivity.v1.ChangeRequestDiscussionReadService.ListChangeRequestComments:output_type -> conduit.changeactivity.v1.ListChangeRequestCommentsResponse
	3, // [3:4] is the sub-list for method output_type
	2, // [2:3] is the sub-list for method input_type
	2, // [2:2] is the sub-list for extension type_name
	2, // [2:2] is the sub-list for extension extendee
	0, // [0:2] is the sub-list for field type_name
}

func init() { file_conduit_changeactivity_v1_change_request_discussion_proto_init() }
func file_conduit_changeactivity_v1_change_request_discussion_proto_init() {
	if File_conduit_changeactivity_v1_change_request_discussion_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_conduit_changeactivity_v1_change_request_discussion_proto_rawDesc), len(file_conduit_changeactivity_v1_change_request_discussion_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   3,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_conduit_changeactivity_v1_change_request_discussion_proto_goTypes,
		DependencyIndexes: file_conduit_changeactivity_v1_change_request_discussion_proto_depIdxs,
		MessageInfos:      file_conduit_changeactivity_v1_change_request_discussion_proto_msgTypes,
	}.Build()
	File_conduit_changeactivity_v1_change_request_discussion_proto = out.File
	file_conduit_changeactivity_v1_change_request_discussion_proto_goTypes = nil
	file_conduit_changeactivity_v1_change_request_discussion_proto_depIdxs = nil
}

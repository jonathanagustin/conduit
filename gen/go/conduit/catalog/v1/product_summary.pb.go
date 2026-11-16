//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.12
// 	protoc        (unknown)
// source: conduit/catalog/v1/product_summary.proto

package catalogv1

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

type ListProductSummariesRequest struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// Absent uses the native default 100; explicit limits must be 1..200.
	Limit *int32 `protobuf:"varint,1,opt,name=limit,proto3,oneof" json:"limit,omitempty"`
	// Native opaque cursor, bound to the normalized search filter.
	Cursor        string `protobuf:"bytes,2,opt,name=cursor,proto3" json:"cursor,omitempty"`
	Search        string `protobuf:"bytes,3,opt,name=search,proto3" json:"search,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ListProductSummariesRequest) Reset() {
	*x = ListProductSummariesRequest{}
	mi := &file_conduit_catalog_v1_product_summary_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListProductSummariesRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListProductSummariesRequest) ProtoMessage() {}

func (x *ListProductSummariesRequest) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_catalog_v1_product_summary_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListProductSummariesRequest.ProtoReflect.Descriptor instead.
func (*ListProductSummariesRequest) Descriptor() ([]byte, []int) {
	return file_conduit_catalog_v1_product_summary_proto_rawDescGZIP(), []int{0}
}

func (x *ListProductSummariesRequest) GetLimit() int32 {
	if x != nil && x.Limit != nil {
		return *x.Limit
	}
	return 0
}

func (x *ListProductSummariesRequest) GetCursor() string {
	if x != nil {
		return x.Cursor
	}
	return ""
}

func (x *ListProductSummariesRequest) GetSearch() string {
	if x != nil {
		return x.Search
	}
	return ""
}

// ProductSummary is the native catalog summary, not a Release or publication.
type ProductSummary struct {
	state        protoimpl.MessageState `protogen:"open.v1"`
	Product      string                 `protobuf:"bytes,1,opt,name=product,proto3" json:"product,omitempty"`
	ReleaseCount int64                  `protobuf:"varint,2,opt,name=release_count,json=releaseCount,proto3" json:"release_count,omitempty"`
	// Uses the native catalog's ordering; it is not a new semver resolver.
	LatestVersion string `protobuf:"bytes,3,opt,name=latest_version,json=latestVersion,proto3" json:"latest_version,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ProductSummary) Reset() {
	*x = ProductSummary{}
	mi := &file_conduit_catalog_v1_product_summary_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ProductSummary) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ProductSummary) ProtoMessage() {}

func (x *ProductSummary) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_catalog_v1_product_summary_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ProductSummary.ProtoReflect.Descriptor instead.
func (*ProductSummary) Descriptor() ([]byte, []int) {
	return file_conduit_catalog_v1_product_summary_proto_rawDescGZIP(), []int{1}
}

func (x *ProductSummary) GetProduct() string {
	if x != nil {
		return x.Product
	}
	return ""
}

func (x *ProductSummary) GetReleaseCount() int64 {
	if x != nil {
		return x.ReleaseCount
	}
	return 0
}

func (x *ProductSummary) GetLatestVersion() string {
	if x != nil {
		return x.LatestVersion
	}
	return ""
}

type ListProductSummariesResponse struct {
	state    protoimpl.MessageState `protogen:"open.v1"`
	Products []*ProductSummary      `protobuf:"bytes,1,rep,name=products,proto3" json:"products,omitempty"`
	// Counts only visible, filtered products, before pagination.
	Total          int64  `protobuf:"varint,2,opt,name=total,proto3" json:"total,omitempty"`
	PageSize       int32  `protobuf:"varint,3,opt,name=page_size,json=pageSize,proto3" json:"page_size,omitempty"`
	Limit          int32  `protobuf:"varint,4,opt,name=limit,proto3" json:"limit,omitempty"`
	NextCursor     string `protobuf:"bytes,5,opt,name=next_cursor,json=nextCursor,proto3" json:"next_cursor,omitempty"`
	PreviousCursor string `protobuf:"bytes,6,opt,name=previous_cursor,json=previousCursor,proto3" json:"previous_cursor,omitempty"`
	HasPrevious    bool   `protobuf:"varint,7,opt,name=has_previous,json=hasPrevious,proto3" json:"has_previous,omitempty"`
	unknownFields  protoimpl.UnknownFields
	sizeCache      protoimpl.SizeCache
}

func (x *ListProductSummariesResponse) Reset() {
	*x = ListProductSummariesResponse{}
	mi := &file_conduit_catalog_v1_product_summary_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListProductSummariesResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListProductSummariesResponse) ProtoMessage() {}

func (x *ListProductSummariesResponse) ProtoReflect() protoreflect.Message {
	mi := &file_conduit_catalog_v1_product_summary_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListProductSummariesResponse.ProtoReflect.Descriptor instead.
func (*ListProductSummariesResponse) Descriptor() ([]byte, []int) {
	return file_conduit_catalog_v1_product_summary_proto_rawDescGZIP(), []int{2}
}

func (x *ListProductSummariesResponse) GetProducts() []*ProductSummary {
	if x != nil {
		return x.Products
	}
	return nil
}

func (x *ListProductSummariesResponse) GetTotal() int64 {
	if x != nil {
		return x.Total
	}
	return 0
}

func (x *ListProductSummariesResponse) GetPageSize() int32 {
	if x != nil {
		return x.PageSize
	}
	return 0
}

func (x *ListProductSummariesResponse) GetLimit() int32 {
	if x != nil {
		return x.Limit
	}
	return 0
}

func (x *ListProductSummariesResponse) GetNextCursor() string {
	if x != nil {
		return x.NextCursor
	}
	return ""
}

func (x *ListProductSummariesResponse) GetPreviousCursor() string {
	if x != nil {
		return x.PreviousCursor
	}
	return ""
}

func (x *ListProductSummariesResponse) GetHasPrevious() bool {
	if x != nil {
		return x.HasPrevious
	}
	return false
}

var File_conduit_catalog_v1_product_summary_proto protoreflect.FileDescriptor

const file_conduit_catalog_v1_product_summary_proto_rawDesc = "" +
	"\n" +
	"(conduit/catalog/v1/product_summary.proto\x12\x12conduit.catalog.v1\"r\n" +
	"\x1bListProductSummariesRequest\x12\x19\n" +
	"\x05limit\x18\x01 \x01(\x05H\x00R\x05limit\x88\x01\x01\x12\x16\n" +
	"\x06cursor\x18\x02 \x01(\tR\x06cursor\x12\x16\n" +
	"\x06search\x18\x03 \x01(\tR\x06searchB\b\n" +
	"\x06_limit\"v\n" +
	"\x0eProductSummary\x12\x18\n" +
	"\aproduct\x18\x01 \x01(\tR\aproduct\x12#\n" +
	"\rrelease_count\x18\x02 \x01(\x03R\freleaseCount\x12%\n" +
	"\x0elatest_version\x18\x03 \x01(\tR\rlatestVersion\"\x94\x02\n" +
	"\x1cListProductSummariesResponse\x12>\n" +
	"\bproducts\x18\x01 \x03(\v2\".conduit.catalog.v1.ProductSummaryR\bproducts\x12\x14\n" +
	"\x05total\x18\x02 \x01(\x03R\x05total\x12\x1b\n" +
	"\tpage_size\x18\x03 \x01(\x05R\bpageSize\x12\x14\n" +
	"\x05limit\x18\x04 \x01(\x05R\x05limit\x12\x1f\n" +
	"\vnext_cursor\x18\x05 \x01(\tR\n" +
	"nextCursor\x12'\n" +
	"\x0fprevious_cursor\x18\x06 \x01(\tR\x0epreviousCursor\x12!\n" +
	"\fhas_previous\x18\a \x01(\bR\vhasPrevious2\x94\x01\n" +
	"\x12ProductReadService\x12~\n" +
	"\x14ListProductSummaries\x12/.conduit.catalog.v1.ListProductSummariesRequest\x1a0.conduit.catalog.v1.ListProductSummariesResponse\"\x03\x90\x02\x01BHZFgithub.com/jonathanagustin/conduit/gen/go/conduit/catalog/v1;catalogv1b\x06proto3"

var (
	file_conduit_catalog_v1_product_summary_proto_rawDescOnce sync.Once
	file_conduit_catalog_v1_product_summary_proto_rawDescData []byte
)

func file_conduit_catalog_v1_product_summary_proto_rawDescGZIP() []byte {
	file_conduit_catalog_v1_product_summary_proto_rawDescOnce.Do(func() {
		file_conduit_catalog_v1_product_summary_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_conduit_catalog_v1_product_summary_proto_rawDesc), len(file_conduit_catalog_v1_product_summary_proto_rawDesc)))
	})
	return file_conduit_catalog_v1_product_summary_proto_rawDescData
}

var file_conduit_catalog_v1_product_summary_proto_msgTypes = make([]protoimpl.MessageInfo, 3)
var file_conduit_catalog_v1_product_summary_proto_goTypes = []any{
	(*ListProductSummariesRequest)(nil),  // 0: conduit.catalog.v1.ListProductSummariesRequest
	(*ProductSummary)(nil),               // 1: conduit.catalog.v1.ProductSummary
	(*ListProductSummariesResponse)(nil), // 2: conduit.catalog.v1.ListProductSummariesResponse
}
var file_conduit_catalog_v1_product_summary_proto_depIdxs = []int32{
	1, // 0: conduit.catalog.v1.ListProductSummariesResponse.products:type_name -> conduit.catalog.v1.ProductSummary
	0, // 1: conduit.catalog.v1.ProductReadService.ListProductSummaries:input_type -> conduit.catalog.v1.ListProductSummariesRequest
	2, // 2: conduit.catalog.v1.ProductReadService.ListProductSummaries:output_type -> conduit.catalog.v1.ListProductSummariesResponse
	2, // [2:3] is the sub-list for method output_type
	1, // [1:2] is the sub-list for method input_type
	1, // [1:1] is the sub-list for extension type_name
	1, // [1:1] is the sub-list for extension extendee
	0, // [0:1] is the sub-list for field type_name
}

func init() { file_conduit_catalog_v1_product_summary_proto_init() }
func file_conduit_catalog_v1_product_summary_proto_init() {
	if File_conduit_catalog_v1_product_summary_proto != nil {
		return
	}
	file_conduit_catalog_v1_product_summary_proto_msgTypes[0].OneofWrappers = []any{}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_conduit_catalog_v1_product_summary_proto_rawDesc), len(file_conduit_catalog_v1_product_summary_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   3,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_conduit_catalog_v1_product_summary_proto_goTypes,
		DependencyIndexes: file_conduit_catalog_v1_product_summary_proto_depIdxs,
		MessageInfos:      file_conduit_catalog_v1_product_summary_proto_msgTypes,
	}.Build()
	File_conduit_catalog_v1_product_summary_proto = out.File
	file_conduit_catalog_v1_product_summary_proto_goTypes = nil
	file_conduit_catalog_v1_product_summary_proto_depIdxs = nil
}

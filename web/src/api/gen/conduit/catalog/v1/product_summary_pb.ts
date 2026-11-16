// Regenerated from proto/conduit. DO NOT EDIT.
// @generated from file conduit/catalog/v1/product_summary.proto (package conduit.catalog.v1, syntax proto3)
/* eslint-disable */

import type {
  GenFile,
  GenMessage,
  GenService,
} from "@bufbuild/protobuf/codegenv2";
import {
  fileDesc,
  messageDesc,
  serviceDesc,
} from "@bufbuild/protobuf/codegenv2";
import type { Message } from "@bufbuild/protobuf";

/**
 * Describes the file conduit/catalog/v1/product_summary.proto.
 */
export const file_conduit_catalog_v1_product_summary: GenFile =
  /*@__PURE__*/
  fileDesc(
    "Cihjb25kdWl0L2NhdGFsb2cvdjEvcHJvZHVjdF9zdW1tYXJ5LnByb3RvEhJjb25kdWl0LmNhdGFsb2cudjEiWwobTGlzdFByb2R1Y3RTdW1tYXJpZXNSZXF1ZXN0EhIKBWxpbWl0GAEgASgFSACIAQESDgoGY3Vyc29yGAIgASgJEg4KBnNlYXJjaBgDIAEoCUIICgZfbGltaXQiUAoOUHJvZHVjdFN1bW1hcnkSDwoHcHJvZHVjdBgBIAEoCRIVCg1yZWxlYXNlX2NvdW50GAIgASgDEhYKDmxhdGVzdF92ZXJzaW9uGAMgASgJIskBChxMaXN0UHJvZHVjdFN1bW1hcmllc1Jlc3BvbnNlEjQKCHByb2R1Y3RzGAEgAygLMiIuY29uZHVpdC5jYXRhbG9nLnYxLlByb2R1Y3RTdW1tYXJ5Eg0KBXRvdGFsGAIgASgDEhEKCXBhZ2Vfc2l6ZRgDIAEoBRINCgVsaW1pdBgEIAEoBRITCgtuZXh0X2N1cnNvchgFIAEoCRIXCg9wcmV2aW91c19jdXJzb3IYBiABKAkSFAoMaGFzX3ByZXZpb3VzGAcgASgIMpQBChJQcm9kdWN0UmVhZFNlcnZpY2USfgoUTGlzdFByb2R1Y3RTdW1tYXJpZXMSLy5jb25kdWl0LmNhdGFsb2cudjEuTGlzdFByb2R1Y3RTdW1tYXJpZXNSZXF1ZXN0GjAuY29uZHVpdC5jYXRhbG9nLnYxLkxpc3RQcm9kdWN0U3VtbWFyaWVzUmVzcG9uc2UiA5ACAUJIWkZnaXRodWIuY29tL2pvbmF0aGFuYWd1c3Rpbi9jb25kdWl0L2dlbi9nby9jb25kdWl0L2NhdGFsb2cvdjE7Y2F0YWxvZ3YxYgZwcm90bzM",
  );

/**
 * @generated from message conduit.catalog.v1.ListProductSummariesRequest
 */
export type ListProductSummariesRequest =
  Message<"conduit.catalog.v1.ListProductSummariesRequest"> & {
    /**
     * Absent uses the native default 100; explicit limits must be 1..200.
     *
     * @generated from field: optional int32 limit = 1;
     */
    limit?: number | undefined;

    /**
     * Native opaque cursor, bound to the normalized search filter.
     *
     * @generated from field: string cursor = 2;
     */
    cursor: string;

    /**
     * @generated from field: string search = 3;
     */
    search: string;
  };

/**
 * Describes the message conduit.catalog.v1.ListProductSummariesRequest.
 * Use `create(ListProductSummariesRequestSchema)` to create a new message.
 */
export const ListProductSummariesRequestSchema: GenMessage<ListProductSummariesRequest> =
  /*@__PURE__*/
  messageDesc(file_conduit_catalog_v1_product_summary, 0);

/**
 * ProductSummary is the native catalog summary, not a Release or publication.
 *
 * @generated from message conduit.catalog.v1.ProductSummary
 */
export type ProductSummary = Message<"conduit.catalog.v1.ProductSummary"> & {
  /**
   * @generated from field: string product = 1;
   */
  product: string;

  /**
   * @generated from field: int64 release_count = 2;
   */
  releaseCount: bigint;

  /**
   * Uses the native catalog's ordering; it is not a new semver resolver.
   *
   * @generated from field: string latest_version = 3;
   */
  latestVersion: string;
};

/**
 * Describes the message conduit.catalog.v1.ProductSummary.
 * Use `create(ProductSummarySchema)` to create a new message.
 */
export const ProductSummarySchema: GenMessage<ProductSummary> =
  /*@__PURE__*/
  messageDesc(file_conduit_catalog_v1_product_summary, 1);

/**
 * @generated from message conduit.catalog.v1.ListProductSummariesResponse
 */
export type ListProductSummariesResponse =
  Message<"conduit.catalog.v1.ListProductSummariesResponse"> & {
    /**
     * @generated from field: repeated conduit.catalog.v1.ProductSummary products = 1;
     */
    products: ProductSummary[];

    /**
     * Counts only visible, filtered products, before pagination.
     *
     * @generated from field: int64 total = 2;
     */
    total: bigint;

    /**
     * @generated from field: int32 page_size = 3;
     */
    pageSize: number;

    /**
     * @generated from field: int32 limit = 4;
     */
    limit: number;

    /**
     * @generated from field: string next_cursor = 5;
     */
    nextCursor: string;

    /**
     * @generated from field: string previous_cursor = 6;
     */
    previousCursor: string;

    /**
     * @generated from field: bool has_previous = 7;
     */
    hasPrevious: boolean;
  };

/**
 * Describes the message conduit.catalog.v1.ListProductSummariesResponse.
 * Use `create(ListProductSummariesResponseSchema)` to create a new message.
 */
export const ListProductSummariesResponseSchema: GenMessage<ListProductSummariesResponse> =
  /*@__PURE__*/
  messageDesc(file_conduit_catalog_v1_product_summary, 2);

/**
 * ProductReadService reads existing authorized native catalog projections.
 *
 * @generated from service conduit.catalog.v1.ProductReadService
 */
export const ProductReadService: GenService<{
  /**
   * ListProductSummaries reads GET /v1/products?view=summary, never publications.
   *
   * @generated from rpc conduit.catalog.v1.ProductReadService.ListProductSummaries
   */
  listProductSummaries: {
    methodKind: "unary";
    input: typeof ListProductSummariesRequestSchema;
    output: typeof ListProductSummariesResponseSchema;
  };
}> = /*@__PURE__*/ serviceDesc(file_conduit_catalog_v1_product_summary, 0);

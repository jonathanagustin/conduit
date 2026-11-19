// Regenerated from proto/conduit. DO NOT EDIT.
// @generated from file conduit/environments/v1/environment_catalog.proto (package conduit.environments.v1, syntax proto3)
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
import type { EnvironmentRecord } from "./environment_detail_pb.js";
import { file_conduit_environments_v1_environment_detail } from "./environment_detail_pb.js";
import type { Message } from "@bufbuild/protobuf";

/**
 * Describes the file conduit/environments/v1/environment_catalog.proto.
 */
export const file_conduit_environments_v1_environment_catalog: GenFile =
  /*@__PURE__*/
  fileDesc(
    "CjFjb25kdWl0L2Vudmlyb25tZW50cy92MS9lbnZpcm9ubWVudF9jYXRhbG9nLnByb3RvEhdjb25kdWl0LmVudmlyb25tZW50cy52MSJ7ChdMaXN0RW52aXJvbm1lbnRzUmVxdWVzdBIOCgZzZWFyY2gYASABKAkSDgoGY3Vyc29yGAIgASgJEhIKBWxpbWl0GAMgASgFSACIAQESFQoIc3BhY2VfaWQYBCABKAlIAYgBAUIICgZfbGltaXRCCwoJX3NwYWNlX2lkItEBChhMaXN0RW52aXJvbm1lbnRzUmVzcG9uc2USQAoMZW52aXJvbm1lbnRzGAEgAygLMiouY29uZHVpdC5lbnZpcm9ubWVudHMudjEuRW52aXJvbm1lbnRSZWNvcmQSDQoFdG90YWwYAiABKAMSEQoJcGFnZV9zaXplGAMgASgFEg0KBWxpbWl0GAQgASgFEhMKC25leHRfY3Vyc29yGAUgASgJEhcKD3ByZXZpb3VzX2N1cnNvchgGIAEoCRIUCgxoYXNfcHJldmlvdXMYByABKAgymQEKGUVudmlyb25tZW50Q2F0YWxvZ1NlcnZpY2USfAoQTGlzdEVudmlyb25tZW50cxIwLmNvbmR1aXQuZW52aXJvbm1lbnRzLnYxLkxpc3RFbnZpcm9ubWVudHNSZXF1ZXN0GjEuY29uZHVpdC5lbnZpcm9ubWVudHMudjEuTGlzdEVudmlyb25tZW50c1Jlc3BvbnNlIgOQAgFCUlpQZ2l0aHViLmNvbS9qb25hdGhhbmFndXN0aW4vY29uZHVpdC9nZW4vZ28vY29uZHVpdC9lbnZpcm9ubWVudHMvdjE7ZW52aXJvbm1lbnRzdjFiBnByb3RvMw",
    [file_conduit_environments_v1_environment_detail],
  );

/**
 * ListEnvironmentsRequest selects the full projection, not summary view.
 *
 * @generated from message conduit.environments.v1.ListEnvironmentsRequest
 */
export type ListEnvironmentsRequest =
  Message<"conduit.environments.v1.ListEnvironmentsRequest"> & {
    /**
     * @generated from field: string search = 1;
     */
    search: string;

    /**
     * @generated from field: string cursor = 2;
     */
    cursor: string;

    /**
     * Absent uses the native full-list default of 20; present must be 1 through 20.
     * The summary index's 100/200 limits do not apply to this full-record method.
     *
     * @generated from field: optional int32 limit = 3;
     */
    limit?: number | undefined;

    /**
     * Absence is unscoped; present empty/invalid selection remains invalid.
     *
     * @generated from field: optional string space_id = 4;
     */
    spaceId?: string | undefined;
  };

/**
 * Describes the message conduit.environments.v1.ListEnvironmentsRequest.
 * Use `create(ListEnvironmentsRequestSchema)` to create a new message.
 */
export const ListEnvironmentsRequestSchema: GenMessage<ListEnvironmentsRequest> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_catalog, 0);

/**
 * ListEnvironmentsResponse retains native full-record pagination metadata.
 *
 * @generated from message conduit.environments.v1.ListEnvironmentsResponse
 */
export type ListEnvironmentsResponse =
  Message<"conduit.environments.v1.ListEnvironmentsResponse"> & {
    /**
     * @generated from field: repeated conduit.environments.v1.EnvironmentRecord environments = 1;
     */
    environments: EnvironmentRecord[];

    /**
     * Counts only readable records matching search within the selected grouping.
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
 * Describes the message conduit.environments.v1.ListEnvironmentsResponse.
 * Use `create(ListEnvironmentsResponseSchema)` to create a new message.
 */
export const ListEnvironmentsResponseSchema: GenMessage<ListEnvironmentsResponse> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_catalog, 1);

/**
 * EnvironmentCatalogService declares the full native Environment index.
 * It reuses the full record without changing existing summary/detail services.
 * Declaration does not mount a method or grant resource/Space authority.
 *
 * @generated from service conduit.environments.v1.EnvironmentCatalogService
 */
export const EnvironmentCatalogService: GenService<{
  /**
   * ListEnvironments corresponds to GET /v1/environments with absent/full view.
   * Filter by original-carrier resource authority and readable Space membership
   * before search, totals and cursor pagination. Membership grants no authority.
   *
   * @generated from rpc conduit.environments.v1.EnvironmentCatalogService.ListEnvironments
   */
  listEnvironments: {
    methodKind: "unary";
    input: typeof ListEnvironmentsRequestSchema;
    output: typeof ListEnvironmentsResponseSchema;
  };
}> =
  /*@__PURE__*/
  serviceDesc(file_conduit_environments_v1_environment_catalog, 0);

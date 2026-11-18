// Regenerated from proto/conduit. DO NOT EDIT.
// @generated from file conduit/environments/v1/environment_summary.proto (package conduit.environments.v1, syntax proto3)
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
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { file_google_protobuf_timestamp } from "@bufbuild/protobuf/wkt";
import type { Message } from "@bufbuild/protobuf";

/**
 * Describes the file conduit/environments/v1/environment_summary.proto.
 */
export const file_conduit_environments_v1_environment_summary: GenFile =
  /*@__PURE__*/
  fileDesc(
    "CjFjb25kdWl0L2Vudmlyb25tZW50cy92MS9lbnZpcm9ubWVudF9zdW1tYXJ5LnByb3RvEhdjb25kdWl0LmVudmlyb25tZW50cy52MSJxCh9MaXN0RW52aXJvbm1lbnRTdW1tYXJpZXNSZXF1ZXN0EhIKBWxpbWl0GAEgASgFSACIAQESDgoGY3Vyc29yGAIgASgJEg4KBnNlYXJjaBgDIAEoCRIQCghzcGFjZV9pZBgEIAEoCUIICgZfbGltaXQiNQoTRW52aXJvbm1lbnRJZGVudGl0eRIKCgJpZBgBIAEoCRISCgpnZW5lcmF0aW9uGAIgASgJIogBChhFbnZpcm9ubWVudFJlcG9ydFN1bW1hcnkSEwoLZW52aXJvbm1lbnQYASABKAkSEAoIYWdlbnRfaWQYAiABKAkSLwoLb2JzZXJ2ZWRfYXQYAyABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wEhQKDGVudGl0eV9jb3VudBgEIAEoAyJhCg1TcG9rZUxpdmVuZXNzEhAKCGFnZW50X2lkGAEgASgJEg0KBXN0YXRlGAIgASgJEi8KC29ic2VydmVkX2F0GAMgASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcCLTAQoSRW52aXJvbm1lbnRTdW1tYXJ5EkEKC2Vudmlyb25tZW50GAEgASgLMiwuY29uZHVpdC5lbnZpcm9ubWVudHMudjEuRW52aXJvbm1lbnRJZGVudGl0eRJDCghyZXBvcnRlZBgCIAEoCzIxLmNvbmR1aXQuZW52aXJvbm1lbnRzLnYxLkVudmlyb25tZW50UmVwb3J0U3VtbWFyeRI1CgVzcG9rZRgDIAEoCzImLmNvbmR1aXQuZW52aXJvbm1lbnRzLnYxLlNwb2tlTGl2ZW5lc3Mi2gEKIExpc3RFbnZpcm9ubWVudFN1bW1hcmllc1Jlc3BvbnNlEkEKDGVudmlyb25tZW50cxgBIAMoCzIrLmNvbmR1aXQuZW52aXJvbm1lbnRzLnYxLkVudmlyb25tZW50U3VtbWFyeRINCgV0b3RhbBgCIAEoAxIRCglwYWdlX3NpemUYAyABKAUSDQoFbGltaXQYBCABKAUSEwoLbmV4dF9jdXJzb3IYBSABKAkSFwoPcHJldmlvdXNfY3Vyc29yGAYgASgJEhQKDGhhc19wcmV2aW91cxgHIAEoCDKvAQoWRW52aXJvbm1lbnRSZWFkU2VydmljZRKUAQoYTGlzdEVudmlyb25tZW50U3VtbWFyaWVzEjguY29uZHVpdC5lbnZpcm9ubWVudHMudjEuTGlzdEVudmlyb25tZW50U3VtbWFyaWVzUmVxdWVzdBo5LmNvbmR1aXQuZW52aXJvbm1lbnRzLnYxLkxpc3RFbnZpcm9ubWVudFN1bW1hcmllc1Jlc3BvbnNlIgOQAgFCUlpQZ2l0aHViLmNvbS9qb25hdGhhbmFndXN0aW4vY29uZHVpdC9nZW4vZ28vY29uZHVpdC9lbnZpcm9ubWVudHMvdjE7ZW52aXJvbm1lbnRzdjFiBnByb3RvMw",
    [file_google_protobuf_timestamp],
  );

/**
 * @generated from message conduit.environments.v1.ListEnvironmentSummariesRequest
 */
export type ListEnvironmentSummariesRequest =
  Message<"conduit.environments.v1.ListEnvironmentSummariesRequest"> & {
    /**
     * Absent uses native summary default 100; explicit values must be 1..200.
     *
     * @generated from field: optional int32 limit = 1;
     */
    limit?: number | undefined;

    /**
     * Native opaque continuation, bound to normalized search and selected Space.
     *
     * @generated from field: string cursor = 2;
     */
    cursor: string;

    /**
     * Native case-insensitive Environment ID substring search.
     *
     * @generated from field: string search = 3;
     */
    search: string;

    /**
     * Empty means no Space selection, never an authorization bypass.
     *
     * @generated from field: string space_id = 4;
     */
    spaceId: string;
  };

/**
 * Describes the message conduit.environments.v1.ListEnvironmentSummariesRequest.
 * Use `create(ListEnvironmentSummariesRequestSchema)` to create a new message.
 */
export const ListEnvironmentSummariesRequestSchema: GenMessage<ListEnvironmentSummariesRequest> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_summary, 0);

/**
 * EnvironmentIdentity contains existing native identity, not configuration.
 * Generation is an opaque string and must not be parsed as a numeric revision.
 *
 * @generated from message conduit.environments.v1.EnvironmentIdentity
 */
export type EnvironmentIdentity =
  Message<"conduit.environments.v1.EnvironmentIdentity"> & {
    /**
     * @generated from field: string id = 1;
     */
    id: string;

    /**
     * @generated from field: string generation = 2;
     */
    generation: string;
  };

/**
 * Describes the message conduit.environments.v1.EnvironmentIdentity.
 * Use `create(EnvironmentIdentitySchema)` to create a new message.
 */
export const EnvironmentIdentitySchema: GenMessage<EnvironmentIdentity> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_summary, 1);

/**
 * EnvironmentReportSummary is reported evidence, never desired state.
 *
 * @generated from message conduit.environments.v1.EnvironmentReportSummary
 */
export type EnvironmentReportSummary =
  Message<"conduit.environments.v1.EnvironmentReportSummary"> & {
    /**
     * @generated from field: string environment = 1;
     */
    environment: string;

    /**
     * @generated from field: string agent_id = 2;
     */
    agentId: string;

    /**
     * @generated from field: google.protobuf.Timestamp observed_at = 3;
     */
    observedAt?: Timestamp | undefined;

    /**
     * @generated from field: int64 entity_count = 4;
     */
    entityCount: bigint;
  };

/**
 * Describes the message conduit.environments.v1.EnvironmentReportSummary.
 * Use `create(EnvironmentReportSummarySchema)` to create a new message.
 */
export const EnvironmentReportSummarySchema: GenMessage<EnvironmentReportSummary> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_summary, 2);

/**
 * SpokeLiveness preserves open native state vocabulary and observation time.
 * Absence of an observation must not be replaced by the current time.
 *
 * @generated from message conduit.environments.v1.SpokeLiveness
 */
export type SpokeLiveness = Message<"conduit.environments.v1.SpokeLiveness"> & {
  /**
   * @generated from field: string agent_id = 1;
   */
  agentId: string;

  /**
   * @generated from field: string state = 2;
   */
  state: string;

  /**
   * @generated from field: google.protobuf.Timestamp observed_at = 3;
   */
  observedAt?: Timestamp | undefined;
};

/**
 * Describes the message conduit.environments.v1.SpokeLiveness.
 * Use `create(SpokeLivenessSchema)` to create a new message.
 */
export const SpokeLivenessSchema: GenMessage<SpokeLiveness> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_summary, 3);

/**
 * @generated from message conduit.environments.v1.EnvironmentSummary
 */
export type EnvironmentSummary =
  Message<"conduit.environments.v1.EnvironmentSummary"> & {
    /**
     * @generated from field: conduit.environments.v1.EnvironmentIdentity environment = 1;
     */
    environment?: EnvironmentIdentity | undefined;

    /**
     * Message presence distinguishes no report from an observed empty report.
     *
     * @generated from field: conduit.environments.v1.EnvironmentReportSummary reported = 2;
     */
    reported?: EnvironmentReportSummary | undefined;

    /**
     * @generated from field: conduit.environments.v1.SpokeLiveness spoke = 3;
     */
    spoke?: SpokeLiveness | undefined;
  };

/**
 * Describes the message conduit.environments.v1.EnvironmentSummary.
 * Use `create(EnvironmentSummarySchema)` to create a new message.
 */
export const EnvironmentSummarySchema: GenMessage<EnvironmentSummary> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_summary, 4);

/**
 * @generated from message conduit.environments.v1.ListEnvironmentSummariesResponse
 */
export type ListEnvironmentSummariesResponse =
  Message<"conduit.environments.v1.ListEnvironmentSummariesResponse"> & {
    /**
     * @generated from field: repeated conduit.environments.v1.EnvironmentSummary environments = 1;
     */
    environments: EnvironmentSummary[];

    /**
     * Total counts only authorized, Space-selected and search-filtered records.
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
 * Describes the message conduit.environments.v1.ListEnvironmentSummariesResponse.
 * Use `create(ListEnvironmentSummariesResponseSchema)` to create a new message.
 */
export const ListEnvironmentSummariesResponseSchema: GenMessage<ListEnvironmentSummariesResponse> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_summary, 5);

/**
 * EnvironmentReadService declares native reads, not execution authority.
 * A generated service is not registered or served until its authorized adapter
 * is explicitly mounted. REST remains the supported Environment transport.
 *
 * @generated from service conduit.environments.v1.EnvironmentReadService
 */
export const EnvironmentReadService: GenService<{
  /**
   * ListEnvironmentSummaries projects GET /v1/environments?view=summary.
   * It omits Environment configuration/policy collections and Entity bodies.
   *
   * @generated from rpc conduit.environments.v1.EnvironmentReadService.ListEnvironmentSummaries
   */
  listEnvironmentSummaries: {
    methodKind: "unary";
    input: typeof ListEnvironmentSummariesRequestSchema;
    output: typeof ListEnvironmentSummariesResponseSchema;
  };
}> =
  /*@__PURE__*/
  serviceDesc(file_conduit_environments_v1_environment_summary, 0);

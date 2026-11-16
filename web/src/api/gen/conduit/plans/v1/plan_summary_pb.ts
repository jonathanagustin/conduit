// Regenerated from proto/conduit. DO NOT EDIT.
// @generated from file conduit/plans/v1/plan_summary.proto (package conduit.plans.v1, syntax proto3)
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
 * Describes the file conduit/plans/v1/plan_summary.proto.
 */
export const file_conduit_plans_v1_plan_summary: GenFile =
  /*@__PURE__*/
  fileDesc(
    "CiNjb25kdWl0L3BsYW5zL3YxL3BsYW5fc3VtbWFyeS5wcm90bxIQY29uZHVpdC5wbGFucy52MSKWAQoYTGlzdFBsYW5TdW1tYXJpZXNSZXF1ZXN0EhIKBWxpbWl0GAEgASgFSACIAQESDgoGY3Vyc29yGAIgASgJEhMKC2Vudmlyb25tZW50GAMgASgJEg4KBnN0YXR1cxgEIAEoCRIXCg9pbnN0YWxsYXRpb25faWQYBSABKAkSDgoGc2VhcmNoGAYgASgJQggKBl9saW1pdCL4AQoLUGxhblN1bW1hcnkSCgoCaWQYASABKAkSEwoLZW52aXJvbm1lbnQYAiABKAkSDgoGc3RhdHVzGAMgASgJEhIKCnN0ZXBfY291bnQYBCABKAMSFgoOYXBwcm92YWxfY291bnQYBSABKAMSGgoScmVxdWlyZWRfYXBwcm92YWxzGAYgASgDEi4KCmNyZWF0ZWRfYXQYByABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wEi4KCnVwZGF0ZWRfYXQYCCABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wEhAKCHJldmlzaW9uGAkgASgEIr4BChlMaXN0UGxhblN1bW1hcmllc1Jlc3BvbnNlEiwKBXBsYW5zGAEgAygLMh0uY29uZHVpdC5wbGFucy52MS5QbGFuU3VtbWFyeRINCgV0b3RhbBgCIAEoAxIRCglwYWdlX3NpemUYAyABKAUSDQoFbGltaXQYBCABKAUSEwoLbmV4dF9jdXJzb3IYBSABKAkSFwoPcHJldmlvdXNfY3Vyc29yGAYgASgJEhQKDGhhc19wcmV2aW91cxgHIAEoCDKEAQoPUGxhblJlYWRTZXJ2aWNlEnEKEUxpc3RQbGFuU3VtbWFyaWVzEiouY29uZHVpdC5wbGFucy52MS5MaXN0UGxhblN1bW1hcmllc1JlcXVlc3QaKy5jb25kdWl0LnBsYW5zLnYxLkxpc3RQbGFuU3VtbWFyaWVzUmVzcG9uc2UiA5ACAUJEWkJnaXRodWIuY29tL2pvbmF0aGFuYWd1c3Rpbi9jb25kdWl0L2dlbi9nby9jb25kdWl0L3BsYW5zL3YxO3BsYW5zdjFiBnByb3RvMw",
    [file_google_protobuf_timestamp],
  );

/**
 * @generated from message conduit.plans.v1.ListPlanSummariesRequest
 */
export type ListPlanSummariesRequest =
  Message<"conduit.plans.v1.ListPlanSummariesRequest"> & {
    /**
     * limit is absent for the native default 100; valid explicit values are 1..200.
     *
     * @generated from field: optional int32 limit = 1;
     */
    limit?: number | undefined;

    /**
     * cursor is the native opaque continuation, bound to the active filters.
     *
     * @generated from field: string cursor = 2;
     */
    cursor: string;

    /**
     * @generated from field: string environment = 3;
     */
    environment: string;

    /**
     * @generated from field: string status = 4;
     */
    status: string;

    /**
     * @generated from field: string installation_id = 5;
     */
    installationId: string;

    /**
     * @generated from field: string search = 6;
     */
    search: string;
  };

/**
 * Describes the message conduit.plans.v1.ListPlanSummariesRequest.
 * Use `create(ListPlanSummariesRequestSchema)` to create a new message.
 */
export const ListPlanSummariesRequestSchema: GenMessage<ListPlanSummariesRequest> =
  /*@__PURE__*/
  messageDesc(file_conduit_plans_v1_plan_summary, 0);

/**
 * PlanSummary is the existing summary projection, not a full Plan or approval.
 *
 * @generated from message conduit.plans.v1.PlanSummary
 */
export type PlanSummary = Message<"conduit.plans.v1.PlanSummary"> & {
  /**
   * @generated from field: string id = 1;
   */
  id: string;

  /**
   * @generated from field: string environment = 2;
   */
  environment: string;

  /**
   * status is open native vocabulary, including future statuses.
   *
   * @generated from field: string status = 3;
   */
  status: string;

  /**
   * @generated from field: int64 step_count = 4;
   */
  stepCount: bigint;

  /**
   * @generated from field: int64 approval_count = 5;
   */
  approvalCount: bigint;

  /**
   * @generated from field: int64 required_approvals = 6;
   */
  requiredApprovals: bigint;

  /**
   * @generated from field: google.protobuf.Timestamp created_at = 7;
   */
  createdAt?: Timestamp | undefined;

  /**
   * @generated from field: google.protobuf.Timestamp updated_at = 8;
   */
  updatedAt?: Timestamp | undefined;

  /**
   * @generated from field: uint64 revision = 9;
   */
  revision: bigint;
};

/**
 * Describes the message conduit.plans.v1.PlanSummary.
 * Use `create(PlanSummarySchema)` to create a new message.
 */
export const PlanSummarySchema: GenMessage<PlanSummary> =
  /*@__PURE__*/
  messageDesc(file_conduit_plans_v1_plan_summary, 1);

/**
 * @generated from message conduit.plans.v1.ListPlanSummariesResponse
 */
export type ListPlanSummariesResponse =
  Message<"conduit.plans.v1.ListPlanSummariesResponse"> & {
    /**
     * @generated from field: repeated conduit.plans.v1.PlanSummary plans = 1;
     */
    plans: PlanSummary[];

    /**
     * total counts only authorized, filtered matches.
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
 * Describes the message conduit.plans.v1.ListPlanSummariesResponse.
 * Use `create(ListPlanSummariesResponseSchema)` to create a new message.
 */
export const ListPlanSummariesResponseSchema: GenMessage<ListPlanSummariesResponse> =
  /*@__PURE__*/
  messageDesc(file_conduit_plans_v1_plan_summary, 2);

/**
 * PlanReadService reads existing authorized native projections, never commands.
 *
 * @generated from service conduit.plans.v1.PlanReadService
 */
export const PlanReadService: GenService<{
  /**
   * ListPlanSummaries reads GET /v1/plans?view=summary. It never returns a lease.
   *
   * @generated from rpc conduit.plans.v1.PlanReadService.ListPlanSummaries
   */
  listPlanSummaries: {
    methodKind: "unary";
    input: typeof ListPlanSummariesRequestSchema;
    output: typeof ListPlanSummariesResponseSchema;
  };
}> = /*@__PURE__*/ serviceDesc(file_conduit_plans_v1_plan_summary, 0);

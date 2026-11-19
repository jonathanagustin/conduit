// Regenerated from proto/conduit. DO NOT EDIT.
// @generated from file conduit/plans/v1/plan_scoped_summary.proto (package conduit.plans.v1, syntax proto3)
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
import type { ListPlanSummariesResponse } from "./plan_summary_pb.js";
import { file_conduit_plans_v1_plan_summary } from "./plan_summary_pb.js";
import type { Message } from "@bufbuild/protobuf";

/**
 * Describes the file conduit/plans/v1/plan_scoped_summary.proto.
 */
export const file_conduit_plans_v1_plan_scoped_summary: GenFile =
  /*@__PURE__*/
  fileDesc(
    "Cipjb25kdWl0L3BsYW5zL3YxL3BsYW5fc2NvcGVkX3N1bW1hcnkucHJvdG8SEGNvbmR1aXQucGxhbnMudjEirwEKH0xpc3RQbGFuU3VtbWFyaWVzSW5TcGFjZVJlcXVlc3QSEAoIc3BhY2VfaWQYASABKAkSEgoFbGltaXQYAiABKAVIAIgBARIOCgZjdXJzb3IYAyABKAkSEwoLZW52aXJvbm1lbnQYBCABKAkSDgoGc3RhdHVzGAUgASgJEhcKD2luc3RhbGxhdGlvbl9pZBgGIAEoCRIOCgZzZWFyY2gYByABKAlCCAoGX2xpbWl0Im8KIExpc3RQbGFuU3VtbWFyaWVzSW5TcGFjZVJlc3BvbnNlEhAKCHNwYWNlX2lkGAEgASgJEjkKBHBhZ2UYAiABKAsyKy5jb25kdWl0LnBsYW5zLnYxLkxpc3RQbGFuU3VtbWFyaWVzUmVzcG9uc2UyoAEKFVBsYW5TY29wZWRSZWFkU2VydmljZRKGAQoYTGlzdFBsYW5TdW1tYXJpZXNJblNwYWNlEjEuY29uZHVpdC5wbGFucy52MS5MaXN0UGxhblN1bW1hcmllc0luU3BhY2VSZXF1ZXN0GjIuY29uZHVpdC5wbGFucy52MS5MaXN0UGxhblN1bW1hcmllc0luU3BhY2VSZXNwb25zZSIDkAIBQkRaQmdpdGh1Yi5jb20vam9uYXRoYW5hZ3VzdGluL2NvbmR1aXQvZ2VuL2dvL2NvbmR1aXQvcGxhbnMvdjE7cGxhbnN2MWIGcHJvdG8z",
    [file_conduit_plans_v1_plan_summary],
  );

/**
 * @generated from message conduit.plans.v1.ListPlanSummariesInSpaceRequest
 */
export type ListPlanSummariesInSpaceRequest =
  Message<"conduit.plans.v1.ListPlanSummariesInSpaceRequest"> & {
    /**
     * Required native identity; absent/empty/invalid values are input failures.
     *
     * @generated from field: string space_id = 1;
     */
    spaceId: string;

    /**
     * Absent uses the native summary default 100; present must be 1 through 200.
     *
     * @generated from field: optional int32 limit = 2;
     */
    limit?: number | undefined;

    /**
     * Existing opaque cursor, bound to filters and this Space; not a snapshot.
     *
     * @generated from field: string cursor = 3;
     */
    cursor: string;

    /**
     * @generated from field: string environment = 4;
     */
    environment: string;

    /**
     * @generated from field: string status = 5;
     */
    status: string;

    /**
     * @generated from field: string installation_id = 6;
     */
    installationId: string;

    /**
     * @generated from field: string search = 7;
     */
    search: string;
  };

/**
 * Describes the message conduit.plans.v1.ListPlanSummariesInSpaceRequest.
 * Use `create(ListPlanSummariesInSpaceRequestSchema)` to create a new message.
 */
export const ListPlanSummariesInSpaceRequestSchema: GenMessage<ListPlanSummariesInSpaceRequest> =
  /*@__PURE__*/
  messageDesc(file_conduit_plans_v1_plan_scoped_summary, 0);

/**
 * @generated from message conduit.plans.v1.ListPlanSummariesInSpaceResponse
 */
export type ListPlanSummariesInSpaceResponse =
  Message<"conduit.plans.v1.ListPlanSummariesInSpaceResponse"> & {
    /**
     * Echo the validated selection. Clients bind it before accepting any rows;
     * this is a grouping coordinate, not a grant or a delegated identity.
     *
     * @generated from field: string space_id = 1;
     */
    spaceId: string;

    /**
     * Existing summary fields and native current-visibility pagination metadata.
     *
     * @generated from field: conduit.plans.v1.ListPlanSummariesResponse page = 2;
     */
    page?: ListPlanSummariesResponse | undefined;
  };

/**
 * Describes the message conduit.plans.v1.ListPlanSummariesInSpaceResponse.
 * Use `create(ListPlanSummariesInSpaceResponseSchema)` to create a new message.
 */
export const ListPlanSummariesInSpaceResponseSchema: GenMessage<ListPlanSummariesInSpaceResponse> =
  /*@__PURE__*/
  messageDesc(file_conduit_plans_v1_plan_scoped_summary, 1);

/**
 * PlanScopedReadService is a separate native read capability. An older Hub
 * refuses this procedure rather than ignoring an unknown Space selector on the
 * unscoped procedure. Declaration alone mounts no route or resource authority.
 *
 * @generated from service conduit.plans.v1.PlanScopedReadService
 */
export const PlanScopedReadService: GenService<{
  /**
   * ListPlanSummariesInSpace reads GET /v1/plans?view=summary&spaceId=SPACE.
   * Authorize the original carrier's selected persisted Space before private
   * membership and Plan reads. Membership narrows visibility; it grants none.
   *
   * @generated from rpc conduit.plans.v1.PlanScopedReadService.ListPlanSummariesInSpace
   */
  listPlanSummariesInSpace: {
    methodKind: "unary";
    input: typeof ListPlanSummariesInSpaceRequestSchema;
    output: typeof ListPlanSummariesInSpaceResponseSchema;
  };
}> = /*@__PURE__*/ serviceDesc(file_conduit_plans_v1_plan_scoped_summary, 0);

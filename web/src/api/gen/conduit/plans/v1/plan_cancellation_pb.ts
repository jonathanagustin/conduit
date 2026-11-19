// Regenerated from proto/conduit. DO NOT EDIT.
// @generated from file conduit/plans/v1/plan_cancellation.proto (package conduit.plans.v1, syntax proto3)
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
import type { PlanSummary } from "./plan_summary_pb.js";
import { file_conduit_plans_v1_plan_summary } from "./plan_summary_pb.js";
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { file_google_protobuf_timestamp } from "@bufbuild/protobuf/wkt";
import type { Message } from "@bufbuild/protobuf";

/**
 * Describes the file conduit/plans/v1/plan_cancellation.proto.
 */
export const file_conduit_plans_v1_plan_cancellation: GenFile =
  /*@__PURE__*/
  fileDesc(
    "Cihjb25kdWl0L3BsYW5zL3YxL3BsYW5fY2FuY2VsbGF0aW9uLnByb3RvEhBjb25kdWl0LnBsYW5zLnYxIo8BChFDYW5jZWxQbGFuUmVxdWVzdBIPCgdwbGFuX2lkGAEgASgJEg4KBnJlYXNvbhgCIAEoCRIaChJjcmVhdGVfc3VwcHJlc3Npb24YAyABKAgSJAocc3VwcHJlc3Npb25fZHVyYXRpb25fc2Vjb25kcxgEIAEoAxIXCg9pbnN0YWxsYXRpb25faWQYBSABKAkiawoYUmVjb3JkZWRQbGFuQ2FuY2VsbGF0aW9uEg8KB3N1YmplY3QYASABKAkSDgoGcmVhc29uGAIgASgJEi4KCmNyZWF0ZWRfYXQYAyABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wIp4BChJDYW5jZWxQbGFuUmVzcG9uc2USKwoEcGxhbhgBIAEoCzIdLmNvbmR1aXQucGxhbnMudjEuUGxhblN1bW1hcnkSQAoMY2FuY2VsbGF0aW9uGAIgASgLMiouY29uZHVpdC5wbGFucy52MS5SZWNvcmRlZFBsYW5DYW5jZWxsYXRpb24SGQoRY2hhbmdlX3JlcXVlc3RfaWQYAyABKAkiRQoXUGxhbkNhbmNlbGxhdGlvbk91dGNvbWUSDwoHcGxhbl9pZBgBIAEoCRIZChFjb21tYW5kX2NvbXBsZXRlZBgCIAEoCDJyChdQbGFuQ2FuY2VsbGF0aW9uU2VydmljZRJXCgpDYW5jZWxQbGFuEiMuY29uZHVpdC5wbGFucy52MS5DYW5jZWxQbGFuUmVxdWVzdBokLmNvbmR1aXQucGxhbnMudjEuQ2FuY2VsUGxhblJlc3BvbnNlQkRaQmdpdGh1Yi5jb20vam9uYXRoYW5hZ3VzdGluL2NvbmR1aXQvZ2VuL2dvL2NvbmR1aXQvcGxhbnMvdjE7cGxhbnN2MWIGcHJvdG8z",
    [file_conduit_plans_v1_plan_summary, file_google_protobuf_timestamp],
  );

/**
 * @generated from message conduit.plans.v1.CancelPlanRequest
 */
export type CancelPlanRequest =
  Message<"conduit.plans.v1.CancelPlanRequest"> & {
    /**
     * @generated from field: string plan_id = 1;
     */
    planId: string;

    /**
     * @generated from field: string reason = 2;
     */
    reason: string;

    /**
     * Suppression additionally requires Operate on the Plan's Environment.
     *
     * @generated from field: bool create_suppression = 3;
     */
    createSuppression: boolean;

    /**
     * Zero uses the existing 30-minute default when suppression is requested.
     *
     * @generated from field: int64 suppression_duration_seconds = 4;
     */
    suppressionDurationSeconds: bigint;

    /**
     * @generated from field: string installation_id = 5;
     */
    installationId: string;
  };

/**
 * Describes the message conduit.plans.v1.CancelPlanRequest.
 * Use `create(CancelPlanRequestSchema)` to create a new message.
 */
export const CancelPlanRequestSchema: GenMessage<CancelPlanRequest> =
  /*@__PURE__*/
  messageDesc(file_conduit_plans_v1_plan_cancellation, 0);

/**
 * RecordedPlanCancellation describes the native cancellation, not an approval.
 *
 * @generated from message conduit.plans.v1.RecordedPlanCancellation
 */
export type RecordedPlanCancellation =
  Message<"conduit.plans.v1.RecordedPlanCancellation"> & {
    /**
     * @generated from field: string subject = 1;
     */
    subject: string;

    /**
     * @generated from field: string reason = 2;
     */
    reason: string;

    /**
     * @generated from field: google.protobuf.Timestamp created_at = 3;
     */
    createdAt?: Timestamp | undefined;
  };

/**
 * Describes the message conduit.plans.v1.RecordedPlanCancellation.
 * Use `create(RecordedPlanCancellationSchema)` to create a new message.
 */
export const RecordedPlanCancellationSchema: GenMessage<RecordedPlanCancellation> =
  /*@__PURE__*/
  messageDesc(file_conduit_plans_v1_plan_cancellation, 1);

/**
 * CancelPlanResponse is a scoped command result, not the complete REST Plan.
 * Success follows all requested native effects, including suppression creation.
 * Cancellation, Change Request cancellation and suppression are sequential.
 *
 * @generated from message conduit.plans.v1.CancelPlanResponse
 */
export type CancelPlanResponse =
  Message<"conduit.plans.v1.CancelPlanResponse"> & {
    /**
     * @generated from field: conduit.plans.v1.PlanSummary plan = 1;
     */
    plan?: PlanSummary | undefined;

    /**
     * @generated from field: conduit.plans.v1.RecordedPlanCancellation cancellation = 2;
     */
    cancellation?: RecordedPlanCancellation | undefined;

    /**
     * @generated from field: string change_request_id = 3;
     */
    changeRequestId: string;
  };

/**
 * Describes the message conduit.plans.v1.CancelPlanResponse.
 * Use `create(CancelPlanResponseSchema)` to create a new message.
 */
export const CancelPlanResponseSchema: GenMessage<CancelPlanResponse> =
  /*@__PURE__*/
  messageDesc(file_conduit_plans_v1_plan_cancellation, 2);

/**
 * PlanCancellationOutcome is error evidence, never a retry permit. It is emitted
 * only after the native command completed but its response could not be mapped.
 * Absent details mean an unknown outcome, never proof that no write occurred:
 * native command errors may follow a persisted cancellation or other effect.
 *
 * @generated from message conduit.plans.v1.PlanCancellationOutcome
 */
export type PlanCancellationOutcome =
  Message<"conduit.plans.v1.PlanCancellationOutcome"> & {
    /**
     * @generated from field: string plan_id = 1;
     */
    planId: string;

    /**
     * @generated from field: bool command_completed = 2;
     */
    commandCompleted: boolean;
  };

/**
 * Describes the message conduit.plans.v1.PlanCancellationOutcome.
 * Use `create(PlanCancellationOutcomeSchema)` to create a new message.
 */
export const PlanCancellationOutcomeSchema: GenMessage<PlanCancellationOutcome> =
  /*@__PURE__*/
  messageDesc(file_conduit_plans_v1_plan_cancellation, 3);

/**
 * PlanCancellationService applies existing cancellation and suppression policy.
 *
 * @generated from service conduit.plans.v1.PlanCancellationService
 */
export const PlanCancellationService: GenService<{
  /**
   * CancelPlan is POST /v1/plans/{planID}/cancel. Never retry or REST-replay.
   *
   * @generated from rpc conduit.plans.v1.PlanCancellationService.CancelPlan
   */
  cancelPlan: {
    methodKind: "unary";
    input: typeof CancelPlanRequestSchema;
    output: typeof CancelPlanResponseSchema;
  };
}> = /*@__PURE__*/ serviceDesc(file_conduit_plans_v1_plan_cancellation, 0);

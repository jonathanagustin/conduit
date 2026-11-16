// Regenerated from proto/conduit. DO NOT EDIT.
// @generated from file conduit/plans/v1/plan_approval.proto (package conduit.plans.v1, syntax proto3)
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
 * Describes the file conduit/plans/v1/plan_approval.proto.
 */
export const file_conduit_plans_v1_plan_approval: GenFile =
  /*@__PURE__*/
  fileDesc(
    "CiRjb25kdWl0L3BsYW5zL3YxL3BsYW5fYXBwcm92YWwucHJvdG8SEGNvbmR1aXQucGxhbnMudjEiOAoSQXBwcm92ZVBsYW5SZXF1ZXN0Eg8KB3BsYW5faWQYASABKAkSEQoJcmF0aW9uYWxlGAIgASgJImIKDFBsYW5BcHByb3ZhbBIPCgdzdWJqZWN0GAEgASgJEhEKCXJhdGlvbmFsZRgCIAEoCRIuCgpjcmVhdGVkX2F0GAMgASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcCKQAQoTQXBwcm92ZVBsYW5SZXNwb25zZRIrCgRwbGFuGAEgASgLMh0uY29uZHVpdC5wbGFucy52MS5QbGFuU3VtbWFyeRIxCglhcHByb3ZhbHMYAiADKAsyHi5jb25kdWl0LnBsYW5zLnYxLlBsYW5BcHByb3ZhbBIZChFjaGFuZ2VfcmVxdWVzdF9pZBgDIAEoCSJZChNQbGFuQXBwcm92YWxPdXRjb21lEg8KB3BsYW5faWQYASABKAkSGQoRYXBwcm92YWxfcmVjb3JkZWQYAiABKAgSFgoOaGFuZG9mZl9mYWlsZWQYAyABKAgycQoTUGxhbkFwcHJvdmFsU2VydmljZRJaCgtBcHByb3ZlUGxhbhIkLmNvbmR1aXQucGxhbnMudjEuQXBwcm92ZVBsYW5SZXF1ZXN0GiUuY29uZHVpdC5wbGFucy52MS5BcHByb3ZlUGxhblJlc3BvbnNlQkRaQmdpdGh1Yi5jb20vam9uYXRoYW5hZ3VzdGluL2NvbmR1aXQvZ2VuL2dvL2NvbmR1aXQvcGxhbnMvdjE7cGxhbnN2MWIGcHJvdG8z",
    [file_conduit_plans_v1_plan_summary, file_google_protobuf_timestamp],
  );

/**
 * @generated from message conduit.plans.v1.ApprovePlanRequest
 */
export type ApprovePlanRequest =
  Message<"conduit.plans.v1.ApprovePlanRequest"> & {
    /**
     * @generated from field: string plan_id = 1;
     */
    planId: string;

    /**
     * rationale is the native required review explanation, not an identity.
     *
     * @generated from field: string rationale = 2;
     */
    rationale: string;
  };

/**
 * Describes the message conduit.plans.v1.ApprovePlanRequest.
 * Use `create(ApprovePlanRequestSchema)` to create a new message.
 */
export const ApprovePlanRequestSchema: GenMessage<ApprovePlanRequest> =
  /*@__PURE__*/
  messageDesc(file_conduit_plans_v1_plan_approval, 0);

/**
 * @generated from message conduit.plans.v1.PlanApproval
 */
export type PlanApproval = Message<"conduit.plans.v1.PlanApproval"> & {
  /**
   * @generated from field: string subject = 1;
   */
  subject: string;

  /**
   * @generated from field: string rationale = 2;
   */
  rationale: string;

  /**
   * @generated from field: google.protobuf.Timestamp created_at = 3;
   */
  createdAt?: Timestamp | undefined;
};

/**
 * Describes the message conduit.plans.v1.PlanApproval.
 * Use `create(PlanApprovalSchema)` to create a new message.
 */
export const PlanApprovalSchema: GenMessage<PlanApproval> =
  /*@__PURE__*/
  messageDesc(file_conduit_plans_v1_plan_approval, 1);

/**
 * ApprovePlanResponse projects the native result without execution credentials.
 *
 * @generated from message conduit.plans.v1.ApprovePlanResponse
 */
export type ApprovePlanResponse =
  Message<"conduit.plans.v1.ApprovePlanResponse"> & {
    /**
     * @generated from field: conduit.plans.v1.PlanSummary plan = 1;
     */
    plan?: PlanSummary | undefined;

    /**
     * @generated from field: repeated conduit.plans.v1.PlanApproval approvals = 2;
     */
    approvals: PlanApproval[];

    /**
     * @generated from field: string change_request_id = 3;
     */
    changeRequestId: string;
  };

/**
 * Describes the message conduit.plans.v1.ApprovePlanResponse.
 * Use `create(ApprovePlanResponseSchema)` to create a new message.
 */
export const ApprovePlanResponseSchema: GenMessage<ApprovePlanResponse> =
  /*@__PURE__*/
  messageDesc(file_conduit_plans_v1_plan_approval, 2);

/**
 * PlanApprovalOutcome is an error detail, not a request or an approval permit.
 * Absent details mean an unknown outcome, never proof that no write occurred.
 *
 * @generated from message conduit.plans.v1.PlanApprovalOutcome
 */
export type PlanApprovalOutcome =
  Message<"conduit.plans.v1.PlanApprovalOutcome"> & {
    /**
     * @generated from field: string plan_id = 1;
     */
    planId: string;

    /**
     * @generated from field: bool approval_recorded = 2;
     */
    approvalRecorded: boolean;

    /**
     * @generated from field: bool handoff_failed = 3;
     */
    handoffFailed: boolean;
  };

/**
 * Describes the message conduit.plans.v1.PlanApprovalOutcome.
 * Use `create(PlanApprovalOutcomeSchema)` to create a new message.
 */
export const PlanApprovalOutcomeSchema: GenMessage<PlanApprovalOutcome> =
  /*@__PURE__*/
  messageDesc(file_conduit_plans_v1_plan_approval, 3);

/**
 * PlanApprovalService applies existing native governance, never caller authority.
 *
 * @generated from service conduit.plans.v1.PlanApprovalService
 */
export const PlanApprovalService: GenService<{
  /**
   * ApprovePlan is POST /v1/plans/{planID}/approvals. Do not retry or REST-replay.
   *
   * @generated from rpc conduit.plans.v1.PlanApprovalService.ApprovePlan
   */
  approvePlan: {
    methodKind: "unary";
    input: typeof ApprovePlanRequestSchema;
    output: typeof ApprovePlanResponseSchema;
  };
}> = /*@__PURE__*/ serviceDesc(file_conduit_plans_v1_plan_approval, 0);

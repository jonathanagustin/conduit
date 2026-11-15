// Regenerated from proto/conduit. DO NOT EDIT.
// @generated from file conduit/packaging/v1/packaging_run.proto (package conduit.packaging.v1, syntax proto3)
/* eslint-disable */

// Package conduit.packaging.v1 is the read contract for one packaging run.
// Other Hub domains use the same proto/conduit/<domain>/v1 layout.

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
 * Describes the file conduit/packaging/v1/packaging_run.proto.
 */
export const file_conduit_packaging_v1_packaging_run: GenFile =
  /*@__PURE__*/
  fileDesc(
    "Cihjb25kdWl0L3BhY2thZ2luZy92MS9wYWNrYWdpbmdfcnVuLnByb3RvEhRjb25kdWl0LnBhY2thZ2luZy52MSIoChZHZXRQYWNrYWdpbmdSdW5SZXF1ZXN0Eg4KBnJ1bl9pZBgBIAEoCSK0AwoXR2V0UGFja2FnaW5nUnVuUmVzcG9uc2USDAoEdGllchgBIAEoCRISCgpjb25maWd1cmVkGAIgASgIEhEKCWF1dGhvcml0eRgDIAEoCRIWCg5wbGFuX2F1dGhvcml0eRgEIAEoCRIaChJhcHByb3ZhbF9hdXRob3JpdHkYBSABKAkSDgoGcnVuX2lkGAYgASgJEg0KBXBoYXNlGAcgASgJEg8KB291dGNvbWUYCCABKAkSDgoGcmVhc29uGAkgASgJEhUKDXNvdXJjZV9kaWdlc3QYCiABKAkSFwoPYXJ0aWZhY3RfZGlnZXN0GAsgASgJEhkKEXByb3Bvc2FsX3JldmlzaW9uGAwgASgJEhcKD3RhcmdldF9wbGFuX3JlZhgNIAEoCRIPCgdwbGFuX2lkGA4gASgJEhoKEmhhbmRvZmZfcmVjb25jaWxlZBgPIAEoCBITCgtsYXN0X3JldmlldxgQIAEoCRIZChFkdXBsaWNhdGVfcmV2aWV3cxgRIAEoBRIYChB0ZW1wbGF0ZV92ZXJzaW9uGBIgASgJEhUKDXRvb2xfdmVyc2lvbnMYEyABKAkyigEKE1BhY2thZ2luZ1J1blNlcnZpY2UScwoPR2V0UGFja2FnaW5nUnVuEiwuY29uZHVpdC5wYWNrYWdpbmcudjEuR2V0UGFja2FnaW5nUnVuUmVxdWVzdBotLmNvbmR1aXQucGFja2FnaW5nLnYxLkdldFBhY2thZ2luZ1J1blJlc3BvbnNlIgOQAgFCTFpKZ2l0aHViLmNvbS9qb25hdGhhbmFndXN0aW4vY29uZHVpdC9nZW4vZ28vY29uZHVpdC9wYWNrYWdpbmcvdjE7cGFja2FnaW5ndjFiBnByb3RvMw",
  );

/**
 * GetPackagingRunRequest names the run to read.
 *
 * @generated from message conduit.packaging.v1.GetPackagingRunRequest
 */
export type GetPackagingRunRequest =
  Message<"conduit.packaging.v1.GetPackagingRunRequest"> & {
    /**
     * run_id is the path value of GET /v1/packaging-runs/{run_id}.
     *
     * @generated from field: string run_id = 1;
     */
    runId: string;
  };

/**
 * Describes the message conduit.packaging.v1.GetPackagingRunRequest.
 * Use `create(GetPackagingRunRequestSchema)` to create a new message.
 */
export const GetPackagingRunRequestSchema: GenMessage<GetPackagingRunRequest> =
  /*@__PURE__*/
  messageDesc(file_conduit_packaging_v1_packaging_run, 0);

/**
 * GetPackagingRunResponse is the REST body of GET /v1/packaging-runs/{run_id}.
 *
 * @generated from message conduit.packaging.v1.GetPackagingRunResponse
 */
export type GetPackagingRunResponse =
  Message<"conduit.packaging.v1.GetPackagingRunResponse"> & {
    /**
     * tier is the REST field tier.
     *
     * @generated from field: string tier = 1;
     */
    tier: string;

    /**
     * configured is the REST field configured.
     *
     * @generated from field: bool configured = 2;
     */
    configured: boolean;

    /**
     * authority is the REST field authority.
     *
     * @generated from field: string authority = 3;
     */
    authority: string;

    /**
     * plan_authority is the REST field planAuthority.
     *
     * @generated from field: string plan_authority = 4;
     */
    planAuthority: string;

    /**
     * approval_authority is the REST field approvalAuthority.
     *
     * @generated from field: string approval_authority = 5;
     */
    approvalAuthority: string;

    /**
     * run_id is the REST field runId.
     *
     * @generated from field: string run_id = 6;
     */
    runId: string;

    /**
     * phase is the REST field phase.
     *
     * @generated from field: string phase = 7;
     */
    phase: string;

    /**
     * outcome is the REST field outcome.
     *
     * @generated from field: string outcome = 8;
     */
    outcome: string;

    /**
     * reason is the REST field reason.
     *
     * @generated from field: string reason = 9;
     */
    reason: string;

    /**
     * source_digest is the REST field sourceDigest.
     *
     * @generated from field: string source_digest = 10;
     */
    sourceDigest: string;

    /**
     * artifact_digest is the REST field artifactDigest.
     *
     * @generated from field: string artifact_digest = 11;
     */
    artifactDigest: string;

    /**
     * proposal_revision is the REST field proposalRevision.
     *
     * @generated from field: string proposal_revision = 12;
     */
    proposalRevision: string;

    /**
     * target_plan_ref is the REST field targetPlanRef.
     *
     * @generated from field: string target_plan_ref = 13;
     */
    targetPlanRef: string;

    /**
     * plan_id is the REST field planId.
     *
     * @generated from field: string plan_id = 14;
     */
    planId: string;

    /**
     * handoff_reconciled is the REST field handoffReconciled.
     *
     * @generated from field: bool handoff_reconciled = 15;
     */
    handoffReconciled: boolean;

    /**
     * last_review is the REST field lastReview.
     *
     * @generated from field: string last_review = 16;
     */
    lastReview: string;

    /**
     * duplicate_reviews is the REST field duplicateReviews.
     *
     * @generated from field: int32 duplicate_reviews = 17;
     */
    duplicateReviews: number;

    /**
     * template_version is the REST field templateVersion.
     *
     * @generated from field: string template_version = 18;
     */
    templateVersion: string;

    /**
     * tool_versions is the REST field toolVersions.
     *
     * @generated from field: string tool_versions = 19;
     */
    toolVersions: string;
  };

/**
 * Describes the message conduit.packaging.v1.GetPackagingRunResponse.
 * Use `create(GetPackagingRunResponseSchema)` to create a new message.
 */
export const GetPackagingRunResponseSchema: GenMessage<GetPackagingRunResponse> =
  /*@__PURE__*/
  messageDesc(file_conduit_packaging_v1_packaging_run, 1);

/**
 * PackagingRunService reads packaging-run progress.
 * It does not accept an approval and it does not report Plan status.
 *
 * @generated from service conduit.packaging.v1.PackagingRunService
 */
export const PackagingRunService: GenService<{
  /**
   * GetPackagingRun is the read model of GET /v1/packaging-runs/{run_id}.
   *
   * @generated from rpc conduit.packaging.v1.PackagingRunService.GetPackagingRun
   */
  getPackagingRun: {
    methodKind: "unary";
    input: typeof GetPackagingRunRequestSchema;
    output: typeof GetPackagingRunResponseSchema;
  };
}> = /*@__PURE__*/ serviceDesc(file_conduit_packaging_v1_packaging_run, 0);

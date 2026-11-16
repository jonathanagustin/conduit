// Regenerated from proto/conduit. DO NOT EDIT.
// @generated from file conduit/packaging/v1/packaging_run.proto (package conduit.packaging.v1, syntax proto3)
/* eslint-disable */

// Package conduit.packaging.v1 contains packaging-run read contracts.
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
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { file_google_protobuf_timestamp } from "@bufbuild/protobuf/wkt";
import type { Message } from "@bufbuild/protobuf";

/**
 * Describes the file conduit/packaging/v1/packaging_run.proto.
 */
export const file_conduit_packaging_v1_packaging_run: GenFile =
  /*@__PURE__*/
  fileDesc(
    "Cihjb25kdWl0L3BhY2thZ2luZy92MS9wYWNrYWdpbmdfcnVuLnByb3RvEhRjb25kdWl0LnBhY2thZ2luZy52MSIoChZHZXRQYWNrYWdpbmdSdW5SZXF1ZXN0Eg4KBnJ1bl9pZBgBIAEoCSLPAwoXR2V0UGFja2FnaW5nUnVuUmVzcG9uc2USDAoEdGllchgBIAEoCRISCgpjb25maWd1cmVkGAIgASgIEhEKCWF1dGhvcml0eRgDIAEoCRIWCg5wbGFuX2F1dGhvcml0eRgEIAEoCRIaChJhcHByb3ZhbF9hdXRob3JpdHkYBSABKAkSDgoGcnVuX2lkGAYgASgJEg0KBXBoYXNlGAcgASgJEg8KB291dGNvbWUYCCABKAkSDgoGcmVhc29uGAkgASgJEhUKDXNvdXJjZV9kaWdlc3QYCiABKAkSFwoPYXJ0aWZhY3RfZGlnZXN0GAsgASgJEhkKEXByb3Bvc2FsX3JldmlzaW9uGAwgASgJEhcKD3RhcmdldF9wbGFuX3JlZhgNIAEoCRIPCgdwbGFuX2lkGA4gASgJEhoKEmhhbmRvZmZfcmVjb25jaWxlZBgPIAEoCBITCgtsYXN0X3JldmlldxgQIAEoCRIZChFkdXBsaWNhdGVfcmV2aWV3cxgRIAEoBRIYChB0ZW1wbGF0ZV92ZXJzaW9uGBIgASgJEhUKDXRvb2xfdmVyc2lvbnMYEyABKAkSGQoRYXR0YWNobWVudF9zaGEyNTYYFCADKAkilwEKGExpc3RQYWNrYWdpbmdSdW5zUmVxdWVzdBIMCgR2aWV3GAEgASgJEhIKBWxpbWl0GAIgASgFSACIAQESDgoGY3Vyc29yGAMgASgJEg0KBXBoYXNlGAQgASgJEg8KB291dGNvbWUYBSABKAkSDwoHcGxhbl9pZBgGIAEoCRIOCgZzZWFyY2gYByABKAlCCAoGX2xpbWl0IrcCChlMaXN0UGFja2FnaW5nUnVuc1Jlc3BvbnNlEgwKBHRpZXIYASABKAkSEgoKY29uZmlndXJlZBgCIAEoCBIRCglhdXRob3JpdHkYAyABKAkSFgoOcGxhbl9hdXRob3JpdHkYBCABKAkSGgoSYXBwcm92YWxfYXV0aG9yaXR5GAUgASgJEjwKBWl0ZW1zGAYgAygLMi0uY29uZHVpdC5wYWNrYWdpbmcudjEuR2V0UGFja2FnaW5nUnVuUmVzcG9uc2USEQoJcGFnZV9zaXplGAcgASgFEg0KBWxpbWl0GAggASgFEhQKDGhhc19wcmV2aW91cxgJIAEoCBITCgtuZXh0X2N1cnNvchgKIAEoCRIXCg9wcmV2aW91c19jdXJzb3IYCyABKAkSDQoFdG90YWwYDCABKAMiawodTGlzdFBhY2thZ2luZ1J1bkV2ZW50c1JlcXVlc3QSDgoGcnVuX2lkGAEgASgJEgwKBHZpZXcYAiABKAkSEgoFbGltaXQYAyABKAVIAIgBARIOCgZjdXJzb3IYBCABKAlCCAoGX2xpbWl0IoYCChFQYWNrYWdpbmdSdW5FdmVudBIQCghzZXF1ZW5jZRgBIAEoAxImCgJhdBgCIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXASDAoEc3RlcBgDIAEoCRIPCgdvdXRjb21lGAQgASgJEg4KBnJlYXNvbhgFIAEoCRIVCg1zb3VyY2VfZGlnZXN0GAYgASgJEhcKD2FydGlmYWN0X2RpZ2VzdBgHIAEoCRIZChFwcm9wb3NhbF9yZXZpc2lvbhgIIAEoCRITCgthcHByb3ZhbF9pZBgJIAEoCRIPCgdwbGFuX2lkGAogASgJEhcKD3RhcmdldF9wbGFuX3JlZhgLIAEoCSLGAgoeTGlzdFBhY2thZ2luZ1J1bkV2ZW50c1Jlc3BvbnNlEgwKBHRpZXIYASABKAkSEgoKY29uZmlndXJlZBgCIAEoCBIRCglhdXRob3JpdHkYAyABKAkSFgoOcGxhbl9hdXRob3JpdHkYBCABKAkSGgoSYXBwcm92YWxfYXV0aG9yaXR5GAUgASgJEg4KBnJ1bl9pZBgGIAEoCRI2CgVpdGVtcxgHIAMoCzInLmNvbmR1aXQucGFja2FnaW5nLnYxLlBhY2thZ2luZ1J1bkV2ZW50EhEKCXBhZ2Vfc2l6ZRgIIAEoBRINCgVsaW1pdBgJIAEoBRIUCgxoYXNfcHJldmlvdXMYCiABKAgSEwoLbmV4dF9jdXJzb3IYCyABKAkSFwoPcHJldmlvdXNfY3Vyc29yGAwgASgJEg0KBXRvdGFsGA0gASgDMpADChNQYWNrYWdpbmdSdW5TZXJ2aWNlEnkKEUxpc3RQYWNrYWdpbmdSdW5zEi4uY29uZHVpdC5wYWNrYWdpbmcudjEuTGlzdFBhY2thZ2luZ1J1bnNSZXF1ZXN0Gi8uY29uZHVpdC5wYWNrYWdpbmcudjEuTGlzdFBhY2thZ2luZ1J1bnNSZXNwb25zZSIDkAIBEogBChZMaXN0UGFja2FnaW5nUnVuRXZlbnRzEjMuY29uZHVpdC5wYWNrYWdpbmcudjEuTGlzdFBhY2thZ2luZ1J1bkV2ZW50c1JlcXVlc3QaNC5jb25kdWl0LnBhY2thZ2luZy52MS5MaXN0UGFja2FnaW5nUnVuRXZlbnRzUmVzcG9uc2UiA5ACARJzCg9HZXRQYWNrYWdpbmdSdW4SLC5jb25kdWl0LnBhY2thZ2luZy52MS5HZXRQYWNrYWdpbmdSdW5SZXF1ZXN0Gi0uY29uZHVpdC5wYWNrYWdpbmcudjEuR2V0UGFja2FnaW5nUnVuUmVzcG9uc2UiA5ACAUJMWkpnaXRodWIuY29tL2pvbmF0aGFuYWd1c3Rpbi9jb25kdWl0L2dlbi9nby9jb25kdWl0L3BhY2thZ2luZy92MTtwYWNrYWdpbmd2MWIGcHJvdG8z",
    [file_google_protobuf_timestamp],
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

    /**
     * attachment_sha256 binds the same intake-file digests as the native REST read.
     *
     * @generated from field: repeated string attachment_sha256 = 20;
     */
    attachmentSha256: string[];
  };

/**
 * Describes the message conduit.packaging.v1.GetPackagingRunResponse.
 * Use `create(GetPackagingRunResponseSchema)` to create a new message.
 */
export const GetPackagingRunResponseSchema: GenMessage<GetPackagingRunResponse> =
  /*@__PURE__*/
  messageDesc(file_conduit_packaging_v1_packaging_run, 1);

/**
 * ListPackagingRunsRequest uses the native index filters and opaque cursor.
 *
 * @generated from message conduit.packaging.v1.ListPackagingRunsRequest
 */
export type ListPackagingRunsRequest =
  Message<"conduit.packaging.v1.ListPackagingRunsRequest"> & {
    /**
     * view is full (default, max 20) or summary (default 100, max 200).
     *
     * @generated from field: string view = 1;
     */
    view: string;

    /**
     * limit is optional; explicitly zero or negative limits are invalid.
     *
     * @generated from field: optional int32 limit = 2;
     */
    limit?: number | undefined;

    /**
     * cursor must match these filters and is never interpreted by the client.
     *
     * @generated from field: string cursor = 3;
     */
    cursor: string;

    /**
     * phase selects the native phase.
     *
     * @generated from field: string phase = 4;
     */
    phase: string;

    /**
     * outcome selects the native outcome.
     *
     * @generated from field: string outcome = 5;
     */
    outcome: string;

    /**
     * plan_id selects the referenced Plan, without granting Plan authority.
     *
     * @generated from field: string plan_id = 6;
     */
    planId: string;

    /**
     * search is the native case-insensitive search string.
     *
     * @generated from field: string search = 7;
     */
    search: string;
  };

/**
 * Describes the message conduit.packaging.v1.ListPackagingRunsRequest.
 * Use `create(ListPackagingRunsRequestSchema)` to create a new message.
 */
export const ListPackagingRunsRequestSchema: GenMessage<ListPackagingRunsRequest> =
  /*@__PURE__*/
  messageDesc(file_conduit_packaging_v1_packaging_run, 2);

/**
 * ListPackagingRunsResponse counts only visible, filtered runs.
 *
 * @generated from message conduit.packaging.v1.ListPackagingRunsResponse
 */
export type ListPackagingRunsResponse =
  Message<"conduit.packaging.v1.ListPackagingRunsResponse"> & {
    /**
     * tier marks the native read model.
     *
     * @generated from field: string tier = 1;
     */
    tier: string;

    /**
     * configured reports whether a packaging reader is configured.
     *
     * @generated from field: bool configured = 2;
     */
    configured: boolean;

    /**
     * authority is packaging-run.
     *
     * @generated from field: string authority = 3;
     */
    authority: string;

    /**
     * plan_authority remains conduit-plan.
     *
     * @generated from field: string plan_authority = 4;
     */
    planAuthority: string;

    /**
     * approval_authority remains conduit-governance.
     *
     * @generated from field: string approval_authority = 5;
     */
    approvalAuthority: string;

    /**
     * items are redacted rows, ordered by run_id.
     *
     * @generated from field: repeated conduit.packaging.v1.GetPackagingRunResponse items = 6;
     */
    items: GetPackagingRunResponse[];

    /**
     * page_size is the number of returned rows.
     *
     * @generated from field: int32 page_size = 7;
     */
    pageSize: number;

    /**
     * limit is the effective page limit.
     *
     * @generated from field: int32 limit = 8;
     */
    limit: number;

    /**
     * has_previous indicates a previous visible page.
     *
     * @generated from field: bool has_previous = 9;
     */
    hasPrevious: boolean;

    /**
     * next_cursor is absent at the end of the filtered index.
     *
     * @generated from field: string next_cursor = 10;
     */
    nextCursor: string;

    /**
     * previous_cursor is absent on the first page.
     *
     * @generated from field: string previous_cursor = 11;
     */
    previousCursor: string;

    /**
     * total counts visible matches, not all stored runs.
     *
     * @generated from field: int64 total = 12;
     */
    total: bigint;
  };

/**
 * Describes the message conduit.packaging.v1.ListPackagingRunsResponse.
 * Use `create(ListPackagingRunsResponseSchema)` to create a new message.
 */
export const ListPackagingRunsResponseSchema: GenMessage<ListPackagingRunsResponse> =
  /*@__PURE__*/
  messageDesc(file_conduit_packaging_v1_packaging_run, 3);

/**
 * ListPackagingRunEventsRequest names a run and the native seek window.
 *
 * @generated from message conduit.packaging.v1.ListPackagingRunEventsRequest
 */
export type ListPackagingRunEventsRequest =
  Message<"conduit.packaging.v1.ListPackagingRunEventsRequest"> & {
    /**
     * run_id is the REST path parameter, not an approval or actor identity.
     *
     * @generated from field: string run_id = 1;
     */
    runId: string;

    /**
     * view selects full (default, max 20) or summary (default 100, max 200).
     *
     * @generated from field: string view = 2;
     */
    view: string;

    /**
     * limit is optional; explicit zero and negative values are invalid.
     *
     * @generated from field: optional int32 limit = 3;
     */
    limit?: number | undefined;

    /**
     * cursor is the native opaque seek cursor.
     *
     * @generated from field: string cursor = 4;
     */
    cursor: string;
  };

/**
 * Describes the message conduit.packaging.v1.ListPackagingRunEventsRequest.
 * Use `create(ListPackagingRunEventsRequestSchema)` to create a new message.
 */
export const ListPackagingRunEventsRequestSchema: GenMessage<ListPackagingRunEventsRequest> =
  /*@__PURE__*/
  messageDesc(file_conduit_packaging_v1_packaging_run, 4);

/**
 * PackagingRunEvent is one native recorded packaging step, after redaction.
 * References never grant authority or establish Plan approval or execution.
 *
 * @generated from message conduit.packaging.v1.PackagingRunEvent
 */
export type PackagingRunEvent =
  Message<"conduit.packaging.v1.PackagingRunEvent"> & {
    /**
     * sequence is the native monotonic journal position.
     *
     * @generated from field: int64 sequence = 1;
     */
    sequence: bigint;

    /**
     * at is the occurrence time; sequence, not time, determines ordering.
     *
     * @generated from field: google.protobuf.Timestamp at = 2;
     */
    at?: Timestamp | undefined;

    /**
     * step retains the native open step vocabulary.
     *
     * @generated from field: string step = 3;
     */
    step: string;

    /**
     * outcome is the recorded outcome; absent steps are not passes.
     *
     * @generated from field: string outcome = 4;
     */
    outcome: string;

    /**
     * reason has the same credential redaction as REST.
     *
     * @generated from field: string reason = 5;
     */
    reason: string;

    /**
     * source_digest identifies the recorded source.
     *
     * @generated from field: string source_digest = 6;
     */
    sourceDigest: string;

    /**
     * artifact_digest identifies the recorded artifact.
     *
     * @generated from field: string artifact_digest = 7;
     */
    artifactDigest: string;

    /**
     * proposal_revision identifies the recorded proposal revision.
     *
     * @generated from field: string proposal_revision = 8;
     */
    proposalRevision: string;

    /**
     * approval_id is a reference, not approval authority.
     *
     * @generated from field: string approval_id = 9;
     */
    approvalId: string;

    /**
     * plan_id is a reference, not Plan state.
     *
     * @generated from field: string plan_id = 10;
     */
    planId: string;

    /**
     * target_plan_ref is the existing native target reference.
     *
     * @generated from field: string target_plan_ref = 11;
     */
    targetPlanRef: string;
  };

/**
 * Describes the message conduit.packaging.v1.PackagingRunEvent.
 * Use `create(PackagingRunEventSchema)` to create a new message.
 */
export const PackagingRunEventSchema: GenMessage<PackagingRunEvent> =
  /*@__PURE__*/
  messageDesc(file_conduit_packaging_v1_packaging_run, 5);

/**
 * ListPackagingRunEventsResponse is the native authorized history page.
 *
 * @generated from message conduit.packaging.v1.ListPackagingRunEventsResponse
 */
export type ListPackagingRunEventsResponse =
  Message<"conduit.packaging.v1.ListPackagingRunEventsResponse"> & {
    /**
     * tier identifies this Conduit-native contract.
     *
     * @generated from field: string tier = 1;
     */
    tier: string;

    /**
     * configured is false when this Hub has no packaging reader.
     *
     * @generated from field: bool configured = 2;
     */
    configured: boolean;

    /**
     * authority owns packaging progress.
     *
     * @generated from field: string authority = 3;
     */
    authority: string;

    /**
     * plan_authority owns Plan state separately.
     *
     * @generated from field: string plan_authority = 4;
     */
    planAuthority: string;

    /**
     * approval_authority owns approval decisions separately.
     *
     * @generated from field: string approval_authority = 5;
     */
    approvalAuthority: string;

    /**
     * run_id identifies the visible parent run, empty when unconfigured.
     *
     * @generated from field: string run_id = 6;
     */
    runId: string;

    /**
     * items contains only recorded, redacted events in sequence order.
     *
     * @generated from field: repeated conduit.packaging.v1.PackagingRunEvent items = 7;
     */
    items: PackagingRunEvent[];

    /**
     * page_size is the number of returned events.
     *
     * @generated from field: int32 page_size = 8;
     */
    pageSize: number;

    /**
     * limit is the applied native limit.
     *
     * @generated from field: int32 limit = 9;
     */
    limit: number;

    /**
     * has_previous indicates a previous seek window exists.
     *
     * @generated from field: bool has_previous = 10;
     */
    hasPrevious: boolean;

    /**
     * next_cursor is opaque and absent at the end.
     *
     * @generated from field: string next_cursor = 11;
     */
    nextCursor: string;

    /**
     * previous_cursor is opaque; empty can address the first page.
     *
     * @generated from field: string previous_cursor = 12;
     */
    previousCursor: string;

    /**
     * total counts the parent run's recorded events, not future steps.
     *
     * @generated from field: int64 total = 13;
     */
    total: bigint;
  };

/**
 * Describes the message conduit.packaging.v1.ListPackagingRunEventsResponse.
 * Use `create(ListPackagingRunEventsResponseSchema)` to create a new message.
 */
export const ListPackagingRunEventsResponseSchema: GenMessage<ListPackagingRunEventsResponse> =
  /*@__PURE__*/
  messageDesc(file_conduit_packaging_v1_packaging_run, 6);

/**
 * PackagingRunService reads packaging-run progress.
 * It does not accept an approval and it does not report Plan status.
 *
 * @generated from service conduit.packaging.v1.PackagingRunService
 */
export const PackagingRunService: GenService<{
  /**
   * ListPackagingRuns reads GET /v1/packaging-runs with the same visibility and cursors.
   *
   * @generated from rpc conduit.packaging.v1.PackagingRunService.ListPackagingRuns
   */
  listPackagingRuns: {
    methodKind: "unary";
    input: typeof ListPackagingRunsRequestSchema;
    output: typeof ListPackagingRunsResponseSchema;
  };
  /**
   * ListPackagingRunEvents reads the recorded, redacted history of one visible run.
   *
   * @generated from rpc conduit.packaging.v1.PackagingRunService.ListPackagingRunEvents
   */
  listPackagingRunEvents: {
    methodKind: "unary";
    input: typeof ListPackagingRunEventsRequestSchema;
    output: typeof ListPackagingRunEventsResponseSchema;
  };
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

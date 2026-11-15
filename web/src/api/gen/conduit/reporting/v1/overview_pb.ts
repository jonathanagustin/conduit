// Regenerated from proto/conduit. DO NOT EDIT.
// @generated from file conduit/reporting/v1/overview.proto (package conduit.reporting.v1, syntax proto3)
/* eslint-disable */

// Package conduit.reporting.v1 describes the existing operator-filtered overview.

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
import type { Empty, Timestamp } from "@bufbuild/protobuf/wkt";
import {
  file_google_protobuf_empty,
  file_google_protobuf_timestamp,
} from "@bufbuild/protobuf/wkt";
import type { Message } from "@bufbuild/protobuf";

/**
 * Describes the file conduit/reporting/v1/overview.proto.
 */
export const file_conduit_reporting_v1_overview: GenFile =
  /*@__PURE__*/
  fileDesc(
    "CiNjb25kdWl0L3JlcG9ydGluZy92MS9vdmVydmlldy5wcm90bxIUY29uZHVpdC5yZXBvcnRpbmcudjEiFAoSR2V0T3ZlcnZpZXdSZXF1ZXN0It8DChNHZXRPdmVydmlld1Jlc3BvbnNlEjAKDGdlbmVyYXRlZF9hdBgBIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXASGwoTc3RhbGVfYWZ0ZXJfc2Vjb25kcxgCIAEoAxJEChJhd2FpdGluZ19hcHByb3ZhbHMYAyABKAsyKC5jb25kdWl0LnJlcG9ydGluZy52MS5DaGFuZ2VSZXF1ZXN0UGFuZWwSNQoMZmFpbGVkX3BsYW5zGAQgASgLMh8uY29uZHVpdC5yZXBvcnRpbmcudjEuUGxhblBhbmVsEjYKDXJ1bm5pbmdfcGxhbnMYBSABKAsyHy5jb25kdWl0LnJlcG9ydGluZy52MS5QbGFuUGFuZWwSQwoSdW5oZWFsdGh5X2VudGl0aWVzGAYgASgLMicuY29uZHVpdC5yZXBvcnRpbmcudjEuSW5zdGFsbGF0aW9uUGFuZWwSPQoNc3RhbGVfcmVwb3J0cxgHIAEoCzImLmNvbmR1aXQucmVwb3J0aW5nLnYxLlN0YWxlUmVwb3J0UGFuZWwSQAoSYmxvY2tlZF9wcm9tb3Rpb25zGAggASgLMiQuY29uZHVpdC5yZXBvcnRpbmcudjEuUHJvbW90aW9uUGFuZWwiRwoVT3ZlcnZpZXdDaGFuZ2VSZXF1ZXN0EgoKAmlkGAEgASgJEg0KBXRpdGxlGAIgASgJEhMKC2Vudmlyb25tZW50GAMgASgJIi8KDE92ZXJ2aWV3UGxhbhIKCgJpZBgBIAEoCRITCgtlbnZpcm9ubWVudBgCIAEoCSJUChRPdmVydmlld0luc3RhbGxhdGlvbhITCgtlbnZpcm9ubWVudBgBIAEoCRIXCg9pbnN0YWxsYXRpb25faWQYAiABKAkSDgoGaGVhbHRoGAMgASgJImsKEU92ZXJ2aWV3UHJvbW90aW9uEgoKAmlkGAEgASgJEg8KB3Byb2R1Y3QYAiABKAkSDwoHdmVyc2lvbhgDIAEoCRIUCgxmcm9tX2NoYW5uZWwYBCABKAkSEgoKdG9fY2hhbm5lbBgFIAEoCSJvChJDaGFuZ2VSZXF1ZXN0UGFuZWwSDgoGc3RhdHVzGAEgASgJEg0KBWNvdW50GAIgASgDEjoKBWl0ZW1zGAMgAygLMisuY29uZHVpdC5yZXBvcnRpbmcudjEuT3ZlcnZpZXdDaGFuZ2VSZXF1ZXN0Il0KCVBsYW5QYW5lbBIOCgZzdGF0dXMYASABKAkSDQoFY291bnQYAiABKAMSMQoFaXRlbXMYAyADKAsyIi5jb25kdWl0LnJlcG9ydGluZy52MS5PdmVydmlld1BsYW4ibQoRSW5zdGFsbGF0aW9uUGFuZWwSDgoGc3RhdHVzGAEgASgJEg0KBWNvdW50GAIgASgDEjkKBWl0ZW1zGAMgAygLMiouY29uZHVpdC5yZXBvcnRpbmcudjEuT3ZlcnZpZXdJbnN0YWxsYXRpb24iWAoQU3RhbGVSZXBvcnRQYW5lbBIOCgZzdGF0dXMYASABKAkSDQoFY291bnQYAiABKAMSJQoFaXRlbXMYAyADKAsyFi5nb29nbGUucHJvdG9idWYuRW1wdHkiZwoOUHJvbW90aW9uUGFuZWwSDgoGc3RhdHVzGAEgASgJEg0KBWNvdW50GAIgASgDEjYKBWl0ZW1zGAMgAygLMicuY29uZHVpdC5yZXBvcnRpbmcudjEuT3ZlcnZpZXdQcm9tb3Rpb24yewoQUmVwb3J0aW5nU2VydmljZRJnCgtHZXRPdmVydmlldxIoLmNvbmR1aXQucmVwb3J0aW5nLnYxLkdldE92ZXJ2aWV3UmVxdWVzdBopLmNvbmR1aXQucmVwb3J0aW5nLnYxLkdldE92ZXJ2aWV3UmVzcG9uc2UiA5ACAUJMWkpnaXRodWIuY29tL2pvbmF0aGFuYWd1c3Rpbi9jb25kdWl0L2dlbi9nby9jb25kdWl0L3JlcG9ydGluZy92MTtyZXBvcnRpbmd2MWIGcHJvdG8z",
    [file_google_protobuf_empty, file_google_protobuf_timestamp],
  );

/**
 * GetOverviewRequest preserves the native Reporting overview shape.
 *
 * @generated from message conduit.reporting.v1.GetOverviewRequest
 */
export type GetOverviewRequest =
  Message<"conduit.reporting.v1.GetOverviewRequest"> & {};

/**
 * Describes the message conduit.reporting.v1.GetOverviewRequest.
 * Use `create(GetOverviewRequestSchema)` to create a new message.
 */
export const GetOverviewRequestSchema: GenMessage<GetOverviewRequest> =
  /*@__PURE__*/
  messageDesc(file_conduit_reporting_v1_overview, 0);

/**
 * GetOverviewResponse preserves the native Reporting overview shape.
 *
 * @generated from message conduit.reporting.v1.GetOverviewResponse
 */
export type GetOverviewResponse =
  Message<"conduit.reporting.v1.GetOverviewResponse"> & {
    /**
     * generated_at maps the corresponding native JSON field.
     *
     * @generated from field: google.protobuf.Timestamp generated_at = 1;
     */
    generatedAt?: Timestamp | undefined;

    /**
     * stale_after_seconds maps the corresponding native JSON field.
     *
     * @generated from field: int64 stale_after_seconds = 2;
     */
    staleAfterSeconds: bigint;

    /**
     * awaiting_approvals maps the corresponding native JSON field.
     *
     * @generated from field: conduit.reporting.v1.ChangeRequestPanel awaiting_approvals = 3;
     */
    awaitingApprovals?: ChangeRequestPanel | undefined;

    /**
     * failed_plans maps the corresponding native JSON field.
     *
     * @generated from field: conduit.reporting.v1.PlanPanel failed_plans = 4;
     */
    failedPlans?: PlanPanel | undefined;

    /**
     * running_plans maps the corresponding native JSON field.
     *
     * @generated from field: conduit.reporting.v1.PlanPanel running_plans = 5;
     */
    runningPlans?: PlanPanel | undefined;

    /**
     * unhealthy_entities maps the corresponding native JSON field.
     *
     * @generated from field: conduit.reporting.v1.InstallationPanel unhealthy_entities = 6;
     */
    unhealthyEntities?: InstallationPanel | undefined;

    /**
     * stale_reports maps the corresponding native JSON field.
     *
     * @generated from field: conduit.reporting.v1.StaleReportPanel stale_reports = 7;
     */
    staleReports?: StaleReportPanel | undefined;

    /**
     * blocked_promotions maps the corresponding native JSON field.
     *
     * @generated from field: conduit.reporting.v1.PromotionPanel blocked_promotions = 8;
     */
    blockedPromotions?: PromotionPanel | undefined;
  };

/**
 * Describes the message conduit.reporting.v1.GetOverviewResponse.
 * Use `create(GetOverviewResponseSchema)` to create a new message.
 */
export const GetOverviewResponseSchema: GenMessage<GetOverviewResponse> =
  /*@__PURE__*/
  messageDesc(file_conduit_reporting_v1_overview, 1);

/**
 * OverviewChangeRequest preserves the native Reporting overview shape.
 *
 * @generated from message conduit.reporting.v1.OverviewChangeRequest
 */
export type OverviewChangeRequest =
  Message<"conduit.reporting.v1.OverviewChangeRequest"> & {
    /**
     * id maps the corresponding native JSON field.
     *
     * @generated from field: string id = 1;
     */
    id: string;

    /**
     * title maps the corresponding native JSON field.
     *
     * @generated from field: string title = 2;
     */
    title: string;

    /**
     * environment maps the corresponding native JSON field.
     *
     * @generated from field: string environment = 3;
     */
    environment: string;
  };

/**
 * Describes the message conduit.reporting.v1.OverviewChangeRequest.
 * Use `create(OverviewChangeRequestSchema)` to create a new message.
 */
export const OverviewChangeRequestSchema: GenMessage<OverviewChangeRequest> =
  /*@__PURE__*/
  messageDesc(file_conduit_reporting_v1_overview, 2);

/**
 * OverviewPlan preserves the native Reporting overview shape.
 *
 * @generated from message conduit.reporting.v1.OverviewPlan
 */
export type OverviewPlan = Message<"conduit.reporting.v1.OverviewPlan"> & {
  /**
   * id maps the corresponding native JSON field.
   *
   * @generated from field: string id = 1;
   */
  id: string;

  /**
   * environment maps the corresponding native JSON field.
   *
   * @generated from field: string environment = 2;
   */
  environment: string;
};

/**
 * Describes the message conduit.reporting.v1.OverviewPlan.
 * Use `create(OverviewPlanSchema)` to create a new message.
 */
export const OverviewPlanSchema: GenMessage<OverviewPlan> =
  /*@__PURE__*/
  messageDesc(file_conduit_reporting_v1_overview, 3);

/**
 * OverviewInstallation preserves the native Reporting overview shape.
 *
 * @generated from message conduit.reporting.v1.OverviewInstallation
 */
export type OverviewInstallation =
  Message<"conduit.reporting.v1.OverviewInstallation"> & {
    /**
     * environment maps the corresponding native JSON field.
     *
     * @generated from field: string environment = 1;
     */
    environment: string;

    /**
     * installation_id maps the corresponding native JSON field.
     *
     * @generated from field: string installation_id = 2;
     */
    installationId: string;

    /**
     * health maps the corresponding native JSON field.
     *
     * @generated from field: string health = 3;
     */
    health: string;
  };

/**
 * Describes the message conduit.reporting.v1.OverviewInstallation.
 * Use `create(OverviewInstallationSchema)` to create a new message.
 */
export const OverviewInstallationSchema: GenMessage<OverviewInstallation> =
  /*@__PURE__*/
  messageDesc(file_conduit_reporting_v1_overview, 4);

/**
 * OverviewPromotion preserves the native Reporting overview shape.
 *
 * @generated from message conduit.reporting.v1.OverviewPromotion
 */
export type OverviewPromotion =
  Message<"conduit.reporting.v1.OverviewPromotion"> & {
    /**
     * id maps the corresponding native JSON field.
     *
     * @generated from field: string id = 1;
     */
    id: string;

    /**
     * product maps the corresponding native JSON field.
     *
     * @generated from field: string product = 2;
     */
    product: string;

    /**
     * version maps the corresponding native JSON field.
     *
     * @generated from field: string version = 3;
     */
    version: string;

    /**
     * from_channel maps the corresponding native JSON field.
     *
     * @generated from field: string from_channel = 4;
     */
    fromChannel: string;

    /**
     * to_channel maps the corresponding native JSON field.
     *
     * @generated from field: string to_channel = 5;
     */
    toChannel: string;
  };

/**
 * Describes the message conduit.reporting.v1.OverviewPromotion.
 * Use `create(OverviewPromotionSchema)` to create a new message.
 */
export const OverviewPromotionSchema: GenMessage<OverviewPromotion> =
  /*@__PURE__*/
  messageDesc(file_conduit_reporting_v1_overview, 5);

/**
 * ChangeRequestPanel preserves the native Reporting overview shape.
 *
 * @generated from message conduit.reporting.v1.ChangeRequestPanel
 */
export type ChangeRequestPanel =
  Message<"conduit.reporting.v1.ChangeRequestPanel"> & {
    /**
     * status is CURRENT or UNAVAILABLE; unavailable is not an empty successful panel.
     *
     * @generated from field: string status = 1;
     */
    status: string;

    /**
     * count maps the corresponding native JSON field.
     *
     * @generated from field: int64 count = 2;
     */
    count: bigint;

    /**
     * items maps the corresponding native JSON field.
     *
     * @generated from field: repeated conduit.reporting.v1.OverviewChangeRequest items = 3;
     */
    items: OverviewChangeRequest[];
  };

/**
 * Describes the message conduit.reporting.v1.ChangeRequestPanel.
 * Use `create(ChangeRequestPanelSchema)` to create a new message.
 */
export const ChangeRequestPanelSchema: GenMessage<ChangeRequestPanel> =
  /*@__PURE__*/
  messageDesc(file_conduit_reporting_v1_overview, 6);

/**
 * PlanPanel preserves the native Reporting overview shape.
 *
 * @generated from message conduit.reporting.v1.PlanPanel
 */
export type PlanPanel = Message<"conduit.reporting.v1.PlanPanel"> & {
  /**
   * status is CURRENT or UNAVAILABLE; unavailable is not an empty successful panel.
   *
   * @generated from field: string status = 1;
   */
  status: string;

  /**
   * count maps the corresponding native JSON field.
   *
   * @generated from field: int64 count = 2;
   */
  count: bigint;

  /**
   * items maps the corresponding native JSON field.
   *
   * @generated from field: repeated conduit.reporting.v1.OverviewPlan items = 3;
   */
  items: OverviewPlan[];
};

/**
 * Describes the message conduit.reporting.v1.PlanPanel.
 * Use `create(PlanPanelSchema)` to create a new message.
 */
export const PlanPanelSchema: GenMessage<PlanPanel> =
  /*@__PURE__*/
  messageDesc(file_conduit_reporting_v1_overview, 7);

/**
 * InstallationPanel preserves the native Reporting overview shape.
 *
 * @generated from message conduit.reporting.v1.InstallationPanel
 */
export type InstallationPanel =
  Message<"conduit.reporting.v1.InstallationPanel"> & {
    /**
     * status is CURRENT or UNAVAILABLE; unavailable is not an empty successful panel.
     *
     * @generated from field: string status = 1;
     */
    status: string;

    /**
     * count maps the corresponding native JSON field.
     *
     * @generated from field: int64 count = 2;
     */
    count: bigint;

    /**
     * items maps the corresponding native JSON field.
     *
     * @generated from field: repeated conduit.reporting.v1.OverviewInstallation items = 3;
     */
    items: OverviewInstallation[];
  };

/**
 * Describes the message conduit.reporting.v1.InstallationPanel.
 * Use `create(InstallationPanelSchema)` to create a new message.
 */
export const InstallationPanelSchema: GenMessage<InstallationPanel> =
  /*@__PURE__*/
  messageDesc(file_conduit_reporting_v1_overview, 8);

/**
 * StaleReportPanel preserves the native Reporting overview shape.
 *
 * @generated from message conduit.reporting.v1.StaleReportPanel
 */
export type StaleReportPanel =
  Message<"conduit.reporting.v1.StaleReportPanel"> & {
    /**
     * status is CURRENT or UNAVAILABLE; unavailable is not an empty successful panel.
     *
     * @generated from field: string status = 1;
     */
    status: string;

    /**
     * count maps the corresponding native JSON field.
     *
     * @generated from field: int64 count = 2;
     */
    count: bigint;

    /**
     * items maps the corresponding native JSON field.
     *
     * @generated from field: repeated google.protobuf.Empty items = 3;
     */
    items: Empty[];
  };

/**
 * Describes the message conduit.reporting.v1.StaleReportPanel.
 * Use `create(StaleReportPanelSchema)` to create a new message.
 */
export const StaleReportPanelSchema: GenMessage<StaleReportPanel> =
  /*@__PURE__*/
  messageDesc(file_conduit_reporting_v1_overview, 9);

/**
 * PromotionPanel preserves the native Reporting overview shape.
 *
 * @generated from message conduit.reporting.v1.PromotionPanel
 */
export type PromotionPanel = Message<"conduit.reporting.v1.PromotionPanel"> & {
  /**
   * status is CURRENT or UNAVAILABLE; unavailable is not an empty successful panel.
   *
   * @generated from field: string status = 1;
   */
  status: string;

  /**
   * count maps the corresponding native JSON field.
   *
   * @generated from field: int64 count = 2;
   */
  count: bigint;

  /**
   * items maps the corresponding native JSON field.
   *
   * @generated from field: repeated conduit.reporting.v1.OverviewPromotion items = 3;
   */
  items: OverviewPromotion[];
};

/**
 * Describes the message conduit.reporting.v1.PromotionPanel.
 * Use `create(PromotionPanelSchema)` to create a new message.
 */
export const PromotionPanelSchema: GenMessage<PromotionPanel> =
  /*@__PURE__*/
  messageDesc(file_conduit_reporting_v1_overview, 10);

/**
 * ReportingService reads authorized projections; it cannot approve or execute.
 *
 * @generated from service conduit.reporting.v1.ReportingService
 */
export const ReportingService: GenService<{
  /**
   * GetOverview maps GET /v1/overview. Each panel has independent availability.
   *
   * @generated from rpc conduit.reporting.v1.ReportingService.GetOverview
   */
  getOverview: {
    methodKind: "unary";
    input: typeof GetOverviewRequestSchema;
    output: typeof GetOverviewResponseSchema;
  };
}> = /*@__PURE__*/ serviceDesc(file_conduit_reporting_v1_overview, 0);

---
title: Typed RPC contracts
description: The exact native protobuf messages and service methods currently committed.
---

Generated from the native `.proto` files. Each source below defines only its declared methods. REST remains available during the staged migration; no automatic fallback or replay of an ambiguous write is authorized by these contracts. Transport implementation and browser adoption remain tracked separately in #392–#394.

## product_summary.proto

[proto/conduit/catalog/v1/product_summary.proto](https://github.com/jonathanagustin/conduit/blob/main/proto/conduit/catalog/v1/product_summary.proto)

```protobuf
syntax = "proto3";
package conduit.catalog.v1;

option go_package = "github.com/jonathanagustin/conduit/gen/go/conduit/catalog/v1;catalogv1";

// ProductReadService reads existing authorized native catalog projections.
service ProductReadService {
  // ListProductSummaries reads GET /v1/products?view=summary, never publications.
  rpc ListProductSummaries(ListProductSummariesRequest) returns (ListProductSummariesResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
  }
}

message ListProductSummariesRequest {
  // Absent uses the native default 100; explicit limits must be 1..200.
  optional int32 limit = 1;
  // Native opaque cursor, bound to the normalized search filter.
  string cursor = 2;
  string search = 3;
}

// ProductSummary is the native catalog summary, not a Release or publication.
message ProductSummary {
  string product = 1;
  int64 release_count = 2;
  // Uses the native catalog's ordering; it is not a new semver resolver.
  string latest_version = 3;
}

message ListProductSummariesResponse {
  repeated ProductSummary products = 1;
  // Counts only visible, filtered products, before pagination.
  int64 total = 2;
  int32 page_size = 3;
  int32 limit = 4;
  string next_cursor = 5;
  string previous_cursor = 6;
  bool has_previous = 7;
}
```

## notification_event.proto

[proto/conduit/notifications/v1/notification_event.proto](https://github.com/jonathanagustin/conduit/blob/main/proto/conduit/notifications/v1/notification_event.proto)

```protobuf
syntax = "proto3";

// Package conduit.notifications.v1 describes resource-authorized native events.
package conduit.notifications.v1;

import "google/protobuf/timestamp.proto";

option go_package = "github.com/jonathanagustin/conduit/gen/go/conduit/notifications/v1;notificationsv1";

// NotificationService reads existing events; it cannot subscribe, emit or approve.
service NotificationService {
  // ListNotificationEvents maps GET /v1/notifications/events for one resource.
  rpc ListNotificationEvents(ListNotificationEventsRequest) returns (ListNotificationEventsResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
  }
}

// ListNotificationEventsRequest names the existing authorized resource window.
message ListNotificationEventsRequest {
  // resource_rid is the required resourceRid query value, trimmed by the Hub.
  string resource_rid = 1;
  // limit is optional: absent selects the native ceiling; explicit zero is invalid.
  optional int64 limit = 2;
}

// ListNotificationEventsResponse retains the native event read envelope.
message ListNotificationEventsResponse {
  // resource_rid is the resource actually authorized and read.
  string resource_rid = 1;
  // limit is the bounded limit applied by the native Reader.
  int32 limit = 2;
  // events are the returned ordered window, not a total or a resume stream.
  repeated NotificationEvent events = 3;
}

// NotificationEvent is the native notifications/domain.Event projection.
message NotificationEvent {
  // id is the stable native event identifier.
  string id = 1;
  // type preserves the native open event vocabulary.
  string type = 2;
  // resource_rid is the resource the event is about.
  string resource_rid = 3;
  // environment is optional native environment context, not additional authority.
  string environment = 4;
  // summary is the stored human-readable sentence.
  string summary = 5;
  // actor is empty for system-generated events.
  string actor = 6;
  // occurred_at is the native event timestamp.
  google.protobuf.Timestamp occurred_at = 7;
}
```

## packaging_run.proto

[proto/conduit/packaging/v1/packaging_run.proto](https://github.com/jonathanagustin/conduit/blob/main/proto/conduit/packaging/v1/packaging_run.proto)

```protobuf
syntax = "proto3";

// Package conduit.packaging.v1 contains packaging-run read contracts.
// Other Hub domains use the same proto/conduit/<domain>/v1 layout.
package conduit.packaging.v1;

import "google/protobuf/timestamp.proto";

option go_package = "github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1;packagingv1";

// PackagingRunService reads packaging-run progress.
// It does not accept an approval and it does not report Plan status.
service PackagingRunService {
  // ListPackagingRuns reads GET /v1/packaging-runs with the same visibility and cursors.
  rpc ListPackagingRuns(ListPackagingRunsRequest) returns (ListPackagingRunsResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
  }

  // ListPackagingRunEvents reads the recorded, redacted history of one visible run.
  rpc ListPackagingRunEvents(ListPackagingRunEventsRequest) returns (ListPackagingRunEventsResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
  }

  // GetPackagingRun is the read model of GET /v1/packaging-runs/{run_id}.
  rpc GetPackagingRun(GetPackagingRunRequest) returns (GetPackagingRunResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
  }
}

// GetPackagingRunRequest names the run to read.
message GetPackagingRunRequest {
  // run_id is the path value of GET /v1/packaging-runs/{run_id}.
  string run_id = 1;
}

// GetPackagingRunResponse is the REST body of GET /v1/packaging-runs/{run_id}.
message GetPackagingRunResponse {
  // tier is the REST field tier.
  string tier = 1;
  // configured is the REST field configured.
  bool configured = 2;
  // authority is the REST field authority.
  string authority = 3;
  // plan_authority is the REST field planAuthority.
  string plan_authority = 4;
  // approval_authority is the REST field approvalAuthority.
  string approval_authority = 5;
  // run_id is the REST field runId.
  string run_id = 6;
  // phase is the REST field phase.
  string phase = 7;
  // outcome is the REST field outcome.
  string outcome = 8;
  // reason is the REST field reason.
  string reason = 9;
  // source_digest is the REST field sourceDigest.
  string source_digest = 10;
  // artifact_digest is the REST field artifactDigest.
  string artifact_digest = 11;
  // proposal_revision is the REST field proposalRevision.
  string proposal_revision = 12;
  // target_plan_ref is the REST field targetPlanRef.
  string target_plan_ref = 13;
  // plan_id is the REST field planId.
  string plan_id = 14;
  // handoff_reconciled is the REST field handoffReconciled.
  bool handoff_reconciled = 15;
  // last_review is the REST field lastReview.
  string last_review = 16;
  // duplicate_reviews is the REST field duplicateReviews.
  int32 duplicate_reviews = 17;
  // template_version is the REST field templateVersion.
  string template_version = 18;
  // tool_versions is the REST field toolVersions.
  string tool_versions = 19;
  // attachment_sha256 binds the same intake-file digests as the native REST read.
  repeated string attachment_sha256 = 20;
}

// ListPackagingRunsRequest uses the native index filters and opaque cursor.
message ListPackagingRunsRequest {
  // view is full (default, max 20) or summary (default 100, max 200).
  string view = 1;
  // limit is optional; explicitly zero or negative limits are invalid.
  optional int32 limit = 2;
  // cursor must match these filters and is never interpreted by the client.
  string cursor = 3;
  // phase selects the native phase.
  string phase = 4;
  // outcome selects the native outcome.
  string outcome = 5;
  // plan_id selects the referenced Plan, without granting Plan authority.
  string plan_id = 6;
  // search is the native case-insensitive search string.
  string search = 7;
}

// ListPackagingRunsResponse counts only visible, filtered runs.
message ListPackagingRunsResponse {
  // tier marks the native read model.
  string tier = 1;
  // configured reports whether a packaging reader is configured.
  bool configured = 2;
  // authority is packaging-run.
  string authority = 3;
  // plan_authority remains conduit-plan.
  string plan_authority = 4;
  // approval_authority remains conduit-governance.
  string approval_authority = 5;
  // items are redacted rows, ordered by run_id.
  repeated GetPackagingRunResponse items = 6;
  // page_size is the number of returned rows.
  int32 page_size = 7;
  // limit is the effective page limit.
  int32 limit = 8;
  // has_previous indicates a previous visible page.
  bool has_previous = 9;
  // next_cursor is absent at the end of the filtered index.
  string next_cursor = 10;
  // previous_cursor is absent on the first page.
  string previous_cursor = 11;
  // total counts visible matches, not all stored runs.
  int64 total = 12;
}

// ListPackagingRunEventsRequest names a run and the native seek window.
message ListPackagingRunEventsRequest {
  // run_id is the REST path parameter, not an approval or actor identity.
  string run_id = 1;
  // view selects full (default, max 20) or summary (default 100, max 200).
  string view = 2;
  // limit is optional; explicit zero and negative values are invalid.
  optional int32 limit = 3;
  // cursor is the native opaque seek cursor.
  string cursor = 4;
}

// PackagingRunEvent is one native recorded packaging step, after redaction.
// References never grant authority or establish Plan approval or execution.
message PackagingRunEvent {
  // sequence is the native monotonic journal position.
  int64 sequence = 1;
  // at is the occurrence time; sequence, not time, determines ordering.
  google.protobuf.Timestamp at = 2;
  // step retains the native open step vocabulary.
  string step = 3;
  // outcome is the recorded outcome; absent steps are not passes.
  string outcome = 4;
  // reason has the same credential redaction as REST.
  string reason = 5;
  // source_digest identifies the recorded source.
  string source_digest = 6;
  // artifact_digest identifies the recorded artifact.
  string artifact_digest = 7;
  // proposal_revision identifies the recorded proposal revision.
  string proposal_revision = 8;
  // approval_id is a reference, not approval authority.
  string approval_id = 9;
  // plan_id is a reference, not Plan state.
  string plan_id = 10;
  // target_plan_ref is the existing native target reference.
  string target_plan_ref = 11;
}

// ListPackagingRunEventsResponse is the native authorized history page.
message ListPackagingRunEventsResponse {
  // tier identifies this Conduit-native contract.
  string tier = 1;
  // configured is false when this Hub has no packaging reader.
  bool configured = 2;
  // authority owns packaging progress.
  string authority = 3;
  // plan_authority owns Plan state separately.
  string plan_authority = 4;
  // approval_authority owns approval decisions separately.
  string approval_authority = 5;
  // run_id identifies the visible parent run, empty when unconfigured.
  string run_id = 6;
  // items contains only recorded, redacted events in sequence order.
  repeated PackagingRunEvent items = 7;
  // page_size is the number of returned events.
  int32 page_size = 8;
  // limit is the applied native limit.
  int32 limit = 9;
  // has_previous indicates a previous seek window exists.
  bool has_previous = 10;
  // next_cursor is opaque and absent at the end.
  string next_cursor = 11;
  // previous_cursor is opaque; empty can address the first page.
  string previous_cursor = 12;
  // total counts the parent run's recorded events, not future steps.
  int64 total = 13;
}
```

## plan_approval.proto

[proto/conduit/plans/v1/plan_approval.proto](https://github.com/jonathanagustin/conduit/blob/main/proto/conduit/plans/v1/plan_approval.proto)

```protobuf
syntax = "proto3";
package conduit.plans.v1;

import "conduit/plans/v1/plan_summary.proto";
import "google/protobuf/timestamp.proto";

option go_package = "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1;plansv1";

// PlanApprovalService applies existing native governance, never caller authority.
service PlanApprovalService {
  // ApprovePlan is POST /v1/plans/{planID}/approvals. Do not retry or REST-replay.
  rpc ApprovePlan(ApprovePlanRequest) returns (ApprovePlanResponse);
}

message ApprovePlanRequest {
  string plan_id = 1;
  // rationale is the native required review explanation, not an identity.
  string rationale = 2;
}

message PlanApproval {
  string subject = 1;
  string rationale = 2;
  google.protobuf.Timestamp created_at = 3;
}

// ApprovePlanResponse projects the native result without execution credentials.
message ApprovePlanResponse {
  PlanSummary plan = 1;
  repeated PlanApproval approvals = 2;
  string change_request_id = 3;
}

// PlanApprovalOutcome is an error detail, not a request or an approval permit.
// Absent details mean an unknown outcome, never proof that no write occurred.
message PlanApprovalOutcome {
  string plan_id = 1;
  bool approval_recorded = 2;
  bool handoff_failed = 3;
}
```

## plan_summary.proto

[proto/conduit/plans/v1/plan_summary.proto](https://github.com/jonathanagustin/conduit/blob/main/proto/conduit/plans/v1/plan_summary.proto)

```protobuf
syntax = "proto3";
package conduit.plans.v1;

import "google/protobuf/timestamp.proto";

option go_package = "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1;plansv1";

// PlanReadService reads existing authorized native projections, never commands.
service PlanReadService {
  // ListPlanSummaries reads GET /v1/plans?view=summary. It never returns a lease.
  rpc ListPlanSummaries(ListPlanSummariesRequest) returns (ListPlanSummariesResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
  }
}

message ListPlanSummariesRequest {
  // limit is absent for the native default 100; valid explicit values are 1..200.
  optional int32 limit = 1;
  // cursor is the native opaque continuation, bound to the active filters.
  string cursor = 2;
  string environment = 3;
  string status = 4;
  string installation_id = 5;
  string search = 6;
}

// PlanSummary is the existing summary projection, not a full Plan or approval.
message PlanSummary {
  string id = 1;
  string environment = 2;
  // status is open native vocabulary, including future statuses.
  string status = 3;
  int64 step_count = 4;
  int64 approval_count = 5;
  int64 required_approvals = 6;
  google.protobuf.Timestamp created_at = 7;
  google.protobuf.Timestamp updated_at = 8;
  uint64 revision = 9;
}

message ListPlanSummariesResponse {
  repeated PlanSummary plans = 1;
  // total counts only authorized, filtered matches.
  int64 total = 2;
  int32 page_size = 3;
  int32 limit = 4;
  string next_cursor = 5;
  string previous_cursor = 6;
  bool has_previous = 7;
}
```

## overview.proto

[proto/conduit/reporting/v1/overview.proto](https://github.com/jonathanagustin/conduit/blob/main/proto/conduit/reporting/v1/overview.proto)

```protobuf
syntax = "proto3";

// Package conduit.reporting.v1 describes the existing operator-filtered overview.
package conduit.reporting.v1;

import "google/protobuf/empty.proto";
import "google/protobuf/timestamp.proto";

option go_package = "github.com/jonathanagustin/conduit/gen/go/conduit/reporting/v1;reportingv1";

// ReportingService reads authorized projections; it cannot approve or execute.
service ReportingService {
  // GetOverview maps GET /v1/overview. Each panel has independent availability.
  rpc GetOverview(GetOverviewRequest) returns (GetOverviewResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
  }
}

// GetOverviewRequest preserves the native Reporting overview shape.
message GetOverviewRequest {
}

// GetOverviewResponse preserves the native Reporting overview shape.
message GetOverviewResponse {
  // generated_at maps the corresponding native JSON field.
  google.protobuf.Timestamp generated_at = 1;
  // stale_after_seconds maps the corresponding native JSON field.
  int64 stale_after_seconds = 2;
  // awaiting_approvals maps the corresponding native JSON field.
  ChangeRequestPanel awaiting_approvals = 3;
  // failed_plans maps the corresponding native JSON field.
  PlanPanel failed_plans = 4;
  // running_plans maps the corresponding native JSON field.
  PlanPanel running_plans = 5;
  // unhealthy_entities maps the corresponding native JSON field.
  InstallationPanel unhealthy_entities = 6;
  // stale_reports maps the corresponding native JSON field.
  StaleReportPanel stale_reports = 7;
  // blocked_promotions maps the corresponding native JSON field.
  PromotionPanel blocked_promotions = 8;
}

// OverviewChangeRequest preserves the native Reporting overview shape.
message OverviewChangeRequest {
  // id maps the corresponding native JSON field.
  string id = 1;
  // title maps the corresponding native JSON field.
  string title = 2;
  // environment maps the corresponding native JSON field.
  string environment = 3;
}

// OverviewPlan preserves the native Reporting overview shape.
message OverviewPlan {
  // id maps the corresponding native JSON field.
  string id = 1;
  // environment maps the corresponding native JSON field.
  string environment = 2;
}

// OverviewInstallation preserves the native Reporting overview shape.
message OverviewInstallation {
  // environment maps the corresponding native JSON field.
  string environment = 1;
  // installation_id maps the corresponding native JSON field.
  string installation_id = 2;
  // health maps the corresponding native JSON field.
  string health = 3;
}

// OverviewPromotion preserves the native Reporting overview shape.
message OverviewPromotion {
  // id maps the corresponding native JSON field.
  string id = 1;
  // product maps the corresponding native JSON field.
  string product = 2;
  // version maps the corresponding native JSON field.
  string version = 3;
  // from_channel maps the corresponding native JSON field.
  string from_channel = 4;
  // to_channel maps the corresponding native JSON field.
  string to_channel = 5;
}

// ChangeRequestPanel preserves the native Reporting overview shape.
message ChangeRequestPanel {
  // status is CURRENT or UNAVAILABLE; unavailable is not an empty successful panel.
  string status = 1;
  // count maps the corresponding native JSON field.
  int64 count = 2;
  // items maps the corresponding native JSON field.
  repeated OverviewChangeRequest items = 3;
}

// PlanPanel preserves the native Reporting overview shape.
message PlanPanel {
  // status is CURRENT or UNAVAILABLE; unavailable is not an empty successful panel.
  string status = 1;
  // count maps the corresponding native JSON field.
  int64 count = 2;
  // items maps the corresponding native JSON field.
  repeated OverviewPlan items = 3;
}

// InstallationPanel preserves the native Reporting overview shape.
message InstallationPanel {
  // status is CURRENT or UNAVAILABLE; unavailable is not an empty successful panel.
  string status = 1;
  // count maps the corresponding native JSON field.
  int64 count = 2;
  // items maps the corresponding native JSON field.
  repeated OverviewInstallation items = 3;
}

// StaleReportPanel preserves the native Reporting overview shape.
message StaleReportPanel {
  // status is CURRENT or UNAVAILABLE; unavailable is not an empty successful panel.
  string status = 1;
  // count maps the corresponding native JSON field.
  int64 count = 2;
  // items maps the corresponding native JSON field.
  repeated google.protobuf.Empty items = 3;
}

// PromotionPanel preserves the native Reporting overview shape.
message PromotionPanel {
  // status is CURRENT or UNAVAILABLE; unavailable is not an empty successful panel.
  string status = 1;
  // count maps the corresponding native JSON field.
  int64 count = 2;
  // items maps the corresponding native JSON field.
  repeated OverviewPromotion items = 3;
}
```


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

## change_request_discussion.proto

[proto/conduit/changeactivity/v1/change_request_discussion.proto](https://github.com/jonathanagustin/conduit/blob/main/proto/conduit/changeactivity/v1/change_request_discussion.proto)

```protobuf
syntax = "proto3";
package conduit.changeactivity.v1;

import "google/protobuf/timestamp.proto";

option go_package = "github.com/jonathanagustin/conduit/gen/go/conduit/changeactivity/v1;changeactivityv1";

// ChangeRequestDiscussionReadService reads immutable native discussion records.
// It adds no append, review, approval, mutation or execution authority.
service ChangeRequestDiscussionReadService {
  // ListChangeRequestComments preserves the existing eight-comment page and
  // request-bound continuation. Existing callers remain REST; no fallback.
  rpc ListChangeRequestComments(ListChangeRequestCommentsRequest) returns (ListChangeRequestCommentsResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
  }
}

message ListChangeRequestCommentsRequest {
  string change_request_id = 1;
  // Native opaque cursor for this request; not a stable history snapshot.
  string cursor = 2;
}

message ChangeRequestComment {
  string id = 1;
  string change_request_id = 2;
  string author = 3;
  // The original carrier, distinct from the represented author.
  string actor = 4;
  google.protobuf.Timestamp created_at = 5;
  string text = 6;
  string value_tier = 7;
}

message ListChangeRequestCommentsResponse {
  // At most eight native comments, ordered by time then ID.
  repeated ChangeRequestComment comments = 1;
  string next_cursor = 2;
  string value_tier = 3;
}
```

## environment_catalog.proto

[proto/conduit/environments/v1/environment_catalog.proto](https://github.com/jonathanagustin/conduit/blob/main/proto/conduit/environments/v1/environment_catalog.proto)

```protobuf
syntax = "proto3";
package conduit.environments.v1;

import "conduit/environments/v1/environment_detail.proto";

option go_package = "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1;environmentsv1";

// EnvironmentCatalogService declares the full native Environment index.
// It reuses the full record without changing existing summary/detail services.
// Declaration does not mount a method or grant resource/Space authority.
service EnvironmentCatalogService {
  // ListEnvironments corresponds to GET /v1/environments with absent/full view.
  // Filter by original-carrier resource authority and readable Space membership
  // before search, totals and cursor pagination. Membership grants no authority.
  rpc ListEnvironments(ListEnvironmentsRequest) returns (ListEnvironmentsResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
  }
}

// ListEnvironmentsRequest selects the full projection, not summary view.
message ListEnvironmentsRequest {
  string search = 1;
  string cursor = 2;
  // Absent uses the native full-list default of 20; present must be 1 through 20.
  // The summary index's 100/200 limits do not apply to this full-record method.
  optional int32 limit = 3;
  // Absence is unscoped; present empty/invalid selection remains invalid.
  optional string space_id = 4;
}

// ListEnvironmentsResponse retains native full-record pagination metadata.
message ListEnvironmentsResponse {
  repeated EnvironmentRecord environments = 1;
  // Counts only readable records matching search within the selected grouping.
  int64 total = 2;
  int32 page_size = 3;
  int32 limit = 4;
  string next_cursor = 5;
  string previous_cursor = 6;
  bool has_previous = 7;
}
```

## environment_detail.proto

[proto/conduit/environments/v1/environment_detail.proto](https://github.com/jonathanagustin/conduit/blob/main/proto/conduit/environments/v1/environment_detail.proto)

```protobuf
syntax = "proto3";
package conduit.environments.v1;

import "conduit/environments/v1/environment_observation.proto";
import "conduit/environments/v1/environment_summary.proto";
import "google/protobuf/timestamp.proto";

option go_package = "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1;environmentsv1";

// EnvironmentDetailService declares the existing full native Environment read.
// This contract is not registered until its authorized adapter is mounted.
// It adds no approval, execution, credential or resource-grant authority.
service EnvironmentDetailService {
  // GetEnvironment corresponds to GET /v1/environments/{environmentID}.
  // Preserve original-carrier resource authority before Space membership reads.
  rpc GetEnvironment(GetEnvironmentRequest) returns (GetEnvironmentResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
  }
}

message GetEnvironmentRequest {
  // Literal native identity; do not replace it with a minted or inferred ID.
  string environment = 1;
  // Absence is unscoped. Present empty/invalid selection remains invalid.
  // Selection narrows existing resource access and grants none by itself.
  optional string space_id = 2;
}

message GetEnvironmentResponse {
  // Present on success; a missing Environment is a native not-found outcome.
  EnvironmentRecord record = 1;
}

// EnvironmentRecord carries the existing detail read, not a desired Entity list.
message EnvironmentRecord {
  Environment environment = 1;
  // Presence distinguishes no report from an observed empty report.
  ReportedEnvironmentState reported = 2;
  SpokeLiveness spoke = 3;
}

// Environment is raw native configuration, not effective defaults or readiness.
message Environment {
  string id = 1;
  // Opaque generation; never parse it as a numeric revision.
  string generation = 2;
  string package_architecture = 3;
  string connection_setting = 4;
  string declared_state_source = 5;
  string default_release_channel = 6;
  map<string, string> labels = 7;
  // Exact JSON object bytes for the native arbitrary configuration map. Do not convert numbers through protobuf Struct or wrap the whole Environment as JSON.
  bytes config_json = 8;
  repeated NamespaceTarget namespaces = 9;
  ArtifactSecurityPolicy artifact_security_policy = 10;
  ArtifactAvailabilityPolicy artifact_availability_policy = 11;
  repeated ArtifactRegistryAssignment artifact_registries = 12;
  repeated ExternalSecretAssignment secrets = 13;
  bool automatic_security_recall = 14;
  VulnerabilityUpgradePolicy vulnerability_upgrades = 15;
  int64 auto_suppression_threshold = 16;
  MaintenancePolicy maintenance = 17;
  HelmExecutionDefaults helm_execution = 18;
}

// MaintenancePolicy retains configured windows and recorded overrides.
message MaintenancePolicy {
  WeeklyMaintenanceWindow downtime = 1;
  WeeklyMaintenanceWindow no_downtime = 2;
  repeated MaintenanceOverride overrides = 3;
}

// ImagePrePullPolicy retains the native default when absent; present false is an explicit opt-out.
message ImagePrePullPolicy {
  bool enabled = 1;
  int64 timeout_seconds = 2;
}

message HelmExecutionDefaults {
  string default_rollout_strategy = 1;
  int64 install_resource_readiness_timeout_seconds = 2;
  int64 upgrade_resource_readiness_timeout_seconds = 3;
  ImagePrePullPolicy image_pre_pull = 4;
}

// ArtifactSecurityPolicy retains raw scan policy values without resolving freshness defaults.
message ArtifactSecurityPolicy {
  string required_outcome = 1;
  int64 maximum_scan_age_seconds = 2;
}

message ArtifactAvailabilityPolicy {
  string required_outcome = 1;
}

message VulnerabilityUpgradePolicy {
  bool allow_recalled_for_vulnerabilities = 1;
  int64 rescan_sla_seconds = 2;
}

// ArtifactRegistryAssignment carries references and classification, not credential material.
message ArtifactRegistryAssignment {
  string id = 1;
  string url = 2;
  string type = 3;
  int64 revision = 4;
  repeated string mirrors = 5;
  string authentication = 6;
  string credential_ref = 7;
  string image_pull_secret_name = 8;
}

// ExternalSecretAssignment identifies Environment-side material; the Hub does not dereference it.
message ExternalSecretAssignment {
  string name = 1;
  int64 revision = 2;
  string provider_ref = 3;
  string kubernetes_secret_name = 4;
  repeated string keys = 5;
}

// NamespaceTarget retains native flags and installed version/configuration observations.
message NamespaceTarget {
  string rid = 1;
  string name = 2;
  string description = 3;
  // Presence retains the distinction between absent and explicit false.
  optional bool deletion_protected = 4;
  string enrollment_rid = 5;
  repeated Organization organizations = 6;
  bool trashed = 7;
  bool ontology_count_valid = 8;
  map<string, InstalledTarget> installed = 9;
}

message Organization {
  string display_name = 1;
  string primary_hostname = 2;
}

message InstalledTarget {
  string version = 1;
  string config_digest = 2;
}

message WeeklyMaintenanceWindow {
  string time_zone = 1;
  repeated string intervals = 2;
}

// MaintenanceOverride carries recorded review metadata, not approval authority.
message MaintenanceOverride {
  string installation_id = 1;
  string type = 2;
  google.protobuf.Timestamp starts_at = 3;
  google.protobuf.Timestamp ends_at = 4;
  string reason = 5;
  // Existing recorded reviewer metadata; never use it as the request principal.
  string approved_by = 6;
}
```

## environment_observation.proto

[proto/conduit/environments/v1/environment_observation.proto](https://github.com/jonathanagustin/conduit/blob/main/proto/conduit/environments/v1/environment_observation.proto)

```protobuf
syntax = "proto3";
package conduit.environments.v1;

import "google/protobuf/timestamp.proto";

option go_package = "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1;environmentsv1";

// EnvironmentObservationService reads reported evidence, never desired state or
// execution credentials. REST remains supported; no automatic fallback applies.
service EnvironmentObservationService {
  // GetEnvironmentReportedState is GET /v1/environments/{environmentID}/reported-state.
  // It requires ReadResource on that Environment, including secret references.
  rpc GetEnvironmentReportedState(GetEnvironmentReportedStateRequest) returns (GetEnvironmentReportedStateResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
  }
}

message GetEnvironmentReportedStateRequest {
  string environment = 1;
}

// ReportedEnvironmentState retains observed evidence. An absent timestamp means
// native zero/unknown time; no
// current timestamp is invented. Open status strings retain native vocabulary.
message ReportedEnvironmentState {
  string environment = 1;
  string agent_id = 2;
  string plan_id = 3;
  google.protobuf.Timestamp observed_at = 4;
  repeated ReportedEntity entities = 5;
  repeated ReportedSecretReference secrets = 6;
  uint64 revision = 7;
}

message ReportedEntity {
  string installation_id = 1;
  string product = 2;
  string version = 3;
  string release_digest = 4;
  string namespace = 5;
  string config_digest = 6;
  string health = 7;
  string rollout_status = 8;
  string config_status = 9;
  string lifecycle = 10;
  google.protobuf.Timestamp observed_at = 11;
  // Presence distinguishes no health detail from an observed empty detail.
  EntityHealthDetails health_details = 12;
}

message EntityHealthDetails {
  string quorum = 1;
  repeated EntityNodeHealth nodes = 2;
}

message EntityNodeHealth {
  string node_id = 1;
  string product_version = 2;
  string result = 3;
  repeated EntityContainerHealth containers = 4;
}

message EntityContainerHealth {
  string container_id = 1;
  bool liveness = 2;
  bool readiness = 3;
  string result = 4;
  repeated HealthCheck checks = 5;
}

message HealthCheck {
  string id = 1;
  string message = 2;
  string result = 3;
  // JSON encoding of the native arbitrary params map, preserving numbers without
  // a protobuf Struct/double conversion. Native nil encodes as JSON null.
  bytes params_json = 4;
}

// ReportedSecretReference carries metadata only: provider references and key names,
// not secret values. This is still private Environment inventory.
message ReportedSecretReference {
  string assignment = 1;
  string namespace = 2;
  int64 revision = 3;
  string provider_ref = 4;
  string kubernetes_secret_name = 5;
  repeated string keys = 6;
}

message GetEnvironmentReportedStateResponse {
  ReportedEnvironmentState reported = 1;
}
```

## environment_summary.proto

[proto/conduit/environments/v1/environment_summary.proto](https://github.com/jonathanagustin/conduit/blob/main/proto/conduit/environments/v1/environment_summary.proto)

```protobuf
syntax = "proto3";
package conduit.environments.v1;

import "google/protobuf/timestamp.proto";

option go_package = "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1;environmentsv1";

// EnvironmentReadService declares native reads, not execution authority.
// A generated service is not registered or served until its authorized adapter
// is explicitly mounted. REST remains the supported Environment transport.
service EnvironmentReadService {
  // ListEnvironmentSummaries projects GET /v1/environments?view=summary.
  // It omits Environment configuration/policy collections and Entity bodies.
  rpc ListEnvironmentSummaries(ListEnvironmentSummariesRequest) returns (ListEnvironmentSummariesResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
  }
}

message ListEnvironmentSummariesRequest {
  // Absent uses native summary default 100; explicit values must be 1..200.
  optional int32 limit = 1;
  // Native opaque continuation, bound to normalized search and selected Space.
  string cursor = 2;
  // Native case-insensitive Environment ID substring search.
  string search = 3;
  // Empty means no Space selection, never an authorization bypass.
  string space_id = 4;
}

// EnvironmentIdentity contains existing native identity, not configuration.
// Generation is an opaque string and must not be parsed as a numeric revision.
message EnvironmentIdentity {
  string id = 1;
  string generation = 2;
}

// EnvironmentReportSummary is reported evidence, never desired state.
message EnvironmentReportSummary {
  string environment = 1;
  string agent_id = 2;
  google.protobuf.Timestamp observed_at = 3;
  int64 entity_count = 4;
}

// SpokeLiveness preserves open native state vocabulary and observation time.
// Absence of an observation must not be replaced by the current time.
message SpokeLiveness {
  string agent_id = 1;
  string state = 2;
  google.protobuf.Timestamp observed_at = 3;
}

message EnvironmentSummary {
  EnvironmentIdentity environment = 1;
  // Message presence distinguishes no report from an observed empty report.
  EnvironmentReportSummary reported = 2;
  SpokeLiveness spoke = 3;
}

message ListEnvironmentSummariesResponse {
  repeated EnvironmentSummary environments = 1;
  // Total counts only authorized, Space-selected and search-filtered records.
  int64 total = 2;
  int32 page_size = 3;
  int32 limit = 4;
  string next_cursor = 5;
  string previous_cursor = 6;
  bool has_previous = 7;
}
```

## fleet.proto

[proto/conduit/fleet/v1/fleet.proto](https://github.com/jonathanagustin/conduit/blob/main/proto/conduit/fleet/v1/fleet.proto)

```protobuf
syntax = "proto3";
package conduit.fleet.v1;

import "google/protobuf/timestamp.proto";

option go_package = "github.com/jonathanagustin/conduit/gen/go/conduit/fleet/v1;fleetv1";

// FleetReadService serves the existing operator-filtered native Fleet view.
service FleetReadService {
  // ListFleetInstallations is GET /v1/fleet/installations, not a Fleet action.
  rpc ListFleetInstallations(ListFleetInstallationsRequest) returns (ListFleetInstallationsResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
  }
}

message ListFleetInstallationsRequest {
  string environment = 1;
  string installation_id = 2;
  string product = 3;
  string health = 4;
  string rollout_status = 5;
  string config_status = 6;
  string lifecycle = 7;
  string search = 8;
  string cursor = 9;
  // Absent uses 100; explicit limits must be 1..200.
  optional int32 limit = 10;
  // Absent uses native reporting policy; explicit values must be 1..86400.
  optional int32 max_age_seconds = 11;
  // Readable membership narrows current Environment authority, never grants it.
  string space_id = 12;
}

// FleetInstallation is an observation/projection, never execution authority.
message FleetInstallation {
  string environment = 1;
  string installation_id = 2;
  string product = 3;
  string namespace = 4;
  string desired_version = 5;
  string observed_version = 6;
  string desired_release_digest = 7;
  string observed_release_digest = 8;
  string desired_config_digest = 9;
  string observed_config_digest = 10;
  string health = 11;
  string rollout_status = 12;
  string config_status = 13;
  string lifecycle = 14;
  // Missing means no recorded observation, never a fabricated current time.
  google.protobuf.Timestamp observed_at = 15;
  int64 observation_age_seconds = 16;
  bool stale = 17;
}

// FleetSummary counts all matching visible rows before pagination.
message FleetSummary {
  int64 total = 1;
  map<string, int64> health = 2;
  map<string, int64> rollout = 3;
  map<string, int64> config = 4;
  map<string, int64> lifecycle = 5;
  int64 stale = 6;
}

// FleetFacets retains native counterfactual counts: a dimension excludes its
// own filter, but never excludes authority or selected Space membership.
message FleetFacets {
  map<string, int64> environment = 1;
  map<string, int64> product = 2;
  map<string, int64> health = 3;
  map<string, int64> rollout_status = 4;
  map<string, int64> config_status = 5;
  map<string, int64> lifecycle = 6;
}

// ListFleetInstallationsResponse maps only REST-visible native fields. Internal
// facet-contribution scaffolding is not exposed. Empty protobuf collections do
// not retain Go nil-versus-empty distinctions.
message ListFleetInstallationsResponse {
  google.protobuf.Timestamp generated_at = 1;
  int64 max_result_age_seconds = 2;
  repeated FleetInstallation installations = 3;
  FleetSummary summary = 4;
  FleetFacets facets = 5;
  int32 page_size = 6;
  int32 limit = 7;
  string next_cursor = 8;
  string previous_cursor = 9;
  bool has_previous = 10;
}
```

## authorization_audit.proto

[proto/conduit/governance/v1/authorization_audit.proto](https://github.com/jonathanagustin/conduit/blob/main/proto/conduit/governance/v1/authorization_audit.proto)

```protobuf
syntax = "proto3";
package conduit.governance.v1;

import "google/protobuf/timestamp.proto";

option go_package = "github.com/jonathanagustin/conduit/gen/go/conduit/governance/v1;governancev1";

// AuthorizationAuditReadService serves native administrator-only event reads.
// It does not grant audit-chain export, mutation, pruning or archive authority.
service AuthorizationAuditReadService {
  // ListAuthorizationAuditEvents projects the events view of the native audit
  // route. REST remains supported; this method changes no caller transport.
  rpc ListAuthorizationAuditEvents(ListAuthorizationAuditEventsRequest) returns (ListAuthorizationAuditEventsResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
  }
}

message ListAuthorizationAuditEventsRequest {
  // Absent uses 100; explicit limits must be 1..200.
  optional int32 limit = 1;
  // Native opaque continuation bound to resource, time bounds and order.
  string cursor = 2;
  string resource_rid = 3;
  // Inclusive lower and exclusive upper bound; absent leaves a side open.
  google.protobuf.Timestamp since = 4;
  google.protobuf.Timestamp until = 5;
  bool descending = 6;
}

message AuthorizationAuditEvent {
  string id = 1;
  string schema_version = 2;
  repeated string categories = 3;
  string trace_id = 4;
  string subject = 5;
  string action = 6;
  string resource_rid = 7;
  string decision = 8;
  string reason = 9;
  // Native JSON encoded as UTF-8 bytes, not protobuf Struct: metadata numbers
  // must not be rounded through protobuf's floating-point Value representation.
  bytes request_fields_json = 10;
  bytes result_fields_json = 11;
  // Empty means the native optional context was absent/empty.
  bytes context_json = 12;
  // Missing means no recorded timestamp; never filled with the current time.
  google.protobuf.Timestamp created_at = 13;
}

message ListAuthorizationAuditEventsResponse {
  repeated AuthorizationAuditEvent events = 1;
  int64 total = 2;
  int32 page_size = 3;
  int32 limit = 4;
  string next_cursor = 5;
  // Forward seek only. No previous cursor is issued.
  bool has_previous = 6;
}

// AuthorizationAuditChainReadService serves the native filtered chain projection.
// This is not the persisted ledger, a trust anchor or archive custody authority.
// A separate procedure makes older Hubs refuse rather than ignore a view selector.
service AuthorizationAuditChainReadService {
  rpc ListAuthorizationAuditChain(ListAuthorizationAuditChainRequest) returns (ListAuthorizationAuditChainResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
  }
}

message ListAuthorizationAuditChainRequest {
  // Absent uses 100; explicit limits must be 1..200.
  optional int32 limit = 1;
  // Opaque native cursor bound to this projection, resource, time bounds and order.
  string cursor = 2;
  string resource_rid = 3;
  google.protobuf.Timestamp since = 4;
  google.protobuf.Timestamp until = 5;
  bool descending = 6;
}

message AuthorizationAuditChainEntry {
  uint64 sequence = 1;
  string id = 2;
  string actor = 3;
  string action = 4;
  string decision = 5;
  string previous_digest = 6;
  string digest = 7;
  google.protobuf.Timestamp created_at = 8;
  // Optional display context, not a digest input. Exact JSON avoids number loss.
  bytes context_json = 9;
  string resource_rid = 10;
  string origin = 11;
  // Exact native projection digest material, not a persisted ledger archive.
  bytes payload = 12;
}

message ListAuthorizationAuditChainResponse {
  repeated AuthorizationAuditChainEntry entries = 1;
  int64 total = 2;
  int32 page_size = 3;
  int32 limit = 4;
  string next_cursor = 5;
  bool has_previous = 6;
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

## plan_cancellation.proto

[proto/conduit/plans/v1/plan_cancellation.proto](https://github.com/jonathanagustin/conduit/blob/main/proto/conduit/plans/v1/plan_cancellation.proto)

```protobuf
syntax = "proto3";
package conduit.plans.v1;

import "conduit/plans/v1/plan_summary.proto";
import "google/protobuf/timestamp.proto";

option go_package = "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1;plansv1";

// PlanCancellationService applies existing cancellation and suppression policy.
service PlanCancellationService {
  // CancelPlan is POST /v1/plans/{planID}/cancel. Never retry or REST-replay.
  rpc CancelPlan(CancelPlanRequest) returns (CancelPlanResponse);
}

message CancelPlanRequest {
  string plan_id = 1;
  string reason = 2;
  // Suppression additionally requires Operate on the Plan's Environment.
  bool create_suppression = 3;
  // Zero uses the existing 30-minute default when suppression is requested.
  int64 suppression_duration_seconds = 4;
  string installation_id = 5;
}

// RecordedPlanCancellation describes the native cancellation, not an approval.
message RecordedPlanCancellation {
  string subject = 1;
  string reason = 2;
  google.protobuf.Timestamp created_at = 3;
}

// CancelPlanResponse is a scoped command result, not the complete REST Plan.
// Success follows all requested native effects, including suppression creation.
// Cancellation, Change Request cancellation and suppression are sequential.
message CancelPlanResponse {
  PlanSummary plan = 1;
  RecordedPlanCancellation cancellation = 2;
  string change_request_id = 3;
}

// PlanCancellationOutcome is error evidence, never a retry permit. It is emitted
// only after the native command completed but its response could not be mapped.
// Absent details mean an unknown outcome, never proof that no write occurred:
// native command errors may follow a persisted cancellation or other effect.
message PlanCancellationOutcome {
  string plan_id = 1;
  bool command_completed = 2;
}
```

## plan_scoped_summary.proto

[proto/conduit/plans/v1/plan_scoped_summary.proto](https://github.com/jonathanagustin/conduit/blob/main/proto/conduit/plans/v1/plan_scoped_summary.proto)

```protobuf
syntax = "proto3";
package conduit.plans.v1;

import "conduit/plans/v1/plan_summary.proto";

option go_package = "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1;plansv1";

// PlanScopedReadService is a separate native read capability. An older Hub
// refuses this procedure rather than ignoring an unknown Space selector on the
// unscoped procedure. Declaration alone mounts no route or resource authority.
service PlanScopedReadService {
  // ListPlanSummariesInSpace reads GET /v1/plans?view=summary&spaceId=SPACE.
  // Authorize the original carrier's selected persisted Space before private
  // membership and Plan reads. Membership narrows visibility; it grants none.
  rpc ListPlanSummariesInSpace(ListPlanSummariesInSpaceRequest) returns (ListPlanSummariesInSpaceResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
  }
}

message ListPlanSummariesInSpaceRequest {
  // Required native identity; absent/empty/invalid values are input failures.
  string space_id = 1;
  // Absent uses the native summary default 100; present must be 1 through 200.
  optional int32 limit = 2;
  // Existing opaque cursor, bound to filters and this Space; not a snapshot.
  string cursor = 3;
  string environment = 4;
  string status = 5;
  string installation_id = 6;
  string search = 7;
}

message ListPlanSummariesInSpaceResponse {
  // Echo the validated selection. Clients bind it before accepting any rows;
  // this is a grouping coordinate, not a grant or a delegated identity.
  string space_id = 1;
  // Existing summary fields and native current-visibility pagination metadata.
  ListPlanSummariesResponse page = 2;
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


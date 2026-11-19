// Regenerated from proto/conduit. DO NOT EDIT.
// @generated from file conduit/fleet/v1/fleet.proto (package conduit.fleet.v1, syntax proto3)
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
 * Describes the file conduit/fleet/v1/fleet.proto.
 */
export const file_conduit_fleet_v1_fleet: GenFile =
  /*@__PURE__*/
  fileDesc(
    "Chxjb25kdWl0L2ZsZWV0L3YxL2ZsZWV0LnByb3RvEhBjb25kdWl0LmZsZWV0LnYxIrICCh1MaXN0RmxlZXRJbnN0YWxsYXRpb25zUmVxdWVzdBITCgtlbnZpcm9ubWVudBgBIAEoCRIXCg9pbnN0YWxsYXRpb25faWQYAiABKAkSDwoHcHJvZHVjdBgDIAEoCRIOCgZoZWFsdGgYBCABKAkSFgoOcm9sbG91dF9zdGF0dXMYBSABKAkSFQoNY29uZmlnX3N0YXR1cxgGIAEoCRIRCglsaWZlY3ljbGUYByABKAkSDgoGc2VhcmNoGAggASgJEg4KBmN1cnNvchgJIAEoCRISCgVsaW1pdBgKIAEoBUgAiAEBEhwKD21heF9hZ2Vfc2Vjb25kcxgLIAEoBUgBiAEBEhAKCHNwYWNlX2lkGAwgASgJQggKBl9saW1pdEISChBfbWF4X2FnZV9zZWNvbmRzIssDChFGbGVldEluc3RhbGxhdGlvbhITCgtlbnZpcm9ubWVudBgBIAEoCRIXCg9pbnN0YWxsYXRpb25faWQYAiABKAkSDwoHcHJvZHVjdBgDIAEoCRIRCgluYW1lc3BhY2UYBCABKAkSFwoPZGVzaXJlZF92ZXJzaW9uGAUgASgJEhgKEG9ic2VydmVkX3ZlcnNpb24YBiABKAkSHgoWZGVzaXJlZF9yZWxlYXNlX2RpZ2VzdBgHIAEoCRIfChdvYnNlcnZlZF9yZWxlYXNlX2RpZ2VzdBgIIAEoCRIdChVkZXNpcmVkX2NvbmZpZ19kaWdlc3QYCSABKAkSHgoWb2JzZXJ2ZWRfY29uZmlnX2RpZ2VzdBgKIAEoCRIOCgZoZWFsdGgYCyABKAkSFgoOcm9sbG91dF9zdGF0dXMYDCABKAkSFQoNY29uZmlnX3N0YXR1cxgNIAEoCRIRCglsaWZlY3ljbGUYDiABKAkSLwoLb2JzZXJ2ZWRfYXQYDyABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wEh8KF29ic2VydmF0aW9uX2FnZV9zZWNvbmRzGBAgASgDEg0KBXN0YWxlGBEgASgIIuQDCgxGbGVldFN1bW1hcnkSDQoFdG90YWwYASABKAMSOgoGaGVhbHRoGAIgAygLMiouY29uZHVpdC5mbGVldC52MS5GbGVldFN1bW1hcnkuSGVhbHRoRW50cnkSPAoHcm9sbG91dBgDIAMoCzIrLmNvbmR1aXQuZmxlZXQudjEuRmxlZXRTdW1tYXJ5LlJvbGxvdXRFbnRyeRI6CgZjb25maWcYBCADKAsyKi5jb25kdWl0LmZsZWV0LnYxLkZsZWV0U3VtbWFyeS5Db25maWdFbnRyeRJACglsaWZlY3ljbGUYBSADKAsyLS5jb25kdWl0LmZsZWV0LnYxLkZsZWV0U3VtbWFyeS5MaWZlY3ljbGVFbnRyeRINCgVzdGFsZRgGIAEoAxotCgtIZWFsdGhFbnRyeRILCgNrZXkYASABKAkSDQoFdmFsdWUYAiABKAM6AjgBGi4KDFJvbGxvdXRFbnRyeRILCgNrZXkYASABKAkSDQoFdmFsdWUYAiABKAM6AjgBGi0KC0NvbmZpZ0VudHJ5EgsKA2tleRgBIAEoCRINCgV2YWx1ZRgCIAEoAzoCOAEaMAoOTGlmZWN5Y2xlRW50cnkSCwoDa2V5GAEgASgJEg0KBXZhbHVlGAIgASgDOgI4ASLNBQoLRmxlZXRGYWNldHMSQwoLZW52aXJvbm1lbnQYASADKAsyLi5jb25kdWl0LmZsZWV0LnYxLkZsZWV0RmFjZXRzLkVudmlyb25tZW50RW50cnkSOwoHcHJvZHVjdBgCIAMoCzIqLmNvbmR1aXQuZmxlZXQudjEuRmxlZXRGYWNldHMuUHJvZHVjdEVudHJ5EjkKBmhlYWx0aBgDIAMoCzIpLmNvbmR1aXQuZmxlZXQudjEuRmxlZXRGYWNldHMuSGVhbHRoRW50cnkSSAoOcm9sbG91dF9zdGF0dXMYBCADKAsyMC5jb25kdWl0LmZsZWV0LnYxLkZsZWV0RmFjZXRzLlJvbGxvdXRTdGF0dXNFbnRyeRJGCg1jb25maWdfc3RhdHVzGAUgAygLMi8uY29uZHVpdC5mbGVldC52MS5GbGVldEZhY2V0cy5Db25maWdTdGF0dXNFbnRyeRI/CglsaWZlY3ljbGUYBiADKAsyLC5jb25kdWl0LmZsZWV0LnYxLkZsZWV0RmFjZXRzLkxpZmVjeWNsZUVudHJ5GjIKEEVudmlyb25tZW50RW50cnkSCwoDa2V5GAEgASgJEg0KBXZhbHVlGAIgASgDOgI4ARouCgxQcm9kdWN0RW50cnkSCwoDa2V5GAEgASgJEg0KBXZhbHVlGAIgASgDOgI4ARotCgtIZWFsdGhFbnRyeRILCgNrZXkYASABKAkSDQoFdmFsdWUYAiABKAM6AjgBGjQKElJvbGxvdXRTdGF0dXNFbnRyeRILCgNrZXkYASABKAkSDQoFdmFsdWUYAiABKAM6AjgBGjMKEUNvbmZpZ1N0YXR1c0VudHJ5EgsKA2tleRgBIAEoCRINCgV2YWx1ZRgCIAEoAzoCOAEaMAoOTGlmZWN5Y2xlRW50cnkSCwoDa2V5GAEgASgJEg0KBXZhbHVlGAIgASgDOgI4ASL0AgoeTGlzdEZsZWV0SW5zdGFsbGF0aW9uc1Jlc3BvbnNlEjAKDGdlbmVyYXRlZF9hdBgBIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXASHgoWbWF4X3Jlc3VsdF9hZ2Vfc2Vjb25kcxgCIAEoAxI6Cg1pbnN0YWxsYXRpb25zGAMgAygLMiMuY29uZHVpdC5mbGVldC52MS5GbGVldEluc3RhbGxhdGlvbhIvCgdzdW1tYXJ5GAQgASgLMh4uY29uZHVpdC5mbGVldC52MS5GbGVldFN1bW1hcnkSLQoGZmFjZXRzGAUgASgLMh0uY29uZHVpdC5mbGVldC52MS5GbGVldEZhY2V0cxIRCglwYWdlX3NpemUYBiABKAUSDQoFbGltaXQYByABKAUSEwoLbmV4dF9jdXJzb3IYCCABKAkSFwoPcHJldmlvdXNfY3Vyc29yGAkgASgJEhQKDGhhc19wcmV2aW91cxgKIAEoCDKVAQoQRmxlZXRSZWFkU2VydmljZRKAAQoWTGlzdEZsZWV0SW5zdGFsbGF0aW9ucxIvLmNvbmR1aXQuZmxlZXQudjEuTGlzdEZsZWV0SW5zdGFsbGF0aW9uc1JlcXVlc3QaMC5jb25kdWl0LmZsZWV0LnYxLkxpc3RGbGVldEluc3RhbGxhdGlvbnNSZXNwb25zZSIDkAIBQkRaQmdpdGh1Yi5jb20vam9uYXRoYW5hZ3VzdGluL2NvbmR1aXQvZ2VuL2dvL2NvbmR1aXQvZmxlZXQvdjE7ZmxlZXR2MWIGcHJvdG8z",
    [file_google_protobuf_timestamp],
  );

/**
 * @generated from message conduit.fleet.v1.ListFleetInstallationsRequest
 */
export type ListFleetInstallationsRequest =
  Message<"conduit.fleet.v1.ListFleetInstallationsRequest"> & {
    /**
     * @generated from field: string environment = 1;
     */
    environment: string;

    /**
     * @generated from field: string installation_id = 2;
     */
    installationId: string;

    /**
     * @generated from field: string product = 3;
     */
    product: string;

    /**
     * @generated from field: string health = 4;
     */
    health: string;

    /**
     * @generated from field: string rollout_status = 5;
     */
    rolloutStatus: string;

    /**
     * @generated from field: string config_status = 6;
     */
    configStatus: string;

    /**
     * @generated from field: string lifecycle = 7;
     */
    lifecycle: string;

    /**
     * @generated from field: string search = 8;
     */
    search: string;

    /**
     * @generated from field: string cursor = 9;
     */
    cursor: string;

    /**
     * Absent uses 100; explicit limits must be 1..200.
     *
     * @generated from field: optional int32 limit = 10;
     */
    limit?: number | undefined;

    /**
     * Absent uses native reporting policy; explicit values must be 1..86400.
     *
     * @generated from field: optional int32 max_age_seconds = 11;
     */
    maxAgeSeconds?: number | undefined;

    /**
     * Readable membership narrows current Environment authority, never grants it.
     *
     * @generated from field: string space_id = 12;
     */
    spaceId: string;
  };

/**
 * Describes the message conduit.fleet.v1.ListFleetInstallationsRequest.
 * Use `create(ListFleetInstallationsRequestSchema)` to create a new message.
 */
export const ListFleetInstallationsRequestSchema: GenMessage<ListFleetInstallationsRequest> =
  /*@__PURE__*/
  messageDesc(file_conduit_fleet_v1_fleet, 0);

/**
 * FleetInstallation is an observation/projection, never execution authority.
 *
 * @generated from message conduit.fleet.v1.FleetInstallation
 */
export type FleetInstallation =
  Message<"conduit.fleet.v1.FleetInstallation"> & {
    /**
     * @generated from field: string environment = 1;
     */
    environment: string;

    /**
     * @generated from field: string installation_id = 2;
     */
    installationId: string;

    /**
     * @generated from field: string product = 3;
     */
    product: string;

    /**
     * @generated from field: string namespace = 4;
     */
    namespace: string;

    /**
     * @generated from field: string desired_version = 5;
     */
    desiredVersion: string;

    /**
     * @generated from field: string observed_version = 6;
     */
    observedVersion: string;

    /**
     * @generated from field: string desired_release_digest = 7;
     */
    desiredReleaseDigest: string;

    /**
     * @generated from field: string observed_release_digest = 8;
     */
    observedReleaseDigest: string;

    /**
     * @generated from field: string desired_config_digest = 9;
     */
    desiredConfigDigest: string;

    /**
     * @generated from field: string observed_config_digest = 10;
     */
    observedConfigDigest: string;

    /**
     * @generated from field: string health = 11;
     */
    health: string;

    /**
     * @generated from field: string rollout_status = 12;
     */
    rolloutStatus: string;

    /**
     * @generated from field: string config_status = 13;
     */
    configStatus: string;

    /**
     * @generated from field: string lifecycle = 14;
     */
    lifecycle: string;

    /**
     * Missing means no recorded observation, never a fabricated current time.
     *
     * @generated from field: google.protobuf.Timestamp observed_at = 15;
     */
    observedAt?: Timestamp | undefined;

    /**
     * @generated from field: int64 observation_age_seconds = 16;
     */
    observationAgeSeconds: bigint;

    /**
     * @generated from field: bool stale = 17;
     */
    stale: boolean;
  };

/**
 * Describes the message conduit.fleet.v1.FleetInstallation.
 * Use `create(FleetInstallationSchema)` to create a new message.
 */
export const FleetInstallationSchema: GenMessage<FleetInstallation> =
  /*@__PURE__*/
  messageDesc(file_conduit_fleet_v1_fleet, 1);

/**
 * FleetSummary counts all matching visible rows before pagination.
 *
 * @generated from message conduit.fleet.v1.FleetSummary
 */
export type FleetSummary = Message<"conduit.fleet.v1.FleetSummary"> & {
  /**
   * @generated from field: int64 total = 1;
   */
  total: bigint;

  /**
   * @generated from field: map<string, int64> health = 2;
   */
  health: { [key: string]: bigint };

  /**
   * @generated from field: map<string, int64> rollout = 3;
   */
  rollout: { [key: string]: bigint };

  /**
   * @generated from field: map<string, int64> config = 4;
   */
  config: { [key: string]: bigint };

  /**
   * @generated from field: map<string, int64> lifecycle = 5;
   */
  lifecycle: { [key: string]: bigint };

  /**
   * @generated from field: int64 stale = 6;
   */
  stale: bigint;
};

/**
 * Describes the message conduit.fleet.v1.FleetSummary.
 * Use `create(FleetSummarySchema)` to create a new message.
 */
export const FleetSummarySchema: GenMessage<FleetSummary> =
  /*@__PURE__*/
  messageDesc(file_conduit_fleet_v1_fleet, 2);

/**
 * FleetFacets retains native counterfactual counts: a dimension excludes its
 * own filter, but never excludes authority or selected Space membership.
 *
 * @generated from message conduit.fleet.v1.FleetFacets
 */
export type FleetFacets = Message<"conduit.fleet.v1.FleetFacets"> & {
  /**
   * @generated from field: map<string, int64> environment = 1;
   */
  environment: { [key: string]: bigint };

  /**
   * @generated from field: map<string, int64> product = 2;
   */
  product: { [key: string]: bigint };

  /**
   * @generated from field: map<string, int64> health = 3;
   */
  health: { [key: string]: bigint };

  /**
   * @generated from field: map<string, int64> rollout_status = 4;
   */
  rolloutStatus: { [key: string]: bigint };

  /**
   * @generated from field: map<string, int64> config_status = 5;
   */
  configStatus: { [key: string]: bigint };

  /**
   * @generated from field: map<string, int64> lifecycle = 6;
   */
  lifecycle: { [key: string]: bigint };
};

/**
 * Describes the message conduit.fleet.v1.FleetFacets.
 * Use `create(FleetFacetsSchema)` to create a new message.
 */
export const FleetFacetsSchema: GenMessage<FleetFacets> =
  /*@__PURE__*/
  messageDesc(file_conduit_fleet_v1_fleet, 3);

/**
 * ListFleetInstallationsResponse maps only REST-visible native fields. Internal
 * facet-contribution scaffolding is not exposed. Empty protobuf collections do
 * not retain Go nil-versus-empty distinctions.
 *
 * @generated from message conduit.fleet.v1.ListFleetInstallationsResponse
 */
export type ListFleetInstallationsResponse =
  Message<"conduit.fleet.v1.ListFleetInstallationsResponse"> & {
    /**
     * @generated from field: google.protobuf.Timestamp generated_at = 1;
     */
    generatedAt?: Timestamp | undefined;

    /**
     * @generated from field: int64 max_result_age_seconds = 2;
     */
    maxResultAgeSeconds: bigint;

    /**
     * @generated from field: repeated conduit.fleet.v1.FleetInstallation installations = 3;
     */
    installations: FleetInstallation[];

    /**
     * @generated from field: conduit.fleet.v1.FleetSummary summary = 4;
     */
    summary?: FleetSummary | undefined;

    /**
     * @generated from field: conduit.fleet.v1.FleetFacets facets = 5;
     */
    facets?: FleetFacets | undefined;

    /**
     * @generated from field: int32 page_size = 6;
     */
    pageSize: number;

    /**
     * @generated from field: int32 limit = 7;
     */
    limit: number;

    /**
     * @generated from field: string next_cursor = 8;
     */
    nextCursor: string;

    /**
     * @generated from field: string previous_cursor = 9;
     */
    previousCursor: string;

    /**
     * @generated from field: bool has_previous = 10;
     */
    hasPrevious: boolean;
  };

/**
 * Describes the message conduit.fleet.v1.ListFleetInstallationsResponse.
 * Use `create(ListFleetInstallationsResponseSchema)` to create a new message.
 */
export const ListFleetInstallationsResponseSchema: GenMessage<ListFleetInstallationsResponse> =
  /*@__PURE__*/
  messageDesc(file_conduit_fleet_v1_fleet, 4);

/**
 * FleetReadService serves the existing operator-filtered native Fleet view.
 *
 * @generated from service conduit.fleet.v1.FleetReadService
 */
export const FleetReadService: GenService<{
  /**
   * ListFleetInstallations is GET /v1/fleet/installations, not a Fleet action.
   *
   * @generated from rpc conduit.fleet.v1.FleetReadService.ListFleetInstallations
   */
  listFleetInstallations: {
    methodKind: "unary";
    input: typeof ListFleetInstallationsRequestSchema;
    output: typeof ListFleetInstallationsResponseSchema;
  };
}> = /*@__PURE__*/ serviceDesc(file_conduit_fleet_v1_fleet, 0);

// Regenerated from proto/conduit. DO NOT EDIT.
// @generated from file conduit/environments/v1/environment_detail.proto (package conduit.environments.v1, syntax proto3)
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
import type { ReportedEnvironmentState } from "./environment_observation_pb.js";
import { file_conduit_environments_v1_environment_observation } from "./environment_observation_pb.js";
import type { SpokeLiveness } from "./environment_summary_pb.js";
import { file_conduit_environments_v1_environment_summary } from "./environment_summary_pb.js";
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { file_google_protobuf_timestamp } from "@bufbuild/protobuf/wkt";
import type { Message } from "@bufbuild/protobuf";

/**
 * Describes the file conduit/environments/v1/environment_detail.proto.
 */
export const file_conduit_environments_v1_environment_detail: GenFile =
  /*@__PURE__*/
  fileDesc(
    "CjBjb25kdWl0L2Vudmlyb25tZW50cy92MS9lbnZpcm9ubWVudF9kZXRhaWwucHJvdG8SF2NvbmR1aXQuZW52aXJvbm1lbnRzLnYxIlAKFUdldEVudmlyb25tZW50UmVxdWVzdBITCgtlbnZpcm9ubWVudBgBIAEoCRIVCghzcGFjZV9pZBgCIAEoCUgAiAEBQgsKCV9zcGFjZV9pZCJUChZHZXRFbnZpcm9ubWVudFJlc3BvbnNlEjoKBnJlY29yZBgBIAEoCzIqLmNvbmR1aXQuZW52aXJvbm1lbnRzLnYxLkVudmlyb25tZW50UmVjb3JkIsoBChFFbnZpcm9ubWVudFJlY29yZBI5CgtlbnZpcm9ubWVudBgBIAEoCzIkLmNvbmR1aXQuZW52aXJvbm1lbnRzLnYxLkVudmlyb25tZW50EkMKCHJlcG9ydGVkGAIgASgLMjEuY29uZHVpdC5lbnZpcm9ubWVudHMudjEuUmVwb3J0ZWRFbnZpcm9ubWVudFN0YXRlEjUKBXNwb2tlGAMgASgLMiYuY29uZHVpdC5lbnZpcm9ubWVudHMudjEuU3Bva2VMaXZlbmVzcyLUBwoLRW52aXJvbm1lbnQSCgoCaWQYASABKAkSEgoKZ2VuZXJhdGlvbhgCIAEoCRIcChRwYWNrYWdlX2FyY2hpdGVjdHVyZRgDIAEoCRIaChJjb25uZWN0aW9uX3NldHRpbmcYBCABKAkSHQoVZGVjbGFyZWRfc3RhdGVfc291cmNlGAUgASgJEh8KF2RlZmF1bHRfcmVsZWFzZV9jaGFubmVsGAYgASgJEkAKBmxhYmVscxgHIAMoCzIwLmNvbmR1aXQuZW52aXJvbm1lbnRzLnYxLkVudmlyb25tZW50LkxhYmVsc0VudHJ5EhMKC2NvbmZpZ19qc29uGAggASgMEjwKCm5hbWVzcGFjZXMYCSADKAsyKC5jb25kdWl0LmVudmlyb25tZW50cy52MS5OYW1lc3BhY2VUYXJnZXQSUQoYYXJ0aWZhY3Rfc2VjdXJpdHlfcG9saWN5GAogASgLMi8uY29uZHVpdC5lbnZpcm9ubWVudHMudjEuQXJ0aWZhY3RTZWN1cml0eVBvbGljeRJZChxhcnRpZmFjdF9hdmFpbGFiaWxpdHlfcG9saWN5GAsgASgLMjMuY29uZHVpdC5lbnZpcm9ubWVudHMudjEuQXJ0aWZhY3RBdmFpbGFiaWxpdHlQb2xpY3kSUAoTYXJ0aWZhY3RfcmVnaXN0cmllcxgMIAMoCzIzLmNvbmR1aXQuZW52aXJvbm1lbnRzLnYxLkFydGlmYWN0UmVnaXN0cnlBc3NpZ25tZW50EkIKB3NlY3JldHMYDSADKAsyMS5jb25kdWl0LmVudmlyb25tZW50cy52MS5FeHRlcm5hbFNlY3JldEFzc2lnbm1lbnQSIQoZYXV0b21hdGljX3NlY3VyaXR5X3JlY2FsbBgOIAEoCBJTChZ2dWxuZXJhYmlsaXR5X3VwZ3JhZGVzGA8gASgLMjMuY29uZHVpdC5lbnZpcm9ubWVudHMudjEuVnVsbmVyYWJpbGl0eVVwZ3JhZGVQb2xpY3kSIgoaYXV0b19zdXBwcmVzc2lvbl90aHJlc2hvbGQYECABKAMSPwoLbWFpbnRlbmFuY2UYESABKAsyKi5jb25kdWl0LmVudmlyb25tZW50cy52MS5NYWludGVuYW5jZVBvbGljeRJGCg5oZWxtX2V4ZWN1dGlvbhgSIAEoCzIuLmNvbmR1aXQuZW52aXJvbm1lbnRzLnYxLkhlbG1FeGVjdXRpb25EZWZhdWx0cxotCgtMYWJlbHNFbnRyeRILCgNrZXkYASABKAkSDQoFdmFsdWUYAiABKAk6AjgBIt8BChFNYWludGVuYW5jZVBvbGljeRJCCghkb3dudGltZRgBIAEoCzIwLmNvbmR1aXQuZW52aXJvbm1lbnRzLnYxLldlZWtseU1haW50ZW5hbmNlV2luZG93EkUKC25vX2Rvd250aW1lGAIgASgLMjAuY29uZHVpdC5lbnZpcm9ubWVudHMudjEuV2Vla2x5TWFpbnRlbmFuY2VXaW5kb3cSPwoJb3ZlcnJpZGVzGAMgAygLMiwuY29uZHVpdC5lbnZpcm9ubWVudHMudjEuTWFpbnRlbmFuY2VPdmVycmlkZSI+ChJJbWFnZVByZVB1bGxQb2xpY3kSDwoHZW5hYmxlZBgBIAEoCBIXCg90aW1lb3V0X3NlY29uZHMYAiABKAMi5gEKFUhlbG1FeGVjdXRpb25EZWZhdWx0cxIgChhkZWZhdWx0X3JvbGxvdXRfc3RyYXRlZ3kYASABKAkSMgoqaW5zdGFsbF9yZXNvdXJjZV9yZWFkaW5lc3NfdGltZW91dF9zZWNvbmRzGAIgASgDEjIKKnVwZ3JhZGVfcmVzb3VyY2VfcmVhZGluZXNzX3RpbWVvdXRfc2Vjb25kcxgDIAEoAxJDCg5pbWFnZV9wcmVfcHVsbBgEIAEoCzIrLmNvbmR1aXQuZW52aXJvbm1lbnRzLnYxLkltYWdlUHJlUHVsbFBvbGljeSJUChZBcnRpZmFjdFNlY3VyaXR5UG9saWN5EhgKEHJlcXVpcmVkX291dGNvbWUYASABKAkSIAoYbWF4aW11bV9zY2FuX2FnZV9zZWNvbmRzGAIgASgDIjYKGkFydGlmYWN0QXZhaWxhYmlsaXR5UG9saWN5EhgKEHJlcXVpcmVkX291dGNvbWUYASABKAkiZAoaVnVsbmVyYWJpbGl0eVVwZ3JhZGVQb2xpY3kSKgoiYWxsb3dfcmVjYWxsZWRfZm9yX3Z1bG5lcmFiaWxpdGllcxgBIAEoCBIaChJyZXNjYW5fc2xhX3NlY29uZHMYAiABKAMitgEKGkFydGlmYWN0UmVnaXN0cnlBc3NpZ25tZW50EgoKAmlkGAEgASgJEgsKA3VybBgCIAEoCRIMCgR0eXBlGAMgASgJEhAKCHJldmlzaW9uGAQgASgDEg8KB21pcnJvcnMYBSADKAkSFgoOYXV0aGVudGljYXRpb24YBiABKAkSFgoOY3JlZGVudGlhbF9yZWYYByABKAkSHgoWaW1hZ2VfcHVsbF9zZWNyZXRfbmFtZRgIIAEoCSJ+ChhFeHRlcm5hbFNlY3JldEFzc2lnbm1lbnQSDAoEbmFtZRgBIAEoCRIQCghyZXZpc2lvbhgCIAEoAxIUCgxwcm92aWRlcl9yZWYYAyABKAkSHgoWa3ViZXJuZXRlc19zZWNyZXRfbmFtZRgEIAEoCRIMCgRrZXlzGAUgAygJIqYDCg9OYW1lc3BhY2VUYXJnZXQSCwoDcmlkGAEgASgJEgwKBG5hbWUYAiABKAkSEwoLZGVzY3JpcHRpb24YAyABKAkSHwoSZGVsZXRpb25fcHJvdGVjdGVkGAQgASgISACIAQESFgoOZW5yb2xsbWVudF9yaWQYBSABKAkSPAoNb3JnYW5pemF0aW9ucxgGIAMoCzIlLmNvbmR1aXQuZW52aXJvbm1lbnRzLnYxLk9yZ2FuaXphdGlvbhIPCgd0cmFzaGVkGAcgASgIEhwKFG9udG9sb2d5X2NvdW50X3ZhbGlkGAggASgIEkoKCWluc3RhbGxlZBgJIAMoCzI3LmNvbmR1aXQuZW52aXJvbm1lbnRzLnYxLk5hbWVzcGFjZVRhcmdldC5JbnN0YWxsZWRFbnRyeRpaCg5JbnN0YWxsZWRFbnRyeRILCgNrZXkYASABKAkSNwoFdmFsdWUYAiABKAsyKC5jb25kdWl0LmVudmlyb25tZW50cy52MS5JbnN0YWxsZWRUYXJnZXQ6AjgBQhUKE19kZWxldGlvbl9wcm90ZWN0ZWQiPgoMT3JnYW5pemF0aW9uEhQKDGRpc3BsYXlfbmFtZRgBIAEoCRIYChBwcmltYXJ5X2hvc3RuYW1lGAIgASgJIjkKD0luc3RhbGxlZFRhcmdldBIPCgd2ZXJzaW9uGAEgASgJEhUKDWNvbmZpZ19kaWdlc3QYAiABKAkiPwoXV2Vla2x5TWFpbnRlbmFuY2VXaW5kb3cSEQoJdGltZV96b25lGAEgASgJEhEKCWludGVydmFscxgCIAMoCSK9AQoTTWFpbnRlbmFuY2VPdmVycmlkZRIXCg9pbnN0YWxsYXRpb25faWQYASABKAkSDAoEdHlwZRgCIAEoCRItCglzdGFydHNfYXQYAyABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wEisKB2VuZHNfYXQYBCABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wEg4KBnJlYXNvbhgFIAEoCRITCgthcHByb3ZlZF9ieRgGIAEoCTKSAQoYRW52aXJvbm1lbnREZXRhaWxTZXJ2aWNlEnYKDkdldEVudmlyb25tZW50Ei4uY29uZHVpdC5lbnZpcm9ubWVudHMudjEuR2V0RW52aXJvbm1lbnRSZXF1ZXN0Gi8uY29uZHVpdC5lbnZpcm9ubWVudHMudjEuR2V0RW52aXJvbm1lbnRSZXNwb25zZSIDkAIBQlJaUGdpdGh1Yi5jb20vam9uYXRoYW5hZ3VzdGluL2NvbmR1aXQvZ2VuL2dvL2NvbmR1aXQvZW52aXJvbm1lbnRzL3YxO2Vudmlyb25tZW50c3YxYgZwcm90bzM",
    [
      file_conduit_environments_v1_environment_observation,
      file_conduit_environments_v1_environment_summary,
      file_google_protobuf_timestamp,
    ],
  );

/**
 * @generated from message conduit.environments.v1.GetEnvironmentRequest
 */
export type GetEnvironmentRequest =
  Message<"conduit.environments.v1.GetEnvironmentRequest"> & {
    /**
     * Literal native identity; do not replace it with a minted or inferred ID.
     *
     * @generated from field: string environment = 1;
     */
    environment: string;

    /**
     * Absence is unscoped. Present empty/invalid selection remains invalid.
     * Selection narrows existing resource access and grants none by itself.
     *
     * @generated from field: optional string space_id = 2;
     */
    spaceId?: string | undefined;
  };

/**
 * Describes the message conduit.environments.v1.GetEnvironmentRequest.
 * Use `create(GetEnvironmentRequestSchema)` to create a new message.
 */
export const GetEnvironmentRequestSchema: GenMessage<GetEnvironmentRequest> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_detail, 0);

/**
 * @generated from message conduit.environments.v1.GetEnvironmentResponse
 */
export type GetEnvironmentResponse =
  Message<"conduit.environments.v1.GetEnvironmentResponse"> & {
    /**
     * Present on success; a missing Environment is a native not-found outcome.
     *
     * @generated from field: conduit.environments.v1.EnvironmentRecord record = 1;
     */
    record?: EnvironmentRecord | undefined;
  };

/**
 * Describes the message conduit.environments.v1.GetEnvironmentResponse.
 * Use `create(GetEnvironmentResponseSchema)` to create a new message.
 */
export const GetEnvironmentResponseSchema: GenMessage<GetEnvironmentResponse> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_detail, 1);

/**
 * EnvironmentRecord carries the existing detail read, not a desired Entity list.
 *
 * @generated from message conduit.environments.v1.EnvironmentRecord
 */
export type EnvironmentRecord =
  Message<"conduit.environments.v1.EnvironmentRecord"> & {
    /**
     * @generated from field: conduit.environments.v1.Environment environment = 1;
     */
    environment?: Environment | undefined;

    /**
     * Presence distinguishes no report from an observed empty report.
     *
     * @generated from field: conduit.environments.v1.ReportedEnvironmentState reported = 2;
     */
    reported?: ReportedEnvironmentState | undefined;

    /**
     * @generated from field: conduit.environments.v1.SpokeLiveness spoke = 3;
     */
    spoke?: SpokeLiveness | undefined;
  };

/**
 * Describes the message conduit.environments.v1.EnvironmentRecord.
 * Use `create(EnvironmentRecordSchema)` to create a new message.
 */
export const EnvironmentRecordSchema: GenMessage<EnvironmentRecord> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_detail, 2);

/**
 * Environment is raw native configuration, not effective defaults or readiness.
 *
 * @generated from message conduit.environments.v1.Environment
 */
export type Environment = Message<"conduit.environments.v1.Environment"> & {
  /**
   * @generated from field: string id = 1;
   */
  id: string;

  /**
   * Opaque generation; never parse it as a numeric revision.
   *
   * @generated from field: string generation = 2;
   */
  generation: string;

  /**
   * @generated from field: string package_architecture = 3;
   */
  packageArchitecture: string;

  /**
   * @generated from field: string connection_setting = 4;
   */
  connectionSetting: string;

  /**
   * @generated from field: string declared_state_source = 5;
   */
  declaredStateSource: string;

  /**
   * @generated from field: string default_release_channel = 6;
   */
  defaultReleaseChannel: string;

  /**
   * @generated from field: map<string, string> labels = 7;
   */
  labels: { [key: string]: string };

  /**
   * Exact JSON object bytes for the native arbitrary configuration map. Do not convert numbers through protobuf Struct or wrap the whole Environment as JSON.
   *
   * @generated from field: bytes config_json = 8;
   */
  configJson: Uint8Array;

  /**
   * @generated from field: repeated conduit.environments.v1.NamespaceTarget namespaces = 9;
   */
  namespaces: NamespaceTarget[];

  /**
   * @generated from field: conduit.environments.v1.ArtifactSecurityPolicy artifact_security_policy = 10;
   */
  artifactSecurityPolicy?: ArtifactSecurityPolicy | undefined;

  /**
   * @generated from field: conduit.environments.v1.ArtifactAvailabilityPolicy artifact_availability_policy = 11;
   */
  artifactAvailabilityPolicy?: ArtifactAvailabilityPolicy | undefined;

  /**
   * @generated from field: repeated conduit.environments.v1.ArtifactRegistryAssignment artifact_registries = 12;
   */
  artifactRegistries: ArtifactRegistryAssignment[];

  /**
   * @generated from field: repeated conduit.environments.v1.ExternalSecretAssignment secrets = 13;
   */
  secrets: ExternalSecretAssignment[];

  /**
   * @generated from field: bool automatic_security_recall = 14;
   */
  automaticSecurityRecall: boolean;

  /**
   * @generated from field: conduit.environments.v1.VulnerabilityUpgradePolicy vulnerability_upgrades = 15;
   */
  vulnerabilityUpgrades?: VulnerabilityUpgradePolicy | undefined;

  /**
   * @generated from field: int64 auto_suppression_threshold = 16;
   */
  autoSuppressionThreshold: bigint;

  /**
   * @generated from field: conduit.environments.v1.MaintenancePolicy maintenance = 17;
   */
  maintenance?: MaintenancePolicy | undefined;

  /**
   * @generated from field: conduit.environments.v1.HelmExecutionDefaults helm_execution = 18;
   */
  helmExecution?: HelmExecutionDefaults | undefined;
};

/**
 * Describes the message conduit.environments.v1.Environment.
 * Use `create(EnvironmentSchema)` to create a new message.
 */
export const EnvironmentSchema: GenMessage<Environment> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_detail, 3);

/**
 * MaintenancePolicy retains configured windows and recorded overrides.
 *
 * @generated from message conduit.environments.v1.MaintenancePolicy
 */
export type MaintenancePolicy =
  Message<"conduit.environments.v1.MaintenancePolicy"> & {
    /**
     * @generated from field: conduit.environments.v1.WeeklyMaintenanceWindow downtime = 1;
     */
    downtime?: WeeklyMaintenanceWindow | undefined;

    /**
     * @generated from field: conduit.environments.v1.WeeklyMaintenanceWindow no_downtime = 2;
     */
    noDowntime?: WeeklyMaintenanceWindow | undefined;

    /**
     * @generated from field: repeated conduit.environments.v1.MaintenanceOverride overrides = 3;
     */
    overrides: MaintenanceOverride[];
  };

/**
 * Describes the message conduit.environments.v1.MaintenancePolicy.
 * Use `create(MaintenancePolicySchema)` to create a new message.
 */
export const MaintenancePolicySchema: GenMessage<MaintenancePolicy> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_detail, 4);

/**
 * ImagePrePullPolicy retains the native default when absent; present false is an explicit opt-out.
 *
 * @generated from message conduit.environments.v1.ImagePrePullPolicy
 */
export type ImagePrePullPolicy =
  Message<"conduit.environments.v1.ImagePrePullPolicy"> & {
    /**
     * @generated from field: bool enabled = 1;
     */
    enabled: boolean;

    /**
     * @generated from field: int64 timeout_seconds = 2;
     */
    timeoutSeconds: bigint;
  };

/**
 * Describes the message conduit.environments.v1.ImagePrePullPolicy.
 * Use `create(ImagePrePullPolicySchema)` to create a new message.
 */
export const ImagePrePullPolicySchema: GenMessage<ImagePrePullPolicy> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_detail, 5);

/**
 * @generated from message conduit.environments.v1.HelmExecutionDefaults
 */
export type HelmExecutionDefaults =
  Message<"conduit.environments.v1.HelmExecutionDefaults"> & {
    /**
     * @generated from field: string default_rollout_strategy = 1;
     */
    defaultRolloutStrategy: string;

    /**
     * @generated from field: int64 install_resource_readiness_timeout_seconds = 2;
     */
    installResourceReadinessTimeoutSeconds: bigint;

    /**
     * @generated from field: int64 upgrade_resource_readiness_timeout_seconds = 3;
     */
    upgradeResourceReadinessTimeoutSeconds: bigint;

    /**
     * @generated from field: conduit.environments.v1.ImagePrePullPolicy image_pre_pull = 4;
     */
    imagePrePull?: ImagePrePullPolicy | undefined;
  };

/**
 * Describes the message conduit.environments.v1.HelmExecutionDefaults.
 * Use `create(HelmExecutionDefaultsSchema)` to create a new message.
 */
export const HelmExecutionDefaultsSchema: GenMessage<HelmExecutionDefaults> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_detail, 6);

/**
 * ArtifactSecurityPolicy retains raw scan policy values without resolving freshness defaults.
 *
 * @generated from message conduit.environments.v1.ArtifactSecurityPolicy
 */
export type ArtifactSecurityPolicy =
  Message<"conduit.environments.v1.ArtifactSecurityPolicy"> & {
    /**
     * @generated from field: string required_outcome = 1;
     */
    requiredOutcome: string;

    /**
     * @generated from field: int64 maximum_scan_age_seconds = 2;
     */
    maximumScanAgeSeconds: bigint;
  };

/**
 * Describes the message conduit.environments.v1.ArtifactSecurityPolicy.
 * Use `create(ArtifactSecurityPolicySchema)` to create a new message.
 */
export const ArtifactSecurityPolicySchema: GenMessage<ArtifactSecurityPolicy> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_detail, 7);

/**
 * @generated from message conduit.environments.v1.ArtifactAvailabilityPolicy
 */
export type ArtifactAvailabilityPolicy =
  Message<"conduit.environments.v1.ArtifactAvailabilityPolicy"> & {
    /**
     * @generated from field: string required_outcome = 1;
     */
    requiredOutcome: string;
  };

/**
 * Describes the message conduit.environments.v1.ArtifactAvailabilityPolicy.
 * Use `create(ArtifactAvailabilityPolicySchema)` to create a new message.
 */
export const ArtifactAvailabilityPolicySchema: GenMessage<ArtifactAvailabilityPolicy> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_detail, 8);

/**
 * @generated from message conduit.environments.v1.VulnerabilityUpgradePolicy
 */
export type VulnerabilityUpgradePolicy =
  Message<"conduit.environments.v1.VulnerabilityUpgradePolicy"> & {
    /**
     * @generated from field: bool allow_recalled_for_vulnerabilities = 1;
     */
    allowRecalledForVulnerabilities: boolean;

    /**
     * @generated from field: int64 rescan_sla_seconds = 2;
     */
    rescanSlaSeconds: bigint;
  };

/**
 * Describes the message conduit.environments.v1.VulnerabilityUpgradePolicy.
 * Use `create(VulnerabilityUpgradePolicySchema)` to create a new message.
 */
export const VulnerabilityUpgradePolicySchema: GenMessage<VulnerabilityUpgradePolicy> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_detail, 9);

/**
 * ArtifactRegistryAssignment carries references and classification, not credential material.
 *
 * @generated from message conduit.environments.v1.ArtifactRegistryAssignment
 */
export type ArtifactRegistryAssignment =
  Message<"conduit.environments.v1.ArtifactRegistryAssignment"> & {
    /**
     * @generated from field: string id = 1;
     */
    id: string;

    /**
     * @generated from field: string url = 2;
     */
    url: string;

    /**
     * @generated from field: string type = 3;
     */
    type: string;

    /**
     * @generated from field: int64 revision = 4;
     */
    revision: bigint;

    /**
     * @generated from field: repeated string mirrors = 5;
     */
    mirrors: string[];

    /**
     * @generated from field: string authentication = 6;
     */
    authentication: string;

    /**
     * @generated from field: string credential_ref = 7;
     */
    credentialRef: string;

    /**
     * @generated from field: string image_pull_secret_name = 8;
     */
    imagePullSecretName: string;
  };

/**
 * Describes the message conduit.environments.v1.ArtifactRegistryAssignment.
 * Use `create(ArtifactRegistryAssignmentSchema)` to create a new message.
 */
export const ArtifactRegistryAssignmentSchema: GenMessage<ArtifactRegistryAssignment> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_detail, 10);

/**
 * ExternalSecretAssignment identifies Environment-side material; the Hub does not dereference it.
 *
 * @generated from message conduit.environments.v1.ExternalSecretAssignment
 */
export type ExternalSecretAssignment =
  Message<"conduit.environments.v1.ExternalSecretAssignment"> & {
    /**
     * @generated from field: string name = 1;
     */
    name: string;

    /**
     * @generated from field: int64 revision = 2;
     */
    revision: bigint;

    /**
     * @generated from field: string provider_ref = 3;
     */
    providerRef: string;

    /**
     * @generated from field: string kubernetes_secret_name = 4;
     */
    kubernetesSecretName: string;

    /**
     * @generated from field: repeated string keys = 5;
     */
    keys: string[];
  };

/**
 * Describes the message conduit.environments.v1.ExternalSecretAssignment.
 * Use `create(ExternalSecretAssignmentSchema)` to create a new message.
 */
export const ExternalSecretAssignmentSchema: GenMessage<ExternalSecretAssignment> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_detail, 11);

/**
 * NamespaceTarget retains native flags and installed version/configuration observations.
 *
 * @generated from message conduit.environments.v1.NamespaceTarget
 */
export type NamespaceTarget =
  Message<"conduit.environments.v1.NamespaceTarget"> & {
    /**
     * @generated from field: string rid = 1;
     */
    rid: string;

    /**
     * @generated from field: string name = 2;
     */
    name: string;

    /**
     * @generated from field: string description = 3;
     */
    description: string;

    /**
     * Presence retains the distinction between absent and explicit false.
     *
     * @generated from field: optional bool deletion_protected = 4;
     */
    deletionProtected?: boolean | undefined;

    /**
     * @generated from field: string enrollment_rid = 5;
     */
    enrollmentRid: string;

    /**
     * @generated from field: repeated conduit.environments.v1.Organization organizations = 6;
     */
    organizations: Organization[];

    /**
     * @generated from field: bool trashed = 7;
     */
    trashed: boolean;

    /**
     * @generated from field: bool ontology_count_valid = 8;
     */
    ontologyCountValid: boolean;

    /**
     * @generated from field: map<string, conduit.environments.v1.InstalledTarget> installed = 9;
     */
    installed: { [key: string]: InstalledTarget };
  };

/**
 * Describes the message conduit.environments.v1.NamespaceTarget.
 * Use `create(NamespaceTargetSchema)` to create a new message.
 */
export const NamespaceTargetSchema: GenMessage<NamespaceTarget> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_detail, 12);

/**
 * @generated from message conduit.environments.v1.Organization
 */
export type Organization = Message<"conduit.environments.v1.Organization"> & {
  /**
   * @generated from field: string display_name = 1;
   */
  displayName: string;

  /**
   * @generated from field: string primary_hostname = 2;
   */
  primaryHostname: string;
};

/**
 * Describes the message conduit.environments.v1.Organization.
 * Use `create(OrganizationSchema)` to create a new message.
 */
export const OrganizationSchema: GenMessage<Organization> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_detail, 13);

/**
 * @generated from message conduit.environments.v1.InstalledTarget
 */
export type InstalledTarget =
  Message<"conduit.environments.v1.InstalledTarget"> & {
    /**
     * @generated from field: string version = 1;
     */
    version: string;

    /**
     * @generated from field: string config_digest = 2;
     */
    configDigest: string;
  };

/**
 * Describes the message conduit.environments.v1.InstalledTarget.
 * Use `create(InstalledTargetSchema)` to create a new message.
 */
export const InstalledTargetSchema: GenMessage<InstalledTarget> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_detail, 14);

/**
 * @generated from message conduit.environments.v1.WeeklyMaintenanceWindow
 */
export type WeeklyMaintenanceWindow =
  Message<"conduit.environments.v1.WeeklyMaintenanceWindow"> & {
    /**
     * @generated from field: string time_zone = 1;
     */
    timeZone: string;

    /**
     * @generated from field: repeated string intervals = 2;
     */
    intervals: string[];
  };

/**
 * Describes the message conduit.environments.v1.WeeklyMaintenanceWindow.
 * Use `create(WeeklyMaintenanceWindowSchema)` to create a new message.
 */
export const WeeklyMaintenanceWindowSchema: GenMessage<WeeklyMaintenanceWindow> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_detail, 15);

/**
 * MaintenanceOverride carries recorded review metadata, not approval authority.
 *
 * @generated from message conduit.environments.v1.MaintenanceOverride
 */
export type MaintenanceOverride =
  Message<"conduit.environments.v1.MaintenanceOverride"> & {
    /**
     * @generated from field: string installation_id = 1;
     */
    installationId: string;

    /**
     * @generated from field: string type = 2;
     */
    type: string;

    /**
     * @generated from field: google.protobuf.Timestamp starts_at = 3;
     */
    startsAt?: Timestamp | undefined;

    /**
     * @generated from field: google.protobuf.Timestamp ends_at = 4;
     */
    endsAt?: Timestamp | undefined;

    /**
     * @generated from field: string reason = 5;
     */
    reason: string;

    /**
     * Existing recorded reviewer metadata; never use it as the request principal.
     *
     * @generated from field: string approved_by = 6;
     */
    approvedBy: string;
  };

/**
 * Describes the message conduit.environments.v1.MaintenanceOverride.
 * Use `create(MaintenanceOverrideSchema)` to create a new message.
 */
export const MaintenanceOverrideSchema: GenMessage<MaintenanceOverride> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_detail, 16);

/**
 * EnvironmentDetailService declares the existing full native Environment read.
 * This contract is not registered until its authorized adapter is mounted.
 * It adds no approval, execution, credential or resource-grant authority.
 *
 * @generated from service conduit.environments.v1.EnvironmentDetailService
 */
export const EnvironmentDetailService: GenService<{
  /**
   * GetEnvironment corresponds to GET /v1/environments/{environmentID}.
   * Preserve original-carrier resource authority before Space membership reads.
   *
   * @generated from rpc conduit.environments.v1.EnvironmentDetailService.GetEnvironment
   */
  getEnvironment: {
    methodKind: "unary";
    input: typeof GetEnvironmentRequestSchema;
    output: typeof GetEnvironmentResponseSchema;
  };
}> =
  /*@__PURE__*/
  serviceDesc(file_conduit_environments_v1_environment_detail, 0);

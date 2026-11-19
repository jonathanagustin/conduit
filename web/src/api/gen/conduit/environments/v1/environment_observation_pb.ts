// Regenerated from proto/conduit. DO NOT EDIT.
// @generated from file conduit/environments/v1/environment_observation.proto (package conduit.environments.v1, syntax proto3)
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
 * Describes the file conduit/environments/v1/environment_observation.proto.
 */
export const file_conduit_environments_v1_environment_observation: GenFile =
  /*@__PURE__*/
  fileDesc(
    "CjVjb25kdWl0L2Vudmlyb25tZW50cy92MS9lbnZpcm9ubWVudF9vYnNlcnZhdGlvbi5wcm90bxIXY29uZHVpdC5lbnZpcm9ubWVudHMudjEiOQoiR2V0RW52aXJvbm1lbnRSZXBvcnRlZFN0YXRlUmVxdWVzdBITCgtlbnZpcm9ubWVudBgBIAEoCSKTAgoYUmVwb3J0ZWRFbnZpcm9ubWVudFN0YXRlEhMKC2Vudmlyb25tZW50GAEgASgJEhAKCGFnZW50X2lkGAIgASgJEg8KB3BsYW5faWQYAyABKAkSLwoLb2JzZXJ2ZWRfYXQYBCABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wEjkKCGVudGl0aWVzGAUgAygLMicuY29uZHVpdC5lbnZpcm9ubWVudHMudjEuUmVwb3J0ZWRFbnRpdHkSQQoHc2VjcmV0cxgGIAMoCzIwLmNvbmR1aXQuZW52aXJvbm1lbnRzLnYxLlJlcG9ydGVkU2VjcmV0UmVmZXJlbmNlEhAKCHJldmlzaW9uGAcgASgEItYCCg5SZXBvcnRlZEVudGl0eRIXCg9pbnN0YWxsYXRpb25faWQYASABKAkSDwoHcHJvZHVjdBgCIAEoCRIPCgd2ZXJzaW9uGAMgASgJEhYKDnJlbGVhc2VfZGlnZXN0GAQgASgJEhEKCW5hbWVzcGFjZRgFIAEoCRIVCg1jb25maWdfZGlnZXN0GAYgASgJEg4KBmhlYWx0aBgHIAEoCRIWCg5yb2xsb3V0X3N0YXR1cxgIIAEoCRIVCg1jb25maWdfc3RhdHVzGAkgASgJEhEKCWxpZmVjeWNsZRgKIAEoCRIvCgtvYnNlcnZlZF9hdBgLIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXASRAoOaGVhbHRoX2RldGFpbHMYDCABKAsyLC5jb25kdWl0LmVudmlyb25tZW50cy52MS5FbnRpdHlIZWFsdGhEZXRhaWxzIl8KE0VudGl0eUhlYWx0aERldGFpbHMSDgoGcXVvcnVtGAEgASgJEjgKBW5vZGVzGAIgAygLMikuY29uZHVpdC5lbnZpcm9ubWVudHMudjEuRW50aXR5Tm9kZUhlYWx0aCKQAQoQRW50aXR5Tm9kZUhlYWx0aBIPCgdub2RlX2lkGAEgASgJEhcKD3Byb2R1Y3RfdmVyc2lvbhgCIAEoCRIOCgZyZXN1bHQYAyABKAkSQgoKY29udGFpbmVycxgEIAMoCzIuLmNvbmR1aXQuZW52aXJvbm1lbnRzLnYxLkVudGl0eUNvbnRhaW5lckhlYWx0aCKYAQoVRW50aXR5Q29udGFpbmVySGVhbHRoEhQKDGNvbnRhaW5lcl9pZBgBIAEoCRIQCghsaXZlbmVzcxgCIAEoCBIRCglyZWFkaW5lc3MYAyABKAgSDgoGcmVzdWx0GAQgASgJEjQKBmNoZWNrcxgFIAMoCzIkLmNvbmR1aXQuZW52aXJvbm1lbnRzLnYxLkhlYWx0aENoZWNrIk8KC0hlYWx0aENoZWNrEgoKAmlkGAEgASgJEg8KB21lc3NhZ2UYAiABKAkSDgoGcmVzdWx0GAMgASgJEhMKC3BhcmFtc19qc29uGAQgASgMIpYBChdSZXBvcnRlZFNlY3JldFJlZmVyZW5jZRISCgphc3NpZ25tZW50GAEgASgJEhEKCW5hbWVzcGFjZRgCIAEoCRIQCghyZXZpc2lvbhgDIAEoAxIUCgxwcm92aWRlcl9yZWYYBCABKAkSHgoWa3ViZXJuZXRlc19zZWNyZXRfbmFtZRgFIAEoCRIMCgRrZXlzGAYgAygJImoKI0dldEVudmlyb25tZW50UmVwb3J0ZWRTdGF0ZVJlc3BvbnNlEkMKCHJlcG9ydGVkGAEgASgLMjEuY29uZHVpdC5lbnZpcm9ubWVudHMudjEuUmVwb3J0ZWRFbnZpcm9ubWVudFN0YXRlMr8BCh1FbnZpcm9ubWVudE9ic2VydmF0aW9uU2VydmljZRKdAQobR2V0RW52aXJvbm1lbnRSZXBvcnRlZFN0YXRlEjsuY29uZHVpdC5lbnZpcm9ubWVudHMudjEuR2V0RW52aXJvbm1lbnRSZXBvcnRlZFN0YXRlUmVxdWVzdBo8LmNvbmR1aXQuZW52aXJvbm1lbnRzLnYxLkdldEVudmlyb25tZW50UmVwb3J0ZWRTdGF0ZVJlc3BvbnNlIgOQAgFCUlpQZ2l0aHViLmNvbS9qb25hdGhhbmFndXN0aW4vY29uZHVpdC9nZW4vZ28vY29uZHVpdC9lbnZpcm9ubWVudHMvdjE7ZW52aXJvbm1lbnRzdjFiBnByb3RvMw",
    [file_google_protobuf_timestamp],
  );

/**
 * @generated from message conduit.environments.v1.GetEnvironmentReportedStateRequest
 */
export type GetEnvironmentReportedStateRequest =
  Message<"conduit.environments.v1.GetEnvironmentReportedStateRequest"> & {
    /**
     * @generated from field: string environment = 1;
     */
    environment: string;
  };

/**
 * Describes the message conduit.environments.v1.GetEnvironmentReportedStateRequest.
 * Use `create(GetEnvironmentReportedStateRequestSchema)` to create a new message.
 */
export const GetEnvironmentReportedStateRequestSchema: GenMessage<GetEnvironmentReportedStateRequest> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_observation, 0);

/**
 * An absent observation timestamp represents native zero/unknown time; no
 * current timestamp is invented. Open status strings retain native vocabulary.
 *
 * @generated from message conduit.environments.v1.ReportedEnvironmentState
 */
export type ReportedEnvironmentState =
  Message<"conduit.environments.v1.ReportedEnvironmentState"> & {
    /**
     * @generated from field: string environment = 1;
     */
    environment: string;

    /**
     * @generated from field: string agent_id = 2;
     */
    agentId: string;

    /**
     * @generated from field: string plan_id = 3;
     */
    planId: string;

    /**
     * @generated from field: google.protobuf.Timestamp observed_at = 4;
     */
    observedAt?: Timestamp | undefined;

    /**
     * @generated from field: repeated conduit.environments.v1.ReportedEntity entities = 5;
     */
    entities: ReportedEntity[];

    /**
     * @generated from field: repeated conduit.environments.v1.ReportedSecretReference secrets = 6;
     */
    secrets: ReportedSecretReference[];

    /**
     * @generated from field: uint64 revision = 7;
     */
    revision: bigint;
  };

/**
 * Describes the message conduit.environments.v1.ReportedEnvironmentState.
 * Use `create(ReportedEnvironmentStateSchema)` to create a new message.
 */
export const ReportedEnvironmentStateSchema: GenMessage<ReportedEnvironmentState> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_observation, 1);

/**
 * @generated from message conduit.environments.v1.ReportedEntity
 */
export type ReportedEntity =
  Message<"conduit.environments.v1.ReportedEntity"> & {
    /**
     * @generated from field: string installation_id = 1;
     */
    installationId: string;

    /**
     * @generated from field: string product = 2;
     */
    product: string;

    /**
     * @generated from field: string version = 3;
     */
    version: string;

    /**
     * @generated from field: string release_digest = 4;
     */
    releaseDigest: string;

    /**
     * @generated from field: string namespace = 5;
     */
    namespace: string;

    /**
     * @generated from field: string config_digest = 6;
     */
    configDigest: string;

    /**
     * @generated from field: string health = 7;
     */
    health: string;

    /**
     * @generated from field: string rollout_status = 8;
     */
    rolloutStatus: string;

    /**
     * @generated from field: string config_status = 9;
     */
    configStatus: string;

    /**
     * @generated from field: string lifecycle = 10;
     */
    lifecycle: string;

    /**
     * @generated from field: google.protobuf.Timestamp observed_at = 11;
     */
    observedAt?: Timestamp | undefined;

    /**
     * Presence distinguishes no health detail from an observed empty detail.
     *
     * @generated from field: conduit.environments.v1.EntityHealthDetails health_details = 12;
     */
    healthDetails?: EntityHealthDetails | undefined;
  };

/**
 * Describes the message conduit.environments.v1.ReportedEntity.
 * Use `create(ReportedEntitySchema)` to create a new message.
 */
export const ReportedEntitySchema: GenMessage<ReportedEntity> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_observation, 2);

/**
 * @generated from message conduit.environments.v1.EntityHealthDetails
 */
export type EntityHealthDetails =
  Message<"conduit.environments.v1.EntityHealthDetails"> & {
    /**
     * @generated from field: string quorum = 1;
     */
    quorum: string;

    /**
     * @generated from field: repeated conduit.environments.v1.EntityNodeHealth nodes = 2;
     */
    nodes: EntityNodeHealth[];
  };

/**
 * Describes the message conduit.environments.v1.EntityHealthDetails.
 * Use `create(EntityHealthDetailsSchema)` to create a new message.
 */
export const EntityHealthDetailsSchema: GenMessage<EntityHealthDetails> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_observation, 3);

/**
 * @generated from message conduit.environments.v1.EntityNodeHealth
 */
export type EntityNodeHealth =
  Message<"conduit.environments.v1.EntityNodeHealth"> & {
    /**
     * @generated from field: string node_id = 1;
     */
    nodeId: string;

    /**
     * @generated from field: string product_version = 2;
     */
    productVersion: string;

    /**
     * @generated from field: string result = 3;
     */
    result: string;

    /**
     * @generated from field: repeated conduit.environments.v1.EntityContainerHealth containers = 4;
     */
    containers: EntityContainerHealth[];
  };

/**
 * Describes the message conduit.environments.v1.EntityNodeHealth.
 * Use `create(EntityNodeHealthSchema)` to create a new message.
 */
export const EntityNodeHealthSchema: GenMessage<EntityNodeHealth> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_observation, 4);

/**
 * @generated from message conduit.environments.v1.EntityContainerHealth
 */
export type EntityContainerHealth =
  Message<"conduit.environments.v1.EntityContainerHealth"> & {
    /**
     * @generated from field: string container_id = 1;
     */
    containerId: string;

    /**
     * @generated from field: bool liveness = 2;
     */
    liveness: boolean;

    /**
     * @generated from field: bool readiness = 3;
     */
    readiness: boolean;

    /**
     * @generated from field: string result = 4;
     */
    result: string;

    /**
     * @generated from field: repeated conduit.environments.v1.HealthCheck checks = 5;
     */
    checks: HealthCheck[];
  };

/**
 * Describes the message conduit.environments.v1.EntityContainerHealth.
 * Use `create(EntityContainerHealthSchema)` to create a new message.
 */
export const EntityContainerHealthSchema: GenMessage<EntityContainerHealth> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_observation, 5);

/**
 * @generated from message conduit.environments.v1.HealthCheck
 */
export type HealthCheck = Message<"conduit.environments.v1.HealthCheck"> & {
  /**
   * @generated from field: string id = 1;
   */
  id: string;

  /**
   * @generated from field: string message = 2;
   */
  message: string;

  /**
   * @generated from field: string result = 3;
   */
  result: string;

  /**
   * JSON encoding of the native arbitrary params map, preserving numbers without
   * a protobuf Struct/double conversion. Native nil encodes as JSON null.
   *
   * @generated from field: bytes params_json = 4;
   */
  paramsJson: Uint8Array;
};

/**
 * Describes the message conduit.environments.v1.HealthCheck.
 * Use `create(HealthCheckSchema)` to create a new message.
 */
export const HealthCheckSchema: GenMessage<HealthCheck> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_observation, 6);

/**
 * Metadata only: the native report contains provider references and key names,
 * not secret values. This is still private Environment inventory.
 *
 * @generated from message conduit.environments.v1.ReportedSecretReference
 */
export type ReportedSecretReference =
  Message<"conduit.environments.v1.ReportedSecretReference"> & {
    /**
     * @generated from field: string assignment = 1;
     */
    assignment: string;

    /**
     * @generated from field: string namespace = 2;
     */
    namespace: string;

    /**
     * @generated from field: int64 revision = 3;
     */
    revision: bigint;

    /**
     * @generated from field: string provider_ref = 4;
     */
    providerRef: string;

    /**
     * @generated from field: string kubernetes_secret_name = 5;
     */
    kubernetesSecretName: string;

    /**
     * @generated from field: repeated string keys = 6;
     */
    keys: string[];
  };

/**
 * Describes the message conduit.environments.v1.ReportedSecretReference.
 * Use `create(ReportedSecretReferenceSchema)` to create a new message.
 */
export const ReportedSecretReferenceSchema: GenMessage<ReportedSecretReference> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_observation, 7);

/**
 * @generated from message conduit.environments.v1.GetEnvironmentReportedStateResponse
 */
export type GetEnvironmentReportedStateResponse =
  Message<"conduit.environments.v1.GetEnvironmentReportedStateResponse"> & {
    /**
     * @generated from field: conduit.environments.v1.ReportedEnvironmentState reported = 1;
     */
    reported?: ReportedEnvironmentState | undefined;
  };

/**
 * Describes the message conduit.environments.v1.GetEnvironmentReportedStateResponse.
 * Use `create(GetEnvironmentReportedStateResponseSchema)` to create a new message.
 */
export const GetEnvironmentReportedStateResponseSchema: GenMessage<GetEnvironmentReportedStateResponse> =
  /*@__PURE__*/
  messageDesc(file_conduit_environments_v1_environment_observation, 8);

/**
 * EnvironmentObservationService reads reported evidence, never desired state or
 * execution credentials. REST remains supported; no automatic fallback applies.
 *
 * @generated from service conduit.environments.v1.EnvironmentObservationService
 */
export const EnvironmentObservationService: GenService<{
  /**
   * GetEnvironmentReportedState is GET /v1/environments/{environmentID}/reported-state.
   * It requires ReadResource on that Environment, including secret references.
   *
   * @generated from rpc conduit.environments.v1.EnvironmentObservationService.GetEnvironmentReportedState
   */
  getEnvironmentReportedState: {
    methodKind: "unary";
    input: typeof GetEnvironmentReportedStateRequestSchema;
    output: typeof GetEnvironmentReportedStateResponseSchema;
  };
}> =
  /*@__PURE__*/
  serviceDesc(file_conduit_environments_v1_environment_observation, 0);

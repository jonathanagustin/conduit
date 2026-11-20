// Regenerated from proto/conduit. DO NOT EDIT.
// @generated from file conduit/governance/v1/authorization_audit.proto (package conduit.governance.v1, syntax proto3)
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
 * Describes the file conduit/governance/v1/authorization_audit.proto.
 */
export const file_conduit_governance_v1_authorization_audit: GenFile =
  /*@__PURE__*/
  fileDesc(
    "Ci9jb25kdWl0L2dvdmVybmFuY2UvdjEvYXV0aG9yaXphdGlvbl9hdWRpdC5wcm90bxIVY29uZHVpdC5nb3Zlcm5hbmNlLnYxItMBCiNMaXN0QXV0aG9yaXphdGlvbkF1ZGl0RXZlbnRzUmVxdWVzdBISCgVsaW1pdBgBIAEoBUgAiAEBEg4KBmN1cnNvchgCIAEoCRIUCgxyZXNvdXJjZV9yaWQYAyABKAkSKQoFc2luY2UYBCABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wEikKBXVudGlsGAUgASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcBISCgpkZXNjZW5kaW5nGAYgASgIQggKBl9saW1pdCK7AgoXQXV0aG9yaXphdGlvbkF1ZGl0RXZlbnQSCgoCaWQYASABKAkSFgoOc2NoZW1hX3ZlcnNpb24YAiABKAkSEgoKY2F0ZWdvcmllcxgDIAMoCRIQCgh0cmFjZV9pZBgEIAEoCRIPCgdzdWJqZWN0GAUgASgJEg4KBmFjdGlvbhgGIAEoCRIUCgxyZXNvdXJjZV9yaWQYByABKAkSEAoIZGVjaXNpb24YCCABKAkSDgoGcmVhc29uGAkgASgJEhsKE3JlcXVlc3RfZmllbGRzX2pzb24YCiABKAwSGgoScmVzdWx0X2ZpZWxkc19qc29uGAsgASgMEhQKDGNvbnRleHRfanNvbhgMIAEoDBIuCgpjcmVhdGVkX2F0GA0gASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcCLCAQokTGlzdEF1dGhvcml6YXRpb25BdWRpdEV2ZW50c1Jlc3BvbnNlEj4KBmV2ZW50cxgBIAMoCzIuLmNvbmR1aXQuZ292ZXJuYW5jZS52MS5BdXRob3JpemF0aW9uQXVkaXRFdmVudBINCgV0b3RhbBgCIAEoAxIRCglwYWdlX3NpemUYAyABKAUSDQoFbGltaXQYBCABKAUSEwoLbmV4dF9jdXJzb3IYBSABKAkSFAoMaGFzX3ByZXZpb3VzGAYgASgIItIBCiJMaXN0QXV0aG9yaXphdGlvbkF1ZGl0Q2hhaW5SZXF1ZXN0EhIKBWxpbWl0GAEgASgFSACIAQESDgoGY3Vyc29yGAIgASgJEhQKDHJlc291cmNlX3JpZBgDIAEoCRIpCgVzaW5jZRgEIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXASKQoFdW50aWwYBSABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wEhIKCmRlc2NlbmRpbmcYBiABKAhCCAoGX2xpbWl0IpMCChxBdXRob3JpemF0aW9uQXVkaXRDaGFpbkVudHJ5EhAKCHNlcXVlbmNlGAEgASgEEgoKAmlkGAIgASgJEg0KBWFjdG9yGAMgASgJEg4KBmFjdGlvbhgEIAEoCRIQCghkZWNpc2lvbhgFIAEoCRIXCg9wcmV2aW91c19kaWdlc3QYBiABKAkSDgoGZGlnZXN0GAcgASgJEi4KCmNyZWF0ZWRfYXQYCCABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wEhQKDGNvbnRleHRfanNvbhgJIAEoDBIUCgxyZXNvdXJjZV9yaWQYCiABKAkSDgoGb3JpZ2luGAsgASgJEg8KB3BheWxvYWQYDCABKAwixwEKI0xpc3RBdXRob3JpemF0aW9uQXVkaXRDaGFpblJlc3BvbnNlEkQKB2VudHJpZXMYASADKAsyMy5jb25kdWl0LmdvdmVybmFuY2UudjEuQXV0aG9yaXphdGlvbkF1ZGl0Q2hhaW5FbnRyeRINCgV0b3RhbBgCIAEoAxIRCglwYWdlX3NpemUYAyABKAUSDQoFbGltaXQYBCABKAUSEwoLbmV4dF9jdXJzb3IYBSABKAkSFAoMaGFzX3ByZXZpb3VzGAYgASgIMr4BCh1BdXRob3JpemF0aW9uQXVkaXRSZWFkU2VydmljZRKcAQocTGlzdEF1dGhvcml6YXRpb25BdWRpdEV2ZW50cxI6LmNvbmR1aXQuZ292ZXJuYW5jZS52MS5MaXN0QXV0aG9yaXphdGlvbkF1ZGl0RXZlbnRzUmVxdWVzdBo7LmNvbmR1aXQuZ292ZXJuYW5jZS52MS5MaXN0QXV0aG9yaXphdGlvbkF1ZGl0RXZlbnRzUmVzcG9uc2UiA5ACATLAAQoiQXV0aG9yaXphdGlvbkF1ZGl0Q2hhaW5SZWFkU2VydmljZRKZAQobTGlzdEF1dGhvcml6YXRpb25BdWRpdENoYWluEjkuY29uZHVpdC5nb3Zlcm5hbmNlLnYxLkxpc3RBdXRob3JpemF0aW9uQXVkaXRDaGFpblJlcXVlc3QaOi5jb25kdWl0LmdvdmVybmFuY2UudjEuTGlzdEF1dGhvcml6YXRpb25BdWRpdENoYWluUmVzcG9uc2UiA5ACAUJOWkxnaXRodWIuY29tL2pvbmF0aGFuYWd1c3Rpbi9jb25kdWl0L2dlbi9nby9jb25kdWl0L2dvdmVybmFuY2UvdjE7Z292ZXJuYW5jZXYxYgZwcm90bzM",
    [file_google_protobuf_timestamp],
  );

/**
 * @generated from message conduit.governance.v1.ListAuthorizationAuditEventsRequest
 */
export type ListAuthorizationAuditEventsRequest =
  Message<"conduit.governance.v1.ListAuthorizationAuditEventsRequest"> & {
    /**
     * Absent uses 100; explicit limits must be 1..200.
     *
     * @generated from field: optional int32 limit = 1;
     */
    limit?: number | undefined;

    /**
     * Native opaque continuation bound to resource, time bounds and order.
     *
     * @generated from field: string cursor = 2;
     */
    cursor: string;

    /**
     * @generated from field: string resource_rid = 3;
     */
    resourceRid: string;

    /**
     * Inclusive lower and exclusive upper bound; absent leaves a side open.
     *
     * @generated from field: google.protobuf.Timestamp since = 4;
     */
    since?: Timestamp | undefined;

    /**
     * @generated from field: google.protobuf.Timestamp until = 5;
     */
    until?: Timestamp | undefined;

    /**
     * @generated from field: bool descending = 6;
     */
    descending: boolean;
  };

/**
 * Describes the message conduit.governance.v1.ListAuthorizationAuditEventsRequest.
 * Use `create(ListAuthorizationAuditEventsRequestSchema)` to create a new message.
 */
export const ListAuthorizationAuditEventsRequestSchema: GenMessage<ListAuthorizationAuditEventsRequest> =
  /*@__PURE__*/
  messageDesc(file_conduit_governance_v1_authorization_audit, 0);

/**
 * @generated from message conduit.governance.v1.AuthorizationAuditEvent
 */
export type AuthorizationAuditEvent =
  Message<"conduit.governance.v1.AuthorizationAuditEvent"> & {
    /**
     * @generated from field: string id = 1;
     */
    id: string;

    /**
     * @generated from field: string schema_version = 2;
     */
    schemaVersion: string;

    /**
     * @generated from field: repeated string categories = 3;
     */
    categories: string[];

    /**
     * @generated from field: string trace_id = 4;
     */
    traceId: string;

    /**
     * @generated from field: string subject = 5;
     */
    subject: string;

    /**
     * @generated from field: string action = 6;
     */
    action: string;

    /**
     * @generated from field: string resource_rid = 7;
     */
    resourceRid: string;

    /**
     * @generated from field: string decision = 8;
     */
    decision: string;

    /**
     * @generated from field: string reason = 9;
     */
    reason: string;

    /**
     * Native JSON encoded as UTF-8 bytes, not protobuf Struct: metadata numbers
     * must not be rounded through protobuf's floating-point Value representation.
     *
     * @generated from field: bytes request_fields_json = 10;
     */
    requestFieldsJson: Uint8Array;

    /**
     * @generated from field: bytes result_fields_json = 11;
     */
    resultFieldsJson: Uint8Array;

    /**
     * Empty means the native optional context was absent/empty.
     *
     * @generated from field: bytes context_json = 12;
     */
    contextJson: Uint8Array;

    /**
     * Missing means no recorded timestamp; never filled with the current time.
     *
     * @generated from field: google.protobuf.Timestamp created_at = 13;
     */
    createdAt?: Timestamp | undefined;
  };

/**
 * Describes the message conduit.governance.v1.AuthorizationAuditEvent.
 * Use `create(AuthorizationAuditEventSchema)` to create a new message.
 */
export const AuthorizationAuditEventSchema: GenMessage<AuthorizationAuditEvent> =
  /*@__PURE__*/
  messageDesc(file_conduit_governance_v1_authorization_audit, 1);

/**
 * @generated from message conduit.governance.v1.ListAuthorizationAuditEventsResponse
 */
export type ListAuthorizationAuditEventsResponse =
  Message<"conduit.governance.v1.ListAuthorizationAuditEventsResponse"> & {
    /**
     * @generated from field: repeated conduit.governance.v1.AuthorizationAuditEvent events = 1;
     */
    events: AuthorizationAuditEvent[];

    /**
     * @generated from field: int64 total = 2;
     */
    total: bigint;

    /**
     * @generated from field: int32 page_size = 3;
     */
    pageSize: number;

    /**
     * @generated from field: int32 limit = 4;
     */
    limit: number;

    /**
     * @generated from field: string next_cursor = 5;
     */
    nextCursor: string;

    /**
     * Forward seek only. No previous cursor is issued.
     *
     * @generated from field: bool has_previous = 6;
     */
    hasPrevious: boolean;
  };

/**
 * Describes the message conduit.governance.v1.ListAuthorizationAuditEventsResponse.
 * Use `create(ListAuthorizationAuditEventsResponseSchema)` to create a new message.
 */
export const ListAuthorizationAuditEventsResponseSchema: GenMessage<ListAuthorizationAuditEventsResponse> =
  /*@__PURE__*/
  messageDesc(file_conduit_governance_v1_authorization_audit, 2);

/**
 * @generated from message conduit.governance.v1.ListAuthorizationAuditChainRequest
 */
export type ListAuthorizationAuditChainRequest =
  Message<"conduit.governance.v1.ListAuthorizationAuditChainRequest"> & {
    /**
     * Absent uses 100; explicit limits must be 1..200.
     *
     * @generated from field: optional int32 limit = 1;
     */
    limit?: number | undefined;

    /**
     * Opaque native cursor bound to this projection, resource, time bounds and order.
     *
     * @generated from field: string cursor = 2;
     */
    cursor: string;

    /**
     * @generated from field: string resource_rid = 3;
     */
    resourceRid: string;

    /**
     * @generated from field: google.protobuf.Timestamp since = 4;
     */
    since?: Timestamp | undefined;

    /**
     * @generated from field: google.protobuf.Timestamp until = 5;
     */
    until?: Timestamp | undefined;

    /**
     * @generated from field: bool descending = 6;
     */
    descending: boolean;
  };

/**
 * Describes the message conduit.governance.v1.ListAuthorizationAuditChainRequest.
 * Use `create(ListAuthorizationAuditChainRequestSchema)` to create a new message.
 */
export const ListAuthorizationAuditChainRequestSchema: GenMessage<ListAuthorizationAuditChainRequest> =
  /*@__PURE__*/
  messageDesc(file_conduit_governance_v1_authorization_audit, 3);

/**
 * @generated from message conduit.governance.v1.AuthorizationAuditChainEntry
 */
export type AuthorizationAuditChainEntry =
  Message<"conduit.governance.v1.AuthorizationAuditChainEntry"> & {
    /**
     * @generated from field: uint64 sequence = 1;
     */
    sequence: bigint;

    /**
     * @generated from field: string id = 2;
     */
    id: string;

    /**
     * @generated from field: string actor = 3;
     */
    actor: string;

    /**
     * @generated from field: string action = 4;
     */
    action: string;

    /**
     * @generated from field: string decision = 5;
     */
    decision: string;

    /**
     * @generated from field: string previous_digest = 6;
     */
    previousDigest: string;

    /**
     * @generated from field: string digest = 7;
     */
    digest: string;

    /**
     * @generated from field: google.protobuf.Timestamp created_at = 8;
     */
    createdAt?: Timestamp | undefined;

    /**
     * Optional display context, not a digest input. Exact JSON avoids number loss.
     *
     * @generated from field: bytes context_json = 9;
     */
    contextJson: Uint8Array;

    /**
     * @generated from field: string resource_rid = 10;
     */
    resourceRid: string;

    /**
     * @generated from field: string origin = 11;
     */
    origin: string;

    /**
     * Exact native projection digest material, not a persisted ledger archive.
     *
     * @generated from field: bytes payload = 12;
     */
    payload: Uint8Array;
  };

/**
 * Describes the message conduit.governance.v1.AuthorizationAuditChainEntry.
 * Use `create(AuthorizationAuditChainEntrySchema)` to create a new message.
 */
export const AuthorizationAuditChainEntrySchema: GenMessage<AuthorizationAuditChainEntry> =
  /*@__PURE__*/
  messageDesc(file_conduit_governance_v1_authorization_audit, 4);

/**
 * @generated from message conduit.governance.v1.ListAuthorizationAuditChainResponse
 */
export type ListAuthorizationAuditChainResponse =
  Message<"conduit.governance.v1.ListAuthorizationAuditChainResponse"> & {
    /**
     * @generated from field: repeated conduit.governance.v1.AuthorizationAuditChainEntry entries = 1;
     */
    entries: AuthorizationAuditChainEntry[];

    /**
     * @generated from field: int64 total = 2;
     */
    total: bigint;

    /**
     * @generated from field: int32 page_size = 3;
     */
    pageSize: number;

    /**
     * @generated from field: int32 limit = 4;
     */
    limit: number;

    /**
     * @generated from field: string next_cursor = 5;
     */
    nextCursor: string;

    /**
     * @generated from field: bool has_previous = 6;
     */
    hasPrevious: boolean;
  };

/**
 * Describes the message conduit.governance.v1.ListAuthorizationAuditChainResponse.
 * Use `create(ListAuthorizationAuditChainResponseSchema)` to create a new message.
 */
export const ListAuthorizationAuditChainResponseSchema: GenMessage<ListAuthorizationAuditChainResponse> =
  /*@__PURE__*/
  messageDesc(file_conduit_governance_v1_authorization_audit, 5);

/**
 * AuthorizationAuditReadService serves native administrator-only event reads.
 * It does not grant audit-chain export, mutation, pruning or archive authority.
 *
 * @generated from service conduit.governance.v1.AuthorizationAuditReadService
 */
export const AuthorizationAuditReadService: GenService<{
  /**
   * ListAuthorizationAuditEvents projects the events view of the native audit
   * route. REST remains supported; this method changes no caller transport.
   *
   * @generated from rpc conduit.governance.v1.AuthorizationAuditReadService.ListAuthorizationAuditEvents
   */
  listAuthorizationAuditEvents: {
    methodKind: "unary";
    input: typeof ListAuthorizationAuditEventsRequestSchema;
    output: typeof ListAuthorizationAuditEventsResponseSchema;
  };
}> =
  /*@__PURE__*/
  serviceDesc(file_conduit_governance_v1_authorization_audit, 0);

/**
 * AuthorizationAuditChainReadService serves the native filtered chain projection.
 * This is not the persisted ledger, a trust anchor or archive custody authority.
 * A separate procedure makes older Hubs refuse rather than ignore a view selector.
 *
 * @generated from service conduit.governance.v1.AuthorizationAuditChainReadService
 */
export const AuthorizationAuditChainReadService: GenService<{
  /**
   * @generated from rpc conduit.governance.v1.AuthorizationAuditChainReadService.ListAuthorizationAuditChain
   */
  listAuthorizationAuditChain: {
    methodKind: "unary";
    input: typeof ListAuthorizationAuditChainRequestSchema;
    output: typeof ListAuthorizationAuditChainResponseSchema;
  };
}> =
  /*@__PURE__*/
  serviceDesc(file_conduit_governance_v1_authorization_audit, 1);

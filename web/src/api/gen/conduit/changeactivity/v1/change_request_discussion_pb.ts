// Regenerated from proto/conduit. DO NOT EDIT.
// @generated from file conduit/changeactivity/v1/change_request_discussion.proto (package conduit.changeactivity.v1, syntax proto3)
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
 * Describes the file conduit/changeactivity/v1/change_request_discussion.proto.
 */
export const file_conduit_changeactivity_v1_change_request_discussion: GenFile =
  /*@__PURE__*/
  fileDesc(
    "Cjljb25kdWl0L2NoYW5nZWFjdGl2aXR5L3YxL2NoYW5nZV9yZXF1ZXN0X2Rpc2N1c3Npb24ucHJvdG8SGWNvbmR1aXQuY2hhbmdlYWN0aXZpdHkudjEiTQogTGlzdENoYW5nZVJlcXVlc3RDb21tZW50c1JlcXVlc3QSGQoRY2hhbmdlX3JlcXVlc3RfaWQYASABKAkSDgoGY3Vyc29yGAIgASgJIq4BChRDaGFuZ2VSZXF1ZXN0Q29tbWVudBIKCgJpZBgBIAEoCRIZChFjaGFuZ2VfcmVxdWVzdF9pZBgCIAEoCRIOCgZhdXRob3IYAyABKAkSDQoFYWN0b3IYBCABKAkSLgoKY3JlYXRlZF9hdBgFIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXASDAoEdGV4dBgGIAEoCRISCgp2YWx1ZV90aWVyGAcgASgJIo8BCiFMaXN0Q2hhbmdlUmVxdWVzdENvbW1lbnRzUmVzcG9uc2USQQoIY29tbWVudHMYASADKAsyLy5jb25kdWl0LmNoYW5nZWFjdGl2aXR5LnYxLkNoYW5nZVJlcXVlc3RDb21tZW50EhMKC25leHRfY3Vyc29yGAIgASgJEhIKCnZhbHVlX3RpZXIYAyABKAkywgEKIkNoYW5nZVJlcXVlc3REaXNjdXNzaW9uUmVhZFNlcnZpY2USmwEKGUxpc3RDaGFuZ2VSZXF1ZXN0Q29tbWVudHMSOy5jb25kdWl0LmNoYW5nZWFjdGl2aXR5LnYxLkxpc3RDaGFuZ2VSZXF1ZXN0Q29tbWVudHNSZXF1ZXN0GjwuY29uZHVpdC5jaGFuZ2VhY3Rpdml0eS52MS5MaXN0Q2hhbmdlUmVxdWVzdENvbW1lbnRzUmVzcG9uc2UiA5ACAUJWWlRnaXRodWIuY29tL2pvbmF0aGFuYWd1c3Rpbi9jb25kdWl0L2dlbi9nby9jb25kdWl0L2NoYW5nZWFjdGl2aXR5L3YxO2NoYW5nZWFjdGl2aXR5djFiBnByb3RvMw",
    [file_google_protobuf_timestamp],
  );

/**
 * @generated from message conduit.changeactivity.v1.ListChangeRequestCommentsRequest
 */
export type ListChangeRequestCommentsRequest =
  Message<"conduit.changeactivity.v1.ListChangeRequestCommentsRequest"> & {
    /**
     * @generated from field: string change_request_id = 1;
     */
    changeRequestId: string;

    /**
     * Native opaque cursor for this request; not a stable history snapshot.
     *
     * @generated from field: string cursor = 2;
     */
    cursor: string;
  };

/**
 * Describes the message conduit.changeactivity.v1.ListChangeRequestCommentsRequest.
 * Use `create(ListChangeRequestCommentsRequestSchema)` to create a new message.
 */
export const ListChangeRequestCommentsRequestSchema: GenMessage<ListChangeRequestCommentsRequest> =
  /*@__PURE__*/
  messageDesc(file_conduit_changeactivity_v1_change_request_discussion, 0);

/**
 * @generated from message conduit.changeactivity.v1.ChangeRequestComment
 */
export type ChangeRequestComment =
  Message<"conduit.changeactivity.v1.ChangeRequestComment"> & {
    /**
     * @generated from field: string id = 1;
     */
    id: string;

    /**
     * @generated from field: string change_request_id = 2;
     */
    changeRequestId: string;

    /**
     * @generated from field: string author = 3;
     */
    author: string;

    /**
     * The original carrier, distinct from the represented author.
     *
     * @generated from field: string actor = 4;
     */
    actor: string;

    /**
     * @generated from field: google.protobuf.Timestamp created_at = 5;
     */
    createdAt?: Timestamp | undefined;

    /**
     * @generated from field: string text = 6;
     */
    text: string;

    /**
     * @generated from field: string value_tier = 7;
     */
    valueTier: string;
  };

/**
 * Describes the message conduit.changeactivity.v1.ChangeRequestComment.
 * Use `create(ChangeRequestCommentSchema)` to create a new message.
 */
export const ChangeRequestCommentSchema: GenMessage<ChangeRequestComment> =
  /*@__PURE__*/
  messageDesc(file_conduit_changeactivity_v1_change_request_discussion, 1);

/**
 * @generated from message conduit.changeactivity.v1.ListChangeRequestCommentsResponse
 */
export type ListChangeRequestCommentsResponse =
  Message<"conduit.changeactivity.v1.ListChangeRequestCommentsResponse"> & {
    /**
     * At most eight native comments, ordered by time then ID.
     *
     * @generated from field: repeated conduit.changeactivity.v1.ChangeRequestComment comments = 1;
     */
    comments: ChangeRequestComment[];

    /**
     * @generated from field: string next_cursor = 2;
     */
    nextCursor: string;

    /**
     * @generated from field: string value_tier = 3;
     */
    valueTier: string;
  };

/**
 * Describes the message conduit.changeactivity.v1.ListChangeRequestCommentsResponse.
 * Use `create(ListChangeRequestCommentsResponseSchema)` to create a new message.
 */
export const ListChangeRequestCommentsResponseSchema: GenMessage<ListChangeRequestCommentsResponse> =
  /*@__PURE__*/
  messageDesc(file_conduit_changeactivity_v1_change_request_discussion, 2);

/**
 * ChangeRequestDiscussionReadService reads immutable native discussion records.
 * It adds no append, review, approval, mutation or execution authority.
 *
 * @generated from service conduit.changeactivity.v1.ChangeRequestDiscussionReadService
 */
export const ChangeRequestDiscussionReadService: GenService<{
  /**
   * ListChangeRequestComments preserves the existing eight-comment page and
   * request-bound continuation. Existing callers remain REST; no fallback.
   *
   * @generated from rpc conduit.changeactivity.v1.ChangeRequestDiscussionReadService.ListChangeRequestComments
   */
  listChangeRequestComments: {
    methodKind: "unary";
    input: typeof ListChangeRequestCommentsRequestSchema;
    output: typeof ListChangeRequestCommentsResponseSchema;
  };
}> =
  /*@__PURE__*/
  serviceDesc(file_conduit_changeactivity_v1_change_request_discussion, 0);

// Regenerated from proto/conduit. DO NOT EDIT.
// @generated from file conduit/notifications/v1/notification_event.proto (package conduit.notifications.v1, syntax proto3)
/* eslint-disable */

// Package conduit.notifications.v1 describes resource-authorized native events.

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
 * Describes the file conduit/notifications/v1/notification_event.proto.
 */
export const file_conduit_notifications_v1_notification_event: GenFile =
  /*@__PURE__*/
  fileDesc(
    "CjFjb25kdWl0L25vdGlmaWNhdGlvbnMvdjEvbm90aWZpY2F0aW9uX2V2ZW50LnByb3RvEhhjb25kdWl0Lm5vdGlmaWNhdGlvbnMudjEiUwodTGlzdE5vdGlmaWNhdGlvbkV2ZW50c1JlcXVlc3QSFAoMcmVzb3VyY2VfcmlkGAEgASgJEhIKBWxpbWl0GAIgASgDSACIAQFCCAoGX2xpbWl0IoIBCh5MaXN0Tm90aWZpY2F0aW9uRXZlbnRzUmVzcG9uc2USFAoMcmVzb3VyY2VfcmlkGAEgASgJEg0KBWxpbWl0GAIgASgFEjsKBmV2ZW50cxgDIAMoCzIrLmNvbmR1aXQubm90aWZpY2F0aW9ucy52MS5Ob3RpZmljYXRpb25FdmVudCKpAQoRTm90aWZpY2F0aW9uRXZlbnQSCgoCaWQYASABKAkSDAoEdHlwZRgCIAEoCRIUCgxyZXNvdXJjZV9yaWQYAyABKAkSEwoLZW52aXJvbm1lbnQYBCABKAkSDwoHc3VtbWFyeRgFIAEoCRINCgVhY3RvchgGIAEoCRIvCgtvY2N1cnJlZF9hdBgHIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXAyqAEKE05vdGlmaWNhdGlvblNlcnZpY2USkAEKFkxpc3ROb3RpZmljYXRpb25FdmVudHMSNy5jb25kdWl0Lm5vdGlmaWNhdGlvbnMudjEuTGlzdE5vdGlmaWNhdGlvbkV2ZW50c1JlcXVlc3QaOC5jb25kdWl0Lm5vdGlmaWNhdGlvbnMudjEuTGlzdE5vdGlmaWNhdGlvbkV2ZW50c1Jlc3BvbnNlIgOQAgFCVFpSZ2l0aHViLmNvbS9qb25hdGhhbmFndXN0aW4vY29uZHVpdC9nZW4vZ28vY29uZHVpdC9ub3RpZmljYXRpb25zL3YxO25vdGlmaWNhdGlvbnN2MWIGcHJvdG8z",
    [file_google_protobuf_timestamp],
  );

/**
 * ListNotificationEventsRequest names the existing authorized resource window.
 *
 * @generated from message conduit.notifications.v1.ListNotificationEventsRequest
 */
export type ListNotificationEventsRequest =
  Message<"conduit.notifications.v1.ListNotificationEventsRequest"> & {
    /**
     * resource_rid is the required resourceRid query value, trimmed by the Hub.
     *
     * @generated from field: string resource_rid = 1;
     */
    resourceRid: string;

    /**
     * limit is optional: absent selects the native ceiling; explicit zero is invalid.
     *
     * @generated from field: optional int64 limit = 2;
     */
    limit?: bigint | undefined;
  };

/**
 * Describes the message conduit.notifications.v1.ListNotificationEventsRequest.
 * Use `create(ListNotificationEventsRequestSchema)` to create a new message.
 */
export const ListNotificationEventsRequestSchema: GenMessage<ListNotificationEventsRequest> =
  /*@__PURE__*/
  messageDesc(file_conduit_notifications_v1_notification_event, 0);

/**
 * ListNotificationEventsResponse retains the native event read envelope.
 *
 * @generated from message conduit.notifications.v1.ListNotificationEventsResponse
 */
export type ListNotificationEventsResponse =
  Message<"conduit.notifications.v1.ListNotificationEventsResponse"> & {
    /**
     * resource_rid is the resource actually authorized and read.
     *
     * @generated from field: string resource_rid = 1;
     */
    resourceRid: string;

    /**
     * limit is the bounded limit applied by the native Reader.
     *
     * @generated from field: int32 limit = 2;
     */
    limit: number;

    /**
     * events are the returned ordered window, not a total or a resume stream.
     *
     * @generated from field: repeated conduit.notifications.v1.NotificationEvent events = 3;
     */
    events: NotificationEvent[];
  };

/**
 * Describes the message conduit.notifications.v1.ListNotificationEventsResponse.
 * Use `create(ListNotificationEventsResponseSchema)` to create a new message.
 */
export const ListNotificationEventsResponseSchema: GenMessage<ListNotificationEventsResponse> =
  /*@__PURE__*/
  messageDesc(file_conduit_notifications_v1_notification_event, 1);

/**
 * NotificationEvent is the native notifications/domain.Event projection.
 *
 * @generated from message conduit.notifications.v1.NotificationEvent
 */
export type NotificationEvent =
  Message<"conduit.notifications.v1.NotificationEvent"> & {
    /**
     * id is the stable native event identifier.
     *
     * @generated from field: string id = 1;
     */
    id: string;

    /**
     * type preserves the native open event vocabulary.
     *
     * @generated from field: string type = 2;
     */
    type: string;

    /**
     * resource_rid is the resource the event is about.
     *
     * @generated from field: string resource_rid = 3;
     */
    resourceRid: string;

    /**
     * environment is optional native environment context, not additional authority.
     *
     * @generated from field: string environment = 4;
     */
    environment: string;

    /**
     * summary is the stored human-readable sentence.
     *
     * @generated from field: string summary = 5;
     */
    summary: string;

    /**
     * actor is empty for system-generated events.
     *
     * @generated from field: string actor = 6;
     */
    actor: string;

    /**
     * occurred_at is the native event timestamp.
     *
     * @generated from field: google.protobuf.Timestamp occurred_at = 7;
     */
    occurredAt?: Timestamp | undefined;
  };

/**
 * Describes the message conduit.notifications.v1.NotificationEvent.
 * Use `create(NotificationEventSchema)` to create a new message.
 */
export const NotificationEventSchema: GenMessage<NotificationEvent> =
  /*@__PURE__*/
  messageDesc(file_conduit_notifications_v1_notification_event, 2);

/**
 * NotificationService reads existing events; it cannot subscribe, emit or approve.
 *
 * @generated from service conduit.notifications.v1.NotificationService
 */
export const NotificationService: GenService<{
  /**
   * ListNotificationEvents maps GET /v1/notifications/events for one resource.
   *
   * @generated from rpc conduit.notifications.v1.NotificationService.ListNotificationEvents
   */
  listNotificationEvents: {
    methodKind: "unary";
    input: typeof ListNotificationEventsRequestSchema;
    output: typeof ListNotificationEventsResponseSchema;
  };
}> =
  /*@__PURE__*/
  serviceDesc(file_conduit_notifications_v1_notification_event, 0);

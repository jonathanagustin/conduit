import { create, toJson } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { createClient, type Transport } from "@connectrpc/connect";
import {
  ListNotificationEventsRequestSchema,
  NotificationService,
} from "./gen/conduit/notifications/v1/notification_event_pb.js";
import { hubConnectError } from "./hub-connect.js";
import { HubError } from "./hub-error.js";
export { hubConnectTransport as notificationEventsTransport } from "./hub-connect.js";

export async function fetchNotificationEvents(
  resourceRid: string,
  transport: Transport,
  signal?: AbortSignal,
  limit?: bigint,
) {
  try {
    const response = await createClient(
      NotificationService,
      transport,
    ).listNotificationEvents(
      create(ListNotificationEventsRequestSchema, {
        resourceRid,
        ...(limit === undefined ? {} : { limit }),
      }),
      { signal, timeoutMs: 10000 },
    );
    if (
      response.resourceRid !== resourceRid.trim() ||
      response.events.some(
        (event) => event.resourceRid !== response.resourceRid,
      )
    )
      throw new HubError(
        502,
        "Notification events did not match the selected resource.",
      );
    if (
      response.limit < 1 ||
      response.limit > 100 ||
      response.events.length > response.limit
    )
      throw new HubError(502, "Notification event window is unavailable.");
    return {
      resourceRid: response.resourceRid,
      limit: response.limit,
      events: response.events.map((event) => {
        if (!event.occurredAt)
          throw new HubError(
            502,
            "Notification event timestamp is unavailable.",
          );
        const occurredAt = toJson(TimestampSchema, event.occurredAt);
        if (typeof occurredAt !== "string")
          throw new HubError(502, "Notification event timestamp is invalid.");
        return {
          id: event.id,
          type: event.type,
          resourceRid: event.resourceRid,
          environment: event.environment,
          summary: event.summary,
          actor: event.actor,
          occurredAt,
        };
      }),
    };
  } catch (error) {
    if (error instanceof HubError) throw error;
    // Native notification limit errors are HTTP 400; other route families use 422.
    const failure = hubConnectError(error, 400);
    if (failure.status === 501)
      throw new HubError(404, "Notification event read is unavailable.");
    throw failure;
  }
}

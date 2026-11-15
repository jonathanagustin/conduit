import { envelope as validate } from "./live-event-validator.generated.js";

export type LiveArea =
  | "overview"
  | "kpi"
  | "packaging"
  | "plans"
  | "approvals"
  | "fleet"
  | "assistant"
  | "audit";
export type LiveKind =
  | "packaging.step"
  | "plan.changed"
  | "approval.changed"
  | "spoke.reported"
  | "assistant.activity"
  | "index.invalidated";
export type LiveEnvelope = Readonly<{
  version: 1;
  streamID: string;
  sequence: number;
  cursor: string;
  occurredAt: string;
  kind: LiveKind;
  resource: Readonly<{
    type: "packaging-run" | "plan" | "environment" | "assistant-run" | "index";
    id: string;
    revision: number;
  }>;
  data: Readonly<{
    areas?: readonly LiveArea[];
    phase?: string;
    outcome?: string;
    status?: string;
    state?: string;
    activity?: string;
  }>;
}>;
export type LiveWatermark = Readonly<{
  streamID: string;
  sequence: number;
  cursor: string;
}>;
export class LiveProtocolError extends Error {}
export class LiveResetError extends Error {}
const encoder = new TextEncoder();
const streamPattern = /^[A-Za-z0-9][A-Za-z0-9._:/-]{0,199}$/;
const cursorPattern = /^[A-Za-z0-9_-]{1,512}$/;

function timestamp(value: string): boolean {
  const match =
    /^(\d{4})-(\d{2})-(\d{2})[tT](?:[01]\d|2[0-3]):[0-5]\d:(?:[0-5]\d|60)(?:\.\d+)?(?:[zZ]|[+-](?:[01]\d|2[0-3]):[0-5]\d)$/.exec(
      value,
    );
  if (!match) return false;
  const year = Number(match[1]),
    month = Number(match[2]),
    day = Number(match[3]);
  const days = [
    31,
    year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0) ? 29 : 28,
    31,
    30,
    31,
    30,
    31,
    31,
    30,
    31,
    30,
    31,
  ];
  return month >= 1 && month <= 12 && day >= 1 && day <= (days[month - 1] ?? 0);
}
export function parseLiveEnvelope(json: string): LiveEnvelope {
  if (encoder.encode(json).byteLength > 4096)
    throw new LiveProtocolError("Live envelope exceeds its bound");
  let value: unknown;
  try {
    value = JSON.parse(json);
  } catch {
    throw new LiveProtocolError("Invalid live event JSON");
  }
  if (!validate(value))
    throw new LiveProtocolError("Unsupported live event contract");
  const event = value as LiveEnvelope;
  if (!timestamp(event.occurredAt))
    throw new LiveProtocolError("Invalid live event timestamp");
  if (event.data.areas) Object.freeze(event.data.areas);
  Object.freeze(event.data);
  Object.freeze(event.resource);
  return Object.freeze(event);
}
export function liveAreas(event: LiveEnvelope): readonly LiveArea[] {
  switch (event.kind) {
    case "index.invalidated":
      return event.data.areas ?? [];
    case "packaging.step":
      return ["packaging", "overview", "kpi"];
    case "plan.changed":
      return ["plans", "overview", "kpi"];
    case "approval.changed":
      return ["approvals", "plans", "overview", "kpi"];
    case "spoke.reported":
      return ["fleet", "overview", "kpi"];
    case "assistant.activity":
      return ["assistant"];
  }
}
export function liveWatermark(
  response: Response,
  previous: LiveWatermark | null,
): LiveWatermark {
  if (
    response.headers
      .get("content-type")
      ?.split(";", 1)[0]
      ?.trim()
      .toLowerCase() !== "text/event-stream" ||
    !response.body
  )
    throw new LiveProtocolError("Live response is not a stream");
  if (
    !/(?:^|,)\s*no-store\s*(?:,|$)/i.test(
      response.headers.get("cache-control") ?? "",
    )
  )
    throw new LiveProtocolError("Live response is cacheable");
  const streamID = response.headers.get("x-conduit-stream-id") ?? "",
    cursor = response.headers.get("x-conduit-event-cursor") ?? "",
    raw = response.headers.get("x-conduit-event-sequence") ?? "";
  const sequence = Number(raw);
  if (
    !streamPattern.test(streamID) ||
    !cursorPattern.test(cursor) ||
    !/^(0|[1-9]\d*)$/.test(raw) ||
    !Number.isSafeInteger(sequence)
  )
    throw new LiveProtocolError("Invalid live ready watermark");
  if (
    previous
      ? streamID !== previous.streamID ||
        sequence !== previous.sequence ||
        cursor !== previous.cursor
      : sequence !== 0
  )
    throw new LiveResetError("Live ready watermark changed");
  return Object.freeze({ streamID, sequence, cursor });
}

// Incremental UTF-8/SSE framing. It never accumulates an unbounded line/frame
// or interprets a heartbeat, retry field, or partial EOF as a resource event.
export class LiveSSEDecoder {
  private decoder = new TextDecoder("utf-8", { fatal: true });
  private line = "";
  private afterCR = false;
  private bytes = 0;
  private id: string | null = null;
  private kind: string | null = null;
  private data: string[] = [];
  push(
    chunk: Uint8Array,
    emit: (event: LiveEnvelope, bytes: number) => void,
  ): void {
    for (let start = 0; start < chunk.byteLength; start += 4096) {
      let text: string;
      try {
        text = this.decoder.decode(chunk.subarray(start, start + 4096), {
          stream: true,
        });
      } catch {
        throw new LiveProtocolError("Invalid live UTF-8");
      }
      for (const char of text) {
        if (this.afterCR && char === "\n") {
          this.afterCR = false;
          continue;
        }
        this.afterCR = false;
        if (char === "\r" || char === "\n") {
          this.consume(this.line, emit);
          this.line = "";
          this.afterCR = char === "\r";
        } else {
          this.line += char;
          if (this.line.length > 8192)
            throw new LiveProtocolError("Live line exceeds its bound");
        }
      }
    }
  }
  finish(): void {
    try {
      this.decoder.decode();
    } catch {
      throw new LiveProtocolError("Incomplete live UTF-8");
    }
  }
  private consume(
    line: string,
    emit: (event: LiveEnvelope, bytes: number) => void,
  ): void {
    this.bytes += encoder.encode(line).byteLength + 1;
    if (this.bytes > 8192)
      throw new LiveProtocolError("Live frame exceeds its bound");
    if (line === "") {
      if (this.id !== null || this.kind !== null || this.data.length) {
        if (this.id === null || this.kind === null || !this.data.length)
          throw new LiveProtocolError("Incomplete live framing");
        const body = this.data.join("\n"),
          event = parseLiveEnvelope(body);
        if (this.id !== event.cursor || this.kind !== event.kind)
          throw new LiveProtocolError("Live framing disagrees with envelope");
        emit(
          event,
          encoder.encode(body).byteLength +
            event.cursor.length +
            event.kind.length +
            21,
        );
      }
      this.id = null;
      this.kind = null;
      this.data = [];
      this.bytes = 0;
      return;
    }
    if (line.startsWith(":")) return;
    const colon = line.indexOf(":"),
      field = colon < 0 ? line : line.slice(0, colon);
    let value = colon < 0 ? "" : line.slice(colon + 1);
    if (value.startsWith(" ")) value = value.slice(1);
    if (field === "id") {
      if (this.id !== null) throw new LiveProtocolError("Duplicate live id");
      this.id = value;
    } else if (field === "event") {
      if (this.kind !== null)
        throw new LiveProtocolError("Duplicate live kind");
      this.kind = value;
    } else if (field === "data") {
      this.data.push(value);
    }
  }
}

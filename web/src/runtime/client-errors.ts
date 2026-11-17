/**
 * Client error containment: one structured console record per uncaught
 * failure.
 *
 * Console logging only. The runtime bootstrap deliberately carries no API
 * origin or telemetry field (docs/ui/contracts/runtime-bootstrap.md), so there
 * is no reporting endpoint and none is invented here. A record holds the
 * error's name, message and stack and nothing else from the page: no tenant
 * document, no Hub response, no operator input.
 */
export const CLIENT_ERROR_EVENT = "conduit.console.client_error" as const;

export type ClientErrorKind =
  | "error"
  | "unhandledrejection"
  | "render"
  | "react";

export interface ClientErrorRecord {
  event: typeof CLIENT_ERROR_EVENT;
  kind: ClientErrorKind;
  /** The content-hashed entry asset, or "unknown". The bootstrap has no build id. */
  build: string;
  /** The workspace screen id, for a contained render failure. */
  screen?: string;
  name: string;
  message: string;
  stack: string;
}

const MESSAGE_LIMIT = 500;
const STACK_LIMIT = 4000;

const clip = (value: unknown, limit: number): string =>
  typeof value === "string" ? value.slice(0, limit) : "";

/**
 * A thrown or rejected value that is not an Error can be anything, including
 * a Hub response body. Only its type is recorded.
 */
export function clientErrorRecord(
  kind: ClientErrorKind,
  reason: unknown,
  context: { build?: string; screen?: string } = {},
): ClientErrorRecord {
  const error = reason instanceof Error ? reason : null;
  return {
    event: CLIENT_ERROR_EVENT,
    kind,
    build: context.build || "unknown",
    ...(context.screen ? { screen: context.screen } : {}),
    name: error ? clip(error.name, 80) || "Error" : "NonError",
    message: error
      ? clip(error.message, MESSAGE_LIMIT)
      : `non-Error value of type ${reason === null ? "null" : typeof reason}`,
    stack: error ? clip(error.stack, STACK_LIMIT) : "",
  };
}

export interface BuildShell {
  querySelector(
    selectors: string,
  ): { getAttribute(name: string): string | null } | null;
}

/**
 * The build this page is running, read from the entry module the shell names.
 * The bundle is content-addressed, so that file name identifies the build
 * without adding a field to the bootstrap.
 */
export function buildId(shell: BuildShell | undefined): string {
  const source = shell
    ?.querySelector('script[type="module"][src]')
    ?.getAttribute("src");
  if (!source) return "unknown";
  const name = source.split(/[?#]/)[0]!.split("/").pop() || "";
  return /^[\w.-]{1,120}$/.test(name) ? name : "unknown";
}

export interface ErrorEventTarget {
  addEventListener(type: string, listener: (event: any) => void): void;
  removeEventListener(type: string, listener: (event: any) => void): void;
}

/** Log one record per uncaught error and unhandled rejection. Returns the remover. */
export function installClientErrorHandlers(
  target: ErrorEventTarget,
  log: (record: ClientErrorRecord) => void,
  build: string,
): () => void {
  const onError = (event: { error?: unknown; message?: unknown }) => {
    // A resource load failure has no error object; there is nothing to record
    // beyond the fact, and the message is not copied from the page.
    log(clientErrorRecord("error", event?.error, { build }));
  };
  const onRejection = (event: { reason?: unknown }) => {
    log(clientErrorRecord("unhandledrejection", event?.reason, { build }));
  };
  target.addEventListener("error", onError);
  target.addEventListener("unhandledrejection", onRejection);
  return () => {
    target.removeEventListener("error", onError);
    target.removeEventListener("unhandledrejection", onRejection);
  };
}

/** The console sink shared by the global handlers, the engine and React. */
export function logClientError(
  kind: ClientErrorKind,
  reason: unknown,
  screen?: string,
): void {
  const shell = typeof document === "undefined" ? undefined : document;
  console.error(
    clientErrorRecord(kind, reason, { build: buildId(shell), screen }),
  );
}

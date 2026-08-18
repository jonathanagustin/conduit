import { ApiError } from "./errors.js";

/** The Hub serves the console and the API from one origin, so requests are
 * relative and credentials ride along. Identity is established by the
 * deployment -- an operator identity resolver, or client certificates -- and not
 * by anything this client sends, which is why there is no token handling here. */
const BASE = "/v1";

export interface RequestOptions {
  signal?: AbortSignal;
  query?: Record<string, string | number | undefined>;
}

function url(path: string, query?: RequestOptions["query"]): string {
  const target = new URL(`${BASE}${path}`, window.location.origin);
  for (const [key, value] of Object.entries(query ?? {})) {
    if (value !== undefined && value !== "") {
      target.searchParams.set(key, String(value));
    }
  }
  return `${target.pathname}${target.search}`;
}

async function failure(response: Response, requested: string): Promise<ApiError> {
  // The Hub reports errors as {"error": "..."}; anything else is a proxy or a
  // gateway, whose body is not worth surfacing verbatim to an operator.
  let message = `${response.status} ${response.statusText}`;
  try {
    const body: unknown = await response.json();
    if (typeof body === "object" && body !== null && "error" in body) {
      const detail = (body as { error: unknown }).error;
      if (typeof detail === "string" && detail !== "") {
        message = detail;
      }
    }
  } catch {
    // Keep the status line.
  }
  return new ApiError(response.status, requested, message);
}

export async function getJSON<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const requested = url(path, options.query);
  const response = await fetch(requested, {
    method: "GET",
    credentials: "same-origin",
    headers: { Accept: "application/json" },
    ...(options.signal ? { signal: options.signal } : {}),
  });
  if (!response.ok) {
    throw await failure(response, requested);
  }
  return (await response.json()) as T;
}

export async function postJSON<T>(
  path: string,
  body: unknown,
  options: RequestOptions = {},
): Promise<T> {
  const requested = url(path, options.query);
  const response = await fetch(requested, {
    method: "POST",
    credentials: "same-origin",
    headers: { Accept: "application/json", "Content-Type": "application/json" },
    // requireEmptyBody (http.go) rejects any byte, including the "{}" that
    // JSON.stringify(undefined) does not produce but JSON.stringify({}) does.
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    ...(options.signal ? { signal: options.signal } : {}),
  });
  if (!response.ok) {
    throw await failure(response, requested);
  }
  return (await response.json()) as T;
}

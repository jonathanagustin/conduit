export type Transport = (
  path: string,
  init?: { method?: string; body?: unknown },
) => Promise<unknown>;

/** Raised for any non-2xx response from the Hub. */
export class OntologyError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "OntologyError";
    this.status = status;
  }
}

function errorMessage(body: unknown): string | undefined {
  if (typeof body !== "object" || body === null) return undefined;
  const fields = body as Record<string, unknown>;
  for (const name of ["message", "error"]) {
    const value = fields[name];
    if (typeof value === "string" && value !== "") return value;
  }
  return undefined;
}

/** Sends requests with the Hub session cookie. Request bodies are never logged. */
export function fetchTransport(baseUrl = ""): Transport {
  return async (path, init = {}) => {
    const headers: Record<string, string> = { accept: "application/json" };
    let body: string | undefined;
    if (init.body !== undefined) {
      headers["content-type"] = "application/json";
      body = JSON.stringify(init.body);
    }

    const response = await fetch(baseUrl + path, {
      method: init.method ?? (body === undefined ? "GET" : "POST"),
      credentials: "same-origin",
      headers,
      body,
    });

    const text = await response.text();
    let parsed: unknown;
    if (text !== "") {
      try {
        parsed = JSON.parse(text);
      } catch {
        parsed = undefined;
      }
    }

    if (!response.ok) {
      throw new OntologyError(
        response.status,
        errorMessage(parsed) ?? response.statusText,
      );
    }
    return parsed;
  };
}

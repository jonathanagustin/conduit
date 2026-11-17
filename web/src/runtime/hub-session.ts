/**
 * The console's side of the Hub session.
 *
 * The Hub signs a person in and hands the browser one HttpOnly cookie, which
 * this code can neither read nor needs to. Three things are left for the page:
 *
 *  - Every same-origin request that changes state carries `X-Conduit-CSRF: 1`.
 *    The Hub accepts a session cookie on a write only with that header and a
 *    same-origin `Origin`; a cross-site form cannot set one. It is added in one
 *    place, around `fetch`, rather than at each call site, because the console
 *    writes through hand-written helpers, a generated client and Connect
 *    transports, and a header that has to be remembered is one that is
 *    eventually forgotten. A request to any other origin is never touched.
 *  - Sign-in and sign-out are navigations to the Hub's own routes. No token,
 *    code or secret passes through this file.
 *  - A 401 from the Hub while sign-in is available means the session ended. The
 *    person is offered "Sign in again", and the address they return to is read
 *    when they click, so the view they are looking at is the one they get back.
 *
 * Nothing here applies to a Hub without sign-in (the header-identity
 * simulation, or a deployment behind mTLS): no prompt is shown, and the header
 * is harmless to a Hub that does not ask for it.
 */
export const CSRF_HEADER = "X-Conduit-CSRF" as const;
export const CSRF_VALUE = "1" as const;
export const SIGN_IN_PATH = "/v1/auth/login" as const;
export const SIGN_OUT_PATH = "/v1/auth/logout" as const;
export const IDENTITY_PATH = "/v1/operator-identity" as const;
export const CONSOLE_PATH = "/console/" as const;

const SAFE_METHODS = new Set(["GET", "HEAD", "OPTIONS"]);

type FetchInput = Parameters<typeof fetch>[0];
type FetchInit = Parameters<typeof fetch>[1];

function requestMethod(input: FetchInput, init: FetchInit): string {
  const fromInit = init?.method;
  const fromRequest =
    typeof input === "object" && "method" in input ? input.method : undefined;
  return (fromInit ?? fromRequest ?? "GET").toUpperCase();
}

function requestURL(input: FetchInput, base: string): URL | null {
  const raw =
    typeof input === "string"
      ? input
      : input instanceof URL
        ? input.href
        : input.url;
  try {
    return new URL(raw, base);
  } catch {
    return null;
  }
}

/** Whether a request is this origin's and changes state. */
export function needsCsrfHeader(
  input: FetchInput,
  init: FetchInit,
  pageURL: string,
): boolean {
  const url = requestURL(input, pageURL);
  if (url === null || url.origin !== new URL(pageURL).origin) return false;
  return !SAFE_METHODS.has(requestMethod(input, init));
}

/**
 * The address that starts a sign-in and returns to the current view.
 *
 * Only a path beneath /console/ is offered as the return address; anything
 * else returns to the console's front page. The Hub validates the value again
 * and is the one that matters -- this only avoids asking for something it will
 * refuse.
 */
export function signInAddress(location: {
  pathname: string;
  search: string;
  hash: string;
}): string {
  const current = `${location.pathname}${location.search}${location.hash}`;
  const returnTo = location.pathname.startsWith(CONSOLE_PATH)
    ? current
    : CONSOLE_PATH;
  return `${SIGN_IN_PATH}?returnTo=${encodeURIComponent(returnTo)}`;
}

export interface SignInAvailability {
  /** The Hub runs its own sign-in routes. */
  available: boolean;
}

/** Reads whether the Hub published its sign-in routes. Any failure is "no". */
export function signInAvailability(document: unknown): SignInAvailability {
  if (typeof document !== "object" || document === null)
    return { available: false };
  const record = document as { enabled?: unknown; signIn?: unknown };
  const signIn = record.signIn as
    | { loginPath?: unknown; logoutPath?: unknown }
    | null
    | undefined;
  return {
    available:
      record.enabled === true &&
      typeof signIn === "object" &&
      signIn !== null &&
      signIn.loginPath === SIGN_IN_PATH &&
      signIn.logoutPath === SIGN_OUT_PATH,
  };
}

/**
 * Where to send the browser after the Hub has ended the session.
 *
 * The identity provider's end-session address is followed only when it is an
 * absolute https URL; otherwise the console is reloaded, signed out.
 */
export function afterSignOutAddress(body: unknown): string {
  const candidate =
    typeof body === "object" && body !== null
      ? (body as { endSessionUrl?: unknown }).endSessionUrl
      : undefined;
  if (typeof candidate === "string") {
    try {
      if (new URL(candidate).protocol === "https:") return candidate;
    } catch {
      // Not a URL. Fall through to the console.
    }
  }
  return CONSOLE_PATH;
}

/** Whether a Hub response means "you are not signed in". */
export function endsSession(url: URL | null, status: number): boolean {
  if (status !== 401 || url === null) return false;
  // The sign-in routes themselves never mean the session ended.
  return !url.pathname.startsWith("/v1/auth/");
}

export interface SessionHost {
  fetch: typeof fetch;
  location: {
    href: string;
    pathname: string;
    search: string;
    hash: string;
    assign(url: string): void;
  };
  document: Document;
}

const PROMPT_ID = "session-ended-prompt";
const SIGN_OUT_ID = "operator-sign-out";
const INSTALLED = Symbol.for("conduit.console.hub-session");

/**
 * Installs the CSRF header and the session-ended prompt on a window.
 *
 * Idempotent: a second call returns the first installation.
 */
export function installHubSession(host: SessionHost): {
  signIn(): void;
  signOut(): Promise<void>;
} {
  const marked = host as unknown as Record<symbol, unknown>;
  const existing = marked[INSTALLED] as
    | { signIn(): void; signOut(): Promise<void> }
    | undefined;
  if (existing !== undefined) return existing;

  const inner = host.fetch.bind(host);
  let availability: Promise<SignInAvailability> | null = null;
  const readAvailability = (): Promise<SignInAvailability> => {
    availability ??= inner(IDENTITY_PATH, {
      credentials: "same-origin",
      headers: { accept: "application/json" },
    })
      .then((response) => (response.ok ? response.json() : null))
      .then(signInAvailability)
      .catch(() => ({ available: false }));
    return availability;
  };

  const signIn = () => host.location.assign(signInAddress(host.location));

  const showPrompt = () => {
    const document = host.document;
    if (document.getElementById(PROMPT_ID) !== null) return;
    const prompt = document.createElement("div");
    prompt.id = PROMPT_ID;
    prompt.setAttribute("role", "alertdialog");
    prompt.setAttribute("aria-live", "assertive");
    prompt.setAttribute("aria-label", "Your session has ended");
    prompt.setAttribute(
      "style",
      "position:fixed;left:50%;top:44px;transform:translateX(-50%);z-index:2147483000;" +
        "display:flex;align-items:center;gap:12px;max-width:min(560px,92vw);padding:10px 14px;" +
        "border-radius:6px;border:1px solid #4f391b;background:#2a2012;color:#f0f5fa;" +
        "font:500 12px/1.4 Inter,system-ui,sans-serif;box-shadow:0 8px 24px rgba(0,0,0,.45)",
    );
    const text = document.createElement("span");
    text.textContent =
      "Your session has ended. Sign in again to continue; this view is kept.";
    const again = document.createElement("button");
    again.type = "button";
    again.id = "session-sign-in-again";
    again.className = "so-btn so-btn-blue";
    again.textContent = "Sign in again";
    // The address is read at the click, so it names the view on screen then.
    again.addEventListener("click", signIn);
    prompt.append(text, again);
    document.body.append(prompt);
  };

  const signOut = async () => {
    const response = await host.fetch(SIGN_OUT_PATH, {
      method: "POST",
      credentials: "same-origin",
      headers: { accept: "application/json" },
    });
    if (!response.ok) throw new Error(`sign-out returned ${response.status}`);
    const body: unknown = await response.json().catch(() => null);
    host.location.assign(afterSignOutAddress(body));
  };

  const paintSignOut = (available: boolean) => {
    const document = host.document;
    const chip = document.getElementById("operator-chip");
    let control = document.getElementById(SIGN_OUT_ID);
    if (!available || chip?.parentElement == null) {
      control?.remove();
      return;
    }
    if (control === null) {
      const created = document.createElement("button");
      created.type = "button";
      created.id = SIGN_OUT_ID;
      created.className = "so-btn !h-[26px]";
      created.textContent = "Sign out";
      created.title = "End this Hub session";
      created.addEventListener("click", () => {
        created.disabled = true;
        signOut().catch(() => {
          created.disabled = false;
          created.textContent = "Sign out failed; retry";
        });
      });
      control = created;
    }
    // The legacy shell repaints its header. Re-attach after the chip each time.
    if (chip.nextElementSibling !== control) chip.after(control);
  };

  const wrapped: typeof fetch = async (input, init) => {
    let nextInit = init;
    if (needsCsrfHeader(input, init, host.location.href)) {
      const headers = new Headers(
        init?.headers ??
          (typeof input === "object" && "headers" in input
            ? input.headers
            : undefined),
      );
      headers.set(CSRF_HEADER, CSRF_VALUE);
      nextInit = { ...init, headers };
    }
    const response = await inner(input, nextInit);
    const url = requestURL(input, host.location.href);
    const sameOrigin =
      url !== null && url.origin === new URL(host.location.href).origin;
    if (sameOrigin && endsSession(url, response.status)) {
      void readAvailability().then(({ available }) => {
        if (available) showPrompt();
      });
    } else if (sameOrigin && url.pathname === "/v1/session" && response.ok) {
      // The Hub answered who this is: offer sign-out where sign-in exists.
      void readAvailability().then(({ available }) => paintSignOut(available));
    }
    return response;
  };
  host.fetch = wrapped;

  const installation = { signIn, signOut };
  marked[INSTALLED] = installation;
  return installation;
}

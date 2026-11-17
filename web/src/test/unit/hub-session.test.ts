import { describe, expect, it, vi } from "vite-plus/test";

import {
  afterSignOutAddress,
  endsSession,
  installHubSession,
  needsCsrfHeader,
  signInAddress,
  signInAvailability,
  signInFailed,
  type SessionHost,
} from "../../runtime/hub-session";

const PAGE = "https://conduit.example.test/console/#area-fleet";

describe("the CSRF header", () => {
  it.each(["POST", "PUT", "PATCH", "DELETE", "post", "delete"])(
    "is required on a same-origin %s",
    (method) => {
      expect(needsCsrfHeader("/v1/teams/platform", { method }, PAGE)).toBe(
        true,
      );
    },
  );
  it.each([undefined, "GET", "HEAD", "OPTIONS", "get"])(
    "is not added to a %s",
    (method) => {
      expect(
        needsCsrfHeader("/v1/session", method ? { method } : undefined, PAGE),
      ).toBe(false);
    },
  );
  it("reads the method of a Request and of a URL input", () => {
    expect(
      needsCsrfHeader(
        new Request("https://conduit.example.test/v1/plans", {
          method: "POST",
        }),
        undefined,
        PAGE,
      ),
    ).toBe(true);
    expect(
      needsCsrfHeader(
        new URL("https://conduit.example.test/v1/plans"),
        { method: "PUT" },
        PAGE,
      ),
    ).toBe(true);
  });
  it.each([
    "https://evil.example/collect",
    "https://conduit.example.test.evil.example/v1/plans",
    "http://conduit.example.test/v1/plans",
    "https://conduit.example.test:8443/v1/plans",
    "//evil.example/v1/plans",
  ])("is never sent to another origin: %s", (target) => {
    expect(needsCsrfHeader(target, { method: "POST" }, PAGE)).toBe(false);
  });
});

describe("sign-in address", () => {
  it("names the current view, hash included", () => {
    expect(
      signInAddress({
        pathname: "/console/",
        search: "?view=fleet",
        hash: "#area-fleet",
      }),
    ).toBe(
      `/v1/auth/login?returnTo=${encodeURIComponent("/console/?view=fleet#area-fleet")}`,
    );
    expect(
      signInAddress({
        pathname: "/console/notifications",
        search: "",
        hash: "#ntf-inbox",
      }),
    ).toBe(
      `/v1/auth/login?returnTo=${encodeURIComponent("/console/notifications#ntf-inbox")}`,
    );
  });
  it("does not return a retry to the address that says sign-in failed", () => {
    expect(
      signInAddress({
        pathname: "/console/",
        search: "?sign-in=failed",
        hash: "",
      }),
    ).toBe(`/v1/auth/login?returnTo=${encodeURIComponent("/console/")}`);
    expect(
      signInAddress({
        pathname: "/console/",
        search: "?view=fleet&sign-in=failed",
        hash: "#area-fleet",
      }),
    ).toBe(
      `/v1/auth/login?returnTo=${encodeURIComponent("/console/?view=fleet#area-fleet")}`,
    );
  });
  it("recognises only the Hub's failure marker", () => {
    expect(signInFailed("?sign-in=failed")).toBe(true);
    expect(signInFailed("?view=fleet&sign-in=failed")).toBe(true);
    expect(signInFailed("")).toBe(false);
    expect(signInFailed("?sign-in=ok")).toBe(false);
    expect(signInFailed("?signin=failed")).toBe(false);
  });
  it("offers only the console as a return address", () => {
    for (const pathname of ["/", "/v1/session", "//evil.example/console/"])
      expect(signInAddress({ pathname, search: "", hash: "" })).toBe(
        `/v1/auth/login?returnTo=${encodeURIComponent("/console/")}`,
      );
  });
});

describe("sign-in availability", () => {
  const signIn = { loginPath: "/v1/auth/login", logoutPath: "/v1/auth/logout" };
  it("requires the Hub's own routes exactly", () => {
    expect(signInAvailability({ enabled: true, signIn }).available).toBe(true);
    for (const document of [
      null,
      undefined,
      "enabled",
      {},
      { enabled: false, signIn },
      { enabled: true },
      { enabled: true, signIn: null },
      { enabled: true, signIn: { loginPath: "/v1/auth/login" } },
      {
        enabled: true,
        signIn: { ...signIn, loginPath: "https://evil.example/login" },
      },
      { enabled: true, signIn: { ...signIn, logoutPath: "/elsewhere" } },
      {
        enabled: true,
        providers: [{ authorizationEndpoint: "https://idp.example/authorize" }],
      },
    ])
      expect(signInAvailability(document).available).toBe(false);
  });
});

describe("after sign-out", () => {
  it("follows only an https end-session address", () => {
    expect(
      afterSignOutAddress({
        endSessionUrl: "https://idp.example/logout?client_id=conduit",
      }),
    ).toBe("https://idp.example/logout?client_id=conduit");
    for (const body of [
      null,
      {},
      { endSessionUrl: 7 },
      { endSessionUrl: "javascript:alert(1)" },
      { endSessionUrl: "http://idp.example/logout" },
      { endSessionUrl: "/v1/session" },
      { endSessionUrl: "not a url" },
    ])
      expect(afterSignOutAddress(body)).toBe("/console/");
  });
});

describe("a session that ended", () => {
  it("is a 401 from the Hub and not from the sign-in routes", () => {
    const at = (path: string) => new URL(path, PAGE);
    expect(endsSession(at("/v1/session"), 401)).toBe(true);
    expect(endsSession(at("/graphql"), 401)).toBe(true);
    expect(endsSession(at("/v1/session"), 403)).toBe(false);
    expect(endsSession(at("/v1/session"), 500)).toBe(false);
    expect(endsSession(at("/v1/auth/logout"), 401)).toBe(false);
    expect(endsSession(null, 401)).toBe(false);
  });
});

interface FakeElement {
  id: string;
  type: string;
  className: string;
  textContent: string;
  title: string;
  disabled: boolean;
  attributes: Map<string, string>;
  children: FakeElement[];
  listeners: Map<string, () => void>;
  parentElement: FakeElement | null;
  nextElementSibling: FakeElement | null;
  setAttribute(name: string, value: string): void;
  addEventListener(name: string, listener: () => void): void;
  append(...nodes: FakeElement[]): void;
  after(node: FakeElement): void;
  remove(): void;
}

function fakeHost(answers: Record<string, () => Response>) {
  const byId = new Map<string, FakeElement>();
  const element = (): FakeElement => {
    const self: FakeElement = {
      id: "",
      type: "",
      className: "",
      textContent: "",
      title: "",
      disabled: false,
      attributes: new Map(),
      children: [],
      listeners: new Map(),
      parentElement: null,
      nextElementSibling: null,
      setAttribute: (name, value) => void self.attributes.set(name, value),
      addEventListener: (name, listener) =>
        void self.listeners.set(name, listener),
      append: (...nodes) => {
        for (const node of nodes) {
          node.parentElement = self;
          self.children.push(node);
          if (node.id) byId.set(node.id, node);
        }
      },
      after: (node) => {
        self.nextElementSibling = node;
        node.parentElement = self.parentElement;
        if (node.id) byId.set(node.id, node);
      },
      remove: () => void byId.delete(self.id),
    };
    return self;
  };
  const body = element();
  const header = element();
  const chip = element();
  chip.id = "operator-chip";
  header.append(chip);
  const calls: { url: string; init: RequestInit | undefined }[] = [];
  const assigned: string[] = [];
  const location = {
    href: PAGE,
    pathname: "/console/",
    search: "",
    hash: "#area-fleet",
    assign: (url: string) => void assigned.push(url),
  };
  const host = {
    fetch: vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = new URL(
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.href
            : input.url,
        location.href,
      );
      calls.push({ url: url.href, init });
      const answer = answers[url.pathname];
      return answer ? answer() : Response.json({});
    }),
    location,
    document: {
      body,
      createElement: () => element(),
      getElementById: (id: string) => byId.get(id) ?? null,
    },
  };
  return {
    host: host as unknown as SessionHost,
    calls,
    assigned,
    location,
    byId,
  };
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 0));
const published = () =>
  Response.json({
    enabled: true,
    signIn: { loginPath: "/v1/auth/login", logoutPath: "/v1/auth/logout" },
  });
const header = (init: RequestInit | undefined) =>
  new Headers(init?.headers).get("X-Conduit-CSRF");

describe("the installed session", () => {
  it("adds the header to same-origin writes only, keeping the caller's headers", async () => {
    const { host, calls } = fakeHost({});
    installHubSession(host);
    await host.fetch("/v1/teams/platform", {
      method: "PUT",
      headers: { "content-type": "application/json" },
      body: "{}",
    });
    await host.fetch("/v1/session");
    await host.fetch("https://evil.example/collect", { method: "POST" });
    await host.fetch(
      new Request("https://conduit.example.test/v1/plans", {
        method: "POST",
        headers: { "x-existing": "kept" },
      }),
    );
    // Found by address: a read of /v1/session also makes the session ask the
    // Hub, once, whether sign-in is available.
    const sent = (suffix: string) =>
      calls.find((call) => call.url.endsWith(suffix))?.init;
    expect(header(sent("/v1/teams/platform"))).toBe("1");
    expect(
      new Headers(sent("/v1/teams/platform")?.headers).get("content-type"),
    ).toBe("application/json");
    expect(header(sent("/v1/session"))).toBeNull();
    expect(header(sent("/v1/operator-identity"))).toBeNull();
    expect(
      calls.some((call) => call.url === "https://evil.example/collect"),
    ).toBe(true);
    expect(header(sent("evil.example/collect"))).toBeNull();
    expect(header(sent("/v1/plans"))).toBe("1");
    expect(new Headers(sent("/v1/plans")?.headers).get("x-existing")).toBe(
      "kept",
    );
  });

  it("is installed once", () => {
    const { host } = fakeHost({});
    const first = installHubSession(host);
    const wrapped = host.fetch;
    expect(installHubSession(host)).toBe(first);
    expect(host.fetch).toBe(wrapped);
  });

  it("offers to sign in again on a 401 and returns to the view on screen at the click", async () => {
    const { host, byId, assigned, location } = fakeHost({
      "/v1/operator-identity": published,
      "/v1/plans": () => new Response("{}", { status: 401 }),
    });
    installHubSession(host);
    await host.fetch("/v1/plans");
    await settle();
    const prompt = byId.get("session-ended-prompt");
    expect(prompt?.attributes.get("role")).toBe("alertdialog");
    expect(prompt?.children[0]?.textContent).toContain("Sign in again");
    // A second 401 does not stack a second prompt.
    await host.fetch("/v1/plans");
    await settle();
    expect(host.document.body.children).toHaveLength(1);
    // The person moves to another view before clicking.
    location.hash = "#ntf-inbox";
    byId.get("session-sign-in-again")?.listeners.get("click")?.();
    expect(assigned).toEqual([
      `/v1/auth/login?returnTo=${encodeURIComponent("/console/#ntf-inbox")}`,
    ]);
  });

  it("says a refused sign-in did not complete and offers to try again", async () => {
    const { host, byId, assigned, location } = fakeHost({
      "/v1/operator-identity": published,
    });
    location.search = "?sign-in=failed";
    location.hash = "";
    installHubSession(host);
    await settle();
    const prompt = byId.get("session-ended-prompt");
    expect(prompt?.attributes.get("aria-label")).toBe(
      "Sign-in did not complete",
    );
    expect(prompt?.children[0]?.textContent).toContain("you are not signed in");
    byId.get("session-sign-in-again")?.listeners.get("click")?.();
    expect(assigned).toEqual([
      `/v1/auth/login?returnTo=${encodeURIComponent("/console/")}`,
    ]);
  });

  it("says nothing about a failed sign-in on a Hub that runs none, or without the marker", async () => {
    const without = fakeHost({});
    without.location.search = "?sign-in=failed";
    installHubSession(without.host);
    await settle();
    expect(without.byId.has("session-ended-prompt")).toBe(false);
    const unmarked = fakeHost({ "/v1/operator-identity": published });
    installHubSession(unmarked.host);
    await settle();
    expect(unmarked.byId.has("session-ended-prompt")).toBe(false);
    expect(unmarked.calls).toEqual([]);
  });

  it("shows no prompt on a Hub without sign-in, or for a refusal that is not 401", async () => {
    for (const answers of [
      {
        "/v1/operator-identity": () =>
          Response.json({ enabled: false, providers: [] }),
        "/v1/plans": () => new Response("{}", { status: 401 }),
      },
      {
        "/v1/operator-identity": () => new Response("{}", { status: 503 }),
        "/v1/plans": () => new Response("{}", { status: 401 }),
      },
      {
        "/v1/operator-identity": published,
        "/v1/plans": () => new Response("{}", { status: 403 }),
      },
    ]) {
      const { host, byId } = fakeHost(answers);
      installHubSession(host);
      await host.fetch("/v1/plans");
      await settle();
      expect(byId.has("session-ended-prompt")).toBe(false);
    }
  });

  it("offers sign-out beside the operator once the Hub says who is signed in", async () => {
    const { host, byId, calls, assigned } = fakeHost({
      "/v1/operator-identity": published,
      "/v1/auth/logout": () =>
        Response.json({
          signedOut: true,
          endSessionUrl: "https://idp.example/logout",
        }),
    });
    installHubSession(host);
    expect(byId.has("operator-sign-out")).toBe(false);
    await host.fetch("/v1/session");
    await settle();
    const control = byId.get("operator-sign-out");
    expect(control?.textContent).toBe("Sign out");
    control?.listeners.get("click")?.();
    await settle();
    const logout = calls.find((call) => call.url.endsWith("/v1/auth/logout"));
    expect(logout?.init?.method).toBe("POST");
    expect(header(logout?.init)).toBe("1");
    expect(assigned).toEqual(["https://idp.example/logout"]);
  });

  it("does not claim a sign-out the Hub refused", async () => {
    const { host, assigned } = fakeHost({
      "/v1/auth/logout": () => new Response("{}", { status: 503 }),
    });
    await expect(installHubSession(host).signOut()).rejects.toThrow("503");
    expect(assigned).toEqual([]);
  });
});

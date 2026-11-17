import { expect, test, type Page, type Request } from "@playwright/test";

import { session, stubHub, type Handler } from "./hub-stub";

/** What a Hub running its own sign-in publishes. */
const IDENTITY = {
  enabled: true,
  providers: [
    {
      name: "Operator identity",
      issuer: "https://idp.example.test",
      clientId: "conduit-hub",
      authorizationEndpoint: "https://idp.example.test/authorize",
    },
  ],
  signIn: { loginPath: "/v1/auth/login", logoutPath: "/v1/auth/logout" },
};

/** A Hub that verifies tokens but runs no browser sign-in. */
const IDENTITY_WITHOUT_SIGN_IN = { ...IDENTITY, signIn: undefined };

function hub(
  identity: unknown,
  rest: Handler,
): { handler: Handler; writes: Request[] } {
  const writes: Request[] = [];
  const handler: Handler = (request, url) => {
    if (!["GET", "HEAD", "OPTIONS"].includes(request.method()))
      writes.push(request);
    if (url.pathname === "/v1/operator-identity") return { json: identity };
    return rest(request, url);
  };
  return { handler, writes };
}

const returnTo = (request: Request) =>
  new URL(request.url()).searchParams.get("returnTo");

async function waitForLogin(page: Page): Promise<Request> {
  return page.waitForRequest(
    (request) => new URL(request.url()).pathname === "/v1/auth/login",
  );
}

test("a 401 mid-session offers to sign in again and returns to the view on screen", async ({
  page,
}) => {
  let signedIn = true;
  const { handler } = hub(IDENTITY, (_request, url) => {
    if (url.pathname === "/v1/session")
      return signedIn
        ? { json: session() }
        : { status: 401, json: { error: "unauthorized", status: "401" } };
    if (!signedIn && url.pathname.startsWith("/v1/"))
      return { status: 401, json: { error: "unauthorized", status: "401" } };
    return undefined;
  });
  await stubHub(page, handler);
  await page.goto("/console/");
  await expect(page.locator("#operator-name")).toHaveText("riley");
  await expect(page.locator("#session-ended-prompt")).toHaveCount(0);

  // The session ends while the person is looking at another view.
  await page.goto("/console/#area-fleet");
  signedIn = false;
  await page.evaluate(() => fetch("/v1/plans", { credentials: "same-origin" }));
  const prompt = page.locator("#session-ended-prompt");
  await expect(prompt).toBeVisible();
  await expect(prompt).toContainText("Your session has ended");
  await expect(prompt).toHaveAttribute("role", "alertdialog");
  // The view is still there behind the prompt: nothing navigated.
  expect(new URL(page.url()).hash).toBe("#area-fleet");

  const login = waitForLogin(page);
  await prompt.getByRole("button", { name: "Sign in again" }).click();
  const request = await login;
  expect(request.method()).toBe("GET");
  expect(returnTo(request)).toBe("/console/#area-fleet");
  // The redirect is the Hub's. The console sent no OAuth parameter of its own.
  expect([...new URL(request.url()).searchParams.keys()]).toEqual(["returnTo"]);
});

test("the sign-in screen navigates to the Hub's login route, not to the provider", async ({
  page,
}) => {
  const provider: string[] = [];
  page.on("request", (request) => {
    if (new URL(request.url()).hostname === "idp.example.test")
      provider.push(request.url());
  });
  const { handler } = hub(IDENTITY, (_request, url) => {
    if (url.pathname === "/v1/session") return { json: session() };
    return undefined;
  });
  await stubHub(page, handler);
  await page.goto("/console/#adm03-auth-signin");
  const start = page.locator("[data-sign-in]");
  await expect(start).toBeVisible();
  const login = waitForLogin(page);
  await start.click();
  expect(returnTo(await login)).toBe("/console/#adm03-auth-signin");
  expect(provider).toEqual([]);
});

test("a sign-in the Hub refused says so and a retry returns to the console, not to the failure address", async ({
  page,
}) => {
  const { handler } = hub(IDENTITY, (_request, url) => {
    if (url.pathname === "/v1/session")
      return { status: 401, json: { error: "unauthorized", status: "401" } };
    return undefined;
  });
  await stubHub(page, handler);
  await page.goto("/console/?sign-in=failed");
  const prompt = page.locator("#session-ended-prompt");
  await expect(prompt).toBeVisible();
  await expect(prompt).toContainText("Sign-in did not complete");
  const login = waitForLogin(page);
  await page.locator("#session-sign-in-again").click();
  expect(returnTo(await login)).toBe("/console/");
});

test("a Hub without sign-in routes shows no sign-in button and no session prompt", async ({
  page,
}) => {
  const { handler } = hub(IDENTITY_WITHOUT_SIGN_IN, (_request, url) => {
    if (url.pathname === "/v1/session")
      return { status: 401, json: { error: "unauthorized", status: "401" } };
    return undefined;
  });
  await stubHub(page, handler);
  await page.goto("/console/#adm03-auth-signin");
  await expect(page.locator("[data-sign-in-unavailable]")).toBeVisible();
  await expect(page.locator("[data-sign-in]")).toHaveCount(0);
  await expect(page.locator("#operator-sign-out")).toHaveCount(0);
  await expect(page.locator("#session-ended-prompt")).toHaveCount(0);
});

test("sign-out posts to the Hub with the CSRF header and follows the provider's end-session address", async ({
  page,
}) => {
  const { handler, writes } = hub(IDENTITY, (request, url) => {
    if (url.pathname === "/v1/session") return { json: session() };
    if (url.pathname === "/v1/auth/logout" && request.method() === "POST")
      return {
        json: {
          signedOut: true,
          endSessionUrl:
            "https://idp.example.test/logout?client_id=conduit-hub",
        },
      };
    return undefined;
  });
  await stubHub(page, handler);
  await page.route("https://idp.example.test/**", (route) =>
    route.fulfill({ contentType: "text/html", body: "<h1>Signed out</h1>" }),
  );
  await page.goto("/console/");
  await expect(page.locator("#operator-name")).toHaveText("riley");
  const signOut = page.locator("#operator-sign-out");
  await expect(signOut).toBeVisible();
  await expect(signOut).toHaveText("Sign out");
  await signOut.click();
  await page.waitForURL(
    "https://idp.example.test/logout?client_id=conduit-hub",
  );
  const logout = writes.filter(
    (request) => new URL(request.url()).pathname === "/v1/auth/logout",
  );
  expect(logout).toHaveLength(1);
  expect(logout[0]?.method()).toBe("POST");
  expect(logout[0]?.headers()["x-conduit-csrf"]).toBe("1");
});

test("every write the console sends carries the CSRF header, and no read or cross-origin request does", async ({
  page,
}) => {
  const rid = "product/payments";
  const reads: Request[] = [];
  const { handler, writes } = hub(IDENTITY, (request, url) => {
    if (request.method() === "GET") reads.push(request);
    if (url.pathname === "/v1/session")
      return {
        json: session({
          resources: url.searchParams.getAll("resourceRid").map((item) => ({
            resourceRid: item,
            operations: ["resource:read"],
          })),
        }),
      };
    if (url.pathname === "/v1/notifications/contacts")
      return { json: { owner: "riley", contacts: [] } };
    if (url.pathname !== "/v1/notifications/preferences") return undefined;
    if (request.method() === "GET")
      return { status: 404, json: { error: "no notification preference" } };
    return {
      json: {
        id: "pref-1",
        owner: "riley",
        ...request.postDataJSON(),
        revision: 1,
        updatedAt: "2026-11-17T00:00:00Z",
        typesNotYetEmitted: [],
      },
    };
  });
  await stubHub(page, handler);
  const foreign: Request[] = [];
  await page.route("https://elsewhere.example.test/**", (route) => {
    foreign.push(route.request());
    return route.fulfill({
      json: {},
      headers: { "access-control-allow-origin": "*" },
    });
  });
  await page.goto("/console/notifications");
  await expect(page.locator("#operator-name")).toHaveText("riley");

  // A write through one of the console's own editors.
  await page.locator('.kit-subnav [data-ntf-view="preferences"]').click();
  await page.getByLabel("Resource").fill(rid);
  await page.getByRole("button", { name: "Load preference" }).click();
  await page.locator('[data-ntf-type="PLAN_FAILURES"]').check();
  await page.getByRole("button", { name: "Save preference" }).click();
  await expect(page.locator("[data-preference-saved]")).toContainText(
    "stored revision 1",
  );
  expect(
    writes.filter(
      (request) =>
        new URL(request.url()).pathname === "/v1/notifications/preferences",
    ),
  ).toHaveLength(1);

  // Writes by every path the console uses: fetch with a string, with a
  // Request, with each state-changing method, and the Connect transport's
  // POST. And one to another origin, which must not receive the header.
  await page.evaluate(async () => {
    await fetch("/v1/teams/platform", { method: "PUT", body: "{}" });
    await fetch(new Request("/v1/labels/a", { method: "DELETE" }));
    await fetch("/v1/plans", { method: "post", body: "{}" });
    await fetch("/v1/change-requests/a", { method: "PATCH", body: "{}" });
    await fetch("/conduit.reporting.v1.ReportingService/GetOverview", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: "{}",
    }).catch(() => undefined);
    await fetch("https://elsewhere.example.test/collect", {
      method: "POST",
      mode: "cors",
      body: "{}",
    }).catch(() => undefined);
  });

  expect(writes.length).toBeGreaterThanOrEqual(5);
  for (const request of writes)
    expect
      .soft(
        request.headers()["x-conduit-csrf"],
        `${request.method()} ${new URL(request.url()).pathname}`,
      )
      .toBe("1");
  expect(reads.length).toBeGreaterThan(0);
  for (const request of reads)
    expect
      .soft(
        request.headers()["x-conduit-csrf"],
        `GET ${new URL(request.url()).pathname}`,
      )
      .toBeUndefined();
  expect(foreign).toHaveLength(1);
  expect(foreign[0]?.headers()["x-conduit-csrf"]).toBeUndefined();
});

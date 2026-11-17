import { describe, expect, it, vi } from "vite-plus/test";

import {
  failureMarkup,
  renderContained,
} from "../../console/operator/render-containment.js";
import { escapeHtml } from "../../console/operator/palette.js";
import {
  CLIENT_ERROR_EVENT,
  buildId,
  clientErrorRecord,
  installClientErrorHandlers,
} from "../../runtime/client-errors";
import { ConsoleErrorBoundary } from "../../embed/ErrorBoundary";

function fakeRoot() {
  const listeners: Record<string, () => void> = {};
  const root: any = {
    innerHTML: "<p>half painted",
    onclick: () => {},
    onchange: () => {},
    querySelector: (selector: string) =>
      root.innerHTML.includes("data-render-reload") &&
      selector === "[data-render-reload]"
        ? {
            addEventListener: (name: string, listener: () => void) => {
              listeners[name] = listener;
            },
          }
        : null,
  };
  return { root, listeners };
}

describe("contained view render", () => {
  it("leaves a successful render untouched", () => {
    const { root } = fakeRoot();
    const log = vi.fn();
    const painted = renderContained(
      root,
      "s16-approvals-board",
      () => {
        root.innerHTML = "<section>board</section>";
      },
      { esc: escapeHtml, log },
    );
    expect(painted).toBe(true);
    expect(root.innerHTML).toBe("<section>board</section>");
    expect(log).not.toHaveBeenCalled();
  });

  it("replaces a throwing render with the stated failure and a reload control", () => {
    const { root, listeners } = fakeRoot();
    const log = vi.fn();
    const reload = vi.fn();
    const failure = new TypeError("Cannot read properties of null");
    const painted = renderContained(
      root,
      "s16-approvals-board",
      () => {
        root.innerHTML = "<table><tr><td>half";
        throw failure;
      },
      { esc: escapeHtml, log, reload },
    );
    expect(painted).toBe(false);
    expect(root.innerHTML).toContain("This view failed to render");
    expect(root.innerHTML).toContain(
      'data-render-failure="s16-approvals-board"',
    );
    expect(root.innerHTML).toContain(
      "TypeError: Cannot read properties of null",
    );
    expect(root.innerHTML).not.toContain("half");
    expect(root.onclick).toBeNull();
    expect(root.onchange).toBeNull();
    expect(log).toHaveBeenCalledExactlyOnceWith("s16-approvals-board", failure);
    listeners["click"]!();
    expect(reload).toHaveBeenCalledOnce();
  });

  it("still contains the failure when the log sink throws", () => {
    const { root } = fakeRoot();
    expect(
      renderContained(
        root,
        "rm03-plan-detail",
        () => {
          throw new Error("boom");
        },
        {
          esc: escapeHtml,
          log: () => {
            throw new Error("sink down");
          },
        },
      ),
    ).toBe(false);
    expect(root.innerHTML).toContain("This view failed to render");
  });

  it("escapes the screen id and the error text", () => {
    const markup = failureMarkup(
      '"><img src=x onerror=alert(1)>',
      new Error("<script>alert(2)</script>"),
      escapeHtml,
    );
    expect(markup).not.toContain("<img");
    expect(markup).not.toContain("<script>");
    expect(markup).toContain("&lt;script&gt;alert(2)&lt;/script&gt;");
    expect(markup).toContain("&lt;img src=x onerror=alert(1)&gt;");
    expect(markup).not.toMatch(/data-render-failure=""/);
  });

  it("shows no text for a thrown value that is not an Error", () => {
    const markup = failureMarkup(
      "s10-diff",
      { secret: "tenant-row" },
      escapeHtml,
    );
    expect(markup).not.toContain("tenant-row");
    expect(markup).toContain(">Error</p>");
  });
});

describe("client error record", () => {
  it("carries the event, kind, build, and only name, message and stack", () => {
    const error = new RangeError("out of range");
    const record = clientErrorRecord("render", error, {
      build: "index-abc123.js",
      screen: "rm03-plan-detail",
    });
    expect(record).toEqual({
      event: CLIENT_ERROR_EVENT,
      kind: "render",
      build: "index-abc123.js",
      screen: "rm03-plan-detail",
      name: "RangeError",
      message: "out of range",
      stack: error.stack,
    });
  });

  it("records only the type of a non-Error value", () => {
    const record = clientErrorRecord("unhandledrejection", {
      tenant: "acme",
      body: "private",
    });
    expect(record).toEqual({
      event: CLIENT_ERROR_EVENT,
      kind: "unhandledrejection",
      build: "unknown",
      name: "NonError",
      message: "non-Error value of type object",
      stack: "",
    });
    expect(JSON.stringify(record)).not.toContain("acme");
    expect(clientErrorRecord("error", "private text").message).toBe(
      "non-Error value of type string",
    );
  });

  it("bounds the message and stack", () => {
    const error = new Error("m".repeat(5000));
    error.stack = "s".repeat(50_000);
    const record = clientErrorRecord("error", error);
    expect(record.message).toHaveLength(500);
    expect(record.stack).toHaveLength(4000);
  });

  it("reads the build from the content-hashed entry module", () => {
    const shell = (src: string | null) => ({
      querySelector: (selector: string) =>
        selector === 'script[type="module"][src]'
          ? { getAttribute: () => src }
          : null,
    });
    expect(buildId(shell("/console/assets/index-D4f9_a1b.js"))).toBe(
      "index-D4f9_a1b.js",
    );
    expect(buildId(shell("/console/assets/index-1.js?v=2#x"))).toBe(
      "index-1.js",
    );
    expect(buildId(shell(null))).toBe("unknown");
    expect(buildId({ querySelector: () => null })).toBe("unknown");
    expect(buildId(undefined)).toBe("unknown");
  });

  it("logs one record per uncaught error and unhandled rejection, then detaches", () => {
    const listeners = new Map<string, (event: any) => void>();
    const target = {
      addEventListener: (type: string, listener: (event: any) => void) =>
        listeners.set(type, listener),
      removeEventListener: (type: string, listener: (event: any) => void) => {
        if (listeners.get(type) === listener) listeners.delete(type);
      },
    };
    const log = vi.fn();
    const remove = installClientErrorHandlers(target, log, "index-1.js");
    expect([...listeners.keys()].sort()).toEqual([
      "error",
      "unhandledrejection",
    ]);
    listeners.get("error")!({
      error: new Error("thrown"),
      message: "ignored",
    });
    listeners.get("unhandledrejection")!({ reason: new TypeError("rejected") });
    expect(log).toHaveBeenCalledTimes(2);
    expect(log.mock.calls[0]![0]).toMatchObject({
      event: CLIENT_ERROR_EVENT,
      kind: "error",
      build: "index-1.js",
      name: "Error",
      message: "thrown",
    });
    expect(log.mock.calls[1]![0]).toMatchObject({
      kind: "unhandledrejection",
      name: "TypeError",
      message: "rejected",
    });
    remove();
    expect(listeners.size).toBe(0);
  });
});

describe("React error boundary", () => {
  it("switches to the fallback state when a child throws", () => {
    expect(ConsoleErrorBoundary.getDerivedStateFromError()).toEqual({
      failed: true,
    });
  });
});

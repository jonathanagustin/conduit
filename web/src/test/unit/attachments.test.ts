import { describe, expect, it } from "vite-plus/test";

import {
  LIMITS,
  attachControl,
  attachmentList,
  formatSize,
  loadAttachments,
  mergeItem,
  precheck,
  uploadAttachment,
} from "../../console/operator/attachments.js";

const digest = "a".repeat(64);

function reply(status: number, body: unknown) {
  return async () =>
    ({
      ok: status >= 200 && status < 300,
      status,
      json: async () => body,
    }) as Response;
}

function file(name: string, text: string) {
  return {
    name,
    size: new TextEncoder().encode(text).length,
    text: async () => text,
  } as unknown as File;
}

describe("attachment limits", () => {
  it("allows text-like intake files", () => {
    for (const name of [
      "Dockerfile",
      "values.yaml",
      "Chart.yaml",
      "a.yml",
      "a.json",
      "a.txt",
      "spec.md",
    ]) {
      expect(precheck(name, 10)).toBe("");
    }
  });

  it("refuses images, archives, and oversized files with a reason", () => {
    expect(precheck("ingress-oom-capture.png", 10)).toContain(
      "not an allowed type",
    );
    expect(precheck("uds-telemetry-0.31.2.tar.zst", 10)).toContain(
      "not an allowed type",
    );
    expect(precheck("big.txt", LIMITS.maxFileBytes + 1)).toContain("limit");
    expect(precheck("spec.md", LIMITS.maxSpecBytes + 1)).toContain("limit");
    expect(precheck("empty.txt", 0)).toContain("empty");
  });
});

describe("attachment upload", () => {
  it("sends one JSON object and returns the stored record", async () => {
    let sent: { url?: string; init?: RequestInit } = {};
    const item = {
      name: "values.yaml",
      kind: "Values",
      size: 4,
      sha256: digest,
    };
    const result = await uploadAttachment(
      file("values.yaml", "a: 1\n"),
      async (url, init) => {
        sent = { url: String(url), init };
        return reply(201, item)();
      },
    );
    expect(result).toEqual({ ok: true, item });
    expect(sent.url).toBe("/v1/intake-attachments");
    expect(JSON.parse(String(sent.init?.body))).toEqual({
      filename: "values.yaml",
      content: "a: 1\n",
    });
  });

  it("shows the Hub's reason when a file is refused", async () => {
    const reason =
      "values.yaml was not stored: it contains a credential assignment";
    const result = await uploadAttachment(
      file("values.yaml", "a: 1\n"),
      reply(422, { error: reason }),
    );
    expect(result).toEqual({ ok: false, reason });
  });

  it("does not call the Hub for a disallowed type", async () => {
    let called = false;
    const result = await uploadAttachment(file("shot.png", "x"), async () => {
      called = true;
      return reply(201, {})();
    });
    expect(called).toBe(false);
    expect(result.ok).toBe(false);
  });

  it("reports an unreachable Hub", async () => {
    const result = await uploadAttachment(file("a.txt", "x"), async () => {
      throw new Error("offline");
    });
    expect(result).toEqual({
      ok: false,
      reason: "The upload did not reach the Hub.",
    });
  });
});

describe("stored attachments", () => {
  it("loads the list and limits", async () => {
    const items = [{ name: "a.txt", kind: "Text", size: 1, sha256: digest }];
    const loaded = await loadAttachments(reply(200, { items, limits: LIMITS }));
    expect(loaded.items).toEqual(items);
    const failed = await loadAttachments(reply(401, {}));
    expect(failed.items).toEqual([]);
    expect(failed.error).not.toBe("");
  });

  it("renders name, type, size, and sha256 read-only", () => {
    const html = attachmentList([
      { name: "values.yaml", kind: "Values", size: 2048, sha256: digest },
    ]);
    expect(html).toContain("values.yaml");
    expect(html).toContain("Values");
    expect(html).toContain("2.0 KB");
    expect(html).toContain(`sha256 ${digest}`);
    expect(html).not.toContain("<input");
    expect(attachmentList([])).toContain("No attachments stored.");
  });

  it("escapes names and shows a visible rejection reason", () => {
    expect(
      attachmentList([
        { name: "<b>.txt", kind: "Text", size: 1, sha256: digest },
      ]),
    ).not.toContain("<b>");
    expect(attachControl("nope <x>")).toContain("nope &lt;x&gt;");
  });

  it("does not repeat a stored digest", () => {
    const item = { name: "a.txt", kind: "Text", size: 1, sha256: digest };
    expect(mergeItem([item], item)).toHaveLength(1);
    expect(formatSize(12)).toBe("12 B");
  });
});

import { describe, expect, it } from "vite-plus/test";
import {
  INSPECTOR_TABS,
  inspectorMarkup,
  loadInspector,
  ontologyRef,
  validateFor,
} from "../../console/operator/object-inspector.js";
import { createConduitClient } from "../../api/ontology-client/index.js";

type Call = { path: string; body: any };

function fakeTransport(opts: { failLink?: string } = {}) {
  const calls: Call[] = [];
  const transport = async (
    path: string,
    init?: { method?: string; body?: unknown },
  ) => {
    const body = init?.body as any;
    calls.push({ path, body });
    if (path.endsWith("/apply")) {
      const valid = { validation: { result: "VALID" } };
      return valid;
    }
    const set = body.objectSet;
    if (set.type === "searchAround") {
      if (set.link === opts.failLink) throw new Error("link failed");
      return { data: [{ id: "p1", name: "Plan 1" }], totalCount: 7 };
    }
    return { data: [{ id: "dev", connection: "<b>x</b>", generation: 3 }] };
  };
  return { transport, calls };
}

describe("ontologyRef", () => {
  it("maps known object types", () => {
    expect(ontologyRef({ objectType: "Environment", name: "dev" })).toEqual({
      objectType: "Environment",
      primaryKey: "dev",
    });
    expect(
      ontologyRef({ objectType: "ChangeRequest", id: "cr-1", name: "n" }),
    ).toEqual({
      objectType: "ChangeRequest",
      primaryKey: "cr-1",
    });
    expect(
      ontologyRef({ objectType: "ChangeRequest", name: "n" })?.primaryKey,
    ).toBe("n");
  });

  it("returns null for unknown types", () => {
    expect(ontologyRef({ objectType: "Nope", name: "x" })).toBeNull();
    expect(ontologyRef({ name: "x" })).toBeNull();
    expect(ontologyRef(null)).toBeNull();
  });
});

describe("loadInspector", () => {
  const ref = { objectType: "Environment", primaryKey: "dev" };

  it("issues one fetchOne and one pivot per link", async () => {
    const { transport, calls } = fakeTransport();
    const model = await loadInspector(createConduitClient({ transport }), ref);
    expect(calls).toHaveLength(1 + 5);
    expect(
      calls.filter((c) => c.body.objectSet.type === "searchAround"),
    ).toHaveLength(5);
    expect(model.object).toMatchObject({ id: "dev" });
    expect(model.links).toHaveLength(5);
    expect(model.links[0]!.count).toBe(7);
    expect(model.links[0]!.items[0]).toEqual({ primaryKey: "p1", title: "p1" });
  });

  it("survives a failing link", async () => {
    const { transport } = fakeTransport({ failLink: "plans" });
    const model = await loadInspector(createConduitClient({ transport }), ref);
    const plans = model.links.find((l: any) => l.link === "plans");
    expect(plans.items).toEqual([]);
    expect(plans.error).toBe("link failed");
    expect(model.links.filter((l: any) => !l.error).length).toBe(4);
    expect(model.error).toBeUndefined();
  });

  it("reports a top-level failure", async () => {
    const client = createConduitClient({
      transport: async () => {
        throw new Error("down");
      },
    });
    const model = await loadInspector(client, ref);
    expect(model.error).toBe("down");
  });
});

describe("inspectorMarkup", () => {
  const model = {
    ref: { objectType: "Environment", primaryKey: "dev" },
    object: {
      id: "dev",
      connection: "<script>x</script>",
      __internal: "hidden",
    },
    links: [
      {
        link: "plans",
        target: "Plan",
        cardinality: "MANY",
        count: 2,
        items: [{ primaryKey: "p1", title: "Plan <1>" }],
      },
    ],
  };

  it("renders five tabs and marks the active one", () => {
    const html = inspectorMarkup(model, { tab: "links" });
    expect(INSPECTOR_TABS).toHaveLength(5);
    expect((html.match(/data-inspector-tab=/g) ?? []).length).toBe(5);
    expect(html).toContain(
      'class="kit-tab is-active" data-inspector-tab="links"',
    );
  });

  it("escapes values and skips internal keys", () => {
    const html = inspectorMarkup(model, { tab: "props" });
    expect(html).not.toContain("<script>");
    expect(html).toContain("&lt;script&gt;");
    expect(html).not.toContain("hidden");
    const links = inspectorMarkup(model, { tab: "links" });
    expect(links).toContain('data-inspect-type="Plan"');
    expect(links).toContain('data-inspect-key="p1"');
    expect(links).toContain("Plan &lt;1&gt;");
  });

  it("shows empty and loading states", () => {
    expect(inspectorMarkup({}, { tab: "props" })).toContain(
      "No object is selected.",
    );
    expect(inspectorMarkup({ loading: true }, { tab: "props" })).toContain(
      "Loading…",
    );
    expect(inspectorMarkup(model, { tab: "timeline", audit: [] })).toContain(
      "No audit entry names this object.",
    );
  });

  it("renders the timeline newest first", () => {
    const audit = [
      {
        sequence: 1,
        action: "a1",
        actor: "u",
        resource: "env/dev",
        recordedAt: "t1",
      },
      {
        sequence: 3,
        action: "a3",
        actor: "u",
        resource: "env/dev",
        recordedAt: "t3",
      },
      {
        sequence: 2,
        action: "other",
        actor: "u",
        resource: "env/prod",
        recordedAt: "t2",
      },
    ];
    const html = inspectorMarkup(model, { tab: "timeline", audit });
    expect(html.indexOf("a3")).toBeLessThan(html.indexOf("a1"));
    expect(html).not.toContain("other");
  });

  it("has no approve button and shows the change-request text", () => {
    const html = inspectorMarkup(
      {
        ...model,
        outcome: { kind: "change-request", action: "delete", stored: false },
      },
      { tab: "actions" },
    );
    expect(html).not.toMatch(/approve<\/button>/i);
    expect(html).not.toContain("data-inspector-approve");
    expect(html).toContain('data-inspector-validate="delete"');
    expect(html).toContain(
      "High risk: proposed as a change request. A different approver must approve it; you cannot approve your own request.",
    );
  });

  it("renders raw JSON escaped", () => {
    const html = inspectorMarkup(model, { tab: "raw" });
    expect(html).toContain('<pre class="code-id');
    expect(html).toContain("&lt;script&gt;");
  });
});

describe("validateFor", () => {
  it("returns change-request for delete", async () => {
    const { transport, calls } = fakeTransport();
    const client = createConduitClient({ transport });
    const outcome = await validateFor(
      client,
      { objectType: "Environment", primaryKey: "dev" },
      "delete",
    );
    expect(outcome.kind).toBe("change-request");
    expect(calls[0]!.body.parameters).toEqual({
      environment: "dev",
      targetKind: "Environment",
      targetId: "dev",
      changeType: "delete",
    });
    expect(calls[0]!.body.options.mode).toBe("VALIDATE_ONLY");
  });

  it("turns errors into an invalid outcome", async () => {
    const client = createConduitClient({
      transport: async () => {
        throw new Error("boom");
      },
    });
    const outcome = await validateFor(
      client,
      { objectType: "Plan", primaryKey: "p1" },
      "fork",
      {
        environment: "dev",
      },
    );
    expect(outcome).toEqual({
      kind: "invalid",
      messages: {},
      submission: "boom",
    });
  });
});

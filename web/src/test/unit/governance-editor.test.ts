import { describe, expect, it, vi } from "vite-plus/test";
import { createGovernanceEditor } from "../../console/operator/governance-editor.mjs";

const policy = {
  id: "release-review",
  displayName: "Release review",
  expression: "no_op()",
  enabled: true,
  revision: 4,
  variables: {
    text: { type: "STRING", string: "original\nvalue" },
    list: { type: "STRING_LIST", list: ["a\nb", "", "c"] },
  },
};
const grant = {
  id: "grt_native",
  team: "reviewers",
  resourceRid: "environment/dev",
  relationship: "CO_OWNER",
  roles: ["environment:viewer"],
  operations: ["resource:read"],
  revision: 3,
};
function harness(reply?: (path: string, options: any) => Promise<Response>) {
  let actor = "alice";
  let allowed: boolean | null = null;
  const saved = vi.fn();
  const fetchImpl = vi.fn(
    reply ||
      (async (_path, options) => {
        const body = JSON.parse(options.body);
        return new Response(
          JSON.stringify({
            ...body,
            id: body.id || grant.id,
            revision: body.revision + 1,
          }),
        );
      }),
  );
  const editor = createGovernanceEditor({
    identityKey: () => actor,
    canPerform: () => ({
      allowed,
      reason: allowed === false ? "No permission" : "Hub checks authority",
    }),
    render: vi.fn(),
    saved,
    fetchImpl,
  });
  return {
    editor,
    saved,
    fetchImpl,
    actor: (value: string) => {
      actor = value;
    },
    allowed: (value: boolean | null) => {
      allowed = value;
    },
    html: (kind = "policy") =>
      editor.markup(kind, kind === "policy" ? [policy] : [grant], true),
  };
}
describe("confirmed native governance editing", () => {
  it.each([
    ["policy", policy],
    ["policy", { ...policy, id: "different-policy" }],
    ["grant", grant],
  ])(
    "blocks editor replacement during a save, including %s row switches",
    async (kind, row) => {
      let release!: (response: Response) => void;
      const h = harness(
        () =>
          new Promise((resolve) => {
            release = resolve;
          }),
      );
      h.editor.open("policy", policy);
      h.editor.review();
      const pending = h.editor.confirm();
      h.editor.open(kind, row);
      h.editor.review();
      h.editor.cancelReview();
      const blocked = h.editor.confirm();
      expect(h.fetchImpl).toHaveBeenCalledTimes(1);
      expect(h.html(kind)).toContain("data-governance-new disabled");
      expect(h.html(kind)).toContain('data-governance-open="0" disabled');
      release(new Response(JSON.stringify({ ...policy, revision: 5 })));
      await Promise.all([pending, blocked]);
      expect(h.saved).toHaveBeenCalledWith(
        "policy",
        expect.objectContaining({ id: policy.id, revision: 5 }),
      );
      h.editor.open(kind, row);
      expect(h.html(kind)).toContain("data-governance-editor");
    },
  );
  it("blocks row switching and writes while explicitly reloading a record", async () => {
    let release!: (response: Response) => void;
    const h = harness(
      () =>
        new Promise((resolve) => {
          release = resolve;
        }),
    );
    h.editor.open("policy", policy);
    const pending = h.editor.reload();
    h.editor.open("grant", grant);
    h.editor.review();
    const blocked = h.editor.confirm();
    expect(h.fetchImpl).toHaveBeenCalledTimes(1);
    expect(h.fetchImpl.mock.calls[0][1].method).toBeUndefined();
    release(
      new Response(
        JSON.stringify({ approvalPolicies: [{ ...policy, revision: 6 }] }),
      ),
    );
    await Promise.all([pending, blocked]);
    expect(h.html()).toContain("Expected revision: 6");
  });
  it("allows the new observed identity to open its own editor and ignores the prior save completion", async () => {
    let release!: (response: Response) => void;
    const h = harness(
      () =>
        new Promise((resolve) => {
          release = resolve;
        }),
    );
    h.editor.open("policy", policy);
    h.editor.review();
    const pending = h.editor.confirm();
    h.actor("bob");
    h.editor.open("grant", grant);
    expect(h.html("grant")).toContain("data-governance-editor");
    expect(h.html("grant")).not.toContain("data-governance-new disabled");
    release(new Response(JSON.stringify({ ...policy, revision: 5 })));
    await pending;
    expect(h.saved).not.toHaveBeenCalled();
    expect(h.html("grant")).toContain("Edit resource grant");
  });
  it("sends no write before confirmation, preserves typed variables and the expected revision, then uses the returned record", async () => {
    const h = harness();
    const original = JSON.stringify(policy);
    h.editor.open("policy", policy);
    h.editor.update("displayName", "Edited review");
    await h.editor.confirm();
    h.editor.review();
    expect(h.fetchImpl).not.toHaveBeenCalled();
    expect(h.html()).toContain("Save policy");
    await h.editor.confirm();
    expect(h.fetchImpl).toHaveBeenCalledTimes(1);
    const [path, request] = h.fetchImpl.mock.calls[0];
    expect(path).toBe("/v1/approval-policies/release-review");
    expect(request.method).toBe("PUT");
    expect(request.credentials).toBe("same-origin");
    expect(JSON.parse(request.body)).toEqual({
      id: policy.id,
      displayName: "Edited review",
      expression: policy.expression,
      enabled: true,
      variables: policy.variables,
      revision: 4,
    });
    expect(JSON.stringify(policy)).toBe(original);
    expect(h.saved).toHaveBeenCalledWith(
      "policy",
      expect.objectContaining({ revision: 5 }),
    );
    expect(h.html()).toContain("revision 5");
  });
  it("keeps existing grant coordinates fixed and preserves relationship, role/operation lists and revision", async () => {
    const h = harness();
    h.editor.open("grant", grant);
    h.editor.update("team", "another-team");
    h.editor.update("resourceRid", "platform/root");
    h.editor.update("roles", "environment:viewer\nenvironment:operator");
    h.editor.review();
    await h.editor.confirm();
    const [path, request] = h.fetchImpl.mock.calls[0];
    expect(path).toBe("/v1/resource-grants");
    expect(request.method).toBe("POST");
    expect(JSON.parse(request.body)).toEqual({
      team: grant.team,
      resourceRid: grant.resourceRid,
      relationship: "CO_OWNER",
      roles: ["environment:viewer", "environment:operator"],
      operations: ["resource:read"],
      revision: 3,
    });
  });
  it("cancels review without sending and requires review again after a changed draft", async () => {
    const h = harness();
    h.editor.open("policy", policy);
    h.editor.review();
    h.editor.cancelReview();
    await h.editor.confirm();
    expect(h.fetchImpl).not.toHaveBeenCalled();
    h.editor.review();
    h.editor.update("expression", "do_not_merge()");
    await h.editor.confirm();
    expect(h.fetchImpl).not.toHaveBeenCalled();
    h.editor.review();
    await h.editor.confirm();
    expect(JSON.parse(h.fetchImpl.mock.calls[0][1].body).expression).toBe(
      "do_not_merge()",
    );
  });
  it("invalidates confirmation when observed identity or current permission changes", async () => {
    const h = harness();
    h.editor.open("policy", policy);
    h.editor.review();
    h.actor("bob");
    await h.editor.confirm();
    expect(h.fetchImpl).not.toHaveBeenCalled();
    expect(h.html()).not.toContain("data-governance-editor");
    h.editor.open("policy", policy);
    h.editor.review();
    h.allowed(false);
    await h.editor.confirm();
    expect(h.fetchImpl).not.toHaveBeenCalled();
    expect(h.html()).toContain("authority changed");
  });
  it("sends once and shows no success while pending; ignores a late response after identity changes", async () => {
    let release!: (response: Response) => void;
    const h = harness(
      () =>
        new Promise((resolve) => {
          release = resolve;
        }),
    );
    h.editor.open("policy", policy);
    h.editor.review();
    const pending = h.editor.confirm();
    const duplicate = h.editor.confirm();
    expect(h.fetchImpl).toHaveBeenCalledTimes(1);
    expect(h.html()).not.toContain("The Hub saved");
    h.actor("bob");
    release(new Response(JSON.stringify({ ...policy, revision: 5 })));
    await Promise.all([pending, duplicate]);
    expect(h.saved).not.toHaveBeenCalled();
    expect(h.html()).not.toContain("data-governance-editor");
  });
  it.each([403, 409, 503])(
    "retains the draft and never retries a Hub refusal (%s)",
    async (status) => {
      const h = harness(
        async () =>
          new Response(JSON.stringify({ error: "<img src=x onerror=bad()>" }), {
            status,
          }),
      );
      h.editor.open("policy", policy);
      h.editor.update("displayName", "Keep my draft");
      h.editor.review();
      await h.editor.confirm();
      await h.editor.confirm();
      expect(h.fetchImpl).toHaveBeenCalledTimes(1);
      expect(h.saved).not.toHaveBeenCalled();
      expect(h.html()).toContain("Keep my draft");
      expect(h.html()).toContain("No retry was sent");
      expect(h.html()).not.toContain("<img");
      if (status === 409)
        expect(h.html()).toContain("record changed on the Hub");
      else expect(h.html()).toContain("&lt;img");
    },
  );
  it.each([
    {},
    { ...policy, revision: 4 },
    { ...policy, id: "different", revision: 5 },
  ])(
    "does not claim a malformed or mismatched success is saved",
    async (record) => {
      const h = harness(async () => new Response(JSON.stringify(record)));
      h.editor.open("policy", policy);
      h.editor.review();
      await h.editor.confirm();
      expect(h.saved).not.toHaveBeenCalled();
      expect(h.html()).toContain("did not return the expected saved record");
    },
  );
  it("explicitly reloads the authoritative revision and typed values before another edit", async () => {
    const h = harness(async (_path, options) =>
      options.method
        ? new Response("{}", { status: 409 })
        : new Response(
            JSON.stringify({
              approvalPolicies: [
                { ...policy, displayName: "Other writer", revision: 8 },
              ],
            }),
          ),
    );
    h.editor.open("policy", policy);
    h.editor.update("displayName", "Discard this");
    h.editor.review();
    await h.editor.confirm();
    await h.editor.reload();
    expect(h.html()).toContain("Other writer");
    expect(h.html()).not.toContain("Discard this");
    h.editor.review();
    await h.editor.confirm();
    expect(JSON.parse(h.fetchImpl.mock.calls[2][1].body).revision).toBe(8);
  });
  it("does not construct a grant deletion, accept an unknown revision, or mutate policy IDs", async () => {
    const h = harness();
    h.editor.open("grant", {
      ...grant,
      relationship: "",
      roles: [],
      operations: [],
    });
    h.editor.review();
    await h.editor.confirm();
    expect(h.fetchImpl).not.toHaveBeenCalled();
    h.editor.open("policy", { ...policy, revision: undefined });
    h.editor.review();
    await h.editor.confirm();
    expect(h.fetchImpl).not.toHaveBeenCalled();
    h.editor.open("policy", policy);
    h.editor.update("id", "other-policy");
    h.editor.review();
    await h.editor.confirm();
    expect(h.fetchImpl.mock.calls[0][0]).toBe(
      "/v1/approval-policies/release-review",
    );
  });
  it("retains the editor across background paints and names native authority without JSON request mechanics", () => {
    const h = harness();
    h.editor.open("policy", policy);
    h.editor.update("displayName", "Focused draft");
    expect(h.html()).toContain("Focused draft");
    expect(h.html()).toContain("Focused draft");
    expect(h.html()).toContain("Conduit-native");
    expect(h.html()).not.toContain("/v1/");
  });
});

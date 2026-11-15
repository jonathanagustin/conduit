import { readFileSync } from "node:fs";
import { describe, expect, it, vi } from "vitest";
import {
  FormToolExecutor,
  formToolOffers,
} from "../../console/operator/agent-form-tools.js";
const call = (
  name = "fill_field",
  args = { field: "question:port", value: "8080" },
) => ({ id: "tool-1", name, arguments: JSON.stringify(args) });
function binding() {
  return {
    fields: () =>
      new Map([
        ["repoUrl", { type: "string", readOnly: true }],
        ["question:port", { type: "string", readOnly: false }],
        ["question:tls", { type: "boolean", readOnly: false }],
      ]),
    dirty: vi.fn(() => false),
    set: vi.fn(),
    focus: vi.fn(),
    confirm: vi.fn(async () => true),
  };
}
describe("browser draft tool policy", () => {
  it("offers the same schemas as the Hub, without extra authority", () => {
    const hub = JSON.parse(
      readFileSync(
        new URL(
          "../../../../internal/planning/agentstream/form-tools.json",
          import.meta.url,
        ),
        "utf8",
      ),
    );
    expect(
      Object.fromEntries(
        formToolOffers().map((item) => [item.name, item.parameters]),
      ),
    ).toEqual(hub);
    expect(formToolOffers().map((item) => item.name)).toEqual([
      "fill_field",
      "clear_field",
      "focus_field",
      "request_confirmation",
    ]);
  });
  it("applies a valid draft only once and never submits it", async () => {
    const executor = new FormToolExecutor();
    const form = binding();
    expect(await executor.execute(call(), formToolOffers(), form)).toEqual({
      ok: true,
    });
    expect(await executor.execute(call(), formToolOffers(), form)).toEqual({
      ok: true,
    });
    expect(form.set).toHaveBeenCalledExactlyOnceWith("question:port", "8080");
  });
  it("refuses unknown, unoffered, invalid, read-only and human-edited fields", async () => {
    for (const [request, offers, code, dirty] of [
      [call(), [], "TOOL_NOT_OFFERED", false],
      [
        call("fill_field", { field: "repoUrl", value: "evil" }),
        formToolOffers(),
        "READ_ONLY_FIELD",
        false,
      ],
      [
        call("fill_field", { field: "unknown", value: "x" }),
        formToolOffers(),
        "UNKNOWN_FIELD",
        false,
      ],
      [
        call("fill_field", { field: "question:tls", value: "true" }),
        formToolOffers(),
        "INVALID_FIELD_VALUE",
        false,
      ],
      [call(), formToolOffers(), "HUMAN_DRAFT_PRESENT", true],
      [
        {
          ...call(),
          arguments: '{"field":"question:port","value":"x","execute":true}',
        },
        formToolOffers(),
        "INVALID_ARGUMENTS",
        false,
      ],
    ] as const) {
      const form = binding();
      form.dirty.mockReturnValue(dirty);
      expect(
        await new FormToolExecutor().execute(request, offers, form),
      ).toEqual({ ok: false, code });
      expect(form.set).not.toHaveBeenCalled();
    }
  });
  it("honors a narrower offered schema and rejects reused IDs with different args", async () => {
    const executor = new FormToolExecutor();
    const form = binding();
    const offers = formToolOffers();
    offers[0].parameters = {
      type: "object",
      properties: {
        field: { const: "question:other" },
        value: { type: "string" },
      },
      required: ["field", "value"],
      additionalProperties: false,
    };
    expect(await executor.execute(call(), offers, form)).toEqual({
      ok: false,
      code: "INVALID_ARGUMENTS",
    });
    expect(
      await executor.execute(
        call("fill_field", { field: "question:port", value: "9999" }),
        offers,
        form,
      ),
    ).toEqual({ ok: false, code: "CALL_ID_CONFLICT" });
  });
  it("reports a human refusal and deduplicates simultaneous confirmations", async () => {
    const executor = new FormToolExecutor();
    const form = binding();
    form.confirm.mockResolvedValue(false);
    const request = {
      id: "confirm",
      name: "request_confirmation",
      arguments: '{"message":"Continue planning?"}',
    };
    const results = await Promise.all([
      executor.execute(request, formToolOffers(), form),
      executor.execute(request, formToolOffers(), form),
    ]);
    expect(results).toEqual([
      { ok: true, confirmed: false },
      { ok: true, confirmed: false },
    ]);
    expect(form.confirm).toHaveBeenCalledTimes(1);
    expect(form.set).not.toHaveBeenCalled();
  });
});

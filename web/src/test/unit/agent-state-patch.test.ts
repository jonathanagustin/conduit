import { describe, expect, it } from "vitest";
import { applyPatch } from "../../console/operator/json-patch.js";

describe("agent RFC 6902 state updates", () => {
  it("replays all six operations, escaped keys and array insertion", () => {
    const original = { list: ["a", "b"], "a/b": { "~key": 1 } };
    expect(
      applyPatch(original, [
        { op: "test", path: "/a~1b", value: { "~key": 1 } },
        { op: "replace", path: "/a~1b/~0key", value: 2 },
        { op: "copy", from: "/a~1b", path: "/copy" },
        { op: "add", path: "/list/1", value: "c" },
        { op: "move", from: "/list/0", path: "/list/-" },
        { op: "remove", path: "/a~1b" },
      ]),
    ).toEqual({ list: ["c", "b", "a"], copy: { "~key": 2 } });
    expect(original).toEqual({ list: ["a", "b"], "a/b": { "~key": 1 } });
  });
  it("compares object values regardless of member order and replaces root", () => {
    expect(
      applyPatch({ a: 1, b: 2 }, [
        { op: "test", path: "", value: { b: 2, a: 1 } },
        { op: "replace", path: "", value: { ready: true } },
      ]),
    ).toEqual({ ready: true });
  });
  it.each([
    { op: "replace", path: "/absent", value: 1 },
    { op: "remove", path: "/list/-" },
    { op: "add", path: "/list/02", value: 1 },
    { op: "add", path: "/list/9", value: 1 },
    { op: "test", path: "/list/0", value: "wrong" },
    { op: "move", from: "/object", path: "/object/child" },
    { op: "copy", from: "/missing", path: "/new" },
    { op: "add", path: "/bad~2", value: 1 },
    { op: "add", path: "/__proto__/polluted", value: true },
    { op: "add", path: "/__proto__", value: { polluted: true } },
    { op: "copy", from: "/constructor", path: "/new" },
    { op: "unknown", path: "" },
  ])("rejects invalid operations atomically: %j", (bad) => {
    const state = { list: ["a"], object: {} };
    expect(() =>
      applyPatch(state, [{ op: "add", path: "/first", value: true }, bad]),
    ).toThrow();
    expect(state).toEqual({ list: ["a"], object: {} });
    expect(({} as Record<string, unknown>).polluted).toBeUndefined();
  });
});

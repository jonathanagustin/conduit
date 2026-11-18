import { publishAssistantContext } from "../../console/operator/assistant-rail.js";
import { expect, it } from "vite-plus/test";
import { createMountBindings } from "../../console/operator/mount-bindings.js";

it("releases exported state/actions and restores exact preexisting properties", () => {
  const original = () => "original";
  const target: Record<string, unknown> = {};
  Object.defineProperty(target, "action", {
    value: original,
    writable: true,
    configurable: true,
    enumerable: false,
  });
  const before = Object.getOwnPropertyDescriptor(target, "action");
  const owner = createMountBindings(target);
  owner.bindings.action = () => "mounted";
  owner.bindings.state = { retained: true };
  expect((target.action as () => string)()).toBe("mounted");
  owner.dispose();
  owner.dispose();
  expect(Object.getOwnPropertyDescriptor(target, "action")).toEqual(before);
  expect(Object.hasOwn(target, "state")).toBe(false);
  owner.bindings.state = { late: true };
  expect(Object.hasOwn(target, "state")).toBe(false);
});

it("does not let an older mount clear a newer mount or restore a disposed owner", () => {
  const target: Record<string, unknown> = {};
  const first = createMountBindings(target),
    second = createMountBindings(target);
  first.bindings.action = "first";
  second.bindings.action = "second";
  first.dispose();
  expect(target.action).toBe("second");
  second.dispose();
  expect(Object.hasOwn(target, "action")).toBe(false);
});

it("restores a still-active older owner after a newer owner disposes", () => {
  const target = { action: "baseline" };
  const first = createMountBindings(target),
    second = createMountBindings(target);
  first.bindings.action = "first";
  second.bindings.action = "second";
  second.dispose();
  expect(target.action).toBe("first");
  first.dispose();
  expect(target.action).toBe("baseline");
});

it("preserves an external replacement made during a mount", () => {
  const target = { action: "baseline" };
  const owner = createMountBindings(target);
  owner.bindings.action = "mounted";
  target.action = "external";
  owner.dispose();
  expect(target.action).toBe("external");
});

it("retains the latest value when an owner writes the same binding again", () => {
  const target = { action: "baseline" };
  const owner = createMountBindings(target);
  owner.bindings.action = "one";
  owner.bindings.action = "two";
  expect(target.action).toBe("two");
  owner.dispose();
  expect(target.action).toBe("baseline");
});

it("releases the assistant context through the same owner", () => {
  const target: Record<string, unknown> = {};
  const owner = createMountBindings(target);
  publishAssistantContext({ assistantMode: "explore" }, owner.bindings);
  expect(target.__conduitAssistantContext).toEqual({
    mode: "explore",
    chips: [],
  });
  owner.dispose();
  expect(Object.hasOwn(target, "__conduitAssistantContext")).toBe(false);
});

import { expect, it, vi } from "vite-plus/test";
import { bindKit } from "../../console/operator/kit.js";

function fixture() {
  class Puck extends EventTarget {
    style = { left: "", width: "", transform: "" };
    attributes = new Map([
      ["data-puck", "wave"],
      ["data-start", "2"],
    ]);
    getAttribute(name: string) {
      return this.attributes.get(name) ?? null;
    }
    setAttribute(name: string, value: string) {
      this.attributes.set(name, value);
    }
  }
  const puck = new Puck();
  const frame = Object.assign(new EventTarget(), { querySelector: () => puck });
  const root = { querySelector: () => frame, querySelectorAll: () => [puck] };
  const platform = new EventTarget();
  const add = vi.spyOn(platform, "addEventListener");
  const remove = vi.spyOn(platform, "removeEventListener");
  const store = {
    pucks: [
      {
        id: "wave",
        startHourUtc: 2,
        durationHours: 1,
        windowStart: 0,
        windowEnd: 24,
      },
    ],
  };
  const dispose = bindKit(root, store, platform as unknown as typeof window);
  const pointer = (target: EventTarget, type: string, clientX = 0) =>
    target.dispatchEvent(Object.assign(new Event(type), { clientX }));
  return { puck, frame, platform, add, remove, store, dispose, pointer };
}

it("preserves live movement and disposes an unfinished drag including queued callbacks", () => {
  const f = fixture();
  f.pointer(f.puck, "pointerdown", 10);
  const queuedMove = f.add.mock.calls.find(
    ([type]) => type === "pointermove",
  )![1] as EventListener;
  f.pointer(f.platform, "pointermove", 28);
  expect(f.store.pucks[0].startHourUtc).toBe(3);
  expect(f.puck.style.left).toBe("12.5%");
  f.dispose();
  f.dispose();
  for (const type of ["pointermove", "pointerup", "pointercancel"]) {
    expect(f.remove.mock.calls.filter(([name]) => name === type)).toHaveLength(
      1,
    );
  }
  f.pointer(f.platform, "pointermove", 46);
  queuedMove(Object.assign(new Event("pointermove"), { clientX: 46 }));
  f.pointer(f.puck, "pointerdown", 10);
  expect(f.store.pucks[0].startHourUtc).toBe(3);
  expect(
    f.add.mock.calls.filter(([name]) => name === "pointermove"),
  ).toHaveLength(1);
});

it.each(["pointerup", "pointercancel"])(
  "ends a drag on %s and permits a new drag",
  (end) => {
    const f = fixture();
    f.pointer(f.puck, "pointerdown");
    const queuedMove = f.add.mock.calls.find(
      ([type]) => type === "pointermove",
    )![1] as EventListener;
    f.pointer(f.platform, end);
    queuedMove(Object.assign(new Event("pointermove"), { clientX: 36 }));
    expect(f.store.pucks[0].startHourUtc).toBe(2);
    f.pointer(f.puck, "pointerdown");
    f.pointer(f.platform, "pointermove", 18);
    expect(f.store.pucks[0].startHourUtc).toBe(3);
    f.dispose();
  },
);

it("replaces active drags and disposes wheel bindings without a store", () => {
  const f = fixture();
  f.pointer(f.puck, "pointerdown");
  f.pointer(f.puck, "pointerdown");
  f.pointer(f.platform, "pointermove", 18);
  expect(f.store.pucks[0].startHourUtc).toBe(3);
  f.dispose();
  const dispose = bindKit(
    { querySelector: () => f.frame },
    null,
    f.platform as unknown as typeof window,
  );
  f.frame.dispatchEvent(
    Object.assign(new Event("wheel", { cancelable: true }), { deltaY: -1 }),
  );
  const scale = f.puck.style.transform;
  expect(scale).not.toBe("");
  dispose();
  f.frame.dispatchEvent(
    Object.assign(new Event("wheel", { cancelable: true }), { deltaY: -1 }),
  );
  expect(f.puck.style.transform).toBe(scale);
});

const owners = new WeakMap();

function sameDescriptor(left, right) {
  if (!left || !right) return left === right;
  return [
    "value",
    "get",
    "set",
    "writable",
    "enumerable",
    "configurable",
  ].every((key) => left[key] === right[key]);
}

// Export legacy inline actions without leaving a disposed mount on window.
// Per-property stacks allow an older mount to dispose after a newer one starts.
export function createMountBindings(target) {
  let active = true;
  let properties = owners.get(target);
  if (!properties) owners.set(target, (properties = new Map()));
  const installed = new Map();
  const bindings = new Proxy(Object.create(null), {
    get(_object, key) {
      return Reflect.get(target, key);
    },
    set(_object, key, value) {
      if (!active) return true;
      const before = Object.getOwnPropertyDescriptor(target, key);
      let ownership = properties.get(key);
      if (
        !ownership ||
        !sameDescriptor(before, ownership.records.at(-1)?.descriptor)
      ) {
        ownership = { baseline: before, records: [] };
        properties.set(key, ownership);
      }
      if (!Reflect.set(target, key, value)) return false;
      const previous = installed.get(key);
      if (previous?.ownership === ownership) {
        ownership.records = ownership.records.filter(
          (record) => record !== previous.record,
        );
      }
      const record = {
        descriptor: Object.getOwnPropertyDescriptor(target, key),
      };
      ownership.records.push(record);
      installed.set(key, { ownership, record });
      return true;
    },
  });
  return {
    bindings,
    dispose() {
      if (!active) return;
      active = false;
      for (const [key, { ownership, record }] of installed) {
        if (properties.get(key) !== ownership) continue;
        const top = ownership.records.at(-1);
        ownership.records = ownership.records.filter(
          (entry) => entry !== record,
        );
        if (
          top === record &&
          sameDescriptor(
            Object.getOwnPropertyDescriptor(target, key),
            record.descriptor,
          )
        ) {
          const restore =
            ownership.records.at(-1)?.descriptor || ownership.baseline;
          if (restore) Object.defineProperty(target, key, restore);
          else Reflect.deleteProperty(target, key);
        }
        if (!ownership.records.length) properties.delete(key);
      }
      installed.clear();
    },
  };
}

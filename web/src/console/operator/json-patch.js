// RFC 6902 updates are transactional: validate and mutate a private JSON copy.
// Pointer paths cannot traverse JavaScript prototype properties.
const own = (value, key) => Object.hasOwn(value, key);
const forbidden = new Set(["__proto__", "prototype", "constructor"]);
function fail() {
  throw new Error("Invalid agent state patch.");
}
function clone(value) {
  return JSON.parse(JSON.stringify(value));
}
function tokens(path) {
  if (typeof path !== "string" || path.length > 4096) fail();
  if (path === "") return [];
  if (!path.startsWith("/")) fail();
  return path
    .slice(1)
    .split("/")
    .map((part) => {
      if (/~(?![01])/u.test(part)) fail();
      const key = part.replace(/~1/g, "/").replace(/~0/g, "~");
      if (forbidden.has(key)) fail();
      return key;
    });
}
function index(array, key, insert) {
  if (key === "-" && insert) return array.length;
  if (!/^(0|[1-9][0-9]*)$/u.test(key)) fail();
  const n = Number(key);
  if (!Number.isSafeInteger(n) || n >= array.length + (insert ? 1 : 0)) fail();
  return n;
}
function at(root, path) {
  let value = root;
  for (const key of path) {
    if (!value || typeof value !== "object") fail();
    if (Array.isArray(value)) value = value[index(value, key, false)];
    else {
      if (!own(value, key)) fail();
      value = value[key];
    }
  }
  return value;
}
function edit(root, path, operation, value) {
  if (!path.length) {
    if (operation === "remove") return null;
    return clone(value);
  }
  const parent = at(root, path.slice(0, -1));
  const key = path.at(-1);
  if (!parent || typeof parent !== "object") fail();
  if (Array.isArray(parent)) {
    const n = index(parent, key, operation === "add");
    if (operation === "add") parent.splice(n, 0, clone(value));
    else if (operation === "remove") parent.splice(n, 1);
    else parent[n] = clone(value);
  } else {
    if (operation !== "add" && !own(parent, key)) fail();
    if (operation === "remove") delete parent[key];
    else parent[key] = clone(value);
  }
  return root;
}
function equal(a, b) {
  if (a === b) return true;
  if (!a || !b || typeof a !== "object" || typeof b !== "object") return false;
  if (Array.isArray(a) !== Array.isArray(b)) return false;
  const keys = Object.keys(a);
  return (
    keys.length === Object.keys(b).length &&
    keys.every((key) => own(b, key) && equal(a[key], b[key]))
  );
}
export function applyPatch(state, patch) {
  if (
    !Array.isArray(patch) ||
    patch.length > 256 ||
    JSON.stringify(patch).length > 1024 * 1024
  )
    fail();
  let next = clone(state);
  for (const item of patch) {
    if (!item || typeof item !== "object") fail();
    const path = tokens(item.path);
    if (["add", "replace", "test"].includes(item.op) && !own(item, "value"))
      fail();
    switch (item.op) {
      case "add":
      case "replace":
      case "remove":
        next = edit(next, path, item.op, item.value);
        break;
      case "test":
        if (!equal(at(next, path), item.value)) fail();
        break;
      case "copy":
      case "move": {
        const from = tokens(item.from);
        if (
          item.op === "move" &&
          from.length < path.length &&
          from.every((key, n) => path[n] === key)
        )
          fail();
        const value = clone(at(next, from));
        if (item.op === "move") next = edit(next, from, "remove");
        next = edit(next, path, "add", value);
        break;
      }
      default:
        fail();
    }
  }
  if (JSON.stringify(next).length > 4 * 1024 * 1024) fail();
  return next;
}

export function readPointer(state, pointer) {
  return at(state, tokens(pointer));
}

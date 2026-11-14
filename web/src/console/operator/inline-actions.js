// Runs the console's declarative event attributes without inline script.
//
// The Hub serves the console under `script-src 'self'`, which blocks inline
// event handlers. Views therefore write `data-onclick="..."` (and the input,
// change, keydown, focus, mouseenter, and mouseleave equivalents) and this
// module runs them through one delegated listener per event type.
//
// The attribute text is a small statement language, not JavaScript. It covers
// what the views write: calls to functions the console puts on `window`,
// assignments into `state`, `this.value` / `this.checked`, `event` members,
// `!`, `===` / `!==`, `&&` / `||`, `?:`, and `if (...) {...} else ...`.
// Nothing here builds code from strings, and prototype keys are refused.

const BLOCKED_KEYS = new Set(["constructor", "__proto__", "prototype"]);

export const EVENT_ATTRIBUTES = Object.freeze({
  click: "data-onclick",
  input: "data-oninput",
  change: "data-onchange",
  keydown: "data-onkeydown",
  focusin: "data-onfocus",
  mouseover: "data-onmouseenter",
  mouseout: "data-onmouseleave",
});

function tokenize(source) {
  const tokens = [];
  let i = 0;
  while (i < source.length) {
    const ch = source[i];
    if (/\s/.test(ch)) {
      i += 1;
      continue;
    }
    if (ch === "'" || ch === '"') {
      let value = "";
      i += 1;
      while (i < source.length && source[i] !== ch) {
        if (source[i] === "\\" && i + 1 < source.length) {
          value += source[i + 1];
          i += 2;
          continue;
        }
        value += source[i];
        i += 1;
      }
      if (source[i] !== ch) throw new SyntaxError("unterminated string");
      i += 1;
      tokens.push({ type: "str", value });
      continue;
    }
    const number = /^\d+(\.\d+)?/.exec(source.slice(i));
    if (number) {
      tokens.push({ type: "num", value: Number(number[0]) });
      i += number[0].length;
      continue;
    }
    const word = /^[A-Za-z_$][\w$]*/.exec(source.slice(i));
    if (word) {
      tokens.push({ type: "id", value: word[0] });
      i += word[0].length;
      continue;
    }
    const op = /^(===|!==|&&|\|\||[=!?:;.,()[\]{}])/.exec(source.slice(i));
    if (!op) throw new SyntaxError(`unexpected "${ch}"`);
    tokens.push({ type: "op", value: op[0] });
    i += op[0].length;
  }
  return tokens;
}

/** Parses attribute text into a tree. Throws SyntaxError on anything else. */
export function parseAction(source) {
  const tokens = tokenize(String(source ?? ""));
  let pos = 0;
  const peek = (value) =>
    pos < tokens.length &&
    tokens[pos].type !== "str" &&
    tokens[pos].value === value;
  const take = (value) => {
    if (!peek(value)) throw new SyntaxError(`expected "${value}"`);
    pos += 1;
  };

  function program(end) {
    const body = [];
    while (pos < tokens.length && !(end && peek(end))) {
      if (peek(";")) {
        pos += 1;
        continue;
      }
      body.push(statement());
    }
    return { type: "block", body };
  }
  function block() {
    take("{");
    const body = program("}");
    take("}");
    return body;
  }
  function statement() {
    if (peek("if") && tokens[pos].type === "id") {
      pos += 1;
      take("(");
      const test = expression();
      take(")");
      const then = peek("{") ? block() : statement();
      let otherwise = null;
      if (peek("else") && tokens[pos].type === "id") {
        pos += 1;
        otherwise = peek("{") ? block() : statement();
      }
      return { type: "if", test, consequent: then, otherwise };
    }
    return expression();
  }
  function expression() {
    const left = ternary();
    if (peek("=")) {
      pos += 1;
      if (left.type !== "member" && left.type !== "name") {
        throw new SyntaxError("can only assign to a name or member");
      }
      return { type: "assign", target: left, value: expression() };
    }
    return left;
  }
  function ternary() {
    const test = logical();
    if (!peek("?")) return test;
    pos += 1;
    const then = expression();
    take(":");
    return { type: "cond", test, consequent: then, otherwise: expression() };
  }
  function logical() {
    let left = equality();
    while (peek("&&") || peek("||")) {
      const op = tokens[pos].value;
      pos += 1;
      left = { type: "logical", op, left, right: equality() };
    }
    return left;
  }
  function equality() {
    let left = unary();
    while (peek("===") || peek("!==")) {
      const op = tokens[pos].value;
      pos += 1;
      left = { type: "compare", op, left, right: unary() };
    }
    return left;
  }
  function unary() {
    if (peek("!")) {
      pos += 1;
      return { type: "not", value: unary() };
    }
    return postfix();
  }
  function postfix() {
    let node = primary();
    for (;;) {
      if (peek(".")) {
        pos += 1;
        const name = tokens[pos];
        if (!name || name.type !== "id")
          throw new SyntaxError("expected a name");
        pos += 1;
        node = {
          type: "member",
          object: node,
          key: { type: "lit", value: name.value },
        };
      } else if (peek("[")) {
        pos += 1;
        const key = expression();
        take("]");
        node = { type: "member", object: node, key };
      } else if (peek("(")) {
        pos += 1;
        const args = [];
        while (!peek(")")) {
          args.push(expression());
          if (!peek(")")) take(",");
        }
        take(")");
        node = { type: "call", callee: node, args };
      } else {
        return node;
      }
    }
  }
  function primary() {
    const token = tokens[pos];
    if (!token) throw new SyntaxError("unexpected end");
    pos += 1;
    if (token.type === "str" || token.type === "num")
      return { type: "lit", value: token.value };
    if (token.type === "id") {
      if (token.value === "true") return { type: "lit", value: true };
      if (token.value === "false") return { type: "lit", value: false };
      if (token.value === "null") return { type: "lit", value: null };
      return { type: "name", name: token.value };
    }
    if (token.value === "(") {
      const inner = expression();
      take(")");
      return inner;
    }
    throw new SyntaxError(`unexpected "${token.value}"`);
  }

  const tree = program(null);
  if (pos !== tokens.length) throw new SyntaxError("trailing input");
  return tree;
}

function key(value) {
  const name = String(value);
  if (BLOCKED_KEYS.has(name)) throw new Error(`"${name}" is not allowed`);
  return name;
}

/** Runs a parsed tree. `scope` supplies this, event, and global lookups. */
export function runAction(tree, scope) {
  const lookup = (name) => {
    if (name === "this") return scope.self;
    if (name === "event") return scope.event;
    if (name === "document") return scope.document;
    return scope.globals[key(name)];
  };
  function evaluate(node) {
    switch (node.type) {
      case "block": {
        let last;
        for (const statement of node.body) last = evaluate(statement);
        return last;
      }
      case "if":
        if (evaluate(node.test)) return evaluate(node.consequent);
        return node.otherwise ? evaluate(node.otherwise) : undefined;
      case "lit":
        return node.value;
      case "name":
        return lookup(node.name);
      case "member": {
        const object = evaluate(node.object);
        return object == null ? undefined : object[key(evaluate(node.key))];
      }
      case "not":
        return !evaluate(node.value);
      case "compare": {
        const same = evaluate(node.left) === evaluate(node.right);
        return node.op === "===" ? same : !same;
      }
      case "logical": {
        const left = evaluate(node.left);
        if (node.op === "&&") return left && evaluate(node.right);
        return left || evaluate(node.right);
      }
      case "cond":
        return evaluate(node.test)
          ? evaluate(node.consequent)
          : evaluate(node.otherwise);
      case "assign": {
        const value = evaluate(node.value);
        if (node.target.type === "name") {
          scope.globals[key(node.target.name)] = value;
        } else {
          const object = evaluate(node.target.object);
          if (object == null) throw new Error("cannot assign into nothing");
          object[key(evaluate(node.target.key))] = value;
        }
        return value;
      }
      case "call": {
        let receiver;
        let fn;
        if (node.callee.type === "member") {
          receiver = evaluate(node.callee.object);
          fn =
            receiver == null
              ? undefined
              : receiver[key(evaluate(node.callee.key))];
        } else {
          fn = evaluate(node.callee);
        }
        if (typeof fn !== "function") throw new Error("not a function");
        return fn.apply(receiver, node.args.map(evaluate));
      }
      default:
        throw new Error(`unknown node ${node.type}`);
    }
  }
  return evaluate(tree);
}

const cache = new Map();
function compiled(source) {
  let tree = cache.get(source);
  if (!tree) {
    tree = parseAction(source);
    if (cache.size > 500) cache.clear();
    cache.set(source, tree);
  }
  return tree;
}

/**
 * Installs one listener per event type on `root` and returns a cleanup.
 * Click handlers run from the target up through its ancestors, as inline
 * handlers would, and stop when one calls event.stopPropagation().
 */
export function installInlineActions(root, globals = window, doc = document) {
  const run = (element, attribute, event) => {
    const source = element.getAttribute(attribute);
    if (!source) return;
    try {
      runAction(compiled(source), {
        self: element,
        event,
        globals,
        document: doc,
      });
    } catch (error) {
      console.error(`console action failed: ${attribute}="${source}"`, error);
    }
  };
  const handlers = {
    click(event) {
      let element = event.target instanceof Element ? event.target : null;
      while (element && element !== root.parentNode) {
        if (element.hasAttribute?.("data-onclick")) {
          run(element, "data-onclick", event);
          if (event.cancelBubble) break;
        }
        element = element.parentElement;
      }
    },
    mouseover(event) {
      const element =
        event.target instanceof Element
          ? event.target.closest("[data-onmouseenter]")
          : null;
      if (!element || element.contains(event.relatedTarget)) return;
      run(element, "data-onmouseenter", event);
    },
    mouseout(event) {
      const element =
        event.target instanceof Element
          ? event.target.closest("[data-onmouseleave]")
          : null;
      if (!element || element.contains(event.relatedTarget)) return;
      run(element, "data-onmouseleave", event);
    },
  };
  for (const type of ["input", "change", "keydown", "focusin"]) {
    handlers[type] = (event) => {
      const element = event.target;
      if (
        element instanceof Element &&
        element.hasAttribute(EVENT_ATTRIBUTES[type])
      ) {
        run(element, EVENT_ATTRIBUTES[type], event);
      }
    };
  }
  for (const [type, handler] of Object.entries(handlers))
    root.addEventListener(type, handler);
  return () => {
    for (const [type, handler] of Object.entries(handlers))
      root.removeEventListener(type, handler);
  };
}

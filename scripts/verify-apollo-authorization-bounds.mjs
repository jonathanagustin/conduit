#!/usr/bin/env node
// Fails when the two Apollo authorization artifacts stop agreeing with the
// generators that are supposed to produce them.
//
// Neither artifact can be regenerated here. `apollo-authorization-vocabulary.json`
// names a source capture at a path that does not exist in this repository, and
// `apollo-authorization-graph.json` needs a live tenant credential. So the
// generator and its output are two copies of the same thing that nothing was
// comparing, and they had already drifted: three fields in the committed graph
// -- `rolesWithNoRetainedOperationsNote`, `rolesWithNoTransitiveOperations` and
// `rolesWithNoTransitiveOperationsNote` -- were hand-added after generation and
// the script emitted none of them. A regeneration would have silently deleted
// all three, taking with it the only statement in the file that
// apollo-hubs:administrator authorizes nothing recoverable.
//
// That is the drift class this gate closes, in two directions:
//
//   1. every top-level field in a committed artifact must be one the generator
//      emits, or be named in HAND_AUTHORED below -- an explicit, reviewed list
//      somebody has to edit on purpose. Silence becomes a declaration.
//   2. every string constant the generator hard-codes must appear byte-identical
//      at the same path in the artifact. Prose edited in one copy and not the
//      other is exactly what a regeneration reverts.
//
// On top of that it holds the bound #122 asked for. Both artifacts partition
// Apollo's authorization surface on the `apollo-` identifier prefix, and neither
// said so, so a reader who grepped either file for a resource family and found
// nothing had no way to tell "filtered out" from "does not exist" -- and the
// second reading is the natural one for a file named *apollo*-authorization-*.
// The bound is now stated in both, and the check below requires it to stay:
// present, complete, keyed to the generator's own PREFIX constant, and worded
// identically across the two files so a reader can diff them.
//
// What this gate does NOT do: it cannot tell you the artifact's DATA is what the
// tenant serves. Only a re-capture does that, and for the vocabulary not even
// that -- its source capture is gone. This gate is about the artifacts agreeing
// with the code that claims to write them.
//
// Set APOLLO_BOUNDS_ROOT to point every path below at another tree; the
// self-test uses it to plant violations without touching the working copy.
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), "..");
const root = process.env.APOLLO_BOUNDS_ROOT || repoRoot;

const PAIRS = [
  {
    label: "authorization graph",
    artifact: "docs/evidence/apollo-authorization-graph.json",
    generator: "scripts/capture-apollo-authorization-graph.mjs",
    language: "js",
    // The object literal the generator serializes, and nothing else in the file.
    anchor: "const evidence = {",
    // Fields present in the committed artifact that the generator does not
    // emit. Empty on purpose: the three that used to belong here are emitted
    // now. Adding a name here is a decision that a regeneration will delete the
    // field, and it should be made in review rather than by omission.
    handAuthored: [],
  },
  {
    label: "authorization vocabulary",
    artifact: "docs/evidence/apollo-authorization-vocabulary.json",
    generator: "scripts/extract-apollo-authorization-vocabulary.py",
    language: "py",
    anchor: "evidence = {",
    handAuthored: [],
  },
];

const failures = [];
const fail = (message) => failures.push(message);

// --- reading the generator ------------------------------------------------
// Both generators build one object literal and serialize it verbatim, so the
// literal is a readable specification of the artifact's shape. This walks it
// rather than matching lines, because indentation is not a contract and a
// reformat must not be able to switch the gate off.

/** Lazy token stream over `source` from `start`, skipping comments. */
function tokenizer(source, start, language) {
  let i = start;
  const lineComment = language === "py" ? "#" : "//";
  const quotes = language === "py" ? ['"""', "'''", '"', "'"] : ['"', "'", "`"];

  const skipTrivia = () => {
    for (;;) {
      while (i < source.length && /\s/.test(source[i])) i += 1;
      if (source.startsWith(lineComment, i)) {
        while (i < source.length && source[i] !== "\n") i += 1;
        continue;
      }
      if (language === "js" && source.startsWith("/*", i)) {
        const end = source.indexOf("*/", i + 2);
        i = end === -1 ? source.length : end + 2;
        continue;
      }
      return;
    }
  };

  const readString = (quote) => {
    i += quote.length;
    let value = "";
    while (i < source.length && !source.startsWith(quote, i)) {
      if (source[i] === "\\") {
        const escape = source[i + 1];
        const table = { n: "\n", t: "\t", r: "\r", "\\": "\\", '"': '"', "'": "'", "`": "`", 0: "\0" };
        value += Object.prototype.hasOwnProperty.call(table, escape) ? table[escape] : escape;
        i += 2;
        continue;
      }
      value += source[i];
      i += 1;
    }
    i += quote.length;
    return { type: "string", value };
  };

  return () => {
    skipTrivia();
    if (i >= source.length) return { type: "eof", value: "" };
    for (const quote of quotes) {
      if (source.startsWith(quote, i)) return readString(quote);
    }
    const char = source[i];
    if ("{}[](),:".includes(char)) {
      i += 1;
      return { type: "punct", value: char };
    }
    const identifier = /^[A-Za-z_$][A-Za-z0-9_$]*/.exec(source.slice(i));
    if (identifier) {
      i += identifier[0].length;
      return { type: "ident", value: identifier[0] };
    }
    i += 1;
    return { type: "other", value: char };
  };
}

/** Top-level `NAME = "literal"` constants, so `prefix: PREFIX` resolves. */
function literalConstants(source, language) {
  const pattern =
    language === "py"
      ? /^([A-Z][A-Z0-9_]*) = "((?:[^"\\]|\\.)*)"\s*$/gm
      : /^const ([A-Z][A-Z0-9_]*) = "((?:[^"\\]|\\.)*)";\s*$/gm;
  const constants = new Map();
  for (const [, name, value] of source.matchAll(pattern)) {
    constants.set(name, value.replace(/\\(.)/g, "$1"));
  }
  return constants;
}

/**
 * Walk the object literal at `anchor`. Returns the top-level keys in source
 * order and every value that is a pure string expression, keyed by dotted path.
 *
 * A "pure string expression" is one or more string literals, optionally
 * parenthesized and optionally joined by `+`, or a bare reference to a
 * top-level string constant. Python's implicit concatenation and JavaScript's
 * `+` both reduce to the same thing. Anything else -- a call, a count, a
 * comprehension -- is opaque, and opaque is fine: the gate simply makes no
 * claim about it.
 */
function readObjectLiteral(source, anchor, language, constants) {
  const anchorAt = source.indexOf(anchor);
  if (anchorAt === -1) throw new Error(`cannot find \`${anchor}\`; the generator has changed shape`);
  const next = tokenizer(source, anchorAt + anchor.length, language);
  const topLevelKeys = [];
  const strings = new Map();
  // A parser that stops making progress hangs the gate, and a gate that hangs
  // gets killed rather than read. Every advance is counted, so a shape this
  // walker does not understand fails loudly and finitely instead.
  let budget = 400000;
  const advance = () => {
    if ((budget -= 1) <= 0) throw new Error(`gave up walking \`${anchor}\`; the generator has changed shape`);
    return next();
  };
  let token = advance();

  // Consume a value. `path` is null inside constructs we do not index.
  const readValue = (path, depth) => {
    if (token.type === "punct" && token.value === "{") {
      token = advance();
      readMembers(path, depth + 1);
      return;
    }
    if (token.type === "punct" && token.value === "[") {
      token = advance();
      let index = 0;
      while (!(token.type === "punct" && token.value === "]") && token.type !== "eof") {
        if (token.type === "punct" && token.value === ",") {
          token = advance();
          continue;
        }
        if (token.type === "punct" && (token.value === ")" || token.value === "}")) {
          token = advance();
          continue;
        }
        readValue(path === null ? null : `${path}.${index}`, depth + 1);
        index += 1;
      }
      token = advance();
      return;
    }
    // A simple expression: everything up to a `,` or a closing bracket that
    // belongs to the enclosing container.
    const pieces = [];
    let pure = true;
    let parens = 0;
    for (;;) {
      if (token.type === "eof") break;
      if (token.type === "punct") {
        if (token.value === "(") parens += 1;
        else if (token.value === ")") parens -= 1;
        else if (parens === 0 && (token.value === "," || token.value === "}" || token.value === "]")) break;
        else if (token.value === "{" || token.value === "[") pure = false;
        token = advance();
        continue;
      }
      if (token.type === "string") pieces.push(token.value);
      else if (token.type === "ident" && constants.has(token.value)) pieces.push(constants.get(token.value));
      else if (token.type === "other" && token.value === "+") {
        /* JavaScript concatenation; contributes nothing itself */
      } else pure = false;
      token = advance();
    }
    if (pure && pieces.length && path !== null) strings.set(path, pieces.join(""));
  };

  // Consume members until the object closes. Assumes `token` is the first one.
  function readMembers(path, depth) {
    for (;;) {
      if (token.type === "eof") return;
      if (token.type === "punct" && token.value === "}") {
        token = advance();
        return;
      }
      if (token.type === "punct" && token.value === ",") {
        token = advance();
        continue;
      }
      if (token.type === "string" || token.type === "ident") {
        const key = token.value;
        const after = advance();
        if (after.type === "punct" && after.value === ":") {
          if (depth === 1) topLevelKeys.push(key);
          token = advance();
          readValue(path === null ? null : path ? `${path}.${key}` : key, depth);
          continue;
        }
        // JavaScript shorthand `{ rolesWithNoTransitiveOperations, }`.
        if (after.type === "punct" && (after.value === "," || after.value === "}")) {
          if (depth === 1) topLevelKeys.push(key);
          token = after;
          continue;
        }
        token = after;
      }
      // Not a member we understand -- a dict comprehension, say. Skip it
      // wholesale rather than guessing, and keep the walk in sync. The stepped
      // counter is what guarantees the outer loop cannot spin on a token this
      // block declines to consume.
      let nesting = 0;
      let stepped = 0;
      for (;;) {
        if (token.type === "eof") return;
        if (token.type === "punct") {
          if ("{[(".includes(token.value)) nesting += 1;
          else if ("}])".includes(token.value)) {
            if (nesting === 0) break;
            nesting -= 1;
          } else if (token.value === "," && nesting === 0) break;
        }
        token = advance();
        stepped += 1;
      }
      if (stepped === 0) token = advance();
    }
  }

  // The anchor ends with the opening brace, so the first token is a member.
  readMembers("", 1);
  return { topLevelKeys, strings };
}

const at = (document, path) =>
  path.split(".").reduce((node, segment) => (node == null ? undefined : node[segment]), document);

const preview = (text) => (text.length > 70 ? `${text.slice(0, 67)}...` : text);

// --- the checks -----------------------------------------------------------

const artifacts = new Map();

for (const pair of PAIRS) {
  let artifact;
  let generatorSource;
  try {
    artifact = JSON.parse(readFileSync(join(root, pair.artifact), "utf8"));
    generatorSource = readFileSync(join(root, pair.generator), "utf8");
  } catch (error) {
    fail(`${pair.label}: could not read both sides -- ${error.message}`);
    continue;
  }
  artifacts.set(pair.label, { artifact, source: generatorSource });

  let emitted;
  try {
    emitted = readObjectLiteral(
      generatorSource,
      pair.anchor,
      pair.language,
      literalConstants(generatorSource, pair.language),
    );
  } catch (error) {
    fail(`${pair.label}: ${error.message}`);
    continue;
  }
  artifacts.get(pair.label).emitted = emitted;

  const generated = new Set(emitted.topLevelKeys);
  const declared = new Set(pair.handAuthored);

  for (const key of Object.keys(artifact)) {
    if (generated.has(key) || declared.has(key)) continue;
    fail(
      `${pair.label}: \`${key}\` is in ${pair.artifact} but ${pair.generator} does not emit it, ` +
        `so regenerating would delete it. Emit it from the generator, or add it to handAuthored ` +
        `in this gate to say on the record that it is expected to be lost.`,
    );
  }
  for (const key of generated) {
    if (key in artifact) continue;
    fail(
      `${pair.label}: ${pair.generator} emits \`${key}\` and ${pair.artifact} has no such field, ` +
        `so the committed artifact predates the generator.`,
    );
  }
  for (const key of declared) {
    if (key in artifact) continue;
    fail(`${pair.label}: handAuthored names \`${key}\`, which is not in ${pair.artifact}.`);
  }

  for (const [path, expected] of emitted.strings) {
    const actual = at(artifact, path);
    if (actual === undefined) {
      fail(`${pair.label}: ${pair.generator} hard-codes \`${path}\` and the artifact has no value there.`);
      continue;
    }
    if (actual !== expected) {
      fail(
        `${pair.label}: \`${path}\` differs between generator and artifact, so a regeneration ` +
          `would rewrite it.\n    generator: ${preview(expected)}\n    artifact:  ${preview(String(actual))}`,
      );
    }
  }
}

// --- the bound #122 asked for ---------------------------------------------
// Everything above is generic. This part is the specific claim: both files
// filter on `apollo-`, and both have to keep saying so. A future editor who
// deletes the statement because it reads like boilerplate takes the file back
// to the state where absence looks like a finding.

const bounds = [];
for (const pair of PAIRS) {
  const entry = artifacts.get(pair.label);
  if (!entry) continue;
  const partition = entry.artifact.identifierPartition;
  if (!partition || typeof partition !== "object") {
    fail(
      `${pair.label}: no \`identifierPartition\`. Both artifacts filter on the apollo- prefix ` +
        `before anything is written, and a file that does not say so lets a missing family read ` +
        `as a finding rather than as the filter (#122).`,
    );
    continue;
  }
  for (const field of ["prefix", "filter", "bound", "reportedNotEstablished"]) {
    if (typeof partition[field] === "string" && partition[field].length > 0) continue;
    fail(`${pair.label}: identifierPartition.${field} is missing or empty.`);
  }
  const constants = literalConstants(entry.source, pair.language);
  const declaredPrefix = constants.get("PREFIX");
  if (declaredPrefix === undefined) {
    fail(`${pair.label}: ${pair.generator} no longer declares a PREFIX constant to check against.`);
  } else if (partition.prefix !== declaredPrefix) {
    fail(
      `${pair.label}: identifierPartition.prefix is \`${partition.prefix}\` but ${pair.generator} ` +
        `filters on \`${declaredPrefix}\`. The stated bound must be the filter that ran.`,
    );
  }
  if (typeof partition.bound === "string") bounds.push([pair.label, partition.bound]);
  // The bound must survive as a bound, not decay into a description of the file.
  for (const required of ["filter", "not as a finding"]) {
    if (String(partition.bound ?? "").includes(required)) continue;
    fail(`${pair.label}: identifierPartition.bound no longer says "${required}".`);
  }
}
if (bounds.length === 2 && bounds[0][1] !== bounds[1][1]) {
  fail(
    `the two artifacts state different bounds. They apply the same partition, so the wording is ` +
      `shared on purpose -- a reader comparing them must not have to decide whether a difference ` +
      `in the prose means a difference in the filter.`,
  );
}

// --- the second, smaller drift #122 records --------------------------------
// The vocabulary quotes /roles/v2/all at 167 roles. An independent capture three
// weeks earlier reported 173, and the same table shows up in the graph capture
// with twelve more apollo- roles again. A count with no capture attached to it
// is the thing that misleads, so the file has to carry the caveat.

const vocabulary = artifacts.get("authorization vocabulary")?.artifact;
if (vocabulary) {
  const caveat = vocabulary.countsCaveat;
  if (typeof caveat !== "string" || caveat.length === 0) {
    fail(
      `authorization vocabulary: no \`countsCaveat\`. contributingEndpoints quotes ` +
        `/multipass/api/roles/v2/all at a fixed total and the artifact records no capture date, ` +
        `so every one of those totals needs the caveat beside it (#122).`,
    );
  } else {
    for (const required of ["drifts", "capture date"]) {
      if (caveat.includes(required)) continue;
      fail(`authorization vocabulary: countsCaveat no longer says "${required}".`);
    }
  }
  const notRecovered = (vocabulary.notRecovered ?? []).join("\n");
  if (!notRecovered.includes("identifierPartition")) {
    fail(
      `authorization vocabulary: notRecovered does not point at identifierPartition. That list is ` +
        `where a reader looks for what the capture could not establish, and the unprefixed ` +
        `families belong in it.`,
    );
  }
}

// The drift the caveat describes is re-derivable from the two committed files,
// and it is the only half of #122's count claim that is. Check it here so the
// prose cannot outlive the evidence: the vocabulary's apollo- roles must remain
// a strict subset of the later graph capture's.
const graph = artifacts.get("authorization graph")?.artifact;
if (vocabulary && graph) {
  const vocabularyRoles = new Set(vocabulary.vocabulary?.roles ?? []);
  const graphRoles = new Set(Object.keys(graph.graph?.roles ?? {}));
  const onlyInVocabulary = [...vocabularyRoles].filter((id) => !graphRoles.has(id));
  if (onlyInVocabulary.length) {
    fail(
      `the vocabulary carries ${onlyInVocabulary.length} apollo- role(s) the later graph capture ` +
        `does not, so the two captures no longer nest and countsCaveat's account of the drift is ` +
        `wrong: ${onlyInVocabulary.slice(0, 4).join(", ")}`,
    );
  }
  if (graphRoles.size <= vocabularyRoles.size) {
    fail(
      `countsCaveat says the role table grew between the two captures, but the graph holds ` +
        `${graphRoles.size} apollo- roles against the vocabulary's ${vocabularyRoles.size}.`,
    );
  }
}

if (failures.length) {
  for (const failure of failures) console.error(`FAIL Apollo authorization bounds: ${failure}`);
  process.exit(1);
}

const [graphPair, vocabularyPair] = PAIRS;
const graphFields = artifacts.get(graphPair.label)?.emitted.topLevelKeys.length ?? 0;
const vocabularyFields = artifacts.get(vocabularyPair.label)?.emitted.topLevelKeys.length ?? 0;
const pinned = PAIRS.reduce((sum, pair) => sum + (artifacts.get(pair.label)?.emitted.strings.size ?? 0), 0);
console.log(
  `PASS Apollo authorization bounds: ${graphFields} + ${vocabularyFields} generated fields match the ` +
    `committed artifacts with 0 hand-authored exceptions, ${pinned} generator string constant(s) are ` +
    `byte-identical, and both artifacts state the apollo- prefix partition as a bound`,
);

#!/usr/bin/env node
// Recovers the Apollo Hub's GraphQL contract from an authorized Hub, and
// records only the contract.
//
// The Hub's introspection is deliberately thin: its Query type advertises one
// field, `me`, and names none of the Apollo surface. But the surface resolves
// when addressed by name, and the server validates every document it is sent.
// That is the whole method -- ask whether a name exists and let validation
// answer:
//
//   FieldUndefined        the candidate is not a field. Recorded as absent.
//   MissingFieldArgument  the field exists and names a required argument.
//   WrongType             the argument's exact type, quoted back by the server.
//   SubSelectionRequired  the field returns a composite rather than a scalar.
//
// Candidates come from `candidates.json`, which is committed and reviewable. A
// name that is not a field is reported undefined and never reaches the observed
// schema, so this cannot invent a surface -- it can only fail to find one,
// which the completeness report states rather than hides.
//
// Three safety properties, enforced here rather than left to a caller.
//
// Nothing executes during discovery. Every discovery document carries a
// guaranteed-invalid field, so the document fails validation as a whole and the
// server runs none of it while still reporting what it validated. Only the
// return-type probe executes, and it selects `__typename` alone on read-only
// query fields -- the response can carry a type name and nothing else.
//
// The guard is also a canary, and this is what makes the method fail closed.
// "No error for this candidate" is evidence the candidate exists ONLY if the
// server actually validated the document. A malformed document returns a parse
// error and no field errors at all, which reads identically to "every candidate
// exists". So every discovery response must contain the guard's own
// FieldUndefined error; if it does not, the probe aborts instead of inferring.
// An earlier revision of this script lacked that check, produced an unbalanced
// document, and duly reported fifteen invented fields on two types.
//
// No tenant data is written. This records field names, type names, argument
// names and argument types. It never records a response body, an object, a RID,
// a Hub origin, or a credential. `verify-observed-schema.mjs` re-checks that
// offline at the commit boundary.
//
// Usage:
//   APOLLO_GRAPHQL_URL=... APOLLO_GRAPHQL_TOKEN=... \
//     node scripts/apollo-graphql/probe-apollo-schema.mjs --out <observed.json>
import { readFileSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const GUARD = "__conduitGuardFieldThatCannotExist";
const MAXIMUM_DEPTH = 3;

function usage() {
  return [
    "Usage: APOLLO_GRAPHQL_URL=<hub> APOLLO_GRAPHQL_TOKEN=<token> \\",
    "  node scripts/apollo-graphql/probe-apollo-schema.mjs --out <observed.json> [--candidates <file>]",
    "",
    "The URL and token are read from the environment and never written anywhere.",
  ].join("\n");
}

function parseArgs(argv) {
  const options = { candidates: resolve(HERE, "candidates.json") };
  for (let index = 0; index < argv.length; index += 1) {
    const flag = argv[index];
    if (flag === "--help") return { help: true };
    if (!["--out", "--candidates"].includes(flag)) throw new Error(`Unknown argument: ${flag}`);
    const value = argv[index + 1];
    index += 1;
    if (!value) throw new Error(`${flag} requires a value`);
    options[flag.slice(2)] = resolve(value);
  }
  if (!options.out) throw new Error("--out is required");
  return options;
}

// A path is a list of selection segments, each already carrying its arguments.
// Rendering closes every brace it opens, because a document that does not parse
// produces no field errors and would otherwise read as universal success.
function renderDocument(path, selection, { guarded }) {
  const opened = path.map((segment) => `${segment} {`).join(" ");
  const closed = "}".repeat(path.length);
  return `query { ${opened} ${selection} ${closed} ${guarded ? GUARD : ""} }`.replace(/\s+/g, " ").trim();
}

function newClient(endpoint, token) {
  let requests = 0;
  return {
    get requestCount() {
      return requests;
    },
    async query(document) {
      requests += 1;
      const response = await fetch(endpoint, {
        method: "POST",
        headers: { authorization: `Bearer ${token}`, "content-type": "application/json" },
        body: JSON.stringify({ query: document }),
      });
      if (!response.ok) throw new Error(`Hub returned HTTP ${response.status}`);
      return response.json();
    },
  };
}

const messagesOf = (payload) => (payload.errors ?? []).map((error) => String(error.message ?? ""));

// The canary. Absence of the guard's error means the server never got as far as
// validating fields, so nothing in this response is evidence about anything.
function assertValidated(messages, document) {
  const validated = messages.some((message) => message.includes("FieldUndefined") && message.includes(GUARD));
  if (validated) return;
  const first = messages[0] ?? "(no errors returned)";
  throw new Error(
    `guard error absent -- the Hub did not validate this document, so no inference is safe.\n` +
      `  first message: ${first.slice(0, 200)}\n` +
      `  document: ${document.slice(0, 200)}`,
  );
}

async function discoverFields(client, path, ownerType, candidates) {
  const document = renderDocument(path, candidates.join(" "), { guarded: true });
  const messages = messagesOf(await client.query(document));
  assertValidated(messages, document);

  const absent = new Set();
  const shapes = new Map();
  for (const message of messages) {
    const owner = message.match(/in type '([^']+)'/)?.[1];
    const field = message.match(/Field '([^']+)' in type/)?.[1];
    if (message.includes("FieldUndefined") && field && field !== GUARD) {
      // Only believe an "undefined" that names the type being probed.
      if (!owner || owner === ownerType) absent.add(field);
    }
    const target = message.match(/@\[([^\]]+)\]/)?.[1]?.split("/").pop();
    if (message.includes("MissingFieldArgument") && target) shapes.set(target, "requires-arguments");
    if (/Sub[Ss]election[Rr]equired/.test(message) && target) shapes.set(target, "composite");
  }
  const present = [];
  for (const candidate of candidates) {
    if (absent.has(candidate)) continue;
    present.push({ name: candidate, shape: shapes.get(candidate) ?? "scalar" });
  }
  return { present, absent: [...absent].sort() };
}

async function requiredArguments(client, path, ownerType, field) {
  const document = renderDocument(path, field, { guarded: true });
  const messages = messagesOf(await client.query(document));
  assertValidated(messages, document);
  return messages
    .filter((message) => message.includes("MissingFieldArgument") && message.includes(`/${field}]`))
    .map((message) => message.match(/argument '([^']+)'/)?.[1])
    .filter(Boolean)
    .sort();
}

// An object literal can never coerce to a scalar argument, so the server quotes
// the argument's declared type back while still executing nothing.
async function argumentType(client, path, field, argument) {
  const document = renderDocument(path, `${field}(${argument}: {conduitProbe: 1}) { __typename }`, { guarded: true });
  const messages = messagesOf(await client.query(document));
  assertValidated(messages, document);
  for (const message of messages) {
    if (!message.includes("WrongType")) continue;
    const declared = message.match(/is not a valid '([^']+)'/)?.[1];
    if (declared) return declared;
  }
  return null;
}

// The one probe that runs. `__typename` alone: a type name and nothing else.
async function returnTypeName(client, path, call) {
  const payload = await client.query(renderDocument(path, `${call} { __typename }`, { guarded: false }));
  const walk = (value) => {
    if (!value || typeof value !== "object") return null;
    if (typeof value.__typename === "string") return value.__typename;
    for (const nested of Object.values(value)) {
      const found = walk(nested);
      if (found) return found;
    }
    return null;
  };
  return walk(payload.data ?? null);
}

// A probe value for a required argument, chosen so the request stays
// well-formed without naming any real tenant resource.
function probeLiteral(typeName) {
  if (!typeName) return '"conduit-probe"';
  if (/^\[/.test(typeName)) return "[]";
  if (/Int|Long|Float|Number|Size|Count/i.test(typeName)) return "1";
  if (/Bool/i.test(typeName)) return "false";
  return '"conduit-probe"';
}

const callFor = (field) =>
  field.arguments.length
    ? `${field.name}(${field.arguments.map((a) => `${a.name}: ${probeLiteral(a.type)}`).join(", ")})`
    : field.name;

async function recoverType(client, { path, typeName, candidates, types, diagnostics, depth }) {
  if (types.has(typeName) || depth > MAXIMUM_DEPTH) return;
  const vocabulary = /Page|Connection|List|Results/i.test(typeName)
    ? candidates.pageFieldCandidates
    : depth === 0
      ? candidates.fieldCandidates
      : candidates.nodeFieldCandidates;

  let discovery;
  try {
    discovery = await discoverFields(client, path, typeName, vocabulary);
  } catch (error) {
    diagnostics.push(`fields not recovered for ${typeName}: ${error.message.split("\n")[0]}`);
    return;
  }
  types.set(typeName, { name: typeName, fields: [], absentCandidates: discovery.absent });

  const fields = [];
  for (const { name, shape } of discovery.present.sort((left, right) => left.name.localeCompare(right.name))) {
    const field = { name, shape, arguments: [], returns: null };
    if (shape === "requires-arguments" || shape === "composite") {
      let required = [];
      try {
        required = await requiredArguments(client, path, typeName, name);
      } catch (error) {
        diagnostics.push(`arguments not recovered for ${typeName}.${name}: ${error.message.split("\n")[0]}`);
      }
      for (const argument of required) {
        const declared = await argumentType(client, path, name, argument).catch(() => null);
        if (!declared) diagnostics.push(`argument type not recovered: ${typeName}.${name}(${argument})`);
        field.arguments.push({ name: argument, type: declared, required: true });
      }
    }
    if (shape !== "scalar" && field.arguments.every((argument) => argument.type)) {
      field.returns = await returnTypeName(client, path, callFor(field)).catch(() => null);
      if (!field.returns) diagnostics.push(`return type not recovered: ${typeName}.${name}`);
    }
    fields.push(field);
  }
  types.get(typeName).fields = fields;

  for (const field of fields) {
    if (!field.returns) continue;
    await recoverType(client, {
      path: [...path, callFor(field)],
      typeName: field.returns,
      candidates,
      types,
      diagnostics,
      depth: depth + 1,
    });
  }
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  if (options.help) {
    console.log(usage());
    return;
  }
  const endpoint = process.env.APOLLO_GRAPHQL_URL;
  const token = process.env.APOLLO_GRAPHQL_TOKEN;
  if (!endpoint || !token) {
    console.error(`APOLLO_GRAPHQL_URL and APOLLO_GRAPHQL_TOKEN must be set.\n\n${usage()}`);
    process.exit(2);
  }

  const candidates = JSON.parse(readFileSync(options.candidates, "utf8"));
  const client = newClient(endpoint, token);
  const types = new Map();
  const diagnostics = [];
  await recoverType(client, {
    path: [candidates.rootField],
    typeName: candidates.rootType,
    candidates,
    types,
    diagnostics,
    depth: 0,
  });

  const recovered = [...types.values()].sort((left, right) => left.name.localeCompare(right.name));
  const fieldsRecovered = recovered.reduce((total, type) => total + type.fields.length, 0);
  const observed = {
    schemaVersion: 1,
    // The Hub is identified by a salted digest of its origin so two captures of
    // the same Hub are comparable without the origin being committed.
    hubFingerprint: createHash("sha256").update(`conduit-apollo-hub ${new URL(endpoint).origin}`).digest("hex"),
    method: "validation-probe",
    candidatesSha256: createHash("sha256").update(readFileSync(options.candidates)).digest("hex"),
    rootField: candidates.rootField,
    rootType: candidates.rootType,
    types: recovered,
    completeness: {
      typesRecovered: recovered.length,
      fieldsRecovered,
      argumentsRecovered: recovered.reduce(
        (total, type) => total + type.fields.reduce((count, field) => count + field.arguments.length, 0),
        0,
      ),
      requests: client.requestCount,
      unresolved: diagnostics.length,
    },
    diagnostics: diagnostics.sort(),
  };

  writeFileSync(options.out, `${JSON.stringify(observed, null, 2)}\n`);
  console.log(
    `recovered ${fieldsRecovered} field(s) across ${recovered.length} type(s) ` +
      `in ${client.requestCount} request(s); ${diagnostics.length} unresolved`,
  );
  if (!fieldsRecovered) {
    console.error("no fields were recovered; the Hub exposed nothing this candidate set names");
    process.exit(1);
  }
}

await main();

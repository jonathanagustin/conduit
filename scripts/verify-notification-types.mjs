#!/usr/bin/env node
// Binds the notification vocabulary in Go to the capture it was read from.
//
// The vocabulary is the whole of what was recovered here. Apollo does not
// publish this API, so the type labels, the two channels and the three contact
// kinds are the contract, and they came off screenshots that are not committed
// and cannot be re-read by a test. Without this gate the identifiers are free to
// drift back toward whatever seems reasonable, which is where they started.
//
// Every identifier is derived from the published label by one rule -- uppercase,
// non-alphanumeric runs to underscores -- so the check is mechanical rather than
// a second hand-maintained list that could disagree with both.
//
// Runs offline against committed files and needs no credentials.
import { existsSync, readFileSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(HERE, "..");
const CAPTURE = resolve(ROOT, "docs/evidence/apollo-notification-types.json");
const SOURCE = resolve(ROOT, "internal/notifications/domain/notifications.go");

const failures = [];
const fail = (message) => failures.push(message);

for (const path of [CAPTURE, SOURCE]) {
  if (!existsSync(path)) {
    console.error(`FAIL missing ${relative(ROOT, path)}`);
    process.exit(1);
  }
}

const capture = JSON.parse(readFileSync(CAPTURE, "utf8"));
const source = readFileSync(SOURCE, "utf8");

const identifier = (label) =>
  label.trim().toUpperCase().replace(/[^A-Z0-9]+/g, "_").replace(/^_|_$/g, "");

// Constant values declared for a given Go type, read from the const blocks.
const declared = (goType) => {
  const found = new Set();
  for (const match of source.matchAll(
    new RegExp(`^\\s*\\w+\\s+${goType}\\s*=\\s*"([^"]+)"`, "gm"),
  )) {
    found.add(match[1]);
  }
  return found;
};

const compare = (what, expected, actual) => {
  for (const value of expected) {
    if (!actual.has(value)) {
      fail(`${what}: the capture records ${value}, which ${relative(ROOT, SOURCE)} does not declare`);
    }
  }
  for (const value of actual) {
    if (!expected.has(value)) {
      fail(
        `${what}: ${relative(ROOT, SOURCE)} declares ${value}, which the capture does not record. ` +
          `Add the evidence before the constant, or the vocabulary is invention again`,
      );
    }
  }
};

// 1. Event types, and the scope each was observed at. Scope is provenance, so a
// type filed under the wrong one is a citation error even though nothing
// behaves differently.
const expectedTypes = new Map();
for (const entry of capture.vocabulary?.productScopedTypes ?? []) {
  expectedTypes.set(identifier(entry.label), "ScopeProduct");
}
for (const entry of capture.vocabulary?.crossResourceTypes ?? []) {
  expectedTypes.set(identifier(entry.label), "ScopeCrossResource");
}
if (expectedTypes.size === 0) fail("the capture records no notification event types at all");
compare("event types", new Set(expectedTypes.keys()), declared("EventType"));

for (const [value, scope] of expectedTypes) {
  const entry = source.match(new RegExp(`Event\\w+:\\s*${scope},[^\\n]*`, "g"));
  if (!entry) continue;
  const constantName = source.match(new RegExp(`(\\w+)\\s+EventType\\s*=\\s*"${value}"`))?.[1];
  if (!constantName) continue;
  if (!new RegExp(`${constantName}:\\s*${scope}\\b`).test(source)) {
    fail(`event type ${value} was observed at ${scope} in the capture, but the source files it elsewhere`);
  }
}

// 2. Channels and contact kinds. These are separate on purpose: Slack and
// PagerDuty are destinations a team owns, not channels a resource can express a
// preference on, and collapsing them would promise per-resource Slack settings
// no captured page shows.
compare(
  "channels",
  new Set((capture.deliveryChannels?.perResourcePreference ?? []).map(identifier)),
  declared("Channel"),
);
compare(
  "contact kinds",
  new Set((capture.deliveryChannels?.teamContactKinds ?? []).map((entry) => identifier(entry.label))),
  declared("ContactKind"),
);

// 3. The open-vocabulary property, which is the one the previous implementation
// got wrong and the one a future edit is most likely to quietly undo -- closing
// the set looks tidier and every test still passes except the one that matters.
if (capture.vocabulary?.isOpen !== true) {
  fail("the capture no longer records the vocabulary as open; that is a contract change, not a correction");
}
if (!/AllFutureTypes\s+bool/.test(source)) {
  fail(
    "the source has no AllFutureTypes flag. Apollo's settings page opts a subscriber into types added later; " +
      "without it every type Apollo adds reaches nobody",
  );
}
if (!/if\s+preference\.AllFutureTypes\s*\{\s*\n\s*return true/.test(source)) {
  fail(
    "AllFutureTypes no longer short-circuits the type check in Matches. Expanding it into the known set " +
      "freezes the answer at this build's vocabulary, which is the bug it exists to prevent",
  );
}

// 4. PagerDuty is creatable and does not deliver. Both halves are Apollo's, and
// a later reader will be tempted to fix the contradiction in one direction.
if (!/func \(kind ContactKind\) Deliverable\(\) bool/.test(source)) {
  fail("ContactKind no longer distinguishes deliverable kinds, so the PagerDuty contradiction has been resolved silently");
}

if (failures.length) {
  for (const failure of failures) console.error(`FAIL ${failure}`);
  process.exit(1);
}
console.log(
  `PASS notification vocabulary: ${expectedTypes.size} event types, ` +
    `${declared("Channel").size} channels and ${declared("ContactKind").size} contact kinds match the capture`,
);

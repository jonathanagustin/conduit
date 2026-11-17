#!/usr/bin/env node
// Fails when the console's remaining Hub contract drifts.
//
// Two contracts are shared between Go and the console.
//
// The tenant bootstrap: the schema version and the meta name are written in
// Go, injected into the shell, and parsed by the console before it renders. If
// those two strings diverge, the Hub serves a document the console refuses,
// which looks like a broken deployment. An invented version and a renamed meta
// are both drift.
//
// The closed vocabularies in web/src/console/operator/hub-vocabulary.js: the
// notification event types the preference editor offers as checkboxes, the
// channels and contact kinds it sends, and the session application ids that
// gate navigation. Each is compared to the Go source as a set, in both
// directions: a Go value the console does not list is a checkbox or an
// application the operator never sees, and a console value Go does not define
// is a request the Hub refuses or a navigation entry that can never appear.
// The preference editor must also build its checkboxes from that list, and the
// navigation table must name only application ids in it.
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const read = (relative) => readFileSync(resolve(ROOT, relative), "utf8");

function capture(text, pattern, file) {
  const match = pattern.exec(text);
  if (!match) throw new Error(`could not read the contract string in ${file}`);
  return match[1];
}

const pairs = [
  {
    name: "bootstrap schema version",
    go: capture(read("internal/consoleui/bootstrap.go"), /BootstrapSchemaVersion\s*=\s*"([^"]+)"/, "internal/consoleui/bootstrap.go"),
    ui: capture(read("web/src/runtime/bootstrap.ts"), /BOOTSTRAP_SCHEMA_VERSION\s*=\s*"([^"]+)"/, "web/src/runtime/bootstrap.ts"),
  },
  {
    name: "bootstrap meta name",
    go: capture(read("internal/consoleui/bootstrap.go"), /BootstrapMetaName\s*=\s*"([^"]+)"/, "internal/consoleui/bootstrap.go"),
    ui: capture(read("web/src/runtime/bootstrap.ts"), /BOOTSTRAP_META_NAME\s*=\s*"([^"]+)"/, "web/src/runtime/bootstrap.ts"),
  },
];

const failures = [];
for (const pair of pairs) {
  if (pair.go !== pair.ui) {
    failures.push(`${pair.name}: Go says "${pair.go}" and the console says "${pair.ui}"`);
  }
}

function captureAll(text, pattern, file) {
  const values = [...text.matchAll(pattern)].map((match) => match[1]);
  if (!values.length) throw new Error(`could not read a vocabulary in ${file}`);
  return values;
}

const NOTIFICATIONS_GO = "internal/notifications/domain/notifications.go";
const SESSION_GO = "internal/controlplane/session.go";
const OPERATOR = "web/src/console/operator";
const vocabulary = await import(pathToFileURL(resolve(ROOT, OPERATOR, "hub-vocabulary.js")).href);
const notificationsGo = read(NOTIFICATIONS_GO);
const sets = [
  {
    name: "notification event types",
    go: captureAll(notificationsGo, /\bEvent\w+\s+EventType\s*=\s*"([^"]+)"/g, NOTIFICATIONS_GO),
    ui: vocabulary.EVENT_TYPES,
  },
  {
    name: "notification channels",
    go: captureAll(notificationsGo, /\bChannel\w+\s+Channel\s*=\s*"([^"]+)"/g, NOTIFICATIONS_GO),
    ui: vocabulary.CHANNELS,
  },
  {
    name: "notification contact kinds",
    go: captureAll(notificationsGo, /\bContact\w+\s+ContactKind\s*=\s*"([^"]+)"/g, NOTIFICATIONS_GO),
    ui: vocabulary.CONTACT_KINDS,
  },
  {
    name: "session applications",
    go: captureAll(read(SESSION_GO), /\{ID:\s*"([^"]+)",\s*Enabled:/g, SESSION_GO),
    ui: vocabulary.HUB_APPLICATIONS,
  },
];
for (const set of sets) {
  if (!Array.isArray(set.ui)) {
    failures.push(`${set.name}: hub-vocabulary.js does not export the list`);
    continue;
  }
  const missing = set.go.filter((value) => !set.ui.includes(value));
  const invented = set.ui.filter((value) => !set.go.includes(value));
  if (new Set(set.ui).size !== set.ui.length) failures.push(`${set.name}: the console lists a value twice`);
  if (missing.length) failures.push(`${set.name}: Go defines ${missing.join(", ")} and the console does not list it`);
  if (invented.length) failures.push(`${set.name}: the console lists ${invented.join(", ")} and Go does not define it`);
}

// The lists are only a contract if the screens are built from them.
const notificationsUI = read(`${OPERATOR}/notifications.js`);
if (
  !/EVENT_TYPES\s*\.filter\([\s\S]{0,200}?type="checkbox"\s+data-ntf-type=/.test(notificationsUI) ||
  notificationsUI.split('type="checkbox" data-ntf-type=').length !== 2
) {
  failures.push("notification event types: the preference editor does not build its type checkboxes from EVENT_TYPES alone");
}
const capabilitiesUI = read(`${OPERATOR}/session-capabilities.js`);
const navTable = /NAV_APPLICATIONS\s*=\s*Object\.freeze\(\{([\s\S]*?)\}\);/.exec(capabilitiesUI);
if (!navTable) {
  failures.push("session applications: the navigation table NAV_APPLICATIONS was not found");
} else {
  const mapped = [...navTable[1].matchAll(/:\s*\[([^\]]*)\]/g)].flatMap((match) =>
    [...match[1].matchAll(/"([^"]+)"/g)].map((id) => id[1]),
  );
  if (!mapped.length) failures.push("session applications: the navigation table maps nothing");
  const unknown = mapped.filter((id) => !sets[3].go.includes(id));
  if (unknown.length) failures.push(`session applications: navigation is mapped to ${unknown.join(", ")}, which the Hub never returns`);
}

if (failures.length) {
  for (const failure of failures) console.error(`FAIL console contract: ${failure}`);
  process.exit(1);
}
console.log(`PASS console contract: ${[...pairs, ...sets].map((item) => item.name).join(", ")}`);

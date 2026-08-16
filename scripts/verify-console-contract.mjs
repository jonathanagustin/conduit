#!/usr/bin/env node
// Fails when a console status vocabulary drifts from the Hub's Go source.
//
// The console hand-writes small tables and switches that turn a Hub status
// string into a colour or a route. Nothing tied those to the Go constants they
// mirror, so they drifted: a table keyed on an invented value ("IMPORTED",
// "SUPERSEDED", "RELINQUISHED") while omitting the real one, so a genuine
// BLOCKED rollout or RECORDED plan rendered as an unstyled tag -- and the
// browser tests passed, because their fixtures used the same invented values.
// A fixture cannot vouch for a value the Hub never sends; only the Go source
// can, so this reads the Go source directly on every candidate.
//
// It checks the value SET, not the rendering: for each vocabulary it extracts
// the set the Hub emits (the Go consts) and the set the console handles (the
// map keys / switch cases), and fails when either has a value the other does
// not. An invented console value is a dead branch; a missing one renders
// neutral where a real status needed a colour. Both are caught here.
//
// Scope: the const-backed vocabularies, where the Go set extracts unambiguously
// by naming convention. Import-level statuses (PENDING_APPROVAL/ADMITTED/ERROR)
// are inline string literals rather than named consts in the Go, so they are
// not extracted here; they are pinned by unit tests instead
// (web/src/test/unit/logic.test.ts). If those become named consts, add them.
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const read = (relative) => readFileSync(resolve(ROOT, relative), "utf8");

// Return the text between the first match of `start` and the first match of
// `end` after it. A vocabulary lives inside a declaration, so pulling the block
// first stops a regex from matching an unrelated const or a second map in the
// same file.
function block(text, start, end) {
  const opened = start.exec(text);
  if (!opened) return null;
  const rest = text.slice(opened.index + opened[0].length);
  const closed = end.exec(rest);
  if (!closed) return null;
  return rest.slice(0, closed.index);
}

// Every distinct capture-group-1 match of `pattern` in `text`, as a sorted set.
function values(text, pattern) {
  const found = new Set();
  for (const match of text.matchAll(pattern)) found.add(match[1]);
  return found;
}

// A vocabulary: the set the Hub emits (Go) and the set the console handles.
// `go`/`console` each say which file to read, an optional block to scope to,
// and the pattern whose first group is a value.
const VOCABULARIES = [
  {
    name: "fleet rollout status",
    go: { file: "internal/environments/domain/reporting.go", pattern: /\bRollout\w+\s*=\s*"([A-Z_]+)"/g },
    console: {
      file: "web/src/features/fleet/statusTags.tsx",
      start: /export const ROLLOUT_INTENT[^{]*\{/,
      end: /\n\};/,
      pattern: /^\s*([A-Z_]+):/gm,
    },
  },
  {
    name: "fleet config status",
    go: { file: "internal/environments/domain/reporting.go", pattern: /\bConfig\w+\s*=\s*"([A-Z_]+)"/g },
    console: {
      file: "web/src/features/fleet/statusTags.tsx",
      start: /export const CONFIG_INTENT[^{]*\{/,
      end: /\n\};/,
      pattern: /^\s*([A-Z_]+):/gm,
    },
  },
  {
    name: "fleet lifecycle",
    go: { file: "internal/environments/domain/reporting.go", pattern: /\bLifecycle\w+\s*=\s*"([A-Z_]+)"/g },
    console: {
      file: "web/src/features/fleet/statusTags.tsx",
      start: /export const LIFECYCLE_INTENT[^{]*\{/,
      end: /\n\};/,
      pattern: /^\s*([A-Z_]+):/gm,
    },
  },
  {
    name: "fleet health",
    go: { file: "internal/environments/domain/reporting.go", pattern: /\bHealth\w+\s*=\s*"([A-Z_]+)"/g },
    console: {
      file: "web/src/features/fleet/statusTags.tsx",
      start: /export const HEALTH_INTENT[^{]*\{/,
      end: /\n\};/,
      pattern: /^\s*([A-Z_]+):/gm,
    },
  },
  {
    name: "plan status",
    go: { file: "internal/controlplane/lifecycle.go", pattern: /\bPlan\w+\s+PlanStatus\s*=\s*"([A-Z_]+)"/g },
    console: {
      file: "web/src/features/plans/PlansPage.tsx",
      start: /export const PLAN_INTENT[^{]*\{/,
      end: /\n\};/,
      pattern: /^\s*([A-Z_]+):/gm,
    },
  },
  {
    name: "export pipeline status",
    go: { file: "internal/controlplane/bundles.go", pattern: /ExportPipeline(?:Active|Archived|Deleted)\s*=\s*"([A-Z_]+)"/g },
    console: {
      file: "web/src/api/exports.ts",
      start: /export function pipelineStatusIntent/,
      end: /\n\}/,
      pattern: /case "([A-Z_]+)":/g,
    },
  },
  {
    name: "session application ids",
    go: {
      file: "internal/controlplane/session.go",
      start: /Applications: \[\]SessionApplication\{/,
      end: /\n\t\t\},/,
      pattern: /ID:\s*"([a-z-]+)"/g,
    },
    console: {
      file: "web/src/app/navigation.ts",
      start: /export const APPLICATION_DESTINATIONS[^[]*\[/,
      end: /\n\];/,
      pattern: /id:\s*"([a-z-]+)"/g,
    },
  },
];

function extract(spec) {
  let text = read(spec.file);
  if (spec.start) {
    text = block(text, spec.start, spec.end);
    if (text === null) {
      throw new Error(`could not locate the ${spec.start} block in ${spec.file}; the gate can no longer read it`);
    }
  }
  const found = values(text, spec.pattern);
  if (found.size === 0) {
    throw new Error(`extracted no values from ${spec.file} with ${spec.pattern}; the source has changed shape`);
  }
  return found;
}

const failures = [];
for (const vocabulary of VOCABULARIES) {
  let hub, ui;
  try {
    hub = extract(vocabulary.go);
    ui = extract(vocabulary.console);
  } catch (error) {
    failures.push(`${vocabulary.name}: ${error.message}`);
    continue;
  }
  const invented = [...ui].filter((value) => !hub.has(value)).sort();
  const missing = [...hub].filter((value) => !ui.has(value)).sort();
  if (invented.length) {
    failures.push(
      `${vocabulary.name}: the console handles ${invented.map((v) => `"${v}"`).join(", ")}, ` +
        `which the Hub never emits (${vocabulary.go.file})`,
    );
  }
  if (missing.length) {
    failures.push(
      `${vocabulary.name}: the console omits ${missing.map((v) => `"${v}"`).join(", ")}, ` +
        `which the Hub emits (${vocabulary.go.file}) -- a real status would render unstyled`,
    );
  }
}

if (failures.length) {
  for (const failure of failures) console.error(`FAIL console contract: ${failure}`);
  process.exit(1);
}
console.log(`PASS console contract: ${VOCABULARIES.length} status vocabularies match the Hub's Go source`);

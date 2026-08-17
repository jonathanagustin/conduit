#!/usr/bin/env node
// Guards the invention register.
//
// This repository used to refuse any Apollo-facing shape it could not ground in
// evidence. That rule was reversed deliberately: a missing feature is now built
// on an educated guess rather than declined. The reversal is only safe if two
// things hold for as long as the guess lives -- it stays distinguishable from a
// recovery, and the arrival of real evidence is an obligation to reimplement.
//
// Neither survives as a convention. The first erodes the moment someone reads an
// invented shape, finds it reasonable, and cites it as though it were recovered;
// the second erodes because nobody is watching for evidence that would embarrass
// a decision they made. So both are checked here.
//
// What this cannot do is notice that evidence has landed. Nothing can, without a
// crawl. What it can do is make the register and the source disagree loudly the
// moment either moves, and refuse an invention that never said what would settle
// it -- because an invention with no displacement condition is not a guess, it is
// just an assertion with better manners.
//
// Runs offline against committed files and needs no credentials.
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(HERE, "..");
const REGISTER = resolve(ROOT, "docs/evidence/conduit-inventions.json");

const failures = [];
const fail = (message) => failures.push(message);

if (!existsSync(REGISTER)) {
  console.error(`FAIL missing ${relative(ROOT, REGISTER)}`);
  process.exit(1);
}
const register = JSON.parse(readFileSync(REGISTER, "utf8"));
const entries = register.inventions ?? [];

// 1. Find every marker in the source. Read from tracked files so an untracked
// scratch file cannot register an invention, and so a deleted file's markers
// disappear with it.
const tracked = execFileSync("git", ["ls-files", "-z"], { cwd: ROOT, encoding: "utf8" })
  .split("\0")
  .filter((path) => path.endsWith(".go") && !path.endsWith("_test.go"));

const found = new Map();
for (const path of tracked) {
  const lines = readFileSync(resolve(ROOT, path), "utf8").split("\n");
  for (let index = 0; index < lines.length; index += 1) {
    const marker = lines[index].match(/^\s*\/\/\s*CONDUIT-INVENTED:\s*(\S+)\s*$/);
    if (!marker) continue;
    const id = marker[1];
    if (found.has(id)) {
      fail(`invention ${id} is marked in two places: ${found.get(id).path} and ${path}`);
      continue;
    }
    // The displacement condition must sit on the very next line. Adjacency is
    // the point: a condition three paragraphs away gets edited independently of
    // the thing it governs, which is how a stale one survives.
    const next = (lines[index + 1] ?? "").match(/^\s*\/\/\s*Displaced-by:\s*(.+?)\s*$/);
    if (!next || next[1].length < 20) {
      fail(
        `invention ${id} at ${path}:${index + 1} has no Displaced-by line naming what would settle it; ` +
          `an invention with no displacement condition is an assertion, not a guess`,
      );
      continue;
    }
    found.set(id, { path, line: index + 1, displacedBy: next[1] });
  }
}

// 2. Both directions. A marker with no entry means somebody invented something
// and did not record the risk; an entry with no marker means the register is
// describing code that no longer exists, which is worse -- it reads as coverage.
const registered = new Set(entries.map((entry) => entry.id));
for (const [id, where] of found) {
  if (!registered.has(id)) {
    fail(`${where.path}:${where.line} marks invention ${id}, which ${relative(ROOT, REGISTER)} does not record`);
  }
}
for (const entry of entries) {
  if (!found.has(entry.id)) {
    fail(
      `the register records invention ${entry.id}, which no source marker claims. ` +
        `If it was displaced, delete the entry in the change that reimplemented it`,
    );
  }
}

// 3. Every entry has to carry the four things a later reader needs, and the
// riskIfWrong field is the one most likely to be skipped -- it is also the only
// one that helps somebody decide what to displace first.
for (const entry of entries) {
  for (const field of ["what", "why", "displacedBy", "riskIfWrong"]) {
    if (!entry[field] || String(entry[field]).trim().length < 20) {
      fail(`invention ${entry.id} has no meaningful ${field}`);
    }
  }
  const marker = found.get(entry.id);
  if (marker && !entry.displacedBy.toLowerCase().includes(marker.displacedBy.slice(0, 24).toLowerCase())) {
    // Compared on a prefix rather than exactly: the source comment wraps and the
    // register does not, so demanding equality would fail on line breaks and
    // teach people to keep the condition short enough to fit.
    fail(
      `invention ${entry.id} states a different displacement condition in source and register:\n` +
        `  ${marker.path}:${marker.line}: ${marker.displacedBy}\n  register: ${entry.displacedBy}`,
    );
  }
}

// 4. The procedure is what makes the register more than a list, and its third
// step is the one that would quietly go missing -- deleting the entry in the
// same change as the reimplementation is what stops this file becoming a museum.
const procedure = (register.displacementProcedure ?? []).join("\n").toLowerCase();
for (const [needle, why] of [
  ["reimplement", "displacement means reimplementing against the evidence, not adapting the guess to fit"],
  ["same change", "the entry and the code must move together or the register goes stale"],
  ["got wrong", "recording what the invention got wrong is the only feedback the next guess gets"],
]) {
  if (!procedure.includes(needle)) fail(`displacementProcedure no longer says to ${needle}: ${why}`);
}

if (failures.length) {
  for (const failure of failures) console.error(`FAIL ${failure}`);
  process.exit(1);
}
console.log(
  `PASS Conduit inventions: ${found.size} marked and registered, each naming the evidence that would displace it`,
);

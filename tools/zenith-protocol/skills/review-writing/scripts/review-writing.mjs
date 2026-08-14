#!/usr/bin/env node
import { readFileSync } from "node:fs";

const args = process.argv.slice(2);
const file = args.find((arg) => !arg.startsWith("--"));
const json = args.includes("--json");
if (!file) {
  process.stderr.write("usage: review-writing.mjs <file> [--json]\n");
  process.exit(2);
}

const text = readFileSync(file, "utf8");
const lines = text.split(/\r?\n/);
const words = text.match(/[\p{L}\p{N}'’-]+/gu) ?? [];
const patterns = [
  ["vocabulary", "promotional or stock vocabulary", /\b(delve|showcasing|tapestry|pivotal|meticulous|groundbreaking|seamless(?:ly)?|multifaceted|transformative|cutting-edge|best-in-class)\b/giu],
  ["phrase", "formulaic opening or conclusion", /\b(in today['’]s world|in this (?:article|guide|post)|let['’]s (?:dive|delve)|in conclusion|to sum up|at the end of the day)\b/giu],
  ["phrase", "vague attribution", /\b(experts (?:argue|say)|observers note|studies suggest|many agree|it['’]s (?:important|worth) to (?:note|mention|remember|consider))\b/giu],
  ["phrase", "importance or legacy puffery", /\b(?:stands|serves) as a testament\b|\bplays a (?:vital|crucial|pivotal|significant) role\b|\bunderscor(?:e|es|ing) (?:its|the) (?:importance|significance)\b/giu],
  ["structure", "forced negative parallelism", /\bnot (?:just|simply|merely) .{1,50},? but\b|\bnot only .{1,80}but also\b|\bmore than (?:just|simply|merely)\b/giu],
  ["structure", "inline-header bullet", /^\s*[-*]\s+\*\*[^*]+:\*\*/gu],
];

const findings = [];
for (let index = 0; index < lines.length; index += 1) {
  for (const [category, label, expression] of patterns) {
    expression.lastIndex = 0;
    for (const match of lines[index].matchAll(expression)) {
      findings.push({ category, label, line: index + 1, match: match[0] });
    }
  }
}
const emDashes = (text.match(/—/gu) ?? []).length;
const emDashRatePer1000 = words.length === 0 ? 0 : emDashes * 1000 / words.length;
if (emDashRatePer1000 > 5) {
  findings.push({ category: "structure", label: "em-dash density above 5 per 1000 words", line: null, match: `${emDashes} em-dashes` });
}
const categories = [...new Set(findings.map((finding) => finding.category))].sort();
const densityScore = findings.length + Math.max(0, categories.length - 1);
const confidence = densityScore >= 6 ? "HIGH" : densityScore >= 3 ? "MEDIUM" : "LOW";
const report = {
  file,
  wordCount: words.length,
  shortInput: words.length < 100,
  emDashes,
  emDashRatePer1000: Number(emDashRatePer1000.toFixed(2)),
  patternCount: findings.length,
  categories,
  confidence,
  confidenceMeaning: "pattern density only; not evidence of authorship",
  findings,
};

if (json) {
  process.stdout.write(`${JSON.stringify(report, null, 2)}\n`);
} else {
  process.stdout.write(`# Writing pattern review: ${file}\n\n`);
  process.stdout.write(`- Confidence: ${confidence} (pattern density only; not evidence of authorship)\n`);
  process.stdout.write(`- Words: ${words.length}${report.shortInput ? " (short input; interpret cautiously)" : ""}\n`);
  process.stdout.write(`- Patterns: ${findings.length} across ${categories.length} categories\n`);
  process.stdout.write(`- Em-dashes: ${emDashes} (${report.emDashRatePer1000} per 1000 words)\n\n`);
  for (const finding of findings) {
    const location = finding.line === null ? "document" : `line ${finding.line}`;
    process.stdout.write(`- ${location}: ${finding.label}: ${JSON.stringify(finding.match)}\n`);
  }
}

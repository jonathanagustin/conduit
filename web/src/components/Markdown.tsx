import type { ReactNode } from "react";

/** A dependency-free renderer for the Markdown subset an operator authors in an
 * Environment README (internal/environments/domain profile, `README` up to
 * 64 KiB). The README is privileged content -- it only lands through a
 * Change-Request-gated profile apply -- but this renderer is still built to be
 * injection-proof by construction: parseMarkdown produces a data model and the
 * component turns that model into React elements. No branch ever reaches
 * dangerouslySetInnerHTML, so a README can never introduce markup the parser did
 * not recognise, and an unrecognised link scheme degrades to plain text rather
 * than becoming a live `javascript:` href.
 *
 * The supported subset is what a service README actually uses: ATX headings,
 * paragraphs, fenced code blocks, unordered and ordered lists, and the inline
 * spans strong / emphasis / code / link. Anything outside the subset renders as
 * its literal text, which is the honest failure mode for documentation. The
 * parser is exported and unit-pinned in logic.test.ts; the component is a thin
 * projection of it. */

/** One inline span within a heading, paragraph, or list item. */
export type MarkdownSpan =
  | { kind: "text"; text: string }
  | { kind: "strong"; text: string }
  | { kind: "emphasis"; text: string }
  | { kind: "code"; text: string }
  | { kind: "link"; text: string; href: string };

/** One block-level element. Heading levels are clamped to 3-5 so a README can
 * never emit an h1/h2 that competes with the page's own object header. */
export type MarkdownBlock =
  | { kind: "heading"; level: 3 | 4 | 5; spans: MarkdownSpan[] }
  | { kind: "paragraph"; spans: MarkdownSpan[] }
  | { kind: "list"; ordered: boolean; items: MarkdownSpan[][] }
  | { kind: "code"; text: string };

const HEADING = /^(#{1,6})\s+(.*)$/;
const UNORDERED_ITEM = /^\s*[-*]\s+(.*)$/;
const ORDERED_ITEM = /^\s*\d+\.\s+(.*)$/;
const FENCE = /^\s*```/;
const LINK = /^\[([^\]]*)\]\(([^)\s]+)\)/;

/** True when a link target is safe to render as a live href. Only the schemes a
 * README legitimately needs are allowed; a relative path or fragment is kept,
 * and everything else (notably `javascript:` and `data:`) is refused so an
 * authored link cannot become script. */
function isSafeHref(href: string): boolean {
  const trimmed = href.trim();
  if (trimmed === "") {
    return false;
  }
  if (trimmed.startsWith("/") || trimmed.startsWith("#") || trimmed.startsWith("./") || trimmed.startsWith("../")) {
    return true;
  }
  return /^(https?:|mailto:)/i.test(trimmed);
}

/** Split one line of text into inline spans. Code spans win first so that
 * emphasis markers inside backticks stay literal; links are matched next; then
 * strong before emphasis so `**x**` is not read as two emphasis markers. An
 * unmatched marker is treated as ordinary text, which keeps prose containing a
 * lone `*` or `[` intact. */
export function parseInline(source: string): MarkdownSpan[] {
  const spans: MarkdownSpan[] = [];
  let text = "";
  const flush = (): void => {
    if (text !== "") {
      spans.push({ kind: "text", text });
      text = "";
    }
  };
  let index = 0;
  while (index < source.length) {
    const rest = source.slice(index);
    if (rest.startsWith("`")) {
      const end = rest.indexOf("`", 1);
      if (end > 0) {
        flush();
        spans.push({ kind: "code", text: rest.slice(1, end) });
        index += end + 1;
        continue;
      }
    }
    if (rest.startsWith("[")) {
      const match = LINK.exec(rest);
      if (match !== null) {
        const whole = match[0];
        const label = match[1] ?? "";
        const href = match[2] ?? "";
        flush();
        if (isSafeHref(href)) {
          spans.push({ kind: "link", text: label, href });
        } else {
          spans.push({ kind: "text", text: label });
        }
        index += whole.length;
        continue;
      }
    }
    if (rest.startsWith("**")) {
      const end = rest.indexOf("**", 2);
      if (end > 0) {
        flush();
        spans.push({ kind: "strong", text: rest.slice(2, end) });
        index += end + 2;
        continue;
      }
    }
    if (rest.startsWith("*")) {
      const end = rest.indexOf("*", 1);
      if (end > 0) {
        flush();
        spans.push({ kind: "emphasis", text: rest.slice(1, end) });
        index += end + 1;
        continue;
      }
    }
    text += source[index] ?? "";
    index += 1;
  }
  flush();
  return spans;
}

function headingLevel(hashes: number): 3 | 4 | 5 {
  if (hashes <= 1) {
    return 3;
  }
  if (hashes === 2) {
    return 4;
  }
  return 5;
}

/** Parse the README subset into blocks. Blank lines separate paragraphs; a
 * fenced block runs to its closing fence (or end of input); consecutive list
 * items of the same ordering coalesce into one list. */
export function parseMarkdown(source: string): MarkdownBlock[] {
  const lines = source.replace(/\r\n?/g, "\n").split("\n");
  const blocks: MarkdownBlock[] = [];
  let paragraph: string[] = [];
  const flushParagraph = (): void => {
    if (paragraph.length > 0) {
      blocks.push({ kind: "paragraph", spans: parseInline(paragraph.join(" ")) });
      paragraph = [];
    }
  };
  let index = 0;
  while (index < lines.length) {
    const line = lines[index];
    if (line === undefined) {
      break;
    }
    if (FENCE.test(line)) {
      flushParagraph();
      const body: string[] = [];
      index += 1;
      while (index < lines.length) {
        const bodyLine = lines[index];
        if (bodyLine === undefined || FENCE.test(bodyLine)) {
          break;
        }
        body.push(bodyLine);
        index += 1;
      }
      index += 1; // consume the closing fence when present
      blocks.push({ kind: "code", text: body.join("\n") });
      continue;
    }
    if (line.trim() === "") {
      flushParagraph();
      index += 1;
      continue;
    }
    const heading = HEADING.exec(line);
    if (heading !== null) {
      flushParagraph();
      blocks.push({
        kind: "heading",
        level: headingLevel((heading[1] ?? "").length),
        spans: parseInline(heading[2] ?? ""),
      });
      index += 1;
      continue;
    }
    const ordered = ORDERED_ITEM.exec(line);
    const unordered = UNORDERED_ITEM.exec(line);
    if (ordered !== null || unordered !== null) {
      flushParagraph();
      const isOrdered = ordered !== null;
      const items: MarkdownSpan[][] = [];
      while (index < lines.length) {
        const itemLine = lines[index];
        if (itemLine === undefined) {
          break;
        }
        const itemMatch = isOrdered ? ORDERED_ITEM.exec(itemLine) : UNORDERED_ITEM.exec(itemLine);
        if (itemMatch === null) {
          break;
        }
        items.push(parseInline(itemMatch[1] ?? ""));
        index += 1;
      }
      blocks.push({ kind: "list", ordered: isOrdered, items });
      continue;
    }
    paragraph.push(line.trim());
    index += 1;
  }
  flushParagraph();
  return blocks;
}

function renderSpans(spans: MarkdownSpan[]): ReactNode[] {
  return spans.map((span, position) => {
    const key = `${span.kind}-${position}`;
    switch (span.kind) {
      case "strong":
        return <strong key={key}>{span.text}</strong>;
      case "emphasis":
        return <em key={key}>{span.text}</em>;
      case "code":
        return <code key={key}>{span.text}</code>;
      case "link":
        return (
          <a key={key} href={span.href} rel="noopener noreferrer" target="_blank">
            {span.text}
          </a>
        );
      default:
        return <span key={key}>{span.text}</span>;
    }
  });
}

/** Render the operator-authored README subset. Given empty or whitespace-only
 * source it renders nothing, so a profile with no README adds no empty block. */
export function Markdown({
  source,
  className,
  "data-testid": testId,
}: {
  source: string;
  className?: string;
  "data-testid"?: string;
}): ReactNode {
  if (source.trim() === "") {
    return null;
  }
  const blocks = parseMarkdown(source);
  return (
    <div className={className} data-testid={testId}>
      {blocks.map((block, position) => {
        const key = `${block.kind}-${position}`;
        switch (block.kind) {
          case "heading": {
            const Tag = (`h${block.level}` as const);
            return <Tag key={key}>{renderSpans(block.spans)}</Tag>;
          }
          case "code":
            return (
              <pre key={key}>
                <code>{block.text}</code>
              </pre>
            );
          case "list":
            return block.ordered ? (
              <ol key={key}>
                {block.items.map((item, itemPosition) => (
                  <li key={`item-${itemPosition}`}>{renderSpans(item)}</li>
                ))}
              </ol>
            ) : (
              <ul key={key}>
                {block.items.map((item, itemPosition) => (
                  <li key={`item-${itemPosition}`}>{renderSpans(item)}</li>
                ))}
              </ul>
            );
          default:
            return <p key={key}>{renderSpans(block.spans)}</p>;
        }
      })}
    </div>
  );
}

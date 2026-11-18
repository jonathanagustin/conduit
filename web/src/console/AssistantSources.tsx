import type { GroundingEvidence } from "./assistant-grounding";

/** Read-only excerpts and evidence identities; source text never becomes HTML. */
export function AssistantSources({
  evidence,
}: {
  evidence: GroundingEvidence;
}) {
  if (evidence.state === "unavailable")
    return (
      <p data-assistant-sources="unavailable">Source metadata unavailable</p>
    );
  if (evidence.sources.length === 0)
    return (
      <p data-assistant-sources="empty">
        No source excerpts retrieved this turn
        {evidence.truncated ? "; source metadata was omitted" : ""}.
      </p>
    );
  return (
    <details
      data-assistant-sources="available"
      className="mt-1 min-w-0 basis-full text-[12px]"
    >
      <summary className="cursor-pointer">
        Sources retrieved ({evidence.sources.length})
      </summary>
      {evidence.truncated ? (
        <p>Additional source excerpts are not shown.</p>
      ) : null}
      <ol className="mt-2 space-y-3">
        {evidence.sources.map((source) => (
          <li
            key={source.id}
            data-source-citation={source.id}
            className="min-w-0 space-y-1 rounded border border-[var(--border-subtle)] p-2 break-words"
          >
            <p className="font-medium text-[var(--text-1)]">{source.title}</p>
            <p>
              {source.kind === "operator-documentation"
                ? "Native operator reference"
                : "Authorized ontology snapshot"}
            </p>
            <code className="block text-[11px] break-all">
              {source.source}
              {source.firstLine > 0
                ? ` · lines ${source.firstLine}–${source.lastLine}`
                : ""}
            </code>
            <blockquote className="border-l-2 border-[var(--border-subtle)] pl-2 break-words whitespace-pre-wrap text-[var(--text-1)]">
              {source.quote}
            </blockquote>
            <details>
              <summary className="cursor-pointer">Source evidence</summary>
              <dl className="space-y-1 text-[11px]">
                <dt>Retrieved</dt>
                <dd>
                  <time dateTime={source.retrievedAt}>
                    {source.retrievedAt}
                  </time>
                </dd>
                <dt>Excerpt SHA-256</dt>
                <dd className="font-mono break-all">{source.excerptSha256}</dd>
                {source.documentSha256 ? (
                  <>
                    <dt>Document SHA-256</dt>
                    <dd className="font-mono break-all">
                      {source.documentSha256}
                    </dd>
                    <dt>Excerpt byte range</dt>
                    <dd>
                      {source.startByte}–{source.endByte} (end exclusive)
                    </dd>
                  </>
                ) : null}
                <dt>Audit reference</dt>
                <dd className="font-mono break-all">{source.auditEventId}</dd>
              </dl>
            </details>
          </li>
        ))}
      </ol>
    </details>
  );
}

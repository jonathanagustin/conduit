import { fetchOwnAIUsage, type OwnAIUsage } from "../../api/ai-usage.js";
import { HubError } from "../../api/hub-error.js";

type Options = {
  identityKey: () => string;
  owner: () => string;
  render: () => void;
  escape: (value: unknown) => string;
  read?: () => Promise<OwnAIUsage>;
};

/** A read-only snapshot, cleared before painting a different identity. */
export function createAIUsagePanel(options: Options) {
  let identity = "";
  let request = 0;
  let status: "idle" | "loading" | "ready" | "failed" = "idle";
  let data: OwnAIUsage | null = null;
  let error = "";
  const esc = options.escape;

  function synchronize() {
    const current = options.identityKey();
    if (identity !== current) {
      identity = current;
      request++;
      data = null;
      status = "idle";
      error = "";
    }
  }

  async function load() {
    synchronize();
    if (status === "loading") return;
    const key = identity;
    const version = ++request;
    const owner = options.owner();
    data = null;
    status = "loading";
    options.render();
    const current = () => key === options.identityKey() && version === request;
    try {
      const result = await (options.read ?? fetchOwnAIUsage)();
      if (!current()) return;
      if (owner && result.owner !== owner)
        throw new HubError(502, "AI usage identity did not match.");
      data = result;
      status = "ready";
    } catch (failure) {
      if (!current()) return;
      status = "failed";
      error =
        failure instanceof HubError && failure.status === 401
          ? "Sign in to read your usage."
          : "AI usage could not be read. Try again.";
    } finally {
      if (current()) options.render();
    }
  }

  function markup() {
    synchronize();
    const row = (label: string, value: string) =>
      `<div><dt class="text-[11px] text-[var(--text-2)]">${esc(label)}</dt><dd class="text-[13px] text-[var(--text-1)]">${esc(value)}</dd></div>`;
    const limit = (remaining: number | null, maximum: number | null) =>
      maximum === null ? "Unlimited" : `${remaining} remaining of ${maximum}`;
    let content =
      '<p class="text-[12px] text-[var(--text-2)]">Load your current daily usage and allowance from the Hub.</p>';
    if (status === "loading")
      content = '<p role="status">Loading your AI usage…</p>';
    if (status === "failed")
      content = `<p role="alert" class="text-[12px] text-[var(--warn-text)]">${esc(error)}</p>`;
    if (status === "ready" && data) {
      const tokens = data.missingTokenCounts
        ? data.hasTokenCounts
          ? `${data.tokens} reported · incomplete`
          : "Not reported"
        : `${data.tokens} reported`;
      content = `<p role="status" class="break-words text-[12px] text-[var(--text-2)]">Hub snapshot for account ${esc(data.owner)} · ${esc(data.day)} UTC</p>
        <dl class="grid gap-3 sm:grid-cols-2">
          ${row("Calls charged", String(data.calls))}
          ${row("Token usage", tokens)}
          ${row("Call allowance", limit(data.remainingCalls, data.callLimit))}
          ${row("Token allowance (reported counts)", limit(data.remainingTokens, data.tokenLimit))}
        </dl>
        <p class="text-[11px] text-[var(--text-2)]">Only calls metered by this Hub are included. Token limits count reported usage and cannot account for missing token counts. Historical call charges can be conservative.</p>
        <p class="text-[11px] text-[var(--text-2)]">Resets ${esc(new Date(data.resetsAt).toISOString())}. Refresh to read a new snapshot.</p>`;
    }
    return `<section aria-label="Your AI usage" aria-busy="${status === "loading"}" class="so-panel p-3.5 space-y-3" data-ai-usage>
      <h2 class="text-[13px] font-semibold text-[var(--text-1)]">Your AI usage</h2>
      ${content}
      <button type="button" class="so-btn" data-onclick="refreshAIUsage()" aria-disabled="${status === "loading"}">${status === "ready" ? "Refresh AI usage" : status === "failed" ? "Retry AI usage" : "Load AI usage"}</button>
    </section>`;
  }

  return { load, markup };
}

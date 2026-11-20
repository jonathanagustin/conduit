import { describe, expect, it, vi } from "vitest";
import {
  parseAdminAITranscriptExport,
  parseAdminAITranscriptsList,
  parseAdminAIUsage,
} from "../../api/ai-governance.js";
import {
  adminAIGovernanceHTML,
  createAdminAIGovernance,
} from "../../console/operator/admin-ai-governance.js";

function sampleUsage() {
  return {
    days: 7,
    dailyCallBudget: 200,
    dailyTokenBudget: 500000,
    usage: [
      {
        day: "2026-11-20",
        owner: "ada",
        capability: "assistant",
        transport: "http",
        configuredModel: "gpt-5",
        providerModel: "gpt-5-2026",
        outcome: "succeeded",
        calls: 3,
        budgetedCalls: 3,
        attempts: 3,
        promptTokens: 1200,
        completionTokens: 450,
        callsWithTokens: 3,
        latencyMillis: 900,
      },
    ],
  };
}

function sampleTranscripts() {
  return {
    transcripts: "redacted",
    retention: "168h0m0s",
    runs: [
      {
        runId: "run-abc-123",
        sessionId: "sess-1",
        owner: "ada",
        capability: "planning",
        rounds: 2,
        bytes: 4096,
        firstAt: "2026-11-20T01:00:00Z",
        expiresAt: "2026-11-27T01:00:00Z",
      },
    ],
  };
}

function sampleExport() {
  return {
    runId: "run-abc-123",
    sessionId: "sess-1",
    owner: "ada",
    verified: true,
    problems: [],
    exchanges: [
      {
        round: 1,
        capability: "planning",
        auditEventId: "evt-1",
        outcome: "succeeded",
        transcript: "stored",
        present: true,
        prompt: "Inspect <script>alert(1)</script>",
        response: "Verified plan summary",
        sentPromptSha256: "aaaa",
        receivedResponseSha256: "bbbb",
        promptFidelity: "redacted",
        responseFidelity: "exact",
        chainSha256: "1234567890abcdef1234567890abcdef",
      },
    ],
  };
}

describe("admin AI governance API parsers", () => {
  it("parses valid fleet usage, transcript index, and verified exports", () => {
    const usage = parseAdminAIUsage(sampleUsage());
    expect(usage.days).toBe(7);
    expect(usage.dailyCallBudget).toBe(200);
    expect(usage.usage[0].providerModel).toBe("gpt-5-2026");

    const list = parseAdminAITranscriptsList(sampleTranscripts());
    expect(list.transcripts).toBe("redacted");
    expect(list.runs[0].runId).toBe("run-abc-123");

    const exp = parseAdminAITranscriptExport(sampleExport());
    expect(exp.verified).toBe(true);
    expect(exp.exchanges[0].promptFidelity).toBe("redacted");
  });

  it("rejects malformed payloads fail-closed", () => {
    expect(() => parseAdminAIUsage({ days: 0, usage: [] })).toThrow();
    expect(() => parseAdminAIUsage({ days: 7, usage: "bad" })).toThrow();
    expect(() =>
      parseAdminAITranscriptsList({ transcripts: 123, runs: [] }),
    ).toThrow();
    expect(() =>
      parseAdminAITranscriptExport({ runId: "", verified: true, exchanges: [] }),
    ).toThrow();
  });
});

describe("admin AI governance panel", () => {
  it("escapes untrusted transcript text and renders chain verification status", () => {
    const html = adminAIGovernanceHTML({
      days: 7,
      usage: parseAdminAIUsage(sampleUsage()),
      transcripts: parseAdminAITranscriptsList(sampleTranscripts()),
      selectedExport: parseAdminAITranscriptExport(sampleExport()),
    });
    expect(html).toContain("gpt-5-2026");
    expect(html).toContain("VERIFIED");
    expect(html).not.toContain("<script>");
    expect(html).toContain("&lt;script&gt;alert(1)&lt;/script&gt;");
  });

  it("loads fleet usage and transcripts and refuses non-admin 403 cleanly", async () => {
    const fetchImpl = vi.fn<typeof fetch>(async (url) => {
      const target = String(url);
      if (target.startsWith("/v1/admin/ai/usage")) {
        return new Response(JSON.stringify(sampleUsage()), { status: 200 });
      }
      if (target.startsWith("/v1/admin/ai/transcripts/run-abc-123")) {
        return new Response(JSON.stringify(sampleExport()), { status: 200 });
      }
      if (target.startsWith("/v1/admin/ai/transcripts")) {
        return new Response(JSON.stringify(sampleTranscripts()), {
          status: 200,
        });
      }
      return new Response("not found", { status: 404 });
    });

    const root = {
      isConnected: true,
      innerHTML: "",
      onclick: null as ((event: unknown) => void) | null,
      querySelector: () => null,
    };
    const panel = createAdminAIGovernance({ fetchImpl });
    await panel.mount(root as unknown as HTMLElement);
    expect(root.innerHTML).toContain("gpt-5-2026");
    expect(root.innerHTML).toContain("run-abc-123");

    const deniedFetch = vi.fn<typeof fetch>(
      async () => new Response("forbidden", { status: 403 }),
    );
    const deniedRoot = {
      isConnected: true,
      innerHTML: "",
      onclick: null as ((event: unknown) => void) | null,
      querySelector: () => null,
    };
    await createAdminAIGovernance({ fetchImpl: deniedFetch }).mount(
      deniedRoot as unknown as HTMLElement,
    );
    expect(deniedRoot.innerHTML).toContain("Administrator access required");
  });
});

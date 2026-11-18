import { describe, expect, it } from "vite-plus/test";
import {
  applyTurnDocument,
  newTurn,
  messagesFromThread,
  toThreadMessage,
  reportedTokenUsage,
  usageLabel,
  reduceTurn,
} from "../../console/assistant-turn";

describe("reported assistant token usage", () => {
  it("keeps a failed stream's reported usage without reporting success or trusting another custom event", () => {
    const snapshot = {
      type: "CUSTOM",
      name: "conduit.assistant.turn-result",
      value: { rounds: 2, usage: { promptTokens: 10, completionTokens: null } },
    };
    const partial = reduceTurn(newTurn(), snapshot);
    expect(partial.status).toBe("running");
    const failed = reduceTurn(partial, {
      type: "RUN_ERROR",
      code: "ROUND_CAP",
      message: "stopped",
    });
    expect(failed.status).toBe("error");
    expect(failed.usage).toEqual({ promptTokens: 10, completionTokens: null });
    expect(
      reduceTurn(newTurn(), { ...snapshot, name: "other.event" }).usage,
    ).toBeNull();
  });
  it("preserves reported zero and unknown sides without coercion or unsafe integers", () => {
    expect(
      reportedTokenUsage({ promptTokens: 0, completionTokens: 7 }),
    ).toEqual({ promptTokens: 0, completionTokens: 7 });
    for (const invalid of [
      undefined,
      null,
      -1,
      1.2,
      "7",
      false,
      Number.MAX_SAFE_INTEGER + 1,
      Infinity,
    ]) {
      expect(
        reportedTokenUsage({ promptTokens: invalid, completionTokens: 0 }),
      ).toEqual({ promptTokens: null, completionTokens: 0 });
    }
    expect(reportedTokenUsage(undefined)).toBeNull();
    expect(reportedTokenUsage([])).toBeNull();
    expect(usageLabel(null)).toBeNull();
    expect(usageLabel({ promptTokens: null, completionTokens: null })).toBe(
      "Token usage not reported",
    );
    expect(usageLabel({ promptTokens: 0, completionTokens: null })).toBe(
      "Reported tokens: 0 input · unknown output",
    );
  });

  it("carries usage through the authoritative turn document and message metadata, without reusing it on another turn", () => {
    const turn = applyTurnDocument(newTurn(), {
      text: "answer",
      usage: { promptTokens: 11, completionTokens: 0 },
    });
    const message = toThreadMessage({
      id: "answer",
      role: "assistant",
      mode: "explore",
      createdAt: "2026-11-18T00:00:00Z",
      turn,
    });
    expect(message.metadata?.custom.conduit.usage).toEqual({
      promptTokens: 11,
      completionTokens: 0,
    });
    expect(
      applyTurnDocument(newTurn(), { text: "older server" }).usage,
    ).toBeNull();
    expect(
      applyTurnDocument(turn, { text: "usage unavailable" }).usage,
    ).toBeNull();
  });

  it("restores only stored reported usage; older thread documents remain unknown", () => {
    const messages = messagesFromThread({
      id: "ath_test",
      messages: [
        { role: "assistant", text: "old" },
        {
          role: "assistant",
          text: "new",
          usage: { promptTokens: 0, completionTokens: null },
        },
      ],
    });
    expect(messages[0]?.role).toBe("assistant");
    const old = messages[0],
      current = messages[1];
    if (old?.role !== "assistant" || current?.role !== "assistant")
      throw new Error("missing assistant messages");
    expect(old.turn.usage).toBeNull();
    expect(current.turn.usage).toEqual({
      promptTokens: 0,
      completionTokens: null,
    });
    expect(toThreadMessage(current).metadata?.custom.conduit.usage).toEqual(
      current.turn.usage,
    );
  });
});

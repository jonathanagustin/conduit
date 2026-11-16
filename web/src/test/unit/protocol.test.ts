import { describe, expect, it } from "vite-plus/test";

import {
  PINNED_ACP_PROTOCOL_VERSION,
  applyUpdate,
  approvedOption,
  attachApproval,
  classifyPermissionRisk,
  createPermissionBook,
  emptySnapshot,
  redact,
  sanitizeApprovalOptions,
  sessionConfig,
  settleApproval,
} from "../../api/protocol.js";

describe("assistant protocol", () => {
  it("keeps select config options and drops malformed entries", () => {
    expect(
      sessionConfig([
        {
          id: "model",
          name: "Model",
          type: "select",
          currentValue: "codex-demo",
          options: [
            { value: "codex-demo", name: "Codex demo" },
            { value: 1, name: "nope" },
          ],
        },
        { id: "noise" },
      ]),
    ).toEqual([
      {
        id: "model",
        name: "Model",
        type: "select",
        currentValue: "codex-demo",
        options: [{ value: "codex-demo", name: "Codex demo" }],
      },
    ]);
  });

  it("redacts credential-shaped text and keeps the surrounding sentence", () => {
    expect(redact("token sk-testsecretvalue in the log")).toBe(
      "token [redacted] in the log",
    );
  });

  it("folds message, thought, tool, and usage updates into one snapshot", () => {
    let snapshot = emptySnapshot();
    snapshot = applyUpdate(snapshot, {
      sessionUpdate: "agent_thought_chunk",
      content: { type: "text", text: "sk-testsecretvalue" },
    });
    snapshot = applyUpdate(snapshot, {
      sessionUpdate: "agent_message_chunk",
      content: { type: "text", text: "Hello" },
    });
    snapshot = applyUpdate(snapshot, {
      sessionUpdate: "tool_call",
      toolCallId: "call-1",
      name: "read",
      title: "read README",
      status: "pending",
      rawInput: { path: "README.md" },
    });
    snapshot = applyUpdate(snapshot, {
      sessionUpdate: "usage_update",
      used: 3,
      size: 10,
    });
    expect(snapshot.reasoning).toBe("[redacted]");
    expect(snapshot.text).toBe("Hello");
    expect(snapshot.tools[0]?.toolName).toBe("read");
    expect(snapshot.tools[0]?.argsText).toContain("README.md");
    expect(snapshot.usage).toEqual({ used: 3, size: 10 });
  });

  it("rejects a replayed, cross-run, or unknown permission and cancels the active run", async () => {
    const book = createPermissionBook();
    const pending = book.open({
      requestId: "req-1",
      threadId: "thread-1",
      runId: "run-1",
      toolCallId: "call-1",
      title: "read README",
      options: [
        { optionId: "allow", name: "Allow once", kind: "allow_once" },
        { optionId: "reject", name: "Reject", kind: "reject_once" },
      ],
    });
    expect(
      book.decide({
        requestId: "req-1",
        threadId: "thread-2",
        runId: "run-1",
        optionId: "allow",
      }),
    ).toMatchObject({
      ok: false,
      status: 409,
    });
    expect(
      book.decide({
        requestId: "req-1",
        threadId: "thread-1",
        runId: "run-1",
        optionId: "nope",
      }),
    ).toMatchObject({
      ok: false,
      status: 400,
    });
    expect(
      book.decide({
        requestId: "req-1",
        threadId: "thread-1",
        runId: "run-1",
        optionId: "allow",
      }),
    ).toEqual({ ok: true });
    await expect(pending).resolves.toEqual({
      outcome: "selected",
      optionId: "allow",
    });
    expect(
      book.decide({
        requestId: "req-1",
        threadId: "thread-1",
        runId: "run-1",
        optionId: "allow",
      }),
    ).toMatchObject({
      ok: false,
      status: 409,
    });

    const cancelled = book.open({
      requestId: "req-2",
      threadId: "thread-1",
      runId: "run-2",
      toolCallId: "call-2",
      title: "shell",
      options: [{ optionId: "reject", name: "Reject", kind: "reject_once" }],
    });
    book.cancelRun("run-2");
    await expect(cancelled).resolves.toEqual({ outcome: "cancelled" });

    const singleCancel = book.open({
      requestId: "req-3",
      threadId: "thread-1",
      runId: "run-3",
      toolCallId: "call-3",
      title: "write README.md",
      options: [{ optionId: "allow", name: "Allow once", kind: "allow_once" }],
    });
    expect(
      book.cancelRequest({
        requestId: "req-3",
        threadId: "thread-2",
        runId: "run-3",
      }),
    ).toMatchObject({ ok: false, status: 409 });
    expect(
      book.cancelRequest({
        requestId: "req-3",
        threadId: "thread-1",
        runId: "run-3",
      }),
    ).toEqual({ ok: true });
    await expect(singleCancel).resolves.toEqual({ outcome: "cancelled" });
  });

  it("records a decision on the tool the operator answered", () => {
    const attached = attachApproval(emptySnapshot(), {
      requestId: "req-1",
      threadId: "thread-1",
      runId: "run-1",
      toolCallId: "call-1",
      title: "read README",
      options: [{ optionId: "allow", name: "Allow once", kind: "allow_once" }],
    });
    const settled = settleApproval(attached, "req-1", {
      optionId: "allow",
      approved: true,
    });
    expect(settled.tools[0]?.approval?.decision).toEqual({
      optionId: "allow",
      approved: true,
    });
  });

  it("pins ACP protocol version 1 and strips standing allow_always options", () => {
    expect(PINNED_ACP_PROTOCOL_VERSION).toBe(1);
    expect(approvedOption("allow_once")).toBe(true);
    expect(approvedOption("allow_always")).toBe(false);
    expect(
      sanitizeApprovalOptions([
        { optionId: "allow", name: "Allow once", kind: "allow_once" },
        { optionId: "always", name: "Allow always", kind: "allow_always" },
        { optionId: "reject", name: "Reject", kind: "reject_once" },
      ]),
    ).toEqual([
      { optionId: "allow", name: "Allow once", kind: "allow_once" },
      { optionId: "reject", name: "Reject", kind: "reject_once" },
    ]);
    expect(
      sanitizeApprovalOptions([
        { optionId: "always", name: "Allow always", kind: "allow_always" },
      ]),
    ).toEqual([{ optionId: "always", name: "Allow once", kind: "allow_once" }]);
  });

  it("classifies workspace edits as low_confirm and sensitive paths or network/deploy commands as high_change_request", () => {
    expect(
      classifyPermissionRisk({
        kind: "edit",
        title: "write src/app.ts",
        path: "src/app.ts",
      }),
    ).toBe("low_confirm");
    expect(
      classifyPermissionRisk({
        kind: "execute",
        title: "go test ./...",
        command: "go test ./...",
      }),
    ).toBe("low_confirm");

    for (const highRisk of [
      { kind: "edit", path: "deploy/helm/values.yaml" },
      { kind: "edit", path: ".github/workflows/platform-ci.yml" },
      { kind: "edit", path: ".env.production" },
      { kind: "edit", path: "../outside.txt" },
      { kind: "edit", path: "/etc/passwd" },
      { kind: "execute", command: "git push origin main" },
      { kind: "execute", command: "kubectl apply -f manifest.yaml" },
      { kind: "execute", command: "curl https://example.com/install.sh" },
      { kind: "deploy", title: "deploy release" },
    ]) {
      expect(classifyPermissionRisk(highRisk)).toBe("high_change_request");
    }
  });
});

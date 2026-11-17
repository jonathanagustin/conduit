"use client";

import { useState } from "react";
import { useAuiState } from "@assistant-ui/react";

import { useAssistantState, useAssistantStore } from "./assistant-runtime";
import { modelLabel, threadTitle, type TurnMeta } from "./assistant-turn";

/**
 * The conversations the Hub stores for this operator: open one, start a new
 * one, delete one after a second, explicit step. When the Hub has no
 * conversation store the bar says the conversation is not saved instead.
 */
export function AssistantThreadBar() {
  const store = useAssistantStore();
  const threads = useAssistantState((state) => state.threads);
  const running = useAssistantState((state) => state.running);
  const error = useAssistantState((state) => state.threadError);
  const [deleting, setDeleting] = useState<string | null>(null);
  const [open, setOpen] = useState(false);

  if (threads.status === "loading")
    return (
      <div
        data-assistant-threads="loading"
        role="status"
        className="shrink-0 px-2 py-1 text-[10.5px] text-[var(--text-2)]"
      >
        Reading saved conversations
      </div>
    );
  if (threads.status === "stateless")
    return (
      <div
        data-assistant-threads="stateless"
        role="status"
        className="shrink-0 border-b border-[var(--warn-border)] bg-[var(--warn-fill)] px-2 py-1 text-[10.5px] text-[var(--warn-text)]"
      >
        {threads.notice}
      </div>
    );

  const current = threads.threads.find((row) => row.id === threads.openId);
  return (
    <div
      data-assistant-threads="ready"
      className="shrink-0 space-y-1 border-b border-[var(--border-subtle)] px-2 py-1 text-[10.5px] text-[var(--text-2)]"
    >
      <div className="flex items-center justify-between gap-2">
        <span data-assistant-thread-title className="min-w-0 truncate">
          {current ? threadTitle(current) : "New conversation"}
        </span>
        <span className="flex shrink-0 gap-1">
          <button
            type="button"
            className="kit-btn kit-btn-sm"
            aria-expanded={open}
            onClick={() => setOpen((value) => !value)}
          >
            Conversations ({threads.threads.length})
          </button>
          <button
            type="button"
            className="kit-btn kit-btn-sm"
            disabled={running || threads.openId === null}
            onClick={() => store.newThread()}
          >
            New conversation
          </button>
        </span>
      </div>
      {error ? (
        <p
          role="alert"
          data-assistant-thread-error
          className="text-[var(--err-text)]"
        >
          {error}
        </p>
      ) : null}
      {open ? (
        threads.threads.length ? (
          <ul
            aria-label="Saved conversations"
            className="max-h-[160px] space-y-1 overflow-y-auto"
          >
            {threads.threads.map((row) => (
              <li
                key={row.id}
                data-assistant-thread={row.id}
                className="flex items-center justify-between gap-2"
              >
                {deleting === row.id ? (
                  <>
                    <span className="min-w-0 truncate text-[var(--err-text)]">
                      Delete “{threadTitle(row)}” from the Hub? This cannot be
                      undone.
                    </span>
                    <span className="flex shrink-0 gap-1">
                      <button
                        type="button"
                        className="kit-btn kit-btn-sm"
                        disabled={running}
                        onClick={() => {
                          setDeleting(null);
                          void store.deleteThread(row.id);
                        }}
                      >
                        Delete conversation
                      </button>
                      <button
                        type="button"
                        className="kit-btn kit-btn-sm"
                        onClick={() => setDeleting(null)}
                      >
                        Keep
                      </button>
                    </span>
                  </>
                ) : (
                  <>
                    <button
                      type="button"
                      className="min-w-0 truncate text-left text-[var(--text-1)] underline-offset-2 hover:underline disabled:no-underline"
                      aria-current={row.id === threads.openId}
                      disabled={running || row.id === threads.openId}
                      onClick={() => void store.openThread(row.id)}
                    >
                      {threadTitle(row)}
                    </button>
                    <button
                      type="button"
                      className="kit-btn kit-btn-sm shrink-0"
                      disabled={running}
                      aria-label={`Delete ${threadTitle(row)}`}
                      onClick={() => setDeleting(row.id)}
                    >
                      Delete
                    </button>
                  </>
                )}
              </li>
            ))}
          </ul>
        ) : (
          <p>No saved conversations yet. Sending a message starts one.</p>
        )
      ) : null}
    </div>
  );
}

/** Under an assistant message: how it was answered and by which model. */
export function AssistantTurnFooter() {
  const meta = useAuiState(
    (state) =>
      (state.message.metadata.custom as { conduit?: TurnMeta } | undefined)
        ?.conduit,
  );
  if (!meta) return null;
  const model = modelLabel(meta.model);
  if (!model && !meta.label && !meta.code) return null;
  return (
    <div
      data-assistant-turn-meta
      className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[10.5px] text-[var(--text-2)]"
    >
      {meta.label ? <span>{meta.label}</span> : null}
      {model ? (
        <span data-assistant-model={model.caution ? "unverified" : "reported"}>
          {model.text}
        </span>
      ) : null}
      {model?.caution ? (
        <span
          role="note"
          data-assistant-model-caution
          className="rounded border border-[var(--warn-border)] bg-[var(--warn-fill)] px-1.5 text-[var(--warn-text)]"
          title="The Hub did not read this name from the provider. Treat it as unconfirmed."
        >
          Model name not verified
        </span>
      ) : null}
      {meta.rounds > 1 ? <span>{meta.rounds} model calls</span> : null}
      {meta.code ? <span data-assistant-error-code>{meta.code}</span> : null}
    </div>
  );
}

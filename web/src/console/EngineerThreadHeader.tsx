"use client";

import { useAuiState } from "@assistant-ui/react";

/** Navigation over the active runtime, never another conversation store. */
export function EngineerThreadHeader() {
  const messages = useAuiState((state) => state.thread.messages);
  const turns = messages.filter((message) => message.role === "user");
  return (
    <details className="engineer-thread-outline" data-engineer-outline>
      <summary>
        Conversation · {turns.length} user{" "}
        {turns.length === 1 ? "turn" : "turns"}
      </summary>
      {turns.length ? (
        <ol aria-label="Conversation turns">
          {turns.map((message, index) => {
            const text = message.content
              .map((part) => (part.type === "text" ? part.text : ""))
              .join(" ")
              .trim();
            const label = text.slice(0, 120) || "Attachment or tool input";
            return (
              <li key={message.id}>
                <button
                  type="button"
                  className="kit-btn"
                  onClick={(event) => {
                    const target = event.currentTarget
                      .closest(".assistant-thread-mount")
                      ?.querySelector<HTMLElement>(
                        `[data-message-id="${CSS.escape(message.id)}"]`,
                      );
                    if (!target) return;
                    target.tabIndex = -1;
                    target.focus({ preventScroll: true });
                    target.scrollIntoView({
                      block: "start",
                      behavior: window.matchMedia(
                        "(prefers-reduced-motion: reduce)",
                      ).matches
                        ? "auto"
                        : "smooth",
                    });
                  }}
                >
                  {index + 1}. {label}
                </button>
              </li>
            );
          })}
        </ol>
      ) : (
        <p>No user turn in this conversation yet.</p>
      )}
    </details>
  );
}

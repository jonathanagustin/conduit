"use client";

import { useAui } from "@assistant-ui/react";

import {
  EmptyState,
  EmptyStateGreeting,
  EmptyStateSuggestion,
  EmptyStateSuggestions,
} from "@/components/assistant-ui/elements/empty-state";
import { STARTERS } from "@/src/api/runtime";

export function ThreadWelcome() {
  const aui = useAui();
  return (
    <EmptyState>
      <EmptyStateGreeting>How can I help you today?</EmptyStateGreeting>
      <EmptyStateSuggestions>
        {STARTERS.map((starter, index) => (
          <EmptyStateSuggestion
            key={starter.prompt}
            index={index}
            onClick={() => {
              aui.composer.setText(starter.prompt);
              aui.composer.send();
            }}
          >
            {starter.title}
          </EmptyStateSuggestion>
        ))}
      </EmptyStateSuggestions>
    </EmptyState>
  );
}

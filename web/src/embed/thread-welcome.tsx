"use client";

import { useAui } from "@assistant-ui/react";
import { useState } from "react";

import {
  EmptyState,
  EmptyStateGreeting,
  EmptyStateSuggestion,
  EmptyStateSuggestions,
} from "@/components/assistant-ui/elements/empty-state";
import { Onboarding } from "@/components/assistant-ui/elements/onboarding";
import { STARTERS } from "@/src/api/runtime";
import { ONBOARDED_KEY, ONBOARDING_STEPS } from "@/src/embed/console-prefs";

export function ThreadWelcome() {
  const aui = useAui();
  const [step, setStep] = useState(0);
  const [done, setDone] = useState(
    () => window.localStorage.getItem(ONBOARDED_KEY) === "done",
  );
  const finish = () => {
    window.localStorage.setItem(ONBOARDED_KEY, "done");
    setDone(true);
  };
  return (
    <EmptyState>
      <EmptyStateGreeting>How can I help you today?</EmptyStateGreeting>
      {done ? null : (
        <Onboarding
          steps={ONBOARDING_STEPS}
          index={step}
          onSkip={finish}
          onNext={() => {
            if (step >= ONBOARDING_STEPS.length - 1) finish();
            else setStep((current) => current + 1);
          }}
        />
      )}
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

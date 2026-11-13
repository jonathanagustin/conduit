"use client";

import { useAuiState } from "@assistant-ui/react";
import { useEffect, useState } from "react";

import { ModelSelector } from "@/components/assistant-ui/elements/model-selector.aui";
import {
  defaultSessionConfig,
  rememberedSessionChoice,
  rememberSessionChoice,
  sessionConfigOf,
  setSessionConfig,
  subscribeSessionChoice,
} from "@/src/api/client";
import { loadSettings } from "@/src/embed/console-prefs";
import type { SessionConfigOption } from "@/src/api/protocol";

const MODEL_FALLBACK =
  "This ACP session does not advertise a model config option, so the selector stays on the current model and does not call session/set_config_option.";
export const EFFORT_FALLBACK =
  "This ACP session does not advertise a reasoning_effort config option, so no effort control is shown. Codex ACP exposes it as session config id reasoning_effort when the agent sends it.";

function selectOption(
  options: readonly SessionConfigOption[],
  id: string,
): SessionConfigOption | undefined {
  return options.find((option) => option.id === id && option.type === "select");
}

export function SessionControls() {
  const remoteId = useAuiState((state) => state.threadListItem.remoteId);
  const [options, setOptions] = useState<SessionConfigOption[] | undefined>(
    undefined,
  );

  useEffect(() => {
    const saved = loadSettings(window.localStorage);
    if (saved.model || saved.effort) {
      rememberSessionChoice({
        ...(saved.model ? { model: saved.model } : {}),
        ...(saved.effort ? { effort: saved.effort } : {}),
      });
    }
    let cancelled = false;
    const threadId =
      remoteId && !remoteId.startsWith("offline-") ? remoteId : undefined;
    const load = threadId ? sessionConfigOf(threadId) : defaultSessionConfig();
    void load
      .then(async (next) => {
        if (cancelled) return next;
        let current = next;
        const choice = rememberedSessionChoice();
        if (threadId && choice.model) {
          const model = selectOption(current, "model");
          if (
            model &&
            model.currentValue !== choice.model &&
            model.options?.some((option) => option.value === choice.model)
          ) {
            current = await setSessionConfig(
              threadId,
              "model",
              choice.model,
            ).catch(() => current);
          }
        }
        if (cancelled) return current;
        if (threadId && choice.effort) {
          const effort = selectOption(current, "reasoning_effort");
          if (
            effort &&
            effort.currentValue !== choice.effort &&
            effort.options?.some((option) => option.value === choice.effort)
          ) {
            current = await setSessionConfig(
              threadId,
              "reasoning_effort",
              choice.effort,
            ).catch(() => current);
          }
        }
        return current;
      })
      .then((next) => {
        if (!cancelled) setOptions(next);
      })
      .catch(() => {
        if (!cancelled) setOptions([]);
      });
    return () => {
      cancelled = true;
    };
  }, [remoteId]);

  useEffect(() => {
    return subscribeSessionChoice(() => {
      const choice = rememberedSessionChoice();
      setOptions((current) =>
        current?.map((option) => {
          if (option.id === "model" && choice.model)
            return { ...option, currentValue: choice.model };
          if (option.id === "reasoning_effort" && choice.effort)
            return { ...option, currentValue: choice.effort };
          return option;
        }),
      );
    });
  }, []);

  if (!options) return null;
  const threadId =
    remoteId && !remoteId.startsWith("offline-") ? remoteId : undefined;
  const model = selectOption(options, "model");
  const effort = selectOption(options, "reasoning_effort");
  const efforts = effort?.options?.map((option) => ({
    id: option.value,
    name: option.name,
  }));
  const models = model?.options?.map((option) => ({
    id: option.value,
    name: option.name,
    ...(option.description ? { description: option.description } : {}),
    ...(efforts && efforts.length > 0 ? { efforts } : {}),
  })) ?? [
    { id: "codex", name: "Codex", description: MODEL_FALLBACK, disabled: true },
  ];
  const modelValue =
    typeof model?.currentValue === "string"
      ? model.currentValue
      : models[0]?.id;
  const effortValue =
    typeof effort?.currentValue === "string" ? effort.currentValue : undefined;

  return (
    <div data-testid="session-controls" className="flex items-center">
      {!model ? <p className="sr-only">{MODEL_FALLBACK}</p> : null}
      {!effort ? <p className="sr-only">{EFFORT_FALLBACK}</p> : null}
      <ModelSelector
        models={models}
        {...(modelValue ? { value: modelValue } : {})}
        {...(effortValue ? { effort: effortValue } : {})}
        variant="ghost"
        size="sm"
        onValueChange={(value) => {
          if (!model) return;
          rememberSessionChoice({ model: value });
          if (!threadId) return;
          void setSessionConfig(threadId, "model", value)
            .then(setOptions)
            .catch(() => undefined);
        }}
        onEffortChange={(value) => {
          if (!effort) return;
          rememberSessionChoice({ effort: value });
          if (!threadId) return;
          void setSessionConfig(threadId, "reasoning_effort", value)
            .then(setOptions)
            .catch(() => undefined);
        }}
      />
    </div>
  );
}

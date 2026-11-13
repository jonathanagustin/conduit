"use client";

import { useAuiState } from "@assistant-ui/react";
import { useEffect, useState } from "react";

import { ModelSelector } from "@/components/assistant-ui/elements/model-selector.aui";
import {
  defaultSessionConfig,
  rememberSessionChoice,
  sessionConfigOf,
  setSessionConfig,
} from "@/src/api/client";
import type { SessionConfigOption } from "@/src/api/protocol";

const MODEL_FALLBACK =
  "This ACP session does not advertise a model config option, so the selector stays on the current model and does not call session/set_config_option.";
const EFFORT_FALLBACK =
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
    if (!remoteId || remoteId.startsWith("offline-")) {
      let cancelled = false;
      void defaultSessionConfig()
        .then((next) => {
          if (!cancelled) setOptions(next);
        })
        .catch(() => {
          if (!cancelled) setOptions([]);
        });
      return () => {
        cancelled = true;
      };
    }
    const threadId = remoteId;
    let cancelled = false;
    void sessionConfigOf(threadId)
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

"use client";

import { ComposerTriggerPopover } from "@/components/assistant-ui/elements/composer-trigger-popover.aui";
import {
  unstable_useMentionAdapter,
  unstable_useSlashCommandAdapter,
} from "@assistant-ui/react";

const DEMO_COMMANDS = [
  {
    id: "summarize",
    label: "Summarize",
    description: "Demo slash command",
    execute: () => undefined,
  },
  {
    id: "plan",
    label: "Plan",
    description: "Demo slash command",
    execute: () => undefined,
  },
];

const DEMO_MENTIONS = [
  {
    id: "search",
    type: "agent",
    label: "Search",
    description: "Demo mention",
  },
];

export function ComposerCommands() {
  const slash = unstable_useSlashCommandAdapter({ commands: DEMO_COMMANDS });
  const mentions = unstable_useMentionAdapter({
    items: DEMO_MENTIONS,
    includeModelContextTools: false,
  });
  return (
    <>
      <ComposerTriggerPopover char="/" {...slash} />
      <ComposerTriggerPopover char="@" {...mentions} />
    </>
  );
}

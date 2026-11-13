import type { ReasoningMessagePartComponent } from "@assistant-ui/react";
import * as Collapsible from "@radix-ui/react-collapsible";
import { useState } from "react";

export const Reasoning: ReasoningMessagePartComponent = ({ text, status }) => {
  const [open, setOpen] = useState(status?.type === "running");
  if (text.length === 0) return null;
  return (
    <Collapsible.Root open={open} onOpenChange={setOpen} className="my-2 rounded-lg border border-border">
      <Collapsible.Trigger className="px-3 py-2 text-sm text-muted-foreground">
        {status?.type === "running" ? "Thinking…" : "Reasoning"}
      </Collapsible.Trigger>
      <Collapsible.Content className="px-3 pb-3 text-sm whitespace-pre-wrap text-muted-foreground">{text}</Collapsible.Content>
    </Collapsible.Root>
  );
};

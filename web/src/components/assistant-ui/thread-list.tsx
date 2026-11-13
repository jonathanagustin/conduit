import { ThreadListItemPrimitive, ThreadListPrimitive } from "@assistant-ui/react";
import { PlusIcon } from "lucide-react";

import { Button } from "../ui/button.js";

export function ThreadList() {
  return (
    <ThreadListPrimitive.Root className="flex h-full w-64 shrink-0 flex-col gap-2 border-r border-border p-3" data-testid="thread-list">
      <ThreadListPrimitive.New asChild>
        <Button variant="outline" className="justify-start">
          <PlusIcon />
          New chat
        </Button>
      </ThreadListPrimitive.New>
      <ThreadListPrimitive.Items
        components={{
          ThreadListItem: () => (
            <ThreadListItemPrimitive.Root className="group flex items-center gap-1 rounded-md px-1 py-1 text-sm data-[active]:bg-accent">
              <ThreadListItemPrimitive.Trigger className="min-w-0 flex-1 truncate text-left">
                <ThreadListItemPrimitive.Title />
              </ThreadListItemPrimitive.Trigger>
              <ThreadListItemPrimitive.Archive className="text-xs text-muted-foreground opacity-0 group-hover:opacity-100">
                Archive
              </ThreadListItemPrimitive.Archive>
              <ThreadListItemPrimitive.Delete className="text-xs text-muted-foreground opacity-0 group-hover:opacity-100">
                Delete
              </ThreadListItemPrimitive.Delete>
            </ThreadListItemPrimitive.Root>
          ),
        }}
      />
    </ThreadListPrimitive.Root>
  );
}

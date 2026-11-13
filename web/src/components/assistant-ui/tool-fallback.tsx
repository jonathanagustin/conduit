import { decidePermission } from "../../api/client.js";
import type { ToolCallMessagePartComponent } from "@assistant-ui/react";
import * as Collapsible from "@radix-ui/react-collapsible";
import { ChevronDownIcon } from "lucide-react";
import { useState } from "react";

import { Button } from "../ui/button.js";

function binding(approvalId: string): { threadId: string; runId: string } | undefined {
  const [threadId, runId] = approvalId.split("::");
  if (!threadId || !runId) return undefined;
  return { threadId, runId };
}

export const ToolFallback: ToolCallMessagePartComponent = ({
  toolName,
  argsText,
  result,
  isError,
  status,
  approval,
  respondToApproval,
}) => {
  const [open, setOpen] = useState(Boolean(approval && approval.approved === undefined && !approval.resolution));
  const [choice, setChoice] = useState<string | undefined>();
  const pending = approval !== undefined && approval.approved === undefined && approval.resolution === undefined && choice === undefined;
  return (
    <Collapsible.Root open={open} onOpenChange={setOpen} className="my-2 rounded-lg border border-border bg-muted/40">
      <Collapsible.Trigger className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm">
        <ChevronDownIcon className="size-4" />
        <span className="font-medium">{toolName}</span>
        <span className="text-muted-foreground">{status?.type ?? "running"}</span>
        {isError ? <span className="text-destructive">failed</span> : null}
      </Collapsible.Trigger>
      <Collapsible.Content className="space-y-2 px-3 pb-3 text-xs">
        {argsText.length > 0 ? <pre className="overflow-auto whitespace-pre-wrap">{argsText}</pre> : null}
        {result !== undefined ? (
          <pre className="overflow-auto whitespace-pre-wrap">{typeof result === "string" ? result : JSON.stringify(result, null, 2)}</pre>
        ) : null}
        {pending && approval ? (
          <div className="flex flex-wrap gap-2" data-testid="permission-request">
            <p className="w-full text-sm text-foreground">{approval.prompt ?? "The agent needs permission for this action."}</p>
            <p className="w-full text-muted-foreground">
              Allowing a tool runs it in the local workspace. It does not approve a Conduit deployment.
            </p>
            {(approval.options ?? []).map((option) => (
              <Button
                key={option.id}
                size="sm"
                variant={option.kind.startsWith("reject") ? "outline" : "default"}
                onClick={() => {
                  const allowed = option.kind.startsWith("allow");
                  setChoice(allowed ? "Allowed" : "Rejected");
                  const bound = binding(approval.id);
                  if (!bound) return;
                  void decidePermission({
                    requestId: approval.id,
                    threadId: bound.threadId,
                    runId: bound.runId,
                    optionId: option.id,
                  })
                    .then(() =>
                      respondToApproval({
                        approved: allowed,
                        optionId: option.id,
                      }),
                    )
                    .catch(() => undefined);
                }}
              >
                {option.label ?? option.kind}
              </Button>
            ))}
          </div>
        ) : null}
        {approval?.resolution === "cancelled" ? <p>Permission cancelled.</p> : null}
        {choice !== undefined || approval?.approved !== undefined ? (
          <p>{choice ?? (approval?.approved ? "Allowed" : "Rejected")} by the operator.</p>
        ) : null}
      </Collapsible.Content>
    </Collapsible.Root>
  );
};

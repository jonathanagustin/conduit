import { useEffect, useState } from "react";

import { bridgeInfo } from "../../api/client.js";
import { AssistantShell } from "../../components/assistant-ui/shell.js";
import { Thread } from "../../components/assistant-ui/thread.js";
import { ThreadList } from "../../components/assistant-ui/thread-list.js";
import type { ConsoleBootstrap } from "../../runtime/bootstrap.js";

export function AssistantPage({ bootstrap }: { bootstrap: ConsoleBootstrap }) {
  const [bridge, setBridge] = useState("Checking the local Codex bridge");

  useEffect(() => {
    const controller = new AbortController();
    bridgeInfo(controller.signal).then(
      (info) => {
        setBridge(info.fixture ? "Fixture agent" : `Codex ACP · ${info.mode}`);
      },
      () => {
        setBridge("Codex bridge is not running");
      },
    );
    return () => controller.abort();
  }, []);

  return (
    <AssistantShell bootstrap={bootstrap} bridge={bridge}>
      <ThreadList />
      <div className="min-w-0 flex-1">
        <Thread />
      </div>
    </AssistantShell>
  );
}

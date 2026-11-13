import { AssistantPage } from "../features/assistant/AssistantPage.js";
import type { ConsoleBootstrap } from "../runtime/bootstrap.js";

export function App({ bootstrap }: { bootstrap: ConsoleBootstrap }) {
  return <AssistantPage bootstrap={bootstrap} />;
}

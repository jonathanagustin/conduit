import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "./app/App.js";
import { Providers } from "./app/providers.js";
import { loadRuntimeBootstrap } from "./runtime/bootstrap.js";
import "./styles/index.css";

const container = document.getElementById("root");
if (container === null) {
  throw new Error("index.html has no #root element to mount into");
}

const root = createRoot(container);

try {
  const bootstrap = loadRuntimeBootstrap();
  root.render(
    <StrictMode>
      <Providers>
        <App bootstrap={bootstrap} />
      </Providers>
    </StrictMode>,
  );
} catch {
  root.render(
    <StrictMode>
      <main role="alert" data-testid="console-configuration-unavailable">
        Console configuration is unavailable. Contact the Conduit operator.
      </main>
    </StrictMode>,
  );
}

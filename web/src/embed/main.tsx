import "../../app/globals.css";
import "../styles/index.css";

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { Assistant } from "../../app/assistant";
import { Providers } from "../app/providers";
import { loadRuntimeBootstrap } from "../runtime/bootstrap";

const container = document.getElementById("root");
if (container === null) {
  throw new Error("index.html has no #root element to mount into");
}

const root = createRoot(container);

try {
  loadRuntimeBootstrap();
  root.render(
    <StrictMode>
      <Providers>
        <div className="antialiased">
          <Assistant />
        </div>
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

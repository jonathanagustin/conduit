import "../../app/globals.css";
import "../styles/index.css";

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { Route, Routes } from "react-router-dom";

import { Assistant } from "../../app/assistant";
import { Providers } from "../app/providers";
import { PackagingRunPage } from "../features/packaging/PackagingRunPage.js";
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
        <Routes>
          <Route path="packaging-runs/:runId" element={<PackagingRunPage />} />
          <Route
            path="*"
            element={
              <div className="antialiased">
                <Assistant />
              </div>
            }
          />
        </Routes>
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

import "../../app/globals.css";
import "../styles/index.css";

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { Route, Routes } from "react-router-dom";

import { Assistant } from "../../app/assistant";
import { Providers } from "../app/providers";
import { PackagingRunPage } from "../features/packaging/PackagingRunPage.js";
import { loadRuntimeBootstrap } from "../runtime/bootstrap";
import { buildId, installClientErrorHandlers } from "../runtime/client-errors";
import { ConsoleErrorBoundary } from "./ErrorBoundary";

const container = document.getElementById("root");
if (container === null) {
  throw new Error("index.html has no #root element to mount into");
}

// One structured console record per uncaught error or unhandled rejection.
// Console only: the bootstrap carries no reporting endpoint by design.
installClientErrorHandlers(
  window,
  (record) => console.error(record),
  buildId(document),
);

const root = createRoot(container);

const gallery =
  import.meta.env.DEV && window.location.pathname.includes("/dev/components");

if (gallery) {
  void import("../console/gallery").then(({ ComponentGallery }) => {
    root.render(
      <StrictMode>
        <ComponentGallery />
      </StrictMode>,
    );
  });
} else {
  try {
    loadRuntimeBootstrap();
    root.render(
      <StrictMode>
        <ConsoleErrorBoundary region="console">
          <Providers>
            <Routes>
              <Route
                path="packaging-runs/:runId"
                element={<PackagingRunPage />}
              />
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
        </ConsoleErrorBoundary>
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
}

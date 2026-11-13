import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The Hub serves these assets from /console/ and the APIs from the same origin,
// so the base path is not "/" and asset URLs must carry the prefix. Getting this
// wrong produces an app that works under `vite dev` and 404s once embedded.
// In production the Hub renders the tenant bootstrap into the shell before
// serving it. `vite dev` serves the shell straight from source, so without this
// the console would correctly refuse to start and every developer would meet a
// configuration error rather than the app.
//
// Deliberately dev-only. A build that carried a default would ship a document
// claiming a tenant nobody configured, and the marker surviving into `dist` is
// what lets the Hub -- and only the Hub -- decide whose console this is.
const hydrateForDevelopment = {
  name: "conduit-console-dev-bootstrap",
  apply: "serve",
  transformIndexHtml(html: string): string {
    const bootstrap = {
      schemaVersion: "conduit.console.bootstrap/v1",
      tenant: { id: "conduit", displayName: "Conduit" },
      brand: { title: "Conduit", subtitle: "Control plane", accent: "#2d72d2", icon: "dashboard" },
      provenance: { claimClass: "conduit-extension", evidenceRef: "conduit://console/default" },
    };
    const content = JSON.stringify(bootstrap).replaceAll("&", "&amp;").replaceAll('"', "&#34;");
    return html.replace(
      "<!--conduit-console-bootstrap-->",
      `<meta name="x-conduit-console-bootstrap" content="${content}" />`,
    );
  },
} as const;

const bridge = { target: "http://127.0.0.1:8787", changeOrigin: true };

export default defineConfig({
  base: "/console/",
  plugins: [react(), tailwindcss(), hydrateForDevelopment],
  build: {
    // The Hub embeds this committed, content-hashed tree. Keeping it under the
    // Go package makes ordinary/offline `go build` independent of Node and the
    // package registry; CI rebuilds it and rejects drift.
    outDir: process.env["PLATFORM_CONSOLE_OUT_DIR"] ?? "../internal/consoleui/dist",
    // The Go embed directive takes whatever is here verbatim, so a stale file
    // from a previous layout would ship inside the binary.
    emptyOutDir: true,
    // Source maps add roughly 3.7 MB, expose implementation source in the Hub,
    // and are not required for the operator contract.
    sourcemap: false,
  },
  server: {
    proxy: { "/api/assistant": bridge },
  },
  preview: {
    proxy: { "/api/assistant": bridge },
  },
});

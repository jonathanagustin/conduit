import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";

// Dev-only stand-in for the marker the Hub replaces at serve time.
const hydrateForDevelopment = {
  name: "conduit-console-dev-bootstrap",
  apply: "serve" as const,
  transformIndexHtml(html: string): string {
    const bootstrap = {
      schemaVersion: "conduit.console.bootstrap/v1",
      tenant: { id: "conduit", displayName: "Conduit" },
      brand: {
        title: "Conduit",
        subtitle: "Control plane",
        accent: "#2d72d2",
        icon: "dashboard",
      },
      provenance: {
        claimClass: "conduit-extension",
        evidenceRef: "conduit://console/default",
      },
    };
    const content = JSON.stringify(bootstrap)
      .replaceAll("&", "&amp;")
      .replaceAll('"', "&#34;");
    return html.replace(
      "<!--conduit-console-bootstrap-->",
      `<meta name="x-conduit-console-bootstrap" content="${content}" />`,
    );
  },
};

const bridge = { target: "http://127.0.0.1:8787", changeOrigin: true };

export default defineConfig({
  base: "/console/",
  resolve: {
    alias: {
      "@conduit/ontology-client": fileURLToPath(
        new URL("./src/api/ontology-client/index.ts", import.meta.url),
      ),
      "@": fileURLToPath(new URL("./", import.meta.url)),
    },
  },
  plugins: [react(), tailwindcss(), hydrateForDevelopment],
  // The template's PostCSS config is for Next. The embed uses @tailwindcss/vite.
  css: { postcss: { plugins: [] } },
  build: {
    outDir:
      process.env["PLATFORM_CONSOLE_OUT_DIR"] ?? "../internal/consoleui/dist",
    emptyOutDir: true,
    sourcemap: false,
    // The Hub serves the console with font-src 'self', which refuses data:
    // URLs, so fonts ship as files rather than being inlined into the CSS.
    assetsInlineLimit: (file: string) =>
      /\.(woff2?|ttf|otf)$/.test(file) ? false : undefined,
  },
  server: {
    proxy: { "/api/assistant": bridge },
  },
  preview: {
    proxy: { "/api/assistant": bridge },
  },
});

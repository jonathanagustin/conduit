import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath, URL } from "node:url";
import { defineConfig, lazyPlugins } from "vite-plus";

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

// Browser traffic never proxies directly to an ACP bridge process; workspace
// coding-agent sessions route exclusively through Hub /v1/dev-workspaces/.../acp.
export default defineConfig({
  lint: {
    ignorePatterns: ["src/api/live-event-validator.generated.js"],
    jsPlugins: [{ name: "vite-plus", specifier: "vite-plus/oxlint-plugin" }],
    rules: { "vite-plus/prefer-vite-plus-imports": "error" },
    options: { typeAware: true, typeCheck: true },
  },
  fmt: {
    printWidth: 80,
    tabWidth: 2,
    useTabs: false,
    semi: true,
    singleQuote: false,
    jsxSingleQuote: false,
    trailingComma: "all",
    bracketSpacing: true,
    bracketSameLine: false,
    arrowParens: "always",
    endOfLine: "lf",
    quoteProps: "as-needed",
    sortTailwindcss: {
      functions: ["clsx", "cva", "tw", "twMerge", "cn", "twJoin", "tv"],
    },
    sortPackageJson: false,
    ignorePatterns: [
      "**/node_modules/**",
      "**/.vercel/**",
      "**/out/**",
      "**/build/**",
      "**/coverage/**",
      "**/*.md",
      "**/*.mdx",
      "**/*.yaml",
      "**/*.yml",
      "**/a2ui-validator.generated.js",
      "**/live-event-validator.generated.js",
      // Generated and integrity-pinned bytes have dedicated drift/hash checks.
      "src/api/ontology-client/generated.ts",
      "src/console/operator/assets/world-borders-110m.json",
    ],
  },
  base: "/console/",
  // Verification owns a disposable cache; ordinary development keeps Vite defaults.
  cacheDir: process.env["PLATFORM_CONSOLE_CACHE_DIR"],
  resolve: {
    alias: {
      "@conduit/ontology-client": fileURLToPath(
        new URL("./src/api/ontology-client/index.ts", import.meta.url),
      ),
      "@": fileURLToPath(new URL("./", import.meta.url)),
    },
  },
  plugins: lazyPlugins(() => [react(), tailwindcss(), hydrateForDevelopment]),
  // Tailwind is handled by the Vite plugin; no separate PostCSS pipeline.
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
});

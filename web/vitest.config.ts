import { fileURLToPath } from "node:url";
import { defineConfig } from "vite-plus";

export default defineConfig({
  cacheDir: process.env["PLATFORM_CONSOLE_CACHE_DIR"],
  resolve: {
    alias: {
      "@conduit/ontology-client": fileURLToPath(
        new URL("./src/api/ontology-client/index.ts", import.meta.url),
      ),
    },
  },
  test: {
    include: ["src/test/unit/**/*.test.ts"],
    environment: "node",
  },
  css: { postcss: { plugins: [] } },
});

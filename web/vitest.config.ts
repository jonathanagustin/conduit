import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

export default defineConfig({
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
});

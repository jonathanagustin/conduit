import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The Hub serves these assets from /console/ and the APIs from the same origin,
// so the base path is not "/" and asset URLs must carry the prefix. Getting this
// wrong produces an app that works under `vite dev` and 404s once embedded.
export default defineConfig({
  base: "/console/",
  plugins: [react()],
  build: {
    // The Hub embeds this committed, content-hashed tree. Keeping it under the
    // Go package makes ordinary/offline `go build` independent of Node and the
    // package registry; CI rebuilds it and rejects drift.
    outDir:
      process.env["PLATFORM_CONSOLE_OUT_DIR"] ?? "../internal/consoleui/dist",
    // The Go embed directive takes whatever is here verbatim, so a stale file
    // from a previous layout would ship inside the binary.
    emptyOutDir: true,
    // Source maps add roughly 3.7 MB, expose implementation source in the Hub,
    // and are not required for the operator contract.
    sourcemap: false,
  },
  server: {
    // Same-origin in production; in development the Hub is a separate process,
    // so proxy rather than teaching the client about two origins.
    proxy: {
      "/v1": { target: "http://localhost:8080", changeOrigin: true },
    },
  },
});

import { fileURLToPath, URL } from "node:url";

import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const backend = process.env.BMANGA_VITE_BACKEND || "http://127.0.0.1:8765";

export default defineConfig({
  base: "/v2/",
  plugins: [react()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  publicDir: false,
  build: {
    outDir: fileURLToPath(new URL("../web/v2", import.meta.url)),
    emptyOutDir: true,
    target: "es2022",
    sourcemap: false,
    manifest: "manifest.json",
    assetsDir: "assets",
  },
  server: {
    host: "127.0.0.1",
    open: false,
    proxy: {
      "/api": backend,
      "/cover": backend,
      "/page": backend,
      "/page-cache": backend,
    },
  },
  preview: {
    host: "127.0.0.1",
    open: false,
  },
});

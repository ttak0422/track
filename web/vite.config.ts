/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Minimal process typing so the config compiles without pulling in @types/node; the Node runtime that
// runs Vite provides it.
declare const process: { env: Record<string, string | undefined> };

// The static-site export build (VITE_TRACK_STATIC=1) uses a relative base so the bundle works under any
// GitHub Pages subpath; the live server build keeps the absolute root base it serves from.
const staticBuild = process.env.VITE_TRACK_STATIC === "1";

export default defineConfig({
  base: staticBuild ? "./" : "/",
  plugins: [react()],
  build: {
    manifest: true,
  },
  server: {
    proxy: {
      "/api": "http://127.0.0.1:8765",
    },
  },
  test: {
    // jsdom gives the pure helpers a window (viewport size, URL) without a real browser, and the
    // components a DOM to render into.
    environment: "jsdom",
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: ["./src/test-setup.ts"],
  },
});

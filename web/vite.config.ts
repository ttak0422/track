/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
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

/// <reference types="vite/client" />

interface Window {
  // Injected by the Go server into index.html as the configured default theme (system/light/dark).
  __trackDefaultTheme?: string;
}

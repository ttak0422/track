/// <reference types="vite/client" />

interface Window {
  // Injected by the Go server into index.html as the configured default theme (system/light/dark).
  __trackDefaultTheme?: string;
  // Injected by the Go server as a token unique to its process, so the tab strip can tell a reload
  // (same token) from a fresh launch (new token) and drop restored tabs on the latter.
  __trackSession?: string;
}

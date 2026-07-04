/// <reference types="vite/client" />

interface Window {
  // Injected by the Go server into index.html as the configured default theme (system/light/dark).
  __trackDefaultTheme?: string;
  // Injected by the Go server as a token unique to its process, so the tab strip can tell a reload
  // (same token) from a fresh launch (new token) and drop restored tabs on the latter.
  __trackSession?: string;
  // Injected by the static export as the root note's published id, so the app can redirect straight to
  // the start page on launch. Empty on the live server (it uses the heatmap home instead).
  __trackStartPage?: string;
  // Injected by the prerender as the dehydrated react-query cache for the page, so the client hydrates
  // the prerendered markup without refetching. Its shape is react-query's DehydratedState.
  __TRACK_STATE__?: import("@tanstack/react-query").DehydratedState;
}

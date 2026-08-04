// Dev-only design preview selection (ADR 0068): `?theme=dark&variant=candidate-a` on any dev-server
// URL applies a theme and a candidate token set (candidates.css) before first paint, so screenshot
// tooling (scripts/design-shots.mjs) can address every combination as a plain URL.

export type DesignPreview = { theme?: "light" | "dark"; variant?: string };

export function parseDesignPreview(search: string): DesignPreview {
  const params = new URLSearchParams(search);
  const theme = params.get("theme");
  const variant = params.get("variant");
  return {
    ...(theme === "light" || theme === "dark" ? { theme } : {}),
    ...(variant ? { variant } : {}),
  };
}

export function applyDesignPreview(preview: DesignPreview): void {
  if (preview.theme) {
    // Through the same key the index.html bootstrap and ThemeMenu read, so the app's own theme
    // plumbing carries the choice — a bare data-theme attribute would be deleted by ThemeMenu's
    // "system" effect right after mount. Persisting is what clicking the theme toggle does too.
    localStorage.setItem("track.theme", preview.theme);
    document.documentElement.dataset.theme = preview.theme;
  }
  if (preview.variant) document.documentElement.dataset.themeVariant = preview.variant;
}

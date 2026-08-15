import { useEffect, useState } from "react";

// The explicit theme selection, shared by the rail's Settings menu and the phone's floating dock:
// both write the same storage key and the same data-theme attribute, so whichever surface is live
// speaks for the whole workspace. "system" is the neutral default and stores nothing.

export type ThemeMode = "system" | "light" | "dark";

export const themeModes: ThemeMode[] = ["system", "light", "dark"];

export const themeStorageKey = "track.theme";

export function storedTheme(): ThemeMode {
  // During prerender there is no localStorage/window; return the neutral default so SSR does not
  // crash (the settings surfaces are closed initially, so the value is not in the prerendered
  // output anyway).
  if (typeof window === "undefined") return "system";
  const value = localStorage.getItem(themeStorageKey);
  if (value === "light" || value === "dark") return value;
  // Fall back to the server-configured default the index bootstrap recorded on window.
  const serverDefault = window.__trackDefaultTheme;
  return serverDefault === "light" || serverDefault === "dark" ? serverDefault : "system";
}

export function applyTheme(theme: ThemeMode) {
  if (theme === "system") {
    localStorage.removeItem(themeStorageKey);
    delete document.documentElement.dataset.theme;
    return;
  }
  localStorage.setItem(themeStorageKey, theme);
  document.documentElement.dataset.theme = theme;
}

export function useThemeMode(): [ThemeMode, (mode: ThemeMode) => void] {
  const [theme, setTheme] = useState<ThemeMode>(() => storedTheme());
  useEffect(() => applyTheme(theme), [theme]);
  return [theme, setTheme];
}

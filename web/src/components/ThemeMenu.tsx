import { useEffect, useState } from "react";

type ThemeMode = "system" | "light" | "dark";

const storageKey = "track.theme";
const themeModes: ThemeMode[] = ["system", "light", "dark"];

export function ThemeMenu() {
  const [theme, setTheme] = useState<ThemeMode>(() => storedTheme());

  useEffect(() => {
    if (theme === "system") {
      localStorage.removeItem(storageKey);
      delete document.documentElement.dataset.theme;
      return;
    }

    localStorage.setItem(storageKey, theme);
    document.documentElement.dataset.theme = theme;
  }, [theme]);

  return (
    <div className="theme-switch" role="group" aria-label="Theme">
      {themeModes.map((mode) => (
        <button
          aria-pressed={theme === mode}
          key={mode}
          type="button"
          onClick={() => setTheme(mode)}
        >
          {label(mode)}
        </button>
      ))}
    </div>
  );
}

function storedTheme(): ThemeMode {
  const value = localStorage.getItem(storageKey);
  return value === "light" || value === "dark" ? value : "system";
}

function label(mode: ThemeMode): string {
  return mode[0].toUpperCase() + mode.slice(1);
}

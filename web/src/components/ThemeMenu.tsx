import { useEffect, useRef, useState } from "react";

type ThemeMode = "system" | "light" | "dark";

const storageKey = "track.theme";
const themeModes: ThemeMode[] = ["system", "light", "dark"];

export function ThemeMenu() {
  const [theme, setTheme] = useState<ThemeMode>(() => storedTheme());
  const [open, setOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (theme === "system") {
      localStorage.removeItem(storageKey);
      delete document.documentElement.dataset.theme;
      return;
    }

    localStorage.setItem(storageKey, theme);
    document.documentElement.dataset.theme = theme;
  }, [theme]);

  useEffect(() => {
    if (!open) {
      return;
    }

    function onPointerDown(event: MouseEvent) {
      if (!menuRef.current?.contains(event.target as Node)) {
        setOpen(false);
      }
    }

    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        setOpen(false);
      }
    }

    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  return (
    <div className="app-menu" ref={menuRef}>
      <button
        className="rail-button"
        type="button"
        aria-label="Settings"
        title="Settings"
        aria-haspopup="true"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        <GearIcon />
      </button>
      {open ? (
        <div className="menu-panel rail-menu-panel">
          <section className="menu-section" aria-label="Theme">
            <h2>Theme</h2>
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
          </section>
        </div>
      ) : null}
    </div>
  );
}

function GearIcon() {
  return (
    <svg
      className="rail-icon-svg"
      viewBox="0 0 24 24"
      width="20"
      height="20"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
    </svg>
  );
}

function storedTheme(): ThemeMode {
  const value = localStorage.getItem(storageKey);
  if (value === "light" || value === "dark") {
    return value;
  }
  // Fall back to the server-configured default the index bootstrap recorded on window.
  const serverDefault = window.__trackDefaultTheme;
  return serverDefault === "light" || serverDefault === "dark" ? serverDefault : "system";
}

function label(mode: ThemeMode): string {
  return mode[0].toUpperCase() + mode.slice(1);
}

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
        className="menu-button"
        type="button"
        aria-label="Open menu"
        aria-haspopup="true"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        <span />
        <span />
        <span />
      </button>
      {open ? (
        <div className="menu-panel">
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

function storedTheme(): ThemeMode {
  const value = localStorage.getItem(storageKey);
  return value === "light" || value === "dark" ? value : "system";
}

function label(mode: ThemeMode): string {
  return mode[0].toUpperCase() + mode.slice(1);
}

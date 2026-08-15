import { useEffect, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { hoverOpen } from "./hoverOpen";
import { railAnchor } from "./railAnchor";
import { themeModes, useThemeMode } from "../themeState";

const fontScaleKey = "track.fontScale";
const contentWidthKey = "track.contentWidth";
// Whole-UI font scale, applied through the --font-scale CSS var every font-size is wrapped in.
const fontScales: { label: string; value: number }[] = [
  { label: "S", value: 0.85 },
  { label: "M", value: 1 },
  { label: "L", value: 1.15 },
  { label: "XL", value: 1.3 },
];
// Reading-column max width, applied through --content-width on .note-reader and the prose measure via
// --content-measure. "none" removes the cap so prose fills the viewport for wide-display use.
const defaultContentWidth = "880px";
const contentWidths: { label: string; value: string }[] = [
  { label: "Normal", value: "880px" },
  { label: "Wide", value: "1280px" },
  { label: "Full", value: "none" },
];

export function ThemeMenu() {
  // The theme lives in the shared themeState module: the phone's floating dock writes the same key,
  // so either surface switching themes shows up on the other.
  const [theme, setTheme] = useThemeMode();
  const [fontScale, setFontScale] = useState<number>(() => storedFontScale());
  const [contentWidth, setContentWidth] = useState<string>(() => storedContentWidth());
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<CSSProperties | undefined>(undefined);
  const menuRef = useRef<HTMLDivElement>(null);
  const overlayRef = useRef<HTMLDivElement>(null);
  const toggleRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (fontScale === 1) {
      localStorage.removeItem(fontScaleKey);
      document.documentElement.style.removeProperty("--font-scale");
      return;
    }

    localStorage.setItem(fontScaleKey, String(fontScale));
    document.documentElement.style.setProperty("--font-scale", String(fontScale));
  }, [fontScale]);

  useEffect(() => {
    if (contentWidth === defaultContentWidth) {
      localStorage.removeItem(contentWidthKey);
      document.documentElement.style.removeProperty("--content-width");
      delete document.documentElement.dataset.contentWidth;
      return;
    }

    localStorage.setItem(contentWidthKey, contentWidth);
    document.documentElement.style.setProperty("--content-width", contentWidth);
    // The "Full" setting stores none, which the aside rail's calc() cap cannot digest, so mirror
    // non-default widths into an attribute its rules can match (see .note-aside in styles.css).
    document.documentElement.dataset.contentWidth = contentWidth;
  }, [contentWidth]);

  // Hover opens and closes the menu, both on a timer. Opening waits so that sweeping the pointer down
  // the rail does not flash the panel under every button it crosses; closing waits so the pointer can
  // cross the gap between the rail and the panel without the panel going out from under it.
  const hoverTimer = useRef<number | undefined>(undefined);

  function cancelHover() {
    if (hoverTimer.current !== undefined) window.clearTimeout(hoverTimer.current);
    hoverTimer.current = undefined;
  }

  // The panel is placed from the gear's own rect, so it lands beside the button whatever the dock is
  // carrying above it. Reading it as the menu opens keeps it right after a scroll or a resize.
  function toggle(next: boolean) {
    if (next) setAnchor(railAnchor(toggleRef.current));
    setOpen(next);
  }

  function scheduleOpen(next: boolean) {
    cancelHover();
    hoverTimer.current = window.setTimeout(() => toggle(next), next ? 180 : 220);
  }

  useEffect(() => cancelHover, []);

  useEffect(() => {
    if (!open) {
      return;
    }

    function onPointerDown(event: MouseEvent) {
      if (
        !menuRef.current?.contains(event.target as Node) &&
        !overlayRef.current?.contains(event.target as Node)
      ) {
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
    // The panel is fixed and so sits in a gap beside the rail, but it stays a DOM descendant of this
    // container, so pointer events from it still count as being in the menu. Crossing that gap does
    // fire a leave, hence the delay: it is long enough to cross and short enough not to feel stuck.
    <div
      className="app-menu"
      ref={menuRef}
      {...hoverOpen(
        () => scheduleOpen(true),
        () => scheduleOpen(false),
      )}
    >
      <button
        ref={toggleRef}
        className="rail-button"
        type="button"
        aria-label="Settings"
        title="Settings"
        aria-haspopup="true"
        aria-expanded={open}
        onClick={() => {
          cancelHover();
          toggle(!open);
        }}
      >
        <GearIcon />
      </button>
      {open
        ? createPortal(
            // The rail is a fixed stacking context; the settings surface must be a body sibling for
            // its layer to compete with previews, while the trigger remains in the rail.
            <div ref={overlayRef}>
              <div className="menu-panel rail-menu-panel" style={anchor}>
                <h2 className="rail-panel-title">Settings</h2>
                <section className="menu-section" aria-label="Theme">
                  <h3>Theme</h3>
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
                <section className="menu-section" aria-label="Text size">
                  <h3>Text size</h3>
                  <div className="theme-switch" role="group" aria-label="Text size">
                    {fontScales.map((scale) => (
                      <button
                        aria-pressed={fontScale === scale.value}
                        key={scale.value}
                        type="button"
                        onClick={() => setFontScale(scale.value)}
                      >
                        {scale.label}
                      </button>
                    ))}
                  </div>
                </section>
                <section className="menu-section" aria-label="Content width">
                  <h3>Content width</h3>
                  <div className="theme-switch" role="group" aria-label="Content width">
                    {contentWidths.map((width) => (
                      <button
                        aria-pressed={contentWidth === width.value}
                        key={width.value}
                        type="button"
                        onClick={() => setContentWidth(width.value)}
                      >
                        {width.label}
                      </button>
                    ))}
                  </div>
                </section>
              </div>
            </div>,
            document.body,
          )
        : null}
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
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
    </svg>
  );
}

function storedFontScale(): number {
  if (typeof window === "undefined") return 1;
  const value = Number(localStorage.getItem(fontScaleKey));
  return fontScales.some((scale) => scale.value === value) ? value : 1;
}

function storedContentWidth(): string {
  if (typeof window === "undefined") return defaultContentWidth;
  const value = localStorage.getItem(contentWidthKey);
  return contentWidths.some((width) => width.value === value) ? (value as string) : defaultContentWidth;
}

function label(mode: string): string {
  return mode[0].toUpperCase() + mode.slice(1);
}

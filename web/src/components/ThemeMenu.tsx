import { useEffect, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { hoverOpen } from "./hoverOpen";
import { IconSettings, RailIcon } from "./icons";
import { railAnchor } from "./railAnchor";
import { themeModes, useThemeMode } from "../themeState";

const fontSizeKey = "track.fontSize";
const previewFontSizeKey = "track.previewFontSize";
const contentWidthKey = "track.contentWidth";
// The size the whole sheet is written against: .markdown-view is 16px, and every other font-size is a
// px literal picked to sit beside it. So a surface asked for a given px size gets it by carrying a
// scale of that size over this base — the reading surface through --font-scale on the root, a preview
// window through --preview-font-scale, which .wiki-preview rebinds as its own --font-scale. Both
// settings are the same number over the same base, which is what makes equal numbers render equal.
const baseFontSize = 16;
// Text size in px, not an abstract step: the reader and the previews are set from one list, so the
// two numbers are comparable and a matching pair is a matching size.
const fontSizes = [14, 16, 18, 20];
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
  const [fontSize, setFontSize] = useState<number>(() => storedFontSize(fontSizeKey));
  const [previewFontSize, setPreviewFontSize] = useState<number>(() => storedFontSize(previewFontSizeKey));
  const [contentWidth, setContentWidth] = useState<string>(() => storedContentWidth());
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<CSSProperties | undefined>(undefined);
  const menuRef = useRef<HTMLDivElement>(null);
  const overlayRef = useRef<HTMLDivElement>(null);
  const toggleRef = useRef<HTMLButtonElement>(null);

  useEffect(() => applyFontSize(fontSizeKey, "--font-scale", fontSize), [fontSize]);
  useEffect(
    () => applyFontSize(previewFontSizeKey, "--preview-font-scale", previewFontSize),
    [previewFontSize],
  );

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
        <RailIcon Icon={IconSettings} />
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
                    {fontSizes.map((size) => (
                      <button
                        aria-pressed={fontSize === size}
                        key={size}
                        type="button"
                        onClick={() => setFontSize(size)}
                      >
                        {size}px
                      </button>
                    ))}
                  </div>
                </section>
                {/* The preview windows are set here rather than on their own chrome: a window's size is
                    a standing preference, not something to redecide per preview, and the number only
                    means anything read against the reader's number directly above it. */}
                <section className="menu-section" aria-label="Preview text size">
                  <h3>Preview text size</h3>
                  <div className="theme-switch" role="group" aria-label="Preview text size">
                    {fontSizes.map((size) => (
                      <button
                        aria-pressed={previewFontSize === size}
                        key={size}
                        type="button"
                        onClick={() => setPreviewFontSize(size)}
                      >
                        {size}px
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

// A size is stored only while it differs from the base, which is also the CSS fallback both scales
// carry — so a reader on defaults has neither key nor custom property, and the two surfaces start out
// at the same number.
function applyFontSize(key: string, property: string, size: number) {
  if (size === baseFontSize) {
    localStorage.removeItem(key);
    document.documentElement.style.removeProperty(property);
    return;
  }

  localStorage.setItem(key, String(size));
  document.documentElement.style.setProperty(property, String(size / baseFontSize));
}

function storedFontSize(key: string): number {
  if (typeof window === "undefined") return baseFontSize;
  const value = Number(localStorage.getItem(key));
  return fontSizes.includes(value) ? value : baseFontSize;
}

function storedContentWidth(): string {
  if (typeof window === "undefined") return defaultContentWidth;
  const value = localStorage.getItem(contentWidthKey);
  return contentWidths.some((width) => width.value === value) ? (value as string) : defaultContentWidth;
}

function label(mode: string): string {
  return mode[0].toUpperCase() + mode.slice(1);
}

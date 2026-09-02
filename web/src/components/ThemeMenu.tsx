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
// The range a typed size is held to. The floor is set by the chrome rather than by the prose: the
// sheet's smallest scaled text is calc(10px * --font-scale), so 13 puts list metadata at 8.1px and
// the mono section labels at 8.9px — about where uppercase small-caps stop resolving, and just under
// the old S step. The ceiling is twice the base, past which the settings panel's own 214px frame and
// the tab strip's fixed 336px tab wrap and ellipsise away what they are labelling.
const minFontSize = 13;
const maxFontSize = 32;
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
  const fontSize = useFontSize(fontSizeKey, "--font-scale");
  const previewFontSize = useFontSize(previewFontSizeKey, "--preview-font-scale");
  const [contentWidth, setContentWidth] = useState<string>(() => storedContentWidth());
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<CSSProperties | undefined>(undefined);
  const menuRef = useRef<HTMLDivElement>(null);
  const overlayRef = useRef<HTMLDivElement>(null);
  const toggleRef = useRef<HTMLButtonElement>(null);

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
                <SizeField id="settings-text-size" name="Text size" setting={fontSize} />
                {/* The preview windows are set here rather than on their own chrome: a window's size is
                    a standing preference, not something to redecide per preview, and the number only
                    means anything read against the reader's number directly above it. */}
                <SizeField id="settings-preview-text-size" name="Preview text size" setting={previewFontSize} />
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

// The two size settings behave identically, so they share one hook. The field keeps its own draft
// text and the size follows it only while that text is a whole number in range: clearing the field to
// retype it, or passing through "1" on the way to "18", leaves the page at the size it already had
// instead of resizing out from under the typing. Leaving the field puts the live size back in it, so
// a rejected draft does not sit there looking accepted. There is no separate commit on Enter or on
// blur, which is also what keeps the native spinner and the arrow keys live as they step.
function useFontSize(key: string, property: string): SizeSetting {
  const [size, setSize] = useState<number>(() => storedFontSize(key));
  const [draft, setDraft] = useState<string>(() => String(size));

  useEffect(() => applyFontSize(key, property, size), [key, property, size]);

  return {
    draft,
    edit(next: string) {
      setDraft(next);
      if (validFontSize(Number(next))) setSize(Number(next));
    },
    restore() {
      setDraft(String(size));
    },
  };
}

interface SizeSetting {
  draft: string;
  edit: (next: string) => void;
  restore: () => void;
}

// The unit stands beside the field as text: an underline input (design.md variant 5) has no box to
// put a suffix inside.
function SizeField({ id, name, setting }: { id: string; name: string; setting: SizeSetting }) {
  return (
    <section className="menu-section">
      <h3>
        <label htmlFor={id}>{name}</label>
      </h3>
      <div className="size-field">
        <input
          className="size-input"
          id={id}
          inputMode="numeric"
          max={maxFontSize}
          min={minFontSize}
          onBlur={setting.restore}
          onChange={(event) => setting.edit(event.target.value)}
          step={1}
          type="number"
          value={setting.draft}
        />
        px
      </div>
    </section>
  );
}

// An empty field, a half-typed number, and a stored value from an older build all fail this the same
// way — by leaving the size alone.
function validFontSize(size: number): boolean {
  return Number.isInteger(size) && size >= minFontSize && size <= maxFontSize;
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
  return validFontSize(value) ? value : baseFontSize;
}

function storedContentWidth(): string {
  if (typeof window === "undefined") return defaultContentWidth;
  const value = localStorage.getItem(contentWidthKey);
  return contentWidths.some((width) => width.value === value) ? (value as string) : defaultContentWidth;
}

function label(mode: string): string {
  return mode[0].toUpperCase() + mode.slice(1);
}

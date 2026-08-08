import { useEffect, useState } from "react";
import "./candidates.css";
import "./gallery.css";

// The dev-only design playground (ADR 0068): every control variant from docs/spec/design.md rendered
// with its canonical markup, under any candidate token set from candidates.css. Switching a candidate
// sets data-theme-variant on <html>, so the page's own chrome — and, after navigating away, the whole
// app — previews the candidate; the compare grid scopes each candidate to one card for side-by-side.

// The [data-theme-variant] blocks in candidates.css are the source of truth; this list must match
// them, and GalleryView.test.tsx fails when it drifts (scripts/design-shots.mjs derives its default
// variant list from the same file).
export const candidateVariants = ["candidate-a", "candidate-b"];

type Theme = "light" | "dark";

export function GalleryView() {
  const [variant, setVariant] = useState<string | null>(
    () => document.documentElement.dataset.themeVariant ?? null,
  );
  // Explicit light/dark only: candidates override the [data-theme] cascade, which the base theme's
  // prefers-color-scheme block would outrank under "system" (see candidates.css).
  const [theme, setTheme] = useState<Theme>(() =>
    document.documentElement.dataset.theme === "dark" ? "dark" : "light",
  );

  // Restore whatever theme/variant selection was active before the playground forced its own.
  useEffect(() => {
    const el = document.documentElement;
    const prevTheme = el.dataset.theme;
    const prevVariant = el.dataset.themeVariant;
    return () => {
      if (prevTheme === undefined) delete el.dataset.theme;
      else el.dataset.theme = prevTheme;
      if (prevVariant === undefined) delete el.dataset.themeVariant;
      else el.dataset.themeVariant = prevVariant;
    };
  }, []);

  useEffect(() => {
    if (variant) document.documentElement.dataset.themeVariant = variant;
    else delete document.documentElement.dataset.themeVariant;
  }, [variant]);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);

  return (
    <section className="design-gallery">
      <header>
        <h1>Design playground</h1>
        <div className="gallery-switch" role="group" aria-label="Candidate">
          <button type="button" aria-pressed={variant === null} onClick={() => setVariant(null)}>
            base
          </button>
          {candidateVariants.map((name) => (
            <button
              key={name}
              type="button"
              aria-pressed={variant === name}
              onClick={() => setVariant(name)}
            >
              {name}
            </button>
          ))}
        </div>
        <div className="gallery-switch" role="group" aria-label="Theme">
          {(["light", "dark"] as const).map((mode) => (
            <button
              key={mode}
              type="button"
              aria-pressed={theme === mode}
              onClick={() => setTheme(mode)}
            >
              {mode}
            </button>
          ))}
        </div>
      </header>
      <p className="gallery-hint">
        Candidates live in web/src/dev/candidates.css; any dev URL takes ?variant=…&theme=…; `make
        design-shots` screenshots every combination.
      </p>

      <div className="gallery-section">
        <h2>Candidates side by side</h2>
        <div className="gallery-compare">
          <MiniCard />
          {candidateVariants.map((name) => (
            <MiniCard key={name} variant={name} />
          ))}
        </div>
      </div>

      <div className="gallery-section">
        <h2>1 · Text control</h2>
        <div className="gallery-row">
          <button className="rail-button" type="button" aria-label="Sample control">
            <GlyphIcon />
          </button>
          <button className="rail-button active" type="button" aria-label="Sample control, active">
            <GlyphIcon />
          </button>
          <nav className="note-tags" aria-label="Sample tags">
            <a>#design</a> <a>#spec</a>
          </nav>
        </div>
      </div>

      <div className="gallery-section">
        <h2>2 · Quiet chip</h2>
        <div className="gallery-row">
          <button className="mermaid-control" type="button" aria-label="Zoom in">
            +
          </button>
          <button className="mermaid-control" type="button" aria-label="Zoom out">
            −
          </button>
          <button className="mermaid-control" type="button" aria-label="Reset">
            ⌂
          </button>
        </div>
      </div>

      <div className="gallery-section">
        <h2>3 · Floating layer</h2>
        <div className="menu-panel">
          <section className="menu-section" aria-label="Sample menu">
            <h2>Theme</h2>
            <div className="theme-switch" role="group" aria-label="Sample switch">
              <button type="button" aria-pressed={false}>
                System
              </button>
              <button type="button" aria-pressed={true}>
                Light
              </button>
              <button type="button" aria-pressed={false}>
                Dark
              </button>
            </div>
          </section>
        </div>
      </div>

      <div className="gallery-section">
        <h2>4 · Filled action</h2>
        <div className="modal-actions">
          <button type="button">Cancel</button>
          <button className="danger-button" type="button">
            Delete
          </button>
        </div>
      </div>

      <div className="gallery-section">
        <h2>5 · Underline input</h2>
        <input className="modal-input" placeholder="Rename note…" aria-label="Sample input" />
      </div>

      <div className="gallery-section">
        <h2>6 · Section label</h2>
        <div className="results-group">Backlinks</div>
      </div>

      <div className="gallery-section">
        <h2>Prose</h2>
        <div className="markdown-view">
          <p>
            Body text with a <a className="gallery-card-link">link</a>, some{" "}
            <code>inline code</code>, and 日本語の本文が混在する段落。Hierarchy comes from ink, not
            boxes.
          </p>
        </div>
      </div>

      <div className="gallery-section">
        <h2>Tokens</h2>
        <div className="gallery-row">
          <Swatches
            names={[
              "--bg",
              "--panel",
              "--panel-soft",
              "--text",
              "--muted",
              "--faint",
              "--line",
              "--line-strong",
              "--line-node",
              "--mark",
              "--danger",
              "--chart-1",
              "--chart-2",
              "--chart-3",
              "--chart-4",
              "--chart-5",
              "--chart-6",
            ]}
          />
        </div>
        <div className="gallery-row" style={{ marginTop: 12 }}>
          {(["--radius-sm", "--radius", "--radius-lg"] as const).map((name) => (
            <div key={name} className="gallery-radius" style={{ borderRadius: `var(${name})` }}>
              {name.slice(2)}
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

export default GalleryView;

function MiniCard({ variant }: { variant?: string }) {
  return (
    <div className="gallery-card" {...(variant ? { "data-theme-variant": variant } : {})}>
      <div className="gallery-card-name">{variant ?? "base"}</div>
      <div className="gallery-card-panel">
        <p>Panel surface / ノート本文</p>
        <p className="gallery-card-muted">muted secondary line</p>
        <a className="gallery-card-link">a link</a>
      </div>
      <Swatches names={["--bg", "--panel", "--panel-soft", "--mark", "--chart-2", "--chart-3"]} />
    </div>
  );
}

function Swatches({ names }: { names: readonly string[] }) {
  return (
    <div className="gallery-swatches">
      {names.map((name) => (
        <div
          key={name}
          className="gallery-swatch"
          style={{ background: `var(${name})` }}
          title={name}
        />
      ))}
    </div>
  );
}

function GlyphIcon() {
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
      <circle cx="12" cy="12" r="8" />
      <path d="M12 8v8M8 12h8" />
    </svg>
  );
}

import { useEffect, useRef, useState, type CSSProperties } from "react";
import { editorModes, useNoteControls, type EditorMode } from "../noteControls";
import { NoteActionsMenu } from "./NoteActionsMenu";
import { hoverOpen } from "./hoverOpen";
import { railAnchor } from "./railAnchor";

// The open note's controls, in the rail under the workspace's views. They used to float over the
// note's top-right corner, which put chrome in the reading column and moved with it; the rail is
// where every other persistent control already is. The note group only exists while a note is open,
// so the rail keeps carrying nothing but navigation the rest of the time.
export function NoteRailControls() {
  const { mode, setMode, follow, setFollow, actions } = useNoteControls();
  if (!actions) return null;

  const followLabel = `Follow the editor: ${follow ? "On" : "Off"}`;

  return (
    <>
      <div className="rail-divider" />
      <button
        className={`rail-button${follow ? " active" : ""}`}
        type="button"
        aria-pressed={follow}
        aria-label={followLabel}
        title={followLabel}
        onClick={() => setFollow(!follow)}
      >
        <FollowIcon active={follow} />
      </button>
      <EditorModeMenu mode={mode} setMode={setMode} />
      <NoteActionsMenu getBody={actions.getBody} onMeta={actions.onMeta} onDelete={actions.onDelete} />
    </>
  );
}

function EditorModeMenu({ mode, setMode }: { mode: EditorMode; setMode: (mode: EditorMode) => void }) {
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<CSSProperties | undefined>(undefined);
  const menuRef = useRef<HTMLDivElement>(null);
  const toggleRef = useRef<HTMLButtonElement>(null);
  const closeTimer = useRef<number | undefined>(undefined);
  const label = `Display mode: ${modeLabel(mode)}`;

  function cancelClose() {
    if (closeTimer.current !== undefined) window.clearTimeout(closeTimer.current);
    closeTimer.current = undefined;
  }

  function showMenu() {
    cancelClose();
    setAnchor(railAnchor(toggleRef.current));
    setOpen(true);
  }

  function scheduleClose() {
    cancelClose();
    closeTimer.current = window.setTimeout(() => setOpen(false), 160);
  }

  useEffect(() => {
    return cancelClose;
  }, []);

  useEffect(() => {
    if (!open) return;

    function onPointerDown(event: MouseEvent) {
      if (!menuRef.current?.contains(event.target as Node)) setOpen(false);
    }

    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setOpen(false);
    }

    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  return (
    <div className="mode-menu" ref={menuRef} {...hoverOpen(showMenu, scheduleClose)}>
      <button
        ref={toggleRef}
        className="rail-button active"
        type="button"
        aria-label={label}
        title={label}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => (open ? setOpen(false) : showMenu())}
      >
        <ModeIcon mode={mode} />
      </button>
      {open ? (
        <div
          className="menu-panel note-menu-panel mode-menu-panel"
          role="menu"
          aria-label="Display mode"
          style={anchor}
        >
          {editorModes.map((each) => (
            <button
              key={each}
              type="button"
              role="menuitemradio"
              aria-checked={mode === each}
              onClick={() => {
                setMode(each);
                setOpen(false);
              }}
            >
              <ModeIcon mode={each} />
              <span>{modeLabel(each)}</span>
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function modeLabel(mode: EditorMode): string {
  switch (mode) {
    case "preview":
      return "Preview";
    case "edit":
      return "Edit";
    default:
      return "Split";
  }
}

// The mode menu pairs each glyph with its label. Preview is a rendered page, edit is a pencil, and
// split keeps the two-pane shape; the current glyph is the only one that remains in the rail.
function ModeIcon({ mode }: { mode: EditorMode }) {
  return (
    <svg
      className="rail-icon-svg"
      data-mode={mode}
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
      {mode === "preview" ? (
        <>
          <rect x="3" y="4" width="18" height="16" rx="2" />
          <line x1="7" y1="9" x2="17" y2="9" />
          <line x1="7" y1="13" x2="17" y2="13" />
          <line x1="7" y1="17" x2="13" y2="17" />
        </>
      ) : null}
      {mode === "edit" ? (
        <>
          <path d="m4 20 4.2-1 10.5-10.5-3.2-3.2L5 15.8 4 20Z" />
          <path d="m13.8 7 3.2 3.2" />
        </>
      ) : null}
      {mode === "split" ? (
        <>
          <rect x="3" y="4" width="18" height="16" rx="2" />
          <line x1="12" y1="4" x2="12" y2="20" />
          <line x1="7" y1="10" x2="9" y2="10" />
          <line x1="15" y1="10" x2="17" y2="10" />
          <line x1="7" y1="14" x2="9" y2="14" />
          <line x1="15" y1="14" x2="17" y2="14" />
        </>
      ) : null}
    </svg>
  );
}

// Follow is the editor's cursor arriving here, so the glyph is an eye: the workspace watching.
// The slash keeps its off state legible without relying on the rail's accent colour alone.
function FollowIcon({ active }: { active: boolean }) {
  return (
    <svg
      className="rail-icon-svg"
      data-state={active ? "on" : "off"}
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
      <path d="M2 12s3.6-6 10-6 10 6 10 6-3.6 6-10 6-10-6-10-6Z" />
      <circle cx="12" cy="12" r="2.6" />
      {!active ? <line x1="4" y1="4" x2="20" y2="20" /> : null}
    </svg>
  );
}

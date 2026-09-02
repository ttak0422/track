import { useEffect, useRef, useState, type CSSProperties } from "react";
import { editorModes, useNoteControls, type EditorMode } from "../noteControls";
import { NoteActionsMenu } from "./NoteActionsMenu";
import { hoverOpen } from "./hoverOpen";
import { railAnchor } from "./railAnchor";
import {
  IconArticle,
  IconColumns2,
  IconEye,
  IconEyeOff,
  IconPencil,
  RailIcon,
} from "./icons";

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

function ModeIcon({ mode }: { mode: EditorMode }) {
  const Icon = mode === "preview" ? IconArticle : mode === "edit" ? IconPencil : IconColumns2;
  return <RailIcon Icon={Icon} data-mode={mode} />;
}

function FollowIcon({ active }: { active: boolean }) {
  // The slash keeps its off state legible without relying on the rail's accent colour alone.
  return <RailIcon Icon={active ? IconEye : IconEyeOff} data-state={active ? "on" : "off"} />;
}

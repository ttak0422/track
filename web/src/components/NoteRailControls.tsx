import { editorModes, useNoteControls, type EditorMode } from "../noteControls";
import { NoteActionsMenu } from "./NoteActionsMenu";

// The open note's controls, in the rail under the workspace's views. They used to float over the
// note's top-right corner, which put chrome in the reading column and moved with it; the rail is
// where every other persistent control already is. The note group only exists while a note is open,
// so the rail keeps carrying nothing but navigation the rest of the time.
export function NoteRailControls() {
  const { mode, setMode, follow, setFollow, actions } = useNoteControls();
  if (!actions) return null;

  return (
    <>
      <div className="rail-divider" />
      <button
        className={`rail-button${follow ? " active" : ""}`}
        type="button"
        aria-pressed={follow}
        aria-label="Follow the editor"
        title="Follow the editor"
        onClick={() => setFollow(!follow)}
      >
        <FollowIcon />
      </button>
      {editorModes.map((each) => (
        <button
          key={each}
          className={`rail-button${mode === each ? " active" : ""}`}
          type="button"
          aria-pressed={mode === each}
          aria-label={modeLabel(each)}
          title={modeLabel(each)}
          onClick={() => setMode(each)}
        >
          <ModeIcon mode={each} />
        </button>
      ))}
      <NoteActionsMenu getBody={actions.getBody} onMeta={actions.onMeta} onDelete={actions.onDelete} />
    </>
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

// The rail is icons, so each mode draws the shape of what it shows: one filled column for the
// rendered note, one lined column for the source, two columns side by side for both.
function ModeIcon({ mode }: { mode: EditorMode }) {
  return (
    <svg
      className="rail-icon-svg"
      viewBox="0 0 24 24"
      width="20"
      height="20"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      aria-hidden="true"
    >
      <rect x="3" y="4" width="18" height="16" rx="2" />
      {mode === "preview" ? (
        <>
          <line x1="7" y1="9" x2="17" y2="9" />
          <line x1="7" y1="13" x2="17" y2="13" />
          <line x1="7" y1="17" x2="13" y2="17" />
        </>
      ) : null}
      {mode === "edit" ? (
        <>
          <line x1="7" y1="9" x2="13" y2="9" />
          <line x1="7" y1="13" x2="16" y2="13" />
          <line x1="7" y1="17" x2="10" y2="17" />
          <line x1="17" y1="8" x2="17" y2="18" />
        </>
      ) : null}
      {mode === "split" ? (
        <>
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
function FollowIcon() {
  return (
    <svg
      className="rail-icon-svg"
      viewBox="0 0 24 24"
      width="20"
      height="20"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M2 12s3.6-6 10-6 10 6 10 6-3.6 6-10 6-10-6-10-6Z" />
      <circle cx="12" cy="12" r="2.6" />
    </svg>
  );
}

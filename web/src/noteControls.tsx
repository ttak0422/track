import { ReactNode, createContext, useContext, useMemo, useState } from "react";

// The open note's controls live in the sidebar rail, beside the workspace's own views, rather than
// floating over the reading column. That puts them a component tree away from the note view that
// owns their state, so the state sits here, above both.

export type EditorMode = "preview" | "edit" | "split";

export const editorModes: EditorMode[] = ["preview", "edit", "split"];

// NoteActions is what the open note lets the rail do to it. Only the note view can supply these —
// the body to copy, the dialogs to open — so it hands them over while it is mounted and takes them
// back on the way out; the rail shows the note group only while it holds a set.
//
// The body is a getter rather than a value on purpose: it changes on every keystroke, and storing it
// here would re-render the whole workspace as you type.
export interface NoteActions {
  getBody: () => string;
  onMeta: () => void;
  onDelete: () => void;
}

interface NoteControlsState {
  mode: EditorMode;
  setMode: (mode: EditorMode) => void;
  follow: boolean;
  setFollow: (follow: boolean) => void;
  actions: NoteActions | null;
  setActions: (actions: NoteActions | null) => void;
}

const NoteControlsContext = createContext<NoteControlsState | null>(null);

export function NoteControlsProvider({ children }: { children: ReactNode }) {
  // Mode and follow outlive any one note, as they did when they sat in the note view: that view is
  // the same element across a route change, so React kept its state, and moving them up here keeps
  // the editor open when you switch notes instead of dropping you back into preview.
  const [mode, setMode] = useState<EditorMode>("preview");
  const [follow, setFollow] = useState(false);
  const [actions, setActions] = useState<NoteActions | null>(null);
  const value = useMemo(
    () => ({ mode, setMode, follow, setFollow, actions, setActions }),
    [mode, follow, actions],
  );
  return <NoteControlsContext.Provider value={value}>{children}</NoteControlsContext.Provider>;
}

export function useNoteControls(): NoteControlsState {
  const value = useContext(NoteControlsContext);
  if (!value) {
    throw new Error("useNoteControls must be used inside NoteControlsProvider");
  }
  return value;
}

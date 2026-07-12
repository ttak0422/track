import { createContext } from "react";
import type { NoteID, NoteInclude, NoteTasks } from "../../types";

// Nesting depth of the current markdown render. Each preview renders its body
// one level deeper so nested previews can stack in front of their parent.
export const PreviewDepthContext = createContext(0);

// Kind ("note"/"journal") of the note being rendered, so relative "assets/<file>" references resolve to
// the right per-kind assets directory on the server. Defaults to "note".
export const NoteKindContext = createContext<string>("note");

// Resolved ![[...]] includes of the note being rendered (ADR 0031), indexed by the placeholder
// tokens spliceIncludeTokens left in the markdown. Module-level markdownComponents cannot close
// over per-render data, so the embed component reads them from here.
export const IncludesContext = createContext<NoteInclude[]>([]);

// The note whose ```taskboard fence is being rendered: its id (for the state-set API; "" disables
// dragging, e.g. in hover previews) and its parsed tasks from the note response / static bundle.
export interface TaskBoardData {
  noteID: NoteID;
  tasks?: NoteTasks;
}

export const TaskBoardContext = createContext<TaskBoardData>({ noteID: "" });

// Raw markdown source of the note being rendered, for blocks that reflect over the whole note (an
// empty ```mindmap fence maps the note's heading tree). Same reason as IncludesContext: module-level
// markdownComponents cannot close over per-render data.
export const MarkdownSourceContext = createContext<string>("");

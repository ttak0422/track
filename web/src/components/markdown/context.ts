import { createContext } from "react";
import type { NoteID, NoteInclude, NoteTasks } from "../../types";

// Kind ("note"/"journal") of the note being rendered, so relative "assets/<file>" references resolve to
// the right per-kind assets directory on the server. Defaults to "note".
export const NoteKindContext = createContext<string>("note");

// Vault of the note being rendered (its registry name; "" for the vault the workspace was launched
// in). Everything a body refers to — its "assets/<file>" attachments, its [[links]], the notes its
// ![[includes]] and ```track-query blocks name, its ```viewspec data sources — lives in that same
// vault, and two vaults can hold different files under identical names. Without this the whole
// rendered body would silently resolve against the launch vault.
export const NoteVaultContext = createContext<string>("");

// Resolved ![[...]] includes of the note being rendered (ADR 0031), indexed by the placeholder
// tokens spliceIncludeTokens left in the markdown. Module-level markdownComponents cannot close
// over per-render data, so the embed component reads them from here.
export const IncludesContext = createContext<NoteInclude[]>([]);

// The note whose ```taskboard fence is being rendered: its id (for the state-set API; "" disables
// dragging, e.g. in hover previews) and its parsed tasks from the note response / static bundle.
export interface TaskBoardData {
  noteID: NoteID;
  // The parsed tasks and the etag writes must match. They live behind a ref rather than in the
  // context value: the note query refreshes them on every disk change, and re-rendering the task
  // controls on that churn would take an open native date picker down with it (any re-render of the
  // input re-applies its type, which Chrome treats as a picker close). The ref is updated only when
  // the editor adopts a new body, so the controls re-render exactly when the body they render does.
  tasksRef?: { current: { tasks: NoteTasks; etag: string } };
  // Added to a rendered line to get this note's file line. 0 when a note renders its own body; an
  // excerpt shown through an include renders the source note's lines starting partway in, so its
  // rows resolve through the source's offset (see IncludeEmbed).
  lineOffset?: number;
}

export const TaskBoardContext = createContext<TaskBoardData>({ noteID: "" });

// Raw markdown source of the note being rendered, for blocks that reflect over the whole note (an
// empty ```mindmap fence maps the note's heading tree). Same reason as IncludesContext: module-level
// markdownComponents cannot close over per-render data.
export const MarkdownSourceContext = createContext<string>("");

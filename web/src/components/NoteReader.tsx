import { NoteEditor } from "./NoteEditor";
import { NoteReaderStatic } from "./NoteReaderStatic";
import type { NoteID } from "../types";

// NoteReader picks the note view for the deployment. The published static site is read-only, so it uses
// NoteReaderStatic; the live workspace uses the full NoteEditor. __TRACK_STATIC__ is a build-time
// constant, so the branch not taken — and its dependencies — is tree-shaken from each build: the static
// bundle ships no editor (save/delete/follow/textarea), the live bundle no dead static-only path.
export function NoteReader({ noteID }: { noteID: NoteID }) {
  return __TRACK_STATIC__ ? <NoteReaderStatic noteID={noteID} /> : <NoteEditor noteID={noteID} />;
}

import type { NoteID } from "./types";

// A note's identity when the workspace serves more than one vault. Note ids are vault-local and
// journal ids name a note in every vault at once (their id is the date), so an id alone cannot say
// which note it means. The frontend keeps ids as opaque strings, so the vault rides along inside
// the string as "<vault>:<id>" — the same shape a cross-vault [[vault:title]] link uses, and the
// same asymmetry: an unqualified id belongs to a workspace serving one unnamed vault.
//
// The separator is the first colon, so a slug containing colons still round-trips.
const SEPARATOR = ":";

export interface VaultRef {
  // The vault's registry name; empty when the workspace serves a single, unregistered vault.
  vault: string;
  // The id inside that vault, exactly as the server sent it.
  id: string;
}

// qualify builds the id the router, tab strip, and query cache key notes by. A note from an unnamed
// vault keeps its bare id, so a single-vault workspace's URLs and stored tabs are unchanged.
export function qualify(vault: string | undefined, id: string | number): NoteID {
  const raw = String(id);
  return vault ? `${vault}${SEPARATOR}${raw}` : raw;
}

// split takes an id apart for a request. An unqualified id addresses the vault the workspace was
// launched in, which is what the server does with a missing ?vault=.
export function split(noteID: NoteID): VaultRef {
  const at = noteID.indexOf(SEPARATOR);
  if (at < 0) return { vault: "", id: noteID };
  return { vault: noteID.slice(0, at), id: noteID.slice(at + 1) };
}

// vaultOf is split(id).vault, for the places that only need the label (a tab badge, a graph colour).
export function vaultOf(noteID: NoteID): string {
  return split(noteID).vault;
}

// idParams builds the query parameters addressing one note: its id plus the vault it lives in.
export function idParams(noteID: NoteID): URLSearchParams {
  const { vault, id } = split(noteID);
  const params = new URLSearchParams({ id });
  if (vault) params.set("vault", vault);
  return params;
}

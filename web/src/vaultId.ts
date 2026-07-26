import type { NoteID } from "./types";

// A note's identity when the workspace serves more than one vault. Note ids are vault-local and
// journal ids name a note in every vault at once (their id is the date), so an id alone cannot say
// which note it means. The frontend keeps ids as opaque strings, so the vault rides along inside
// the string — with the same asymmetry a cross-vault link has: an unqualified id means the vault
// you are already in, exactly as [[title]] does next to [[vault:title]].
//
// The separator is "~", not the ":" that link syntax uses, because the id travels through the URL.
// ":" is URI-reserved: a route param interpolates it to "%3A", and the router decodes a pathname
// with decodeURI (which keeps "%3A") but a param with decodeURIComponent (which restores ":"), so
// the tab strip — which reads location.pathname — and the reader would disagree on every qualified
// id. "~" is unreserved, so it survives both decoders unchanged, and it can appear in neither half:
// vault names are [a-z0-9-] and ids are digits or a base62 slug.
const SEPARATOR = "~";

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
  // Coerced because ids arrive from JSON: a payload that skipped normalization would hand us a
  // number, and an id splitter that throws would take a preview down with it.
  const raw = String(noteID ?? "");
  const at = raw.indexOf(SEPARATOR);
  if (at < 0) return { vault: "", id: raw };
  return { vault: raw.slice(0, at), id: raw.slice(at + 1) };
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

// vaultParams is the "&vault=<name>" (or "?vault=<name>") suffix addressing a request at one vault,
// empty for the launch vault. Requests that name no note still need it: a body's attachments,
// links, includes, and chart data all live in the vault the body came from.
export function vaultParams(vault: string, lead = "&"): string {
  return vault ? `${lead}vault=${encodeURIComponent(vault)}` : "";
}

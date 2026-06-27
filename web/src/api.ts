import { dataURL, STATIC_MODE } from "./runtime";
import type {
  ActivityResponse,
  AgendaResponse,
  FollowResponse,
  Graph,
  GraphResponse,
  JournalResponse,
  NoteResponse,
  NotesResponse,
  OgpResponse,
  RenderResponse,
  ResolveResponse,
  SaveNoteRequest,
  SaveNoteResponse,
  SearchResponse,
  SearchResult,
  SiteResponse,
} from "./types";

interface APIOptions {
  method?: string;
  body?: unknown;
}

export async function api<T>(path: string, options: APIOptions = {}): Promise<T> {
  const headers = new Headers();
  const init: RequestInit = {
    method: options.method,
    headers,
  };

  if (options.body !== undefined) {
    headers.set("Content-Type", "application/json");
    init.body = JSON.stringify(options.body);
  }

  const response = await fetch(path, init);
  const body = await response.json().catch(() => ({}));

  if (!response.ok) {
    const message =
      typeof body === "object" && body !== null && "error" in body
        ? String(body.error)
        : `${response.status} ${response.statusText}`;
    throw new Error(message);
  }

  return body as T;
}

// staticData fetches a pre-generated JSON file from the exported data bundle. It is only used in static
// mode; the file is a plain static asset, so an ordinary fetch works without a server.
async function staticData<T>(path: string): Promise<T> {
  const response = await fetch(dataURL(path));
  if (!response.ok) {
    throw new Error(`${response.status} ${response.statusText}`);
  }
  return (await response.json()) as T;
}

const readOnly = () => Promise.reject(new Error("read-only static site"));

export function searchNotes(query: string, limit = 100): Promise<SearchResponse> {
  if (STATIC_MODE) {
    return staticData<NotesResponse>("notes.json").then((data) => ({
      results: filterNotes(data.notes, query).slice(0, limit),
    }));
  }
  const params = new URLSearchParams({ limit: String(limit), q: query });
  return api<SearchResponse>(`/api/search?${params}`);
}

export function listNotes(): Promise<NotesResponse> {
  if (STATIC_MODE) {
    return staticData<NotesResponse>("notes.json");
  }
  return api<NotesResponse>("/api/notes");
}

export function getActivity(since: string, until: string): Promise<ActivityResponse> {
  if (STATIC_MODE) {
    // The published site has no heatmap, so activity is empty.
    return Promise.resolve({ activity: { since, until, total: 0, counts: [] } });
  }
  const params = new URLSearchParams({ since, until });
  return api<ActivityResponse>(`/api/activity?${params}`);
}

export function resolveTerm(term: string): Promise<ResolveResponse> {
  if (STATIC_MODE) {
    return staticData<Record<string, ResolveResponse["note"]>>("resolve.json").then((map) => {
      const note = map[term];
      return note ? { found: true, note } : { found: false, note: { note_id: 0, file_kind: "note", title: term } };
    });
  }
  return api<ResolveResponse>(`/api/resolve?term=${encodeURIComponent(term)}`);
}

export function getAgenda(date: string): Promise<AgendaResponse> {
  if (STATIC_MODE) {
    return Promise.resolve({ date, notes: [] });
  }
  return api<AgendaResponse>(`/api/agenda?date=${encodeURIComponent(date)}`);
}

// openJournal opens or creates the journal for a day and returns its note id, so the activity heatmap
// can jump straight to that day's journal. Disabled in the read-only static site.
export function openJournal(date: string): Promise<JournalResponse> {
  if (STATIC_MODE) {
    return readOnly();
  }
  return api<JournalResponse>(`/api/journal?date=${encodeURIComponent(date)}`, { method: "POST" });
}

export function getNote(noteID: number): Promise<NoteResponse> {
  if (STATIC_MODE) {
    return staticData<NoteResponse>(`note/${noteID}.json`);
  }
  return api<NoteResponse>(`/api/note?id=${encodeURIComponent(noteID)}`);
}

export function saveNote(noteID: number, request: SaveNoteRequest): Promise<SaveNoteResponse> {
  if (STATIC_MODE) {
    return readOnly();
  }
  return api<SaveNoteResponse>(`/api/note?id=${encodeURIComponent(noteID)}`, {
    method: "PUT",
    body: request,
  });
}

export function getFollowState(): Promise<FollowResponse> {
  if (STATIC_MODE) {
    return Promise.resolve({ active: false });
  }
  return api<FollowResponse>("/api/follow");
}

// renderMarkdown asks the server to sanitize a raw note body into the Markdown the frontend renders:
// track action links are flattened to plain text while wiki links and ordinary Markdown pass through.
// Posting the live (possibly unsaved) body keeps the engine the single source of truth for track-specific
// Markdown rules instead of duplicating them in the frontend. In static mode the exported note body is
// already sanitized, so this is the identity.
export function renderMarkdown(body: string): Promise<RenderResponse> {
  if (STATIC_MODE) {
    return Promise.resolve({ markdown: body });
  }
  return api<RenderResponse>("/api/render", { method: "POST", body: { body } });
}

export function getLocalGraph(noteID: number): Promise<GraphResponse> {
  if (STATIC_MODE) {
    return staticData<GraphResponse>("graph.json").then((data) => ({
      graph: localGraph(data.graph, noteID),
    }));
  }
  return api<GraphResponse>(`/api/graph/local?id=${encodeURIComponent(noteID)}`);
}

export function getGraph(): Promise<GraphResponse> {
  if (STATIC_MODE) {
    return staticData<GraphResponse>("graph.json");
  }
  return api<GraphResponse>("/api/graph");
}

// getOgp fetches Open Graph metadata for an embedded link so the preview can render a rich card.
// The static site cannot reach the network at view time, so it returns a bare card.
export function getOgp(url: string): Promise<OgpResponse> {
  if (STATIC_MODE) {
    return Promise.resolve({ url });
  }
  return api<OgpResponse>(`/api/ogp?url=${encodeURIComponent(url)}`);
}

// getSite returns the published site's entry note. Static mode only.
export function getSite(): Promise<SiteResponse> {
  return staticData<SiteResponse>("site.json");
}

// filterNotes is the static-mode search: a case-insensitive match on title, or a "#tag" filter on the
// note's tags. It mirrors the server search closely enough for navigating a published set.
function filterNotes(notes: SearchResult[], query: string): SearchResult[] {
  const q = query.trim().toLowerCase();
  if (q === "") {
    return notes;
  }
  if (q.startsWith("#")) {
    const tag = q.slice(1);
    return notes.filter((n) => (n.tags ?? []).some((t) => t.toLowerCase() === tag));
  }
  return notes.filter((n) => n.title.toLowerCase().includes(q));
}

// localGraph derives the 1-hop neighbourhood of a note from the full graph, marking the center, so the
// static site does not need a separate file per note.
function localGraph(graph: Graph, noteID: number): Graph {
  const keep = new Set<number>([noteID]);
  for (const edge of graph.edges) {
    if (edge.source_id === noteID) keep.add(edge.target_id);
    if (edge.target_id === noteID) keep.add(edge.source_id);
  }
  return {
    center_id: noteID,
    nodes: graph.nodes
      .filter((n) => keep.has(n.note_id))
      .map((n) => ({ ...n, center: n.note_id === noteID })),
    edges: graph.edges.filter((e) => keep.has(e.source_id) && keep.has(e.target_id)),
  };
}

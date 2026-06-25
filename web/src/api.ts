import type {
  ActivityResponse,
  AgendaResponse,
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

export function searchNotes(query: string, limit = 100): Promise<SearchResponse> {
  const params = new URLSearchParams({ limit: String(limit), q: query });
  return api<SearchResponse>(`/api/search?${params}`);
}

export function listNotes(): Promise<NotesResponse> {
  return api<NotesResponse>("/api/notes");
}

export function getActivity(since: string, until: string): Promise<ActivityResponse> {
  const params = new URLSearchParams({ since, until });
  return api<ActivityResponse>(`/api/activity?${params}`);
}

export function resolveTerm(term: string): Promise<ResolveResponse> {
  return api<ResolveResponse>(`/api/resolve?term=${encodeURIComponent(term)}`);
}

export function getAgenda(date: string): Promise<AgendaResponse> {
  return api<AgendaResponse>(`/api/agenda?date=${encodeURIComponent(date)}`);
}

// openJournal opens or creates the journal for a day and returns its note id, so the activity heatmap
// can jump straight to that day's journal.
export function openJournal(date: string): Promise<JournalResponse> {
  return api<JournalResponse>(`/api/journal?date=${encodeURIComponent(date)}`, { method: "POST" });
}

export function getNote(noteID: number): Promise<NoteResponse> {
  return api<NoteResponse>(`/api/note?id=${encodeURIComponent(noteID)}`);
}

export function saveNote(noteID: number, request: SaveNoteRequest): Promise<SaveNoteResponse> {
  return api<SaveNoteResponse>(`/api/note?id=${encodeURIComponent(noteID)}`, {
    method: "PUT",
    body: request,
  });
}

// renderMarkdown asks the server to sanitize a raw note body into the Markdown the frontend renders:
// track action links are flattened to plain text while wiki links and ordinary Markdown pass through.
// Posting the live (possibly unsaved) body keeps the engine the single source of truth for track-specific
// Markdown rules instead of duplicating them in the frontend.
export function renderMarkdown(body: string): Promise<RenderResponse> {
  return api<RenderResponse>("/api/render", { method: "POST", body: { body } });
}

export function getLocalGraph(noteID: number): Promise<GraphResponse> {
  return api<GraphResponse>(`/api/graph/local?id=${encodeURIComponent(noteID)}`);
}

export function getGraph(): Promise<GraphResponse> {
  return api<GraphResponse>("/api/graph");
}

// getOgp fetches Open Graph metadata for an embedded link so the preview can render a rich card.
export function getOgp(url: string): Promise<OgpResponse> {
  return api<OgpResponse>(`/api/ogp?url=${encodeURIComponent(url)}`);
}

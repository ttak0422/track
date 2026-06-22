import type {
  ActivityResponse,
  AgendaResponse,
  GraphResponse,
  NoteResponse,
  NotesResponse,
  OgpResponse,
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

export function getNote(noteID: number): Promise<NoteResponse> {
  return api<NoteResponse>(`/api/note?id=${encodeURIComponent(noteID)}`);
}

export function saveNote(noteID: number, request: SaveNoteRequest): Promise<SaveNoteResponse> {
  return api<SaveNoteResponse>(`/api/note?id=${encodeURIComponent(noteID)}`, {
    method: "PUT",
    body: request,
  });
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

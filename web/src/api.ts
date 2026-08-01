import { dataURL, STATIC_MODE } from "./runtime";
import { bodyHits, titleHits, type SearchCorpus, type SearchDoc } from "./staticSearch";
import { idParams, qualify, vaultParams } from "./vaultId";
import type {
  ActivityResponse,
  AgendaResponse,
  AssetUploadResponse,
  DateField,
  DeleteNoteResponse,
  FollowResponse,
  Graph,
  GraphResponse,
  JournalResponse,
  NoteID,
  NoteMetaResponse,
  NoteResponse,
  NotesResponse,
  OgpResponse,
  RenderResponse,
  ResolveResponse,
  SaveNoteMetaRequest,
  SaveNoteRequest,
  SaveNoteResponse,
  SearchResponse,
  SearchResult,
  SiteResponse,
  TasksResponse,
  ViewSpecResponse,
  TaskListResponse,} from "./types";

interface APIOptions {
  method?: string;
  body?: unknown;
}

export class APIError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = "APIError";
  }
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
  return handleResponse<T>(response);
}

async function handleResponse<T>(response: Response): Promise<T> {
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    const message =
      typeof body === "object" && body !== null && "error" in body
        ? String((body as { error: unknown }).error)
        : `${response.status} ${response.statusText}`;
    throw new APIError(response.status, message);
  }
  return stringifyIDs(body) as T;
}

// The live server marshals note ids as JSON numbers plus a separate "vault" label, but the frontend
// treats ids as opaque strings (so they line up with route params and with the static site's slug
// ids). stringifyIDs normalizes every id field in a response at the boundary: numbers become
// strings, and an id belonging to a named vault becomes "<vault>:<id>" so two vaults' notes can
// never collide in a route, a tab, or the query cache. The rest of the app never sees a bare
// numeric id or a loose vault field.
const ID_KEYS = new Set(["note_id", "source_id", "target_id", "center_id", "root"]);

// normalizeIDs applies the same id normalization to a payload that did not arrive through api() —
// an SSE event body. Server-sent state carries the same (vault, note_id) pair as a response, and a
// consumer that parsed it itself would otherwise navigate with a bare id, i.e. to the launch
// vault's same-numbered note.

// A response labels its vault once at the level the label applies to: per hit for a cross-vault
// search, once at the top for a single vault's note or graph. Child objects inherit the nearest
// enclosing label, so graph edges are qualified by the graph's own vault.
export function normalizeIDs<T>(value: T, vault = ""): T {
  return stringifyIDs(value, vault);
}

function stringifyIDs<T>(value: T, vault = ""): T {
  if (Array.isArray(value)) {
    return value.map((item) => stringifyIDs(item, vault)) as T;
  }
  if (value !== null && typeof value === "object") {
    const own = (value as { vault?: unknown }).vault;
    const scope = typeof own === "string" ? own : vault;
    const out: Record<string, unknown> = {};
    for (const [key, child] of Object.entries(value)) {
      out[key] =
        ID_KEYS.has(key) && (typeof child === "number" || typeof child === "string")
          ? qualify(scope, child)
          : stringifyIDs(child, scope);
    }
    return out as T;
  }
  return value;
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
    return staticSearch(query, limit);
  }
  const params = new URLSearchParams({ limit: String(limit), q: query });
  return api<SearchResponse>(`/api/search?${params}`);
}

// A published bundle never changes under the page, so both files a search reads are fetched once and
// held as the promise, not the value — several keystrokes in flight share the one request. The
// corpus is fetched on the first search that needs it and never as part of first paint, so a reader
// who does not search never downloads it.
let notesFile: Promise<SearchResult[]> | null = null;
let corpus: Promise<SearchDoc[]> | null = null;

function staticNotes(): Promise<SearchResult[]> {
  notesFile ??= staticData<NotesResponse>("notes.json").then(
    (data) => data.notes,
    (err: unknown) => {
      notesFile = null;
      throw err;
    },
  );
  return notesFile;
}

function searchCorpus(): Promise<SearchDoc[]> {
  corpus ??= staticData<SearchCorpus>("search.json").then(
    (data) => data.docs ?? [],
    () => {
      // A bundle built before search.json existed has no corpus, and the site should still answer
      // title-and-tag rather than fail a search it can half-answer. Drop the cache on the way out
      // so a transient failure costs one search rather than the whole session.
      corpus = null;
      return [];
    },
  );
  return corpus;
}

// The published site's search composes exactly like the engine's (internal/track/search): title hits
// first, then body hits for the notes the titles did not already name, sharing one budget.
async function staticSearch(query: string, limit: number): Promise<SearchResponse> {
  const notes = await staticNotes();
  const titles = titleHits(notes, query).slice(0, limit);
  if (titles.length >= limit) {
    return { results: titles };
  }
  const skip = new Set(titles.map((note) => String(note.note_id)));
  const docs = await searchCorpus();
  return { results: [...titles, ...bodyHits(notes, docs, query, limit - titles.length, skip)] };
}

export function listNotes(): Promise<NotesResponse> {
  if (STATIC_MODE) {
    return staticData<NotesResponse>("notes.json");
  }
  return api<NotesResponse>("/api/notes");
}

// listDatedTasks lists every task in the vault carrying a scheduled or due date. The published site
// reads the same shape from the bundle, so the calendar and day pages need no live-only branch.
export function listDatedTasks(): Promise<TaskListResponse> {
  if (STATIC_MODE) {
    return staticData<TaskListResponse>("tasks.json");
  }
  return api<TaskListResponse>("/api/tasks");
}

// listOpenTasks lists every task still to do, dated or not, worst-first. The published bundle holds
// only the dated listing, so this one is live-only and its view is gated the same way.
export function listOpenTasks(): Promise<TaskListResponse> {
  return api<TaskListResponse>("/api/tasks?open=1");
}

export function getActivity(since: string, until: string): Promise<ActivityResponse> {
  if (STATIC_MODE) {
    // The published site has no heatmap, so activity is empty.
    return Promise.resolve({ activity: { since, until, total: 0, counts: [] } });
  }
  const params = new URLSearchParams({ since, until });
  return api<ActivityResponse>(`/api/activity?${params}`);
}

export function resolveTerm(term: string, vault = ""): Promise<ResolveResponse> {
  if (STATIC_MODE) {
    return staticData<Record<string, ResolveResponse["note"]>>("resolve.json").then((map) => {
      const note = map[term];
      return note ? { found: true, note } : { found: false, note: { note_id: "", file_kind: "note", title: term } };
    });
  }
  return api<ResolveResponse>(`/api/resolve?term=${encodeURIComponent(term)}${vaultParams(vault)}`);
}

export function getAgenda(date: string, vault = ""): Promise<AgendaResponse> {
  if (STATIC_MODE) {
    // Derived from the notes list's activity days, mirroring the live /api/agenda (which also lists only
    // real notes — journals carry no activity days).
    return staticData<NotesResponse>("notes.json").then((data) => ({
      date,
      notes: data.notes.filter((note) => (note.days ?? []).includes(date)),
    }));
  }
  return api<AgendaResponse>(`/api/agenda?date=${encodeURIComponent(date)}${vaultParams(vault)}`);
}

// openJournal opens or creates the journal for a day and returns its note id, so the activity heatmap
// can jump straight to that day's journal. Disabled in the read-only static site.
export function openJournal(date: string): Promise<JournalResponse> {
  if (STATIC_MODE) {
    return readOnly();
  }
  return api<JournalResponse>(`/api/journal?date=${encodeURIComponent(date)}`, { method: "POST" });
}

export function getNote(noteID: NoteID): Promise<NoteResponse> {
  if (STATIC_MODE) {
    return staticData<NoteResponse>(`note/${noteID}.json`);
  }
  return api<NoteResponse>(`/api/note?${idParams(noteID)}`);
}

export function saveNote(noteID: NoteID, request: SaveNoteRequest): Promise<SaveNoteResponse> {
  if (STATIC_MODE) {
    return readOnly();
  }
  return api<SaveNoteResponse>(`/api/note?${idParams(noteID)}`, {
    method: "PUT",
    body: request,
  });
}

// getNoteMeta / saveNoteMeta read and edit a note's editable sidecar metadata (tags, description,
// cover image, props) as one YAML document. The static site has no meta editor, so both are
// live-server only.
export function getNoteMeta(noteID: NoteID): Promise<NoteMetaResponse> {
  if (STATIC_MODE) {
    return readOnly();
  }
  return api<NoteMetaResponse>(`/api/note/meta?${idParams(noteID)}`);
}

export function saveNoteMeta(noteID: NoteID, request: SaveNoteMetaRequest): Promise<NoteMetaResponse> {
  if (STATIC_MODE) {
    return readOnly();
  }
  return api<NoteMetaResponse>(`/api/note/meta?${idParams(noteID)}`, {
    method: "POST",
    body: request,
  });
}

// setTaskState moves one task line of a note into a named state through the engine's shared write
// path (completion stamp, sidecar transition log, progress-cookie recompute). The response carries
// the note's refreshed tasks so the board can redraw without a second request. Live server only —
// the published static board is read-only.
// setTaskDate writes one task line's scheduled or due date; an empty date clears it. Same endpoint
// and addressing as setTaskState — a task is a note id plus a file line on every surface — and the
// same expect assertion, since a date is picked against a task the caller has already drawn.
export function setTaskDate(
  noteID: NoteID,
  line: number,
  field: DateField,
  date: string,
  expect: string,
  etag: string,
): Promise<TasksResponse> {
  if (STATIC_MODE) {
    return readOnly();
  }
  return api<TasksResponse>(`/api/task?${idParams(noteID)}`, {
    method: "POST",
    body: { line, [field]: date, expect, etag },
  });
}

export function setTaskState(
  noteID: NoteID,
  line: number,
  state: string,
  expect: string,
  etag: string,
): Promise<TasksResponse> {
  if (STATIC_MODE) {
    return readOnly();
  }
  // expect is the state the caller is looking at: the server refuses the write with 409 when the
  // line has since become something else, rather than overwriting an edit the view never saw.
  return api<TasksResponse>(`/api/task?${idParams(noteID)}`, {
    method: "POST",
    body: { line, state, expect, etag },
  });
}

// uploadAsset imports a picked image into the vault's assets directory and returns its assets/<name>
// reference for the cover-image field. The browser sets the multipart boundary, so this bypasses the
// JSON api() helper. Live server only — the static site has no editor.
export function uploadAsset(file: File, vault = ""): Promise<AssetUploadResponse> {
  if (STATIC_MODE) {
    return readOnly();
  }
  const form = new FormData();
  form.append("file", file);
  return fetch(`/api/asset${vaultParams(vault, "?")}`, { method: "POST", body: form }).then((response) =>
    handleResponse<AssetUploadResponse>(response),
  );
}

// deleteNote permanently removes a note (file + sidecar + index row). The destructive confirmation is in
// the UI; the published static site is read-only and cannot delete.
export function deleteNote(noteID: NoteID): Promise<DeleteNoteResponse> {
  if (STATIC_MODE) {
    return readOnly();
  }
  return api<DeleteNoteResponse>(`/api/note?${idParams(noteID)}`, { method: "DELETE" });
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
export function renderMarkdown(body: string, vault = ""): Promise<RenderResponse> {
  if (STATIC_MODE) {
    return Promise.resolve({ markdown: body });
  }
  return api<RenderResponse>(`/api/render${vaultParams(vault, "?")}`, { method: "POST", body: { body } });
}

// renderViewSpec asks the server to resolve a fenced ```viewspec block (View Spec JSON) to its
// ECharts option, keeping the engine the single source of truth for chart semantics. The static export
// replaces these blocks with pre-rendered images at build time, so this is never called in static mode.
export function renderViewSpec(spec: string, vault = ""): Promise<ViewSpecResponse> {
  return api<ViewSpecResponse>(`/api/viewspec${vaultParams(vault, "?")}`, { method: "POST", body: { spec } });
}

export function getLocalGraph(noteID: NoteID): Promise<GraphResponse> {
  if (STATIC_MODE) {
    return staticData<GraphResponse>("graph.json").then((data) => ({
      graph: localGraph(data.graph, noteID),
    }));
  }
  return api<GraphResponse>(`/api/graph/local?${idParams(noteID)}`);
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

// fetchAssetText loads the raw text of a vault asset from its resolved href (served by /api/asset live,
// or copied to ./assets/<name> in the static export). Text-file embeds — Mermaid diagrams and other
// inlined text files — read their source this way.
export async function fetchAssetText(href: string): Promise<string> {
  const response = await fetch(href);
  if (!response.ok) {
    throw new Error(`${response.status} ${response.statusText}`);
  }
  return response.text();
}

// localGraph derives the 1-hop neighbourhood of a note from the full graph, marking the center, so the
// static site does not need a separate file per note.
function localGraph(graph: Graph, noteID: NoteID): Graph {
  const keep = new Set<NoteID>([noteID]);
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

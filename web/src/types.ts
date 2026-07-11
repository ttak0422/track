// Note ids are opaque strings end to end: the live server's numeric ids are stringified at the api
// boundary (see api.ts), and the static site uses base62 slugs (see internal/track/site/PublishID). The
// frontend never does arithmetic on them — only equality and URL building — so a string suits both.
export type NoteID = string;
export type FileKind = "note" | "journal" | string;

export interface NoteRef {
  note_id: NoteID;
  file_kind: FileKind;
  path?: string;
  title: string;
}

export interface SearchResult extends NoteRef {
  path: string;
  tags?: string[];
  // Activity days (YYYY-MM-DD) the note was created/updated on; filled by the notes listing (live and
  // static), which the calendar derives its per-day note lists from. Journals carry none.
  days?: string[];
  // Icon shown beside the title in lists/search/nav. Resolved by the engine from the config tag/kind
  // mapping and the per-note sidecar override (config.NoteIcon); empty means no icon.
  icon?: string;
  line?: number;
  snippet?: string;
}

export interface SearchResponse {
  results: SearchResult[];
}

export interface NotesResponse {
  notes: SearchResult[];
}

export interface ActivityDay {
  date: string;
  count: number;
}

export interface ActivitySummary {
  since: string;
  until: string;
  total: number;
  counts: ActivityDay[];
}

export interface ActivityResponse {
  activity: ActivitySummary;
}

export interface ResolveResponse {
  found: boolean;
  note: NoteRef;
}

export interface AgendaResponse {
  date: string;
  notes: NoteRef[];
}

export interface JournalResponse {
  note_id: NoteID;
  created: boolean;
}

// NoteInclude is one resolved ![[...]] transclusion directive (ADR 0031): the 0-based body line it
// sits on, the target's extracted lines, and where it points. Emitted by /api/render (line numbers
// align with the rendered markdown) and baked into the static bundle's note JSON.
export interface NoteInclude {
  line: number;
  note_id?: number;
  kind?: string;
  title?: string;
  caption: string;
  lines: string[];
  bad_options?: string[];
  error?: string;
}

export interface NoteDetail extends SearchResult {
  copy_path: string;
  body: string;
  etag: string;
  includes?: NoteInclude[];
}

export interface NoteResponse {
  note: NoteDetail;
  backlinks: NoteRef[];
}

export interface SaveNoteRequest {
  body: string;
  etag: string;
}

export interface SaveNoteResponse {
  note_id: NoteID;
  etag: string;
  saved: boolean;
}

export interface DeleteNoteResponse {
  note_id: NoteID;
  deleted: boolean;
}

// A note's page metadata (sidecar description / cover image), edited via the meta dialog and
// published as og:description / og:image by the static export.
export interface NoteMetaResponse {
  description: string;
  image: string;
}

// A save request leaves an omitted field untouched; an empty string clears it (engine semantics).
export interface SaveNoteMetaRequest {
  description?: string;
  image?: string;
}

export interface FollowState {
  note_id: NoteID;
  file_kind: FileKind;
  path?: string;
  line: number;
  top_line: number;
  line_count: number;
  updated_at: string;
}

export interface FollowResponse {
  active: boolean;
  state?: FollowState;
}

export interface GraphNode {
  note_id: NoteID;
  file_kind: FileKind;
  path?: string;
  title: string;
  center?: boolean;
}

export interface GraphEdge {
  source_id: NoteID;
  target_id: NoteID;
}

export interface Graph {
  center_id: NoteID;
  nodes: GraphNode[];
  edges: GraphEdge[];
}

export interface GraphResponse {
  graph: Graph;
}

export interface OgpResponse {
  url: string;
  title?: string;
  description?: string;
  image?: string;
  site_name?: string;
}

export interface RenderResponse {
  markdown: string;
  includes?: NoteInclude[];
}

// ViewSpecResponse carries the server-resolved ECharts option for a fenced ```viewspec chart block.
export interface ViewSpecResponse {
  echarts: Record<string, unknown>;
}

// SiteResponse describes the published static site: which note is the entry page, and whether the
// export opted into the calendar view (`track export-site --calendar`). It only exists in the static
// export bundle (data/site.json).
export interface SiteResponse {
  root: NoteID;
  title: string;
  calendar?: boolean;
}

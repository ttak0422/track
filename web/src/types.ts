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

// NoteProp is one flattened typed note property, as the engine indexes it: a sidecar props entry
// (line 0) or an inline "key:: value" body field (1-based body line). A list value arrives as one
// entry per item under the same key. Link values carry the resolution key ([[...]] inner text).
export interface NoteProp {
  key: string;
  value: string;
  type: "string" | "number" | "boolean" | "date" | "link" | string;
  line: number;
}

export interface NoteDetail extends SearchResult {
  copy_path: string;
  body: string;
  etag: string;
  includes?: NoteInclude[];
  props?: NoteProp[];
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

// A note's editable sidecar metadata as the dialog's typed fields: title, tags, description, cover
// image (an assets/<file> reference), and typed props. Built-in fields get dedicated controls; props
// stays free-form — a YAML "key: value" block the engine parses and validates. The frontend never
// assembles YAML: it sends these fields and the engine composes/validates the document.
export interface NoteMetaResponse {
  title: string;
  // The note's file kind ("note" | "journal"). Journal titles are date-derived, so the editor
  // disables title editing for them.
  kind: string;
  tags: string[];
  description: string;
  image: string;
  props: string;
}

// A save request replaces the whole editable metadata; a rejected edit changes nothing. tags is the
// comma-split list (the engine dedups/normalizes); props is the free-form block, parsed server-side.
export interface SaveNoteMetaRequest {
  title: string;
  tags: string[];
  description: string;
  image: string;
  props: string;
}

// The vault reference returned after uploading a cover image, e.g. "assets/cover.png".
export interface AssetUploadResponse {
  ref: string;
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

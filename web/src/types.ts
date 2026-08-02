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
  // Icon shown beside the title in search results (SearchPanel — the only surface that draws it).
  // Resolved by the engine (config.NoteIcon): a per-note override, then a tag mapping, then a kind
  // mapping. The override is the note's sidecar icon; the maps come from the vault config. Empty means
  // no icon.
  icon?: string;
  line?: number;
  snippet?: string;
  // Which search produced this hit, so the panel can group title and full-text matches. Not
  // derivable from snippet: a body hit whose terms straddle lines carries none.
  match?: "title" | "body";
}

// One vault a cross-vault search could not read. Without it a short result list is
// indistinguishable from "nothing matched there".
export interface UnavailableVault {
  name: string;
  path: string;
  error?: string;
}

export interface SearchResponse {
  results: SearchResult[];
  unavailable?: UnavailableVault[];
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
  etag?: string;
  title?: string;
  caption: string;
  lines: string[];
  bad_options?: string[];
  error?: string;
  // 0-based line of the target note the excerpt starts at; -1 when it is not one contiguous run.
  source_line?: number;
}

// One named task state: the checkbox marker character and whether the state is done-family
// (completion). The set is fixed and lives in ./taskStates; it defines the task board's columns.
export interface TaskState {
  name: string;
  char: string;
  done: boolean;
}

// One parsed task line of a note. line is 1-based over the note file — the coordinate the state-set
// API takes. The date fields are plain YYYY-MM-DD strings from the inline bracket tokens.
// TaskRow is one task in the vault-wide dated listing: the task plus the note it lives in, so the
// calendar and day pages can show what is planned without opening every note.
export interface TaskRow extends TaskItem {
  note_id: NoteID;
  file_kind: string;
  title: string;
}

export interface TaskListResponse {
  tasks: TaskRow[];
}

// DateField names the two date tokens a client may write on a task line, mirroring the engine's
// task.DateField (internal/track/task). The values are the note's token keywords and the JSON keys
// POST /api/task takes — setTaskDate sends { line, [field]: date } — not display labels. A task's
// parsed dates come back under the different keys `scheduled`/`due` below; keep the two apart.
export type DateField = "sched" | "due";

export interface TaskItem {
  line: number;
  state: string;
  done: boolean;
  priority?: string;
  scheduled?: string;
  due?: string;
  completed?: string;
  text: string;
}

// A note's tasks, as served by /api/tasks, embedded in the note response, and baked into the static
// bundle's note JSON.
export interface NoteTasks {
  items: TaskItem[];
}

export interface TasksResponse {
  tasks: NoteTasks;
  etag: string;
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
  tasks?: NoteTasks;
  props?: NoteProp[];
}

// One inbound reference from another vault, written [[vault:title]] there (ADR 0053). It is a
// separate list because those edges live in the other vaults' indexes, keyed by title rather than id.
export interface ExternalRef {
  vault: string;
  note_id: NoteID;
  file_kind: FileKind;
  title: string;
  path?: string;
}

export interface NoteResponse {
  note: NoteDetail;
  backlinks: NoteRef[];
  // Inbound [[vault:title]] references from other vaults, and the vaults that could not be
  // consulted — a missing backlink must stay distinguishable from a missing vault.
  external?: ExternalRef[];
  unavailable?: UnavailableVault[];
  // Hierarchy navigation from the "up" relation property: the ancestor trail (root first) and the
  // notes whose "up" points here. Both live and static responses carry them.
  trail?: NoteRef[];
  children?: NoteRef[];
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
  // Per-note icon (an emoji) shown beside the title; empty falls back to the config tag/kind mapping.
  icon: string;
  props: string;
}

// A save request replaces the whole editable metadata; a rejected edit changes nothing. tags is the
// comma-split list (the engine dedups/normalizes); props is the free-form block, parsed server-side.
export interface SaveNoteMetaRequest {
  title: string;
  tags: string[];
  description: string;
  image: string;
  icon: string;
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

// SiteResponse describes the published static site: which note is the entry page, its public URL, and
// the opt-in surfaces enabled by export-site. It only exists in the static export bundle (data/site.json).
export interface SiteResponse {
  root: NoteID;
  title: string;
  calendar?: boolean;
  base_url?: string;
  share?: boolean;
  // Published site icon file name at the site root ("icon.<ext>", from config web.icon); replaces
  // the built-in brand mark.
  icon?: string;
}

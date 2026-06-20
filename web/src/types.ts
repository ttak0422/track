export type NoteID = number;
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
  start_date: string;
  days: number;
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

export interface NoteDetail extends SearchResult {
  copy_path: string;
  body: string;
  etag: string;
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

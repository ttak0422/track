import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  deleteNote,
  fetchAssetText,
  getActivity,
  getAgenda,
  getGraph,
  getLocalGraph,
  getNote,
  getNoteMeta,
  getOgp,
  getSite,
  listNotes,
  renderMarkdown,
  renderViewSpec,
  resolveTerm,
  saveNote,
  saveNoteMeta,
  searchNotes,
  setTaskState,
  uploadAsset,
} from "./api";
import { STATIC_MODE } from "./runtime";
import { useDebouncedValue } from "./hooks/useDebouncedValue";
import type { NoteID, NoteMetaResponse, NoteResponse, SaveNoteMetaRequest, SaveNoteRequest } from "./types";

export const queryKeys = {
  site: () => ["site"] as const,
  activity: (since: string, until: string) => ["activity", since, until] as const,
  agenda: (date: string) => ["agenda", date] as const,
  graph: () => ["graph"] as const,
  localGraph: (noteID: NoteID) => ["graph", "local", noteID] as const,
  note: (noteID: NoteID) => ["note", noteID] as const,
  noteMeta: (noteID: NoteID) => ["note-meta", noteID] as const,
  notes: () => ["notes"] as const,
  resolve: (term: string) => ["resolve", term] as const,
  search: (query: string, limit: number) => ["search", query, limit] as const,
  ogp: (url: string) => ["ogp", url] as const,
  render: (body: string) => ["render", body] as const,
  assetText: (href: string) => ["assetText", href] as const,
  viewspec: (spec: string) => ["viewspec", spec] as const,
};

export function useActivityQuery(since: string, until: string) {
  return useQuery({
    queryKey: queryKeys.activity(since, until),
    queryFn: () => getActivity(since, until),
    enabled: since !== "" && until !== "",
  });
}

export function useAgendaQuery(date: string, options?: { enabled?: boolean }) {
  return useQuery({
    queryKey: queryKeys.agenda(date),
    queryFn: () => getAgenda(date),
    enabled: (options?.enabled ?? true) && date !== "",
  });
}

export function useOgpQuery(url: string, enabled = true) {
  return useQuery({
    queryKey: queryKeys.ogp(url),
    queryFn: () => getOgp(url),
    enabled: enabled && url !== "",
    // Link metadata is effectively static for a session and the server caches it too, so never refetch.
    staleTime: Infinity,
    gcTime: Infinity,
    retry: false,
  });
}

export function useAssetTextQuery(href: string, enabled = true) {
  return useQuery({
    queryKey: queryKeys.assetText(href),
    queryFn: () => fetchAssetText(href),
    enabled: enabled && href !== "",
    // The reader re-fetches a note when it changes, so caching an asset's text for the session is enough.
    staleTime: Infinity,
    gcTime: Infinity,
    retry: false,
  });
}

export function useSearchQuery(query: string, limit = 100, options?: { enabled?: boolean }) {
  return useQuery({
    queryKey: queryKeys.search(query, limit),
    queryFn: () => searchNotes(query, limit),
    enabled: options?.enabled ?? true,
  });
}

export function useNotesQuery() {
  return useQuery({
    queryKey: queryKeys.notes(),
    queryFn: listNotes,
  });
}

// useSiteQuery reads the published site's descriptor (entry note, calendar toggle). The file only
// exists in the static export, so the query stays off on the live server.
export function useSiteQuery() {
  return useQuery({
    queryKey: queryKeys.site(),
    queryFn: getSite,
    enabled: STATIC_MODE,
    staleTime: Infinity,
  });
}

export function useResolveQuery(term: string) {
  return useQuery({
    queryKey: queryKeys.resolve(term),
    queryFn: () => resolveTerm(term),
    enabled: term.trim() !== "",
  });
}

// Fallback poll interval for the open note. Live updates normally arrive via the
// SSE change stream (see useLiveEvents); this only covers a dropped stream.
const liveRefetchInterval = 30000;

export function useNoteQuery(noteID: NoteID, options: { live?: boolean } = {}) {
  return useQuery({
    queryKey: queryKeys.note(noteID),
    queryFn: () => getNote(noteID),
    refetchInterval: options.live ? liveRefetchInterval : false,
  });
}

// useRenderQuery turns a raw note body into the sanitized Markdown the preview renders, via the server's
// /api/render endpoint. The body is debounced so typing in the editor does not post on every keystroke,
// and the previous render is kept while the next one loads so the preview never flashes empty mid-edit.
export function useRenderQuery(body: string) {
  const debounced = useDebouncedValue(body, 200);
  return useQuery({
    queryKey: queryKeys.render(debounced),
    queryFn: () => renderMarkdown(debounced),
    enabled: debounced.trim() !== "",
    // Sanitization is a pure function of the body and the server caches nothing per-note, so an identical
    // body never needs re-posting within a session.
    staleTime: Infinity,
    placeholderData: keepPreviousData,
  });
}

// useViewSpecQuery resolves a fenced ```viewspec block to an ECharts option via the server. The key includes
// the spec text, so an edited block refetches on its own; useLiveEvents additionally invalidates the
// ["viewspec"] prefix when the vault's data/ directory changes, re-rendering charts whose data.source /
// overlays[].source files changed without the note body changing. The previous chart is kept while the
// refetch is in flight so a live update never flashes the loading state.
export function useViewSpecQuery(spec: string) {
  return useQuery({
    queryKey: queryKeys.viewspec(spec),
    queryFn: () => renderViewSpec(spec),
    // The static export replaces viewspec fences at build time; a leftover block shows its source.
    enabled: !STATIC_MODE,
    // A bad spec is a deterministic client error the user should see immediately, not retry through.
    retry: false,
    placeholderData: keepPreviousData,
  });
}

export function useGraphQuery(enabled = true) {
  return useQuery({
    queryKey: queryKeys.graph(),
    queryFn: getGraph,
    enabled,
  });
}

export function useLocalGraphQuery(noteID: NoteID | undefined, enabled = noteID !== undefined) {
  return useQuery({
    queryKey: queryKeys.localGraph(noteID ?? ""),
    queryFn: () => {
      if (noteID === undefined) {
        throw new Error("note id is required for local graph");
      }
      return getLocalGraph(noteID);
    },
    enabled,
  });
}

export function useDeleteNoteMutation(noteID: NoteID) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => deleteNote(noteID),
    onSuccess: () => {
      // The note is gone: drop its cache and refresh the lists/graph that referenced it.
      queryClient.removeQueries({ queryKey: queryKeys.note(noteID) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.notes() });
      void queryClient.invalidateQueries({ queryKey: ["search"] });
      void queryClient.invalidateQueries({ queryKey: ["graph"] });
      void queryClient.invalidateQueries({ queryKey: ["activity"] });
    },
  });
}

export function useSaveNoteMutation(noteID: NoteID) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (request: SaveNoteRequest) => saveNote(noteID, request),
    onSuccess: (response, request) => {
      queryClient.setQueryData<NoteResponse>(queryKeys.note(noteID), (current) => {
        if (!current) return current;
        return {
          ...current,
          note: {
            ...current.note,
            body: request.body,
            etag: response.etag,
          },
        };
      });
      void queryClient.invalidateQueries({ queryKey: queryKeys.note(noteID) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.notes() });
      void queryClient.invalidateQueries({ queryKey: ["search"] });
      void queryClient.invalidateQueries({ queryKey: ["graph"] });
      void queryClient.invalidateQueries({ queryKey: ["activity"] });
    },
  });
}

// useNoteMetaQuery loads a note's editable metadata document for the meta dialog; fetched only
// while the dialog is open (live server only — the static site has no editor).
export function useNoteMetaQuery(noteID: NoteID, opts: { enabled: boolean }) {
  return useQuery({
    queryKey: queryKeys.noteMeta(noteID),
    queryFn: () => getNoteMeta(noteID),
    enabled: opts.enabled,
  });
}

// useSetTaskStateMutation moves one task line into a named state (board drag / card select). The
// note's body changed on disk (state marker, completion stamp, progress cookies), so the note query
// is invalidated to refresh both the rendered body and the embedded tasks payload.
export function useSetTaskStateMutation(noteID: NoteID) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ line, state }: { line: number; state: string }) => setTaskState(noteID, line, state),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.note(noteID) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.notes() });
    },
  });
}

export function useSaveNoteMetaMutation(noteID: NoteID) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (request: SaveNoteMetaRequest) => saveNoteMeta(noteID, request),
    onSuccess: (response) => {
      queryClient.setQueryData<NoteMetaResponse>(queryKeys.noteMeta(noteID), response);
      // The edit carries the title, tags, and props, which the note view, lists, and graph render;
      // a title change also rewrites backlinks in other notes.
      void queryClient.invalidateQueries({ queryKey: queryKeys.note(noteID) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.notes() });
      void queryClient.invalidateQueries({ queryKey: ["search"] });
      void queryClient.invalidateQueries({ queryKey: ["graph"] });
    },
  });
}

// useUploadAssetMutation imports a picked cover image into the vault assets and yields its
// assets/<name> reference; the dialog sets its image field to the result. Live server only.
export function useUploadAssetMutation() {
  return useMutation({
    mutationFn: (file: File) => uploadAsset(file),
  });
}
